package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// waitLockKeysPolling waits for the poller to run, which is the proof that a
// consumer holds the service.
func waitLockKeysPolling(t *testing.T, l *services.LockKeys) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !l.Polling() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !l.Polling() {
		t.Fatal("the lock-key poller never started")
	}
}

// osdView reads the last OSD payload the manager showed. The manager writes
// it under Registry.mu, so a reader takes the lock too.
func osdView(r *Registry) OSDView {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.osd.view
}

// waitLockState waits for the registry's copy of the lock state to match.
func waitLockState(t *testing.T, r *Registry, want services.LockState) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		got := r.lockKeysState
		r.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the lock state never reached %+v", want)
}

// A bar that places the lock_keys widget wants the lock state on its own.
// Turning the OSD off must not take the bar's service down.
func TestTurningTheLockKeyOSDOffLeavesTheBarWidgetAlone(t *testing.T) {
	root := t.TempDir()
	lockKeyLED(t, root, "input13::capslock", "0")
	restore := swapLockKeys(func() *services.LockKeys {
		return services.NewLockKeys(root, 10*time.Millisecond)
	})
	t.Cleanup(restore)

	cfg := config.Default()
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	r.startLockKeys(cfg)
	if r.lockKeys == nil {
		t.Fatal("the shell did not build a lock-key service")
	}
	svc := r.lockKeys
	// A bar places the lock_keys widget. The OSD already owns the service, so
	// the bar does not build one; the relay feeds the bar.
	r.ensureLockKeysLocked()
	if r.lockKeys != svc {
		t.Fatal("the bar replaced the service the OSD relay owns")
	}

	// The relay takes the poller's lease when it starts, so wait for the
	// poller before the switch: the assertion below is about the OSD going
	// off, not about the relay having been scheduled.
	waitLockKeysPolling(t, svc)

	off := config.Default()
	off.Panels.OSDLockKeys = false
	reloadLockKeys(t, r, off)
	if r.lockKeys == nil {
		t.Fatal("turning the OSD off took the bar's lock-key service down")
	}
	if !svc.Polling() {
		t.Fatal("the bar's lock-key poller stopped with the OSD")
	}
}

// The other direction: the bar owns the service because the OSD was off at
// startup, and a reload turns the OSD on. The OSD announces from the relay
// the bar already runs.
func TestTurningTheLockKeyOSDOnAnnouncesBesideTheBar(t *testing.T) {
	root := t.TempDir()
	lockKeyLED(t, root, "input13::capslock", "0")
	restore := swapLockKeys(func() *services.LockKeys {
		return services.NewLockKeys(root, 10*time.Millisecond)
	})
	t.Cleanup(restore)

	off := config.Default()
	off.Panels.OSDLockKeys = false
	r := NewRegistry(off)
	t.Cleanup(r.Close)
	r.startLockKeys(off)
	if r.lockKeys != nil {
		t.Fatal("the shell built a lock-key service the OSD does not want")
	}
	// A bar places the lock_keys widget, so the bar owns the service.
	r.ensureLockKeysLocked()
	if r.lockKeys == nil {
		t.Fatal("the bar did not get a lock-key service")
	}
	waitLockKeysPolling(t, r.lockKeys)

	// With the OSD off the relay keeps the state but announces nothing.
	lockKeyLED(t, root, "input13::capslock", "1")
	waitLockState(t, r, services.LockState{Caps: true})
	if view := osdView(r); view.Kind == osdCapsLock {
		t.Fatalf("the OSD announced %+v while it was off", view)
	}

	reloadLockKeys(t, r, config.Default())
	lockKeyLED(t, root, "input13::capslock", "0")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if osdView(r).Kind == osdCapsLock {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the OSD did not announce beside the bar: view = %+v", osdView(r))
}

// A bar that places the lock_keys widget wants the state while the OSD is
// off, so the service runs and the widget is fed.
func TestTheBarWidgetIsFedWhileTheOSDIsOff(t *testing.T) {
	root := t.TempDir()
	lockKeyLED(t, root, "input13::capslock", "0")
	restore := swapLockKeys(func() *services.LockKeys {
		return services.NewLockKeys(root, 10*time.Millisecond)
	})
	t.Cleanup(restore)

	off := config.Default()
	off.Panels.OSDLockKeys = false
	r := NewRegistry(off)
	t.Cleanup(r.Close)
	r.startLockKeys(off)
	r.ensureLockKeysLocked()
	if r.lockKeys == nil {
		t.Fatal("the bar did not get a lock-key service")
	}
	waitLockKeysPolling(t, r.lockKeys)

	lockKeyLED(t, root, "input13::capslock", "1")
	waitLockState(t, r, services.LockState{Caps: true})
	if !r.lockKeys.Available() {
		t.Fatal("the lock-key service is not available, so the widget reads absent")
	}
}

// The last bar leaving gives the claim back, and the service closes when the
// OSD does not want it either.
func TestTheLastBarLeavingClosesTheLockKeyService(t *testing.T) {
	root := t.TempDir()
	lockKeyLED(t, root, "input13::capslock", "0")
	var svc *services.LockKeys
	restore := swapLockKeys(func() *services.LockKeys {
		svc = services.NewLockKeys(root, 10*time.Millisecond)
		return svc
	})
	t.Cleanup(restore)

	off := config.Default()
	off.Panels.OSDLockKeys = false
	r := NewRegistry(off)
	t.Cleanup(r.Close)
	r.startLockKeys(off)
	r.ensureLockKeysLocked()
	if r.lockKeys == nil {
		t.Fatal("the bar did not get a lock-key service")
	}
	waitLockKeysPolling(t, r.lockKeys)

	r.releaseLockKeysBarLocked()
	if r.lockKeys != nil {
		t.Fatal("the service outlived the last bar that wanted the state")
	}
	if svc.Polling() {
		t.Fatal("the poller outlived the last bar that wanted the state")
	}
}
