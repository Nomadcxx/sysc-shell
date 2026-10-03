package services

import (
	"testing"
	"time"
)

// fakeLogindBus drives logindBus without a system bus.
type fakeLogindBus struct {
	signals chan []any
	lid     bool
}

func (f *fakeLogindBus) GetProperty(_, _ string) (any, error) { return f.lid, nil }
func (f *fakeLogindBus) Signals() <-chan []any                { return f.signals }
func (f *fakeLogindBus) Close()                               {}

func newFakeLogind(lid bool) (*Logind, *fakeLogindBus) {
	b := &fakeLogindBus{signals: make(chan []any, 8), lid: lid}
	l := &Logind{b: b, events: make(chan LogindEvent, 1), stop: make(chan struct{})}
	return l, b
}

func TestLogindRunDeliversTransitions(t *testing.T) {
	l, b := newFakeLogind(false)
	go l.Run()
	defer l.Close()

	b.signals <- []any{true}
	ev := <-l.Events()
	if !ev.Sleeping || ev.Lid {
		t.Fatalf("sleep event = %+v, want sleeping, lid open", ev)
	}

	b.lid = true
	b.signals <- []any{false}
	ev = <-l.Events()
	if ev.Sleeping || !ev.Lid {
		t.Fatalf("resume event = %+v, want resumed, lid closed", ev)
	}
	if !l.LidClosed() {
		t.Fatal("LidClosed did not follow the re-read property")
	}
}

func TestLogindRunDropsGarbage(t *testing.T) {
	l, b := newFakeLogind(false)
	go l.Run()
	defer l.Close()

	b.signals <- []any{"not a bool"}
	select {
	case ev := <-l.Events():
		t.Fatalf("garbage body produced event %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestLogindEmitCoalescesToNewest(t *testing.T) {
	l, _ := newFakeLogind(false)
	// Nothing drains: three emits must leave only the newest pending.
	l.emit(LogindEvent{Sleeping: true, Lid: false})
	l.emit(LogindEvent{Sleeping: false, Lid: false})
	l.emit(LogindEvent{Sleeping: false, Lid: true})
	if n := len(l.events); n != 1 {
		t.Fatalf("pending events = %d, want 1", n)
	}
	ev := <-l.events
	if ev.Sleeping || !ev.Lid {
		t.Fatalf("pending event = %+v, want the newest (resumed, lid closed)", ev)
	}
}

func TestResumeGateWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	g := NewResumeGate(time.Second)
	g.now = func() time.Time { return now }

	cases := []struct {
		name string
		adv  time.Duration
		want bool
	}{
		{"first fires", 0, true},
		{"repeat inside window", 500 * time.Millisecond, false},
		{"repeat at window edge", 500 * time.Millisecond, true},
	}
	for _, tc := range cases {
		now = now.Add(tc.adv)
		if got := g.Fire(); got != tc.want {
			t.Fatalf("%s: Fire() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
