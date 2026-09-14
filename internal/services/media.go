package services

import (
	"strings"
	"sync"
)

// PlaybackStatus is the player's transport state.
type PlaybackStatus uint8

const (
	PlaybackStopped PlaybackStatus = iota
	PlaybackPaused
	PlaybackPlaying
)

const (
	// mprisPrefix marks the bus names that are players. A session bus carries
	// hundreds of unrelated names.
	mprisPrefix = "org.mpris.MediaPlayer2."
	// mprisIface is the root player interface, whose Identity property names
	// the player for humans.
	mprisIface = "org.mpris.MediaPlayer2"
)

// MediaState is one immutable snapshot. Consumers never see a D-Bus type.
type MediaState struct {
	Available  bool
	Player     string // bus name of the active player
	Identity   string // human name, from org.mpris.MediaPlayer2.Identity
	Title      string
	Artist     string
	Album      string
	ArtKey     string // identifier for the async art worker, never a decoded image
	Status     PlaybackStatus
	PositionUS int64
	LengthUS   int64
	Rate       float64
	CanNext    bool
	CanPrev    bool
	CanPlay    bool
}

// Player is one discovered player.
type Player struct {
	Bus      string
	Identity string
	Active   bool
}

// Media is a peer of Audio: discovery, active-player selection, metadata,
// position and transport commands for the MPRIS players on the session bus.
//
// Unlike the polled services, its run loop starts at construction rather than
// on the first lease. Bus-name changes are this service's only feed, so a
// player that appears before any consumer watches would otherwise be invisible
// until some unrelated event. The lease still governs the subscription: the
// last release stops the loop and the next Acquire starts it again.
type Media struct {
	mu        sync.Mutex
	leases    leaseSet
	b         bus
	players   map[string]Player
	active    string
	preferred string
	changes   chan MediaState
	stop      chan struct{}
	done      chan struct{}
}

// NewMedia builds the service over b, enumerates the players already on it,
// and begins watching for changes.
func NewMedia(b bus) *Media {
	m := &Media{
		b:       b,
		players: map[string]Player{},
		changes: make(chan MediaState, 1),
	}
	m.enumerate()
	m.startLocked(false)
	return m
}

// Changes carries the newest snapshot. The channel is created once and never
// closed, so it survives stop and start cycles.
func (m *Media) Changes() <-chan MediaState { return m.changes }

// State returns the current snapshot. It answers from memory and interpolates
// position at read time; it performs no bus I/O, so it is safe anywhere
// CachedState is.
func (m *Media) State() MediaState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

// CachedState returns the last snapshot without touching the bus. Callers
// holding Registry.mu or running on the Wayland owner must use this.
func (m *Media) CachedState() MediaState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

// Available reports whether any player is on the bus.
func (m *Media) Available() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.players) > 0
}

// Players returns the discovered players sorted by bus name, so the choice of
// a fallback active player is stable and every consumer sees one order.
func (m *Media) Players() []Player {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Player, 0, len(m.players))
	for _, p := range m.players {
		p.Active = p.Bus == m.active
		out = append(out, p)
	}
	sortPlayers(out)
	return out
}

func sortPlayers(ps []Player) {
	// Insertion sort keeps this file free of a sort import until the list is
	// real; the counts here are single digits.
	for i := 1; i < len(ps); i++ {
		for j := i; j > 0 && ps[j].Bus < ps[j-1].Bus; j-- {
			ps[j], ps[j-1] = ps[j-1], ps[j]
		}
	}
}

// Prefer asks the service to make busName the active player whenever it is
// present. It is the design's "last interacted through this shell" rule: the
// bar widget and the page call it from their click handlers. A preference for
// a vanished name is remembered, so a player that re-registers under the same
// name regains the selection.
func (m *Media) Prefer(busName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.preferred == busName {
		return
	}
	before := m.active
	m.preferred = busName
	m.reselectLocked()
	if m.active != before {
		m.publishLocked()
	}
}

// Acquire registers a consumer. The first lease starts the watch; the last
// release stops it. A start after a stop re-enumerates, because names may have
// come and gone while nobody was watching.
func (m *Media) Acquire() (*Lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lease := &Lease{media: m}
	m.leases.add(lease)
	if m.stop == nil {
		m.startLocked(true)
	}
	return lease, nil
}

func (m *Media) release(l *Lease) {
	m.mu.Lock()
	if !m.leases.remove(l) {
		m.mu.Unlock()
		return
	}
	done := m.stopIfUnusedLocked()
	m.mu.Unlock()
	if done != nil {
		<-done
	}
}

// Close drops every lease, stops the watch and closes the bus. It is safe to
// call twice.
func (m *Media) Close() {
	m.mu.Lock()
	for _, l := range m.leases.clear() {
		l.media = nil
	}
	done := m.stopIfUnusedLocked()
	m.mu.Unlock()
	if done != nil {
		<-done
	}
	m.b.Close()
}

// Running reports whether the watch loop is live.
func (m *Media) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stop != nil
}

func (m *Media) startLocked(reenumerate bool) {
	m.stop, m.done = make(chan struct{}), make(chan struct{})
	go m.run(m.stop, m.done, reenumerate)
}

func (m *Media) stopIfUnusedLocked() chan struct{} {
	if m.leases.len() > 0 || m.stop == nil {
		return nil
	}
	close(m.stop)
	done := m.done
	m.stop, m.done = nil, nil
	return done
}

func (m *Media) run(stop, done chan struct{}, reenumerate bool) {
	defer close(done)
	if reenumerate {
		m.enumerate()
	}
	changes := m.b.NameChanges()
	for {
		select {
		case <-stop:
			return
		case ch := <-changes:
			m.handleNameChange(ch)
		}
	}
}

// enumerate lists the bus and reconciles the player set against it. The
// identity lookups run without the mutex; only the apply takes it, so a paint
// path reader never waits on bus I/O.
func (m *Media) enumerate() {
	names, err := m.b.ListNames()
	if err != nil {
		return
	}
	type discovered struct {
		name     string
		identity string
	}
	var found []discovered
	for _, name := range names {
		if !strings.HasPrefix(name, mprisPrefix) {
			continue
		}
		v, err := m.b.Get(name, mprisIface, "Identity")
		if err != nil {
			continue
		}
		identity, _ := v.(string)
		found = append(found, discovered{name: name, identity: identity})
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	next := make(map[string]Player, len(found))
	for _, d := range found {
		p := Player{Bus: d.name, Identity: d.identity}
		if old, ok := m.players[d.name]; ok && p.Identity == "" {
			p.Identity = old.Identity
		}
		next[d.name] = p
	}
	m.players = next
	m.reselectLocked()
	m.publishLocked()
}

// handleNameChange adds or drops one player. A known name arriving again is
// only interesting once its properties change, which a later slice handles.
func (m *Media) handleNameChange(ch nameChange) {
	if !strings.HasPrefix(ch.Name, mprisPrefix) {
		return
	}
	if ch.Acquired {
		v, err := m.b.Get(ch.Name, mprisIface, "Identity")
		if err != nil {
			return
		}
		identity, _ := v.(string)
		m.mu.Lock()
		m.players[ch.Name] = Player{Bus: ch.Name, Identity: identity}
		m.reselectLocked()
		m.publishLocked()
		m.mu.Unlock()
		return
	}
	m.mu.Lock()
	if _, ok := m.players[ch.Name]; !ok {
		m.mu.Unlock()
		return
	}
	delete(m.players, ch.Name)
	m.reselectLocked()
	m.publishLocked()
	m.mu.Unlock()
}

// reselectLocked keeps the active player pointing at something that exists.
// The design's selection order: the preferred player when it is present; else
// the current choice while it survives; else the first player by bus name, so
// the fallback is stable. The "most recently playing" middle rule needs
// transport status, which arrives with metadata decoding. Callers hold m.mu.
func (m *Media) reselectLocked() {
	if m.preferred != "" {
		if _, ok := m.players[m.preferred]; ok {
			m.active = m.preferred
			return
		}
	}
	if m.active != "" {
		if _, ok := m.players[m.active]; ok {
			return
		}
	}
	m.active = ""
	for name := range m.players {
		if m.active == "" || name < m.active {
			m.active = name
		}
	}
}

// publishLocked republishes the snapshot, dropping the oldest unread one the
// way audio's poll does. Callers hold m.mu.
func (m *Media) publishLocked() {
	st := m.snapshotLocked()
	select {
	case m.changes <- st:
	default:
		select {
		case <-m.changes:
		default:
		}
		m.changes <- st
	}
}

// snapshotLocked builds the immutable view from the live selection. Callers
// hold m.mu.
func (m *Media) snapshotLocked() MediaState {
	var st MediaState
	st.Available = len(m.players) > 0
	st.Player = m.active
	if p, ok := m.players[m.active]; ok {
		st.Identity = p.Identity
	}
	return st
}
