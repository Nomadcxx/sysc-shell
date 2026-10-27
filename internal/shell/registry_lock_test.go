package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

// withLock is the only form of taking the registry lock that survives a
// panic: the release is deferred, so the lock cannot outlive the frame that
// took it.
func TestRegistryWithLockReleasesTheMutexAfterAPanic(t *testing.T) {
	r := &Registry{cfg: config.Default()}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic did not escape the locked region")
			}
		}()
		r.withLock(func() { panic("handler panic") })
	}()
	if !r.mu.TryLock() {
		t.Fatal("the registry lock was stranded by the panic")
	}
	r.mu.Unlock()
}

// A panic inside a bar handler must not leave the registry lock held. The
// Wayland owner recovers a handler panic into an error instead of crashing,
// so a lock stranded by the panic would freeze every later request for the
// life of the process, and Registry.Close would report that it could not
// take the lock.
func TestBarActionHandlerPanicDoesNotStrandTheRegistryLock(t *testing.T) {
	r := &Registry{cfg: config.Default()}
	bar := &Bar{conn: "DP-1"}
	r.bars = map[uint32]*Bar{7: bar}
	r.bindBarPanelActionsLocked(7, bar)
	// The registry has no media strip, so the pin path dereferences a nil
	// host. That is the shape of the panic a handler can hit: a tree
	// builder, a service call, or a slice index under the lock.

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the handler did not panic")
			}
		}()
		bar.onAction(panelMediaAction, buttonLeft)
	}()

	if !r.mu.TryLock() {
		t.Fatal("the registry lock was stranded by the handler panic")
	}
	r.mu.Unlock()
}
