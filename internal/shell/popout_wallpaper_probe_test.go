package shell

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/fakebin"
)

// TestAHangingWallpaperProbeDoesNotHoldTheRegistryLock pins the bound on the
// wallpaper capability probe.
//
// The wallpaper service starts with the registry, and the gslapper probe
// runs while the registry lock is held. A build that does not answer --help
// would therefore hold the shell's global lock for the life of the process:
// the bar, the panels, the tray and the IPC server all stop answering, on
// the login path (sysc-2007).
func TestAHangingWallpaperProbeDoesNotHoldTheRegistryLock(t *testing.T) {
	dir := t.TempDir()
	hanging := filepath.Join(dir, "gslapper")
	if err := fakebin.Write(hanging, "#!/bin/sh\nexec /bin/sleep 30\n", 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	r := &Registry{cfg: config.Default()}
	r.mu.Lock()
	start := time.Now()
	r.wallpaperStartLocked()
	held := time.Since(start)
	r.mu.Unlock()

	if held > 10*time.Second {
		t.Fatalf("the wallpaper start held the registry lock for %s, want it bounded by the probe timeout", held)
	}
	if !r.mu.TryLock() {
		t.Fatal("the registry lock is still held after the wallpaper start")
	}
	r.mu.Unlock()
}
