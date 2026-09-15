package services

import (
	"errors"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

// bus is the D-Bus surface this service needs, and nothing more.
//
// It exists so tests can drive discovery, metadata and commands without a
// session bus. It is not a swappable-backend abstraction: there is exactly one
// real implementation and there will not be a second. If the test seam ever
// stops being the justification, delete the interface rather than populate it.
type bus interface {
	ListNames() ([]string, error)
	NameChanges() <-chan nameChange
	Get(busName, iface, prop string) (any, error)
	// Call issues one org.mpris.MediaPlayer2.Player method on busName, named
	// bare — "Next", not the full interface path. Every command this service
	// sends lives on that interface.
	Call(busName, method string, args ...any) error
	// Set writes one org.mpris.MediaPlayer2.Player property through the
	// standard D-Bus Properties interface.
	Set(busName, iface, prop string, value any) error
	Close()
}

// nameChange is one bus name appearing or vanishing.
type nameChange struct {
	Name     string
	Acquired bool
}

// mprisRoot is the object path every MPRIS player exposes its two interfaces
// on. The specification fixes it, so the seam needs no per-player paths.
const mprisRoot = "/org/mpris/MediaPlayer2"

// sessionBus is the real bus over the user session.
type sessionBus struct {
	conn    *dbus.Conn
	changes chan nameChange
	// owners maps a sender's unique name, like ":1.42", to the well-known
	// MPRIS name it currently owns. PropertiesChanged and Seeked carry only
	// the unique name, so this is how their events find their player. pump
	// and initial ListNames lookup share it under ownersMu.
	owners    map[string]string
	ownersMu  sync.Mutex
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

// newSessionBus connects to the session bus and subscribes to the signals the
// service needs: NameOwnerChanged for discovery, and PropertiesChanged and
// Seeked on the player interface so metadata and position stay live. A
// machine without a session bus returns an error and the caller builds the
// inert service.
func newSessionBus() (bus, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
	); err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"),
	); err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface(mprisPlayerIface),
		dbus.WithMatchMember("Seeked"),
	); err != nil {
		conn.Close()
		return nil, err
	}
	b := &sessionBus{
		conn:    conn,
		changes: make(chan nameChange, 32),
		owners:  map[string]string{},
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	signals := make(chan *dbus.Signal, 32)
	conn.Signal(signals)
	go b.pump(signals)
	return b, nil
}

func (b *sessionBus) pump(signals <-chan *dbus.Signal) {
	defer close(b.done)
	for {
		select {
		case <-b.stop:
			return
		case sig := <-signals:
			if sig == nil {
				return
			}
			switch {
			case sig.Name == "org.freedesktop.DBus.NameOwnerChanged" && len(sig.Body) >= 3:
				name, _ := sig.Body[0].(string)
				oldOwner, _ := sig.Body[1].(string)
				newOwner, _ := sig.Body[2].(string)
				b.handleNameOwnerChanged(name, oldOwner, newOwner)
			case sig.Name == "org.freedesktop.DBus.Properties.PropertiesChanged" && len(sig.Body) >= 2:
				iface, _ := sig.Body[0].(string)
				if iface != mprisPlayerIface || positionOnly(sig.Body[1]) {
					continue
				}
				if name := b.ownerFor(sig.Sender); name != "" {
					b.emit(nameChange{Name: name, Acquired: true})
				}
			case sig.Name == mprisPlayerIface+".Seeked" && len(sig.Body) >= 1:
				if name := b.ownerFor(sig.Sender); name != "" {
					b.emit(nameChange{Name: name, Acquired: true})
				}
			}
		}
	}
}

// emit forwards one event, giving up if the bus closes first.
func (b *sessionBus) emit(ch nameChange) {
	select {
	case b.changes <- ch:
	case <-b.stop:
	}
}

// positionOnly reports whether a PropertiesChanged payload carried nothing
// but Position. Interpolation answers position between reads, so a player
// that announces every second of progress would otherwise trigger a full
// property re-read for each one.
func positionOnly(changed any) bool {
	switch props := changed.(type) {
	case map[string]dbus.Variant:
		for k := range props {
			if k != "Position" {
				return false
			}
		}
		return true
	case map[string]any:
		for k := range props {
			if k != "Position" {
				return false
			}
		}
		return true
	}
	return false
}

func (b *sessionBus) ListNames() ([]string, error) {
	var names []string
	err := b.conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if !strings.HasPrefix(name, mprisPrefix) {
			continue
		}
		var owner string
		if err := b.conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, name).Store(&owner); err == nil {
			b.rememberOwner(name, owner)
		}
	}
	return names, err
}

func (b *sessionBus) NameChanges() <-chan nameChange { return b.changes }

func (b *sessionBus) Get(busName, iface, prop string) (any, error) {
	v, err := b.conn.Object(busName, mprisRoot).GetProperty(iface + "." + prop)
	if err != nil {
		return nil, err
	}
	return v.Value(), nil
}

func (b *sessionBus) Call(busName, method string, args ...any) error {
	call := b.conn.Object(busName, mprisRoot).Call(mprisPlayerIface+"."+method, 0, args...)
	return call.Err
}

func (b *sessionBus) Set(busName, iface, prop string, value any) error {
	call := b.conn.Object(busName, mprisRoot).Call(
		"org.freedesktop.DBus.Properties.Set", 0, iface, prop, dbus.MakeVariant(value),
	)
	return call.Err
}

func (b *sessionBus) Close() {
	b.closeOnce.Do(func() {
		close(b.stop)
		<-b.done
		b.conn.Close()
	})
}

func (b *sessionBus) handleNameOwnerChanged(name, oldOwner, newOwner string) {
	if !strings.HasPrefix(name, mprisPrefix) {
		return
	}
	b.ownersMu.Lock()
	if oldOwner != "" && b.owners[oldOwner] == name {
		delete(b.owners, oldOwner)
	}
	if newOwner != "" {
		b.owners[newOwner] = name
	}
	b.ownersMu.Unlock()
	b.emit(nameChange{Name: name, Acquired: newOwner != ""})
}

func (b *sessionBus) rememberOwner(name, owner string) {
	if owner == "" {
		return
	}
	b.ownersMu.Lock()
	b.owners[owner] = name
	b.ownersMu.Unlock()
}

func (b *sessionBus) ownerFor(owner string) string {
	b.ownersMu.Lock()
	defer b.ownersMu.Unlock()
	return b.owners[owner]
}

// unavailableBus stands in when there is no session bus. It is inert: no
// names, no events, and every command refuses.
type unavailableBus struct{}

func (unavailableBus) ListNames() ([]string, error)            { return nil, nil }
func (unavailableBus) NameChanges() <-chan nameChange          { return nil }
func (unavailableBus) Get(string, string, string) (any, error) { return nil, nil }
func (unavailableBus) Call(string, string, ...any) error       { return errNoMediaBus }
func (unavailableBus) Set(string, string, string, any) error   { return errNoMediaBus }
func (unavailableBus) Close()                                  {}

var errNoMediaBus = errors.New("services: session bus is not available")

// NewSessionMedia builds the service over the real session bus. A machine
// without one yields the inert service rather than an error the caller must
// handle: the bar still paints and the glyph stays off, exactly as the
// network service does without NetworkManager.
func NewSessionMedia() *Media {
	b, err := newSessionBus()
	if err != nil {
		return NewUnavailableMedia()
	}
	return NewMedia(b)
}

// NewUnavailableMedia builds the inert service a bus-less machine gets and
// the one a test installs: no players, nothing to command.
func NewUnavailableMedia() *Media {
	return NewMedia(unavailableBus{})
}
