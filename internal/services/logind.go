package services

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// logindBus is the D-Bus surface this service needs, and nothing more.
//
// It exists so tests can drive sleep/resume signals without a system bus. It
// is not a swappable-backend abstraction: there is exactly one real
// implementation. If the test seam ever stops being the justification, delete
// the interface rather than populate it.
type logindBus interface {
	GetProperty(iface, prop string) (any, error)
	// Signals carries PrepareForSleep bodies as vectors.
	Signals() <-chan []any
	Close()
}

const (
	logindBusName = "org.freedesktop.login1"
	logindPath    = "/org/freedesktop/login1"
	logindIface   = "org.freedesktop.login1.Manager"
)

// LogindEvent is one observed power-transport transition. Lid is the
// LidIsClosed property re-read at the moment of the event.
type LogindEvent struct {
	Sleeping bool
	Lid      bool
}

// Logind observes org.freedesktop.login1.Manager on the system bus. It only
// watches: logind's own HandleLidSwitch and HandlePowerKey policies act, and
// nothing here intercepts or delays them. No sleep-delay inhibitor is taken —
// sysc must not be the reason a laptop fails to suspend.
type Logind struct {
	b      logindBus
	events chan LogindEvent
	stop   chan struct{}
	once   sync.Once

	mu       sync.Mutex
	lidState bool
}

// logindSystemBus is the one real implementation of logindBus.
type logindSystemBus struct {
	conn    *dbus.Conn
	signals chan []any
	done    chan struct{}
}

// NewLogind connects to the system bus and subscribes to the Manager's
// PrepareForSleep signal. Machines without logind return an error and the
// shell starts without power observation.
func NewLogind() (*Logind, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	if err := conn.AddMatchSignal(
		dbus.WithMatchSender(logindBusName),
		dbus.WithMatchObjectPath(dbus.ObjectPath(logindPath)),
		dbus.WithMatchInterface(logindIface),
		dbus.WithMatchMember("PrepareForSleep"),
	); err != nil {
		conn.Close()
		return nil, fmt.Errorf("subscribing to logind: %w", err)
	}
	signals := make(chan *dbus.Signal, 8)
	conn.Signal(signals)
	b := &logindSystemBus{conn: conn, signals: make(chan []any, 8), done: make(chan struct{})}
	go pumpLogindSignals(b, signals)
	l := &Logind{
		b:      b,
		events: make(chan LogindEvent, 1),
		stop:   make(chan struct{}),
	}
	if lid, err := b.GetProperty(logindIface, "LidIsClosed"); err == nil {
		if closed, ok := lid.(bool); ok {
			l.setLid(closed)
		}
	}
	return l, nil
}

func pumpLogindSignals(b *logindSystemBus, signals <-chan *dbus.Signal) {
	defer close(b.done)
	for sig := range signals {
		if sig == nil || sig.Name != logindIface+".PrepareForSleep" || len(sig.Body) < 1 {
			continue
		}
		sleeping, ok := sig.Body[0].(bool)
		if !ok {
			continue
		}
		b.signals <- []any{sleeping}
	}
}

func (b *logindSystemBus) GetProperty(iface, prop string) (any, error) {
	call := b.conn.Object(logindBusName, dbus.ObjectPath(logindPath)).
		Call("org.freedesktop.DBus.Properties.Get", dbus.Flags(0), iface, prop)
	if call.Err != nil {
		return nil, call.Err
	}
	if v, ok := call.Body[0].(dbus.Variant); ok {
		return v.Value(), nil
	}
	return call.Body[0], nil
}

func (b *logindSystemBus) Signals() <-chan []any { return b.signals }

func (b *logindSystemBus) Close() {
	b.conn.Close()
}

// Events carries the newest observed transition. The channel coalesces: a
// consumer that is not watching loses intermediate events, never a deadlock.
func (l *Logind) Events() <-chan LogindEvent { return l.events }

// LidClosed reports the last observed LidIsClosed value.
func (l *Logind) LidClosed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lidState
}

func (l *Logind) setLid(closed bool) {
	l.mu.Lock()
	l.lidState = closed
	l.mu.Unlock()
}

// Run delivers transitions until Close. PrepareForSleep arrives twice per
// cycle, true before suspend and false after resume; the lid property is
// re-read with each so a lid opened during sleep reads correctly at resume.
func (l *Logind) Run() {
	for {
		select {
		case <-l.stop:
			return
		case body, ok := <-l.b.Signals():
			if !ok {
				return
			}
			sleeping, ok := body[0].(bool)
			if !ok {
				continue
			}
			event := LogindEvent{Sleeping: sleeping}
			if lid, err := l.b.GetProperty(logindIface, "LidIsClosed"); err == nil {
				if closed, ok := lid.(bool); ok {
					event.Lid = closed
					l.setLid(closed)
				}
			}
			slog.Info("logind power transition", "sleeping", event.Sleeping, "lid_closed", event.Lid)
			l.emit(event)
		}
	}
}

// emit keeps only the newest pending event so this pump can never block on a
// slow or absent consumer.
func (l *Logind) emit(event LogindEvent) {
	for {
		select {
		case l.events <- event:
			return
		default:
			select {
			case <-l.events:
			default:
			}
		}
	}
}

// Close releases the bus. The events channel is not closed; consumers stop on
// their own context.
func (l *Logind) Close() {
	l.once.Do(func() {
		close(l.stop)
		l.b.Close()
	})
}

// ResumeGate deduplicates resume handling inside a window. Some firmwares
// emit PrepareForSleep(false) twice on s2idle cycles; the repaint and the
// idle re-arm must run once. The zero value is not usable — use NewResumeGate.
type ResumeGate struct {
	mu     sync.Mutex
	window time.Duration
	last   time.Time
	now    func() time.Time
}

func NewResumeGate(window time.Duration) *ResumeGate {
	return &ResumeGate{window: window, now: time.Now}
}

func (g *ResumeGate) Fire() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.last.IsZero() && g.now().Sub(g.last) < g.window {
		return false
	}
	g.last = g.now()
	return true
}
