package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

// TestCloseGivesUpOnAStrandedLock pins the shutdown path against a mutex that
// a panicking handler left held.
//
// sysc-wayland turns a panic in an event handler into an error rather than
// crashing, so a handler that panicked between a manual Lock and its Unlock
// leaves r.mu held for the life of the process. Close used to block on it for
// good: the shell sat in futex_do_wait, the startup error that caused the
// panic was never printed, and the service still read as active with nothing
// painted.
func TestCloseGivesUpOnAStrandedLock(t *testing.T) {
	r := NewRegistry(config.Config{})

	// Stand in for a handler that unwound without unlocking.
	r.mu.Lock()

	done := make(chan struct{})
	go func() {
		r.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(closeLockGrace + 5*time.Second):
		t.Fatal("Close is still blocked on a stranded r.mu; shutdown depends on an invariant a panic already broke")
	}

	// The channel every publish selects on must still be closed, or a sender
	// blocked on a full queue never unwinds either.
	select {
	case <-r.closed:
	default:
		t.Fatal("Close returned without closing r.closed")
	}
}
