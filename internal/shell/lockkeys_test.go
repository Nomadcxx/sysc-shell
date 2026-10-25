package shell

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// lockKeyLED writes a fake keyboard LED the way the kernel exposes one.
func lockKeyLED(t *testing.T, root, name, value string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "brightness"), []byte(value+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// swapLockKeys points the shell at another lock-key service and returns the
// restore.
func swapLockKeys(next func() *services.LockKeys) func() {
	return newLockKeys.Swap(next)
}

func TestTheShellOnlyPollsLockKeysWhenTheOSDWantsThem(t *testing.T) {
	root := t.TempDir()
	lockKeyLED(t, root, "input13::capslock", "0")
	built := 0
	restore := swapLockKeys(func() *services.LockKeys {
		built++
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
	if built != 0 {
		t.Fatalf("the shell built %d lock-key services the OSD does not want", built)
	}
}

func TestTheLockKeyPollerRunsWhileTheOSDRelayHoldsItsLease(t *testing.T) {
	root := t.TempDir()
	lockKeyLED(t, root, "input13::capslock", "0")
	restore := swapLockKeys(func() *services.LockKeys {
		return services.NewLockKeys(root, 10*time.Millisecond)
	})
	t.Cleanup(restore)

	cfg := config.Default()
	if !cfg.Panels.OSDLockKeys {
		t.Fatal("the default configuration turned the lock-key OSD off")
	}
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	r.startLockKeys(cfg)
	if r.lockKeys == nil {
		t.Fatal("the shell did not build a lock-key service")
	}
	deadline := time.Now().Add(2 * time.Second)
	for !r.lockKeys.Polling() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !r.lockKeys.Polling() {
		t.Fatal("the relay did not take the lease, so nothing polls the LED class")
	}
}

// reloadLockKeys drives one real config reload: the same prepare and commit
// path a saved config file change takes.
func reloadLockKeys(t *testing.T, r *Registry, cfg config.Config) {
	t.Helper()
	prepared, err := r.PrepareConfigWithGeneration(cfg, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	prepared.Commit()
}

func TestAReloadTurningTheLockKeyOSDOnWiresTheRelayWithoutARestart(t *testing.T) {
	root := t.TempDir()
	lockKeyLED(t, root, "input13::capslock", "0")
	built := 0
	restore := swapLockKeys(func() *services.LockKeys {
		built++
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

	reloadLockKeys(t, r, config.Default())
	if r.lockKeys == nil {
		t.Fatal("a reload turning the lock-key OSD on left the relay unwired")
	}
	deadline := time.Now().Add(2 * time.Second)
	for !r.lockKeys.Polling() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !r.lockKeys.Polling() {
		t.Fatal("the reload wired no relay, so nothing polls the LED class")
	}
	if built != 1 {
		t.Fatalf("the reload built %d lock-key services, want 1", built)
	}
}

func TestAReloadTurningTheLockKeyOSDOffTakesTheRelayDown(t *testing.T) {
	root := t.TempDir()
	lockKeyLED(t, root, "input13::capslock", "0")
	var svc *services.LockKeys
	restore := swapLockKeys(func() *services.LockKeys {
		svc = services.NewLockKeys(root, 10*time.Millisecond)
		return svc
	})
	t.Cleanup(restore)

	cfg := config.Default()
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	r.startLockKeys(cfg)
	if r.lockKeys == nil {
		t.Fatal("the shell did not build a lock-key service")
	}
	deadline := time.Now().Add(2 * time.Second)
	for !r.lockKeys.Polling() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !r.lockKeys.Polling() {
		t.Fatal("the relay did not take the lease, so nothing polls the LED class")
	}

	off := config.Default()
	off.Panels.OSDLockKeys = false
	reloadLockKeys(t, r, off)
	if r.lockKeys != nil {
		t.Fatal("a reload turning the lock-key OSD off left the relay wired")
	}
	if svc.Polling() {
		t.Fatal("the poller outlived the knob that asked for it")
	}
}
