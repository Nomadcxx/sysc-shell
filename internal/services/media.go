package services

import (
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
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
	// mprisPlayerIface carries transport state: metadata, status, rate.
	mprisPlayerIface = "org.mpris.MediaPlayer2.Player"
)

// mediaPlayer is one discovered player's decoded state. The exported Player
// inside it is what Players() hands out; the rest feeds the active snapshot.
type mediaPlayer struct {
	Player
	status   PlaybackStatus
	title    string
	artist   string
	album    string
	artKey   string
	lengthUS int64
	rate     float64
	// positionUS is the last position the bus reported, positionAt the clock
	// reading it was reported at. Together they are the interpolation
	// baseline; nothing ticks to advance them.
	positionUS int64
	positionAt time.Time
	// trackID is the mpris:trackid the player announced, required by
	// SetPosition. Empty means the player never announced one.
	trackID string
	canNext bool
	canPrev bool
	canPlay bool
}

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
	players   map[string]*mediaPlayer
	active    string
	preferred string
	changes   chan MediaState
	stop      chan struct{}
	done      chan struct{}
	// now is the clock position interpolates against. It is a field so tests
	// can move time instead of sleeping; it defaults to time.Now.
	now func() time.Time
}

// NewMedia builds the service over b, enumerates the players already on it,
// and begins watching for changes.
func NewMedia(b bus) *Media {
	m := &Media{
		b:       b,
		players: map[string]*mediaPlayer{},
		changes: make(chan MediaState, 1),
		now:     time.Now,
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
		q := p.Player
		q.Active = q.Bus == m.active
		out = append(out, q)
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
// property lookups run without the mutex; only the apply takes it, so a paint
// path reader never waits on bus I/O.
func (m *Media) enumerate() {
	names, err := m.b.ListNames()
	if err != nil {
		return
	}
	var found []*mediaPlayer
	for _, name := range names {
		if !strings.HasPrefix(name, mprisPrefix) {
			continue
		}
		if p := m.probePlayer(name); p != nil {
			found = append(found, p)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	next := make(map[string]*mediaPlayer, len(found))
	for _, p := range found {
		if old, ok := m.players[p.Bus]; ok && p.Identity == "" {
			p.Identity = old.Identity
		}
		next[p.Bus] = p
	}
	m.players = next
	m.reselectLocked()
	m.publishLocked()
}

// handleNameChange adds or drops one player. A known name arriving again is
// the refresh path: the real bus turns a PropertiesChanged that touches
// anything beyond Position, and every Seeked, into one of these events, and
// the re-read picks up the new metadata or resets the position baseline.
func (m *Media) handleNameChange(ch nameChange) {
	if !strings.HasPrefix(ch.Name, mprisPrefix) {
		return
	}
	if !ch.Acquired {
		m.mu.Lock()
		if _, ok := m.players[ch.Name]; ok {
			delete(m.players, ch.Name)
			m.reselectLocked()
			m.publishLocked()
		}
		m.mu.Unlock()
		return
	}
	p := m.probePlayer(ch.Name)
	if p == nil {
		return
	}
	m.mu.Lock()
	m.players[ch.Name] = p
	m.reselectLocked()
	m.publishLocked()
	m.mu.Unlock()
}

// probePlayer reads one player's properties off the bus. It performs I/O and
// must not run under m.mu. A player that vanished between listing and reading
// yields nil and stays unknown until its next event.
func (m *Media) probePlayer(name string) *mediaPlayer {
	v, err := m.b.Get(name, mprisIface, "Identity")
	if err != nil {
		return nil
	}
	p := &mediaPlayer{}
	p.Bus = name
	p.Identity, _ = v.(string)

	if v, err := m.b.Get(name, mprisPlayerIface, "Metadata"); err == nil {
		p.title, p.artist, p.album, p.artKey, p.lengthUS, p.trackID = decodeMetadata(v)
	}
	if v, err := m.b.Get(name, mprisPlayerIface, "PlaybackStatus"); err == nil {
		p.status = decodeStatus(v)
	}
	if v, err := m.b.Get(name, mprisPlayerIface, "Rate"); err == nil {
		p.rate = decodeRate(v)
	}
	if v, err := m.b.Get(name, mprisPlayerIface, "Position"); err == nil {
		p.positionUS = decodePosition(v)
		p.positionAt = m.now()
	}
	if v, err := m.b.Get(name, mprisPlayerIface, "CanGoNext"); err == nil {
		p.canNext, _ = v.(bool)
	}
	if v, err := m.b.Get(name, mprisPlayerIface, "CanGoPrevious"); err == nil {
		p.canPrev, _ = v.(bool)
	}
	if v, err := m.b.Get(name, mprisPlayerIface, "CanPlay"); err == nil {
		p.canPlay, _ = v.(bool)
	}
	return p
}

// decodeMetadata flattens one player's Metadata property into snapshot fields.
// Every read is a checked assertion with a zero-value fallback: a player is
// free to send nonsense, and a partial map must degrade to a usable snapshot
// rather than fail the service. xesam:artist is a list in the specification
// and a bare string in practice, so both shapes are accepted.
//
// Art stays an identifier, never a decoded image. Only file:// URLs survive:
// a player names a path this shell would then read, so a remote URL is
// refused outright in this first slice and the eventual consumer bounds the
// size and time of the read.
func decodeMetadata(v any) (title, artist, album, artKey string, lengthUS int64, trackID string) {
	meta := flattenMetadata(v)
	if meta == nil {
		return
	}
	title, _ = meta["xesam:title"].(string)
	switch a := meta["xesam:artist"].(type) {
	case []string:
		artist = strings.Join(a, ", ")
	case []any:
		parts := make([]string, 0, len(a))
		for _, item := range a {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		artist = strings.Join(parts, ", ")
	case string:
		artist = a
	}
	album, _ = meta["xesam:album"].(string)
	switch l := meta["mpris:length"].(type) {
	case int64:
		lengthUS = l
	case int:
		lengthUS = int64(l)
	}
	switch t := meta["mpris:trackid"].(type) {
	case string:
		trackID = t
	case dbus.ObjectPath:
		trackID = string(t)
	}
	if u, ok := meta["mpris:artUrl"].(string); ok && strings.HasPrefix(u, "file://") {
		artKey = u
	}
	return
}

// flattenMetadata normalizes the two shapes a Metadata property arrives in:
// the godbus variant map the real bus produces and the plain map the fake
// does.
func flattenMetadata(v any) map[string]any {
	switch meta := v.(type) {
	case map[string]any:
		return meta
	case map[string]dbus.Variant:
		out := make(map[string]any, len(meta))
		for k, val := range meta {
			out[k] = val.Value()
		}
		return out
	}
	return nil
}

func decodeStatus(v any) PlaybackStatus {
	switch s, _ := v.(string); s {
	case "Playing":
		return PlaybackPlaying
	case "Paused":
		return PlaybackPaused
	default:
		return PlaybackStopped
	}
}

// decodeRate reads the playback rate, defaulting to the specification's 1.0
// when the property is absent or nonsense.
func decodeRate(v any) float64 {
	if r, ok := v.(float64); ok && r > 0 {
		return r
	}
	return 1.0
}

func decodePosition(v any) int64 {
	switch p := v.(type) {
	case int64:
		return p
	case int:
		return int64(p)
	}
	return 0
}

// onSeeked resets the position baseline after a discontinuity. The real bus
// delivers a seek as a refresh event whose Position re-read lands in
// probePlayer; this direct form is what that path and future consumers share.
func (m *Media) onSeeked(positionUS int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.players[m.active]
	if !ok {
		return
	}
	p.positionUS = positionUS
	p.positionAt = m.now()
	m.publishLocked()
}

// Transport commands. Each targets the active player and performs bus I/O, so
// the shell issues them off the Wayland owner through its scheduleControl
// seam. A command against a player that vanished mid-flight fails quietly and
// repairs the display instead: the service re-probes, and a confirmed-vanished
// player drops and re-selection publishes. Per the design, no error surfaces
// to the click handler; the error return exists for the house service
// contract.

// PlayPause toggles the active player's transport state.
func (m *Media) PlayPause() error { return m.command("PlayPause") }

// Next skips to the next track on the active player.
func (m *Media) Next() error { return m.command("Next") }

// Previous skips to the previous track on the active player.
func (m *Media) Previous() error { return m.command("Previous") }

// Stop stops the active player.
func (m *Media) Stop() error { return m.command("Stop") }

// SetPosition seeks the active player to us microseconds into the current
// track. The specification requires the track id alongside the position, so
// this is a quiet no-op against a player that did not announce one.
func (m *Media) SetPosition(us int64) error {
	m.mu.Lock()
	name := m.active
	trackID := ""
	if p, ok := m.players[name]; ok {
		trackID = p.trackID
	}
	m.mu.Unlock()
	if name == "" || trackID == "" {
		return nil
	}
	if err := m.b.Call(name, "SetPosition", dbus.ObjectPath(trackID), us); err != nil {
		m.repairAfterCommand(name)
	}
	return nil
}

func (m *Media) command(method string) error {
	m.mu.Lock()
	name := m.active
	m.mu.Unlock()
	if name == "" {
		return nil
	}
	if err := m.b.Call(name, method); err != nil {
		m.repairAfterCommand(name)
	}
	return nil
}

// repairAfterCommand answers a failed command by re-probing the target: an
// alive player keeps its refreshed record, a confirmed-dead one drops and
// re-selection publishes. Callers hold no lock; the probe performs I/O.
func (m *Media) repairAfterCommand(name string) {
	m.mu.Lock()
	_, alive := m.players[name]
	m.mu.Unlock()
	if !alive {
		return
	}
	p := m.probePlayer(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	if p == nil {
		delete(m.players, name)
	} else {
		m.players[name] = p
	}
	m.reselectLocked()
	m.publishLocked()
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
	p, ok := m.players[m.active]
	if !ok {
		return st
	}
	st.Identity = p.Identity
	st.Title = p.title
	st.Artist = p.artist
	st.Album = p.album
	st.ArtKey = p.artKey
	st.Status = p.status
	st.LengthUS = p.lengthUS
	st.Rate = p.rate
	st.CanNext = p.canNext
	st.CanPrev = p.canPrev
	st.CanPlay = p.canPlay
	st.PositionUS = p.positionUS
	if p.status == PlaybackPlaying {
		// Baseline plus rate times elapsed, in microseconds. No timer: a
		// consumer wanting a moving bar drives it from the per-surface
		// animator and asks again. The clamp keeps the bar from running past
		// the track while a player lags behind announcing the change.
		elapsedUS := m.now().Sub(p.positionAt).Nanoseconds() / 1000
		position := p.positionUS + int64(float64(elapsedUS)*p.rate)
		if p.lengthUS > 0 && position > p.lengthUS {
			position = p.lengthUS
		}
		st.PositionUS = position
	}
	return st
}
