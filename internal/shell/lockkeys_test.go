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

// TestTheBarSeesALockHeldWhenTheShellStarts pins the startup seed: a keyboard
// whose Caps Lock is already on must reach the widget on the first frame, not
// at the first change.
func TestTheBarSeesALockHeldWhenTheShellStarts(t *testing.T) {
	root := t.TempDir()
	lockKeyLED(t, root, "input13::capslock", "1")
	restore := swapLockKeys(func() *services.LockKeys {
		return services.NewLockKeys(root, 10*time.Millisecond)
	})
	t.Cleanup(restore)

	cfg := config.Default()
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	r.startLockKeys(cfg)
	if !r.lockKeysState.Caps {
		t.Fatal("the bar feed did not carry the Caps Lock held at startup")
	}
}
