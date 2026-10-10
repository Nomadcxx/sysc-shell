package wayland

import (
	"context"
	"testing"
	"time"
)

func TestStallWatchReportsOnlyUnbrokenWork(t *testing.T) {
	t.Parallel()
	got := make(chan time.Time, 4)
	w := &stallWatch{dump: func(since time.Time) { got <- since }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Ages the busy mark past the threshold without sleeping through it.
	w.busySince.Store(time.Now().Add(-2 * stallAfter).UnixNano())
	go w.run(ctx)

	select {
	case <-got:
	case <-time.After(3 * stallPoll):
		t.Fatal("a stalled owner was not reported")
	}
	select {
	case <-got:
		t.Fatal("the same stall was reported twice")
	case <-time.After(2 * stallPoll):
	}

	w.waiting()
	w.working()
	select {
	case <-got:
		t.Fatal("fresh work was reported as a stall")
	case <-time.After(2 * stallPoll):
	}
}

func TestOwnerMarksInputHandlingAsWork(t *testing.T) {
	o, _ := newDispatchTestOwner(t)
	_, host, _, _ := newKeyedHost()
	panel := newSurfaceUnit("panel:test")
	panel.app.Handle = func(Event) bool {
		if o.stall.busySince.Load() == 0 {
			t.Error("input handler runs while the stall detector thinks the owner is waiting")
		}
		o.closed = true
		return false
	}
	o.keyFocus = keyFocus{host: host, unit: panel}
	o.repeat = keyRepeat{armed: true, rate: 25, key: keyDown, next: time.Now()}
	wake, err := newWakePipe()
	if err != nil {
		t.Fatal(err)
	}
	defer wake.close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := o.loop(ctx, wake, nil); err != nil {
		t.Fatal(err)
	}
	if !o.closed {
		t.Fatal("input handler never ran")
	}
}
