package services

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/godbus/dbus/v5"
)

// ScreenSaverInhibitor is one live org.freedesktop.ScreenSaver inhibition.
type ScreenSaverInhibitor struct {
	Sender string `json:"-"`
	App    string `json:"app"`
	Reason string `json:"reason"`
	Cookie uint32 `json:"cookie"`
}

// idleInhibitors tracks holders by bus name so a crashed client cannot pin
// the idle timer forever.
type idleInhibitors struct {
	mu       sync.Mutex
	byCookie map[uint32]ScreenSaverInhibitor
	next     uint32
}

func (t *idleInhibitors) inhibit(sender, app, reason string) uint32 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.next++
	if t.next == 0 {
		t.next = 1
	}
	t.byCookie[t.next] = ScreenSaverInhibitor{
		Sender: sender, App: app, Reason: reason, Cookie: t.next,
	}
	return t.next
}

func (t *idleInhibitors) uninhibit(sender string, cookie uint32) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	inh, ok := t.byCookie[cookie]
	if !ok || inh.Sender != sender {
		return false
	}
	delete(t.byCookie, cookie)
	return true
}

func (t *idleInhibitors) dropSender(sender string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for c, inh := range t.byCookie {
		if inh.Sender == sender {
			delete(t.byCookie, c)
			n++
		}
	}
	return n
}

func (t *idleInhibitors) list() []ScreenSaverInhibitor {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]ScreenSaverInhibitor, 0, len(t.byCookie))
	for _, inh := range t.byCookie {
		out = append(out, inh)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cookie < out[j].Cookie })
	return out
}

// ScreenSaverService owns the XDG ScreenSaver D-Bus API so any application
// can inhibit the shell's idle timers without a global logind idle block.
// The protocol identifies inhibitors only by cookie; per-process isolation
// is per connection, so one app opening a second connection inhibits again.
type ScreenSaverService struct {
	busConn *dbus.Conn
	tracker idleInhibitors

	onChanged func([]ScreenSaverInhibitor)

	stop chan struct{}
}

// NewScreenSaverService connects to the session bus and exports
// org.freedesktop.ScreenSaver. It fails when the bus is unreachable or the
// name is already owned; the shell keeps its caffeine and media rules and
// logs the collision.
func NewScreenSaverService(
	onChanged func([]ScreenSaverInhibitor),
) (*ScreenSaverService, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("connecting to session bus: %w", err)
	}
	reply, err := conn.RequestName("org.freedesktop.ScreenSaver",
		dbus.NameFlagDoNotQueue)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("requesting ScreenSaver name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		_ = conn.Close()
		return nil, fmt.Errorf("org.freedesktop.ScreenSaver is already owned")
	}
	s := &ScreenSaverService{
		busConn:   conn,
		tracker:   idleInhibitors{byCookie: map[uint32]ScreenSaverInhibitor{}},
		onChanged: onChanged,
		stop:      make(chan struct{}),
	}
	if err := conn.Export(s, dbus.ObjectPath("/org/freedesktop/ScreenSaver"),
		"org.freedesktop.ScreenSaver"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("exporting ScreenSaver: %w", err)
	}
	// Without this match rule the bus delivers no NameOwnerChanged signal,
	// and a crashed client would pin the idle timer forever.
	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
	); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("subscribing to name changes: %w", err)
	}
	signals := make(chan *dbus.Signal, 64)
	conn.Signal(signals)
	go s.watchSignals(signals)
	return s, nil
}

// watchSignals releases every inhibition a client still holds when its bus
// name dies.
func (s *ScreenSaverService) watchSignals(signals <-chan *dbus.Signal) {
	for {
		select {
		case <-s.stop:
			return
		case sig := <-signals:
			if sig == nil {
				return
			}
			if sig.Name != "org.freedesktop.DBus.NameOwnerChanged" ||
				len(sig.Body) < 3 {
				continue
			}
			name, _ := sig.Body[0].(string)
			newOwner, _ := sig.Body[2].(string)
			if newOwner != "" {
				continue
			}
			if n := s.tracker.dropSender(name); n > 0 {
				s.publish()
			}
		}
	}
}

func (s *ScreenSaverService) publish() {
	if s.onChanged != nil {
		s.onChanged(s.tracker.list())
	}
}

// Inhibit requests that the shell's idle timers stay disarmed and returns a
// cookie for UnInhibit.
func (s *ScreenSaverService) Inhibit(
	sender dbus.Sender,
	app string,
	reason string,
) (uint32, *dbus.Error) {
	cookie := s.tracker.inhibit(string(sender), app, reason)
	s.publish()
	slog.Debug("services: ScreenSaver Inhibit", "app", app, "cookie", cookie)
	return cookie, nil
}

// UnInhibit releases one cookie. Cookies belong to their requesting
// connection; another client cannot release an inhibition it did not create.
func (s *ScreenSaverService) UnInhibit(
	sender dbus.Sender,
	cookie uint32,
) *dbus.Error {
	if !s.tracker.uninhibit(string(sender), cookie) {
		return dbus.MakeFailedError(
			fmt.Errorf("cookie %d not held by this sender", cookie))
	}
	s.publish()
	return nil
}

// SimulateUserActivity acknowledges the hint without clearing any inhibitor;
// only real input resumes the idle timers.
func (s *ScreenSaverService) SimulateUserActivity() *dbus.Error {
	return nil
}

// GetActive reports whether some client currently holds an inhibition.
func (s *ScreenSaverService) GetActive() (bool, *dbus.Error) {
	return len(s.tracker.list()) > 0, nil
}

// ListInhibitors is the shell-side view for the inhibitor list.
func (s *ScreenSaverService) ListInhibitors() []ScreenSaverInhibitor {
	return s.tracker.list()
}

// Remove stops the signal watcher and closes the dedicated connection;
// closing releases the bus name.
func (s *ScreenSaverService) Remove() {
	close(s.stop)
	if s.busConn != nil {
		if err := s.busConn.Close(); err != nil {
			slog.Warn("services: closing ScreenSaver bus", "error", err)
		}
		s.busConn = nil
	}
}
