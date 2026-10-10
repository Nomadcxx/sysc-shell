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
