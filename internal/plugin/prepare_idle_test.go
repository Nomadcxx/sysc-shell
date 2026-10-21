package plugin

import (
	"testing"
	"time"
)

// idleWindow is how long the pool is left idle before its wakeups are
// counted. It is long enough that a timer running at the publish rate would
// wake the pool many times over it.
const idleWindow = 2 * time.Second

// idleSettle is how long the pool is given to reach its wait before the
// count starts.
const idleSettle = 50 * time.Millisecond

// An idle shell schedules nothing, and the Preparer is created for every
// session whether or not a plugin is enabled. Its pool must therefore wait on
// the condition variable until Submit signals it: a free-running ticker keeps
// a laptop CPU from reaching its deeper idle states thirty times a second.
//
// The count is the pool's own: it is how many times the workers returned from
// their wait, so nothing else on the machine can decide the result. This test
// used to measure the process's voluntary context switches instead, which made
// the outcome depend on what the rest of the machine was doing: on a loaded
// machine the window it measured landed on a busy stretch and failed the
// check although the pool was idle.
func TestPreparerIdleWakeups(t *testing.T) {
	p := NewPreparer(2, measureFixed)
	defer p.Close()
	// Let the pool reach its wait before the count starts.
	time.Sleep(idleSettle)

	p.mu.Lock()
	before := p.wakeCount
	p.mu.Unlock()

	time.Sleep(idleWindow)

	p.mu.Lock()
	after := p.wakeCount
	p.mu.Unlock()

	if after != before {
		t.Fatalf("an idle Preparer woke itself %d times in %s: a pool with no work must not wake itself",
			after-before, idleWindow)
	}
}

// The pool's timer exists to release a view that the rate gate holds back.
// There is no such view here, so the timer must not exist: an armed timer is
// the pool's promise to wake itself, and this checks that promise where it
// can be checked exactly.
func TestPreparerIdlePoolArmsNoTimer(t *testing.T) {
	p := NewPreparer(2, measureFixed)
	defer p.Close()
	time.Sleep(idleSettle)

	p.mu.Lock()
	armed := p.wake != nil
	p.mu.Unlock()
	if armed {
		t.Fatal("an idle Preparer has a timer armed: the pool would wake itself")
	}
}

// A view held back by the rate gate must still go out when it comes due. The
// pool arms one timer for the earliest held-back view instead of waking on a
// fixed interval, so nothing further has to arrive to move it along.
func TestPreparerPublishesAHeldBackViewWithoutAnotherSubmit(t *testing.T) {
	t.Parallel()

	p := NewPreparer(1, measureFixed)
	defer p.Close()

	p.Submit(barJob("v1", 1, timerBar()))
	if got := await(t, p); got.Revision != 1 {
		t.Fatalf("first revision = %d, want 1", got.Revision)
	}

	// A second update inside the publish interval is held back. No further
	// Submit comes: the timer the pool armed is what releases it.
	p.Submit(barJob("v1", 2, timerBar()))
	select {
	case got := <-p.Results():
		if got.Revision != 2 {
			t.Fatalf("revision = %d, want the held-back newest job", got.Revision)
		}
	case <-time.After(time.Second):
		t.Fatal("the rate-gated view was never published")
	}
}
