package services

import (
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
	Call(busName, method string, args ...any) error
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
	stop    chan struct{}
	done    chan struct{}
}

// newSessionBus connects to the session bus and subscribes to
// NameOwnerChanged, the one signal discovery needs. A machine without a
// session bus returns an error and the caller builds the inert service.
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
	b := &sessionBus{
		conn:    conn,
		changes: make(chan nameChange, 32),
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
			if sig.Name != "org.freedesktop.DBus.NameOwnerChanged" || len(sig.Body) < 3 {
				continue
			}
			name, _ := sig.Body[0].(string)
			newOwner, _ := sig.Body[2].(string)
			select {
			case b.changes <- nameChange{Name: name, Acquired: newOwner != ""}:
			case <-b.stop:
				return
			}
		}
	}
}

func (b *sessionBus) ListNames() ([]string, error) {
	var names []string
	err := b.conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names)
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
	call := b.conn.Object(busName, mprisRoot).Call(method, 0, args...)
	return call.Err
}

func (b *sessionBus) Close() {
	close(b.stop)
	<-b.done
	b.conn.Close()
}
