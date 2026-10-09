package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// fakeTimers records armed timers; fire runs the newest unstopped one.
type fakeTimers struct {
	armed []*fakeTimer
}

type fakeTimer struct {
	d       time.Duration
	f       func()
	stopped bool
}

func (ft *fakeTimers) after(d time.Duration, f func()) func() bool {
	t := &fakeTimer{d: d, f: f}
	ft.armed = append(ft.armed, t)
	return func() bool { was := !t.stopped; t.stopped = true; return was }
}

func (ft *fakeTimers) fireLast(t *testing.T) time.Duration {
	t.Helper()
	last := ft.armed[len(ft.armed)-1]
	if last.stopped {
		t.Fatal("newest timer was stopped")
	}
	last.f()
	return last.d
}

func expectRequest(t *testing.T, i *mediaStripIntent, open bool) {
	t.Helper()
	select {
	case req := <-i.requests():
		if req.open != open {
			t.Fatalf("request open=%v, want %v", req.open, open)
		}
	default:
		t.Fatalf("no request, want open=%v", open)
	}
}

func expectNoRequest(t *testing.T, i *mediaStripIntent) {
	t.Helper()
	select {
	case req := <-i.requests():
		t.Fatalf("unexpected request %+v", req)
	default:
	}
}

func TestMediaStripIntentOpensAfterDwellAndClosesAfterGrace(t *testing.T) {
	ft := &fakeTimers{}
	i := newMediaStripIntent(ft.after)
	pill := ui.Rect{X: 800, W: 160, H: 32}
	i.pill(7, pill, true)
	if d := ft.fireLast(t); d != mediaStripOpenDelay {
		t.Fatalf("open delay = %v", d)
	}
	expectRequest(t, i, true)
	i.setShown(true)
	i.pill(7, pill, false)
	i.strip(true) // crossed the gap into the strip before the grace ran out
	if !ft.armed[len(ft.armed)-1].stopped {
		t.Fatal("entering the strip did not cancel the grace")
	}
	i.strip(false)
	if d := ft.fireLast(t); d != mediaStripGrace {
		t.Fatalf("grace = %v", d)
	}
	expectRequest(t, i, false)
}

func TestMediaStripIntentIgnoresAStaleOpen(t *testing.T) {
	ft := &fakeTimers{}
	i := newMediaStripIntent(ft.after)
	i.pill(7, ui.Rect{W: 10, H: 10}, true)
	stale := ft.armed[0]
	i.pill(7, ui.Rect{}, false) // left before 250 ms
	stale.f()                   // a timer that raced its stop still fires
	expectNoRequest(t, i)
}

func TestMediaStripIntentRepeatedMotionDoesNotRearm(t *testing.T) {
	ft := &fakeTimers{}
	i := newMediaStripIntent(ft.after)
	i.pill(7, ui.Rect{W: 10, H: 10}, true)
	i.pill(7, ui.Rect{W: 10, H: 10}, true)
	i.pill(7, ui.Rect{W: 10, H: 10}, true)
	if len(ft.armed) != 1 {
		t.Fatalf("motion over the pill armed %d timers, want 1", len(ft.armed))
	}
}

func TestMediaStripIntentClosedSendsNothing(t *testing.T) {
	ft := &fakeTimers{}
	i := newMediaStripIntent(ft.after)
	i.pill(7, ui.Rect{W: 10, H: 10}, true)
	i.close()
	ft.armed[0].f()
	expectNoRequest(t, i)
}
