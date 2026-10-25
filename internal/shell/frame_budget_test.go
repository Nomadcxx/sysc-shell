//go:build !race

package shell

import (
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The frame-budget check is excluded from race builds on purpose: the race
// detector inflates every frame by a factor the budget does not model, and a
// timing budget under instrumentation measures the instrumentation.

const (
	// applicationsFrameBudget is the CPU one Applications settings frame may
	// cost at 2x with a full installed-app catalog. GH #454 measures 541.5 ms
	// before the fix against 9-36 ms for every other settings page, and its
	// acceptance line is 16 ms.
	applicationsFrameBudget = 16 * time.Millisecond
	// applicationsFrameAllocBudget is the heap one frame may allocate. The
	// pre-fix frame allocated 47 MB; a frame that only copies and paints the
	// visible rows stays well under this. Allocations are deterministic, so
	// this catches a deep-copy regression even on a loaded machine.
	applicationsFrameAllocBudget = 4 << 20
)

// frameCPU returns the process CPU time used so far.
func frameCPU(t testing.TB) time.Duration {
	t.Helper()
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatalf("getrusage: %v", err)
	}
	return timevalDuration(usage.Utime) + timevalDuration(usage.Stime)
}

func timevalDuration(tv syscall.Timeval) time.Duration {
	return time.Duration(tv.Sec)*time.Second + time.Duration(tv.Usec)*time.Microsecond
}

// TestApplicationsFrameBudget is the GH #454 regression check: the Settings
// window's Applications page, built from a synthetic 1,000-app catalog and
// painted at 2x through the real render callback, must fit the CPU budget one
// frame. The cheapest frame of the batch decides: a machine under load inflates
// the worst frame but never the best, and the fix makes every frame cheap.
func TestApplicationsFrameBudget(t *testing.T) {
	_, spec := benchPanelCatalog(t, PanelSettings, "Applications", 240, syntheticApps(1000))
	scale := ui.Scale120(240)
	pw, ph := scale.Physical(int(spec.Width)), scale.Physical(int(spec.Height))
	pixels := make([]byte, pw*ph*4)
	frame := func() {
		t.Helper()
		if err := spec.Callbacks.Render(pixels, pw, ph, pw*4); err != nil {
			t.Fatalf("render: %v", err)
		}
	}

	// Two warm frames: the first warms the text renderer and the geometry
	// caches, and any budget is about the steady state.
	frame()
	frame()

	best := time.Duration(1<<63 - 1)
	var allocBest uint64 = 1<<64 - 1
	for i := 0; i < 5; i++ {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		cpuStart := frameCPU(t)
		frame()
		cpu := frameCPU(t) - cpuStart
		runtime.ReadMemStats(&after)
		if cpu < best {
			best = cpu
		}
		if allocated := after.TotalAlloc - before.TotalAlloc; allocated < allocBest {
			allocBest = allocated
		}
	}
	if best > applicationsFrameBudget {
		t.Fatalf("Applications frame costs %v CPU, budget is %v (GH #454)", best, applicationsFrameBudget)
	}
	if allocBest > applicationsFrameAllocBudget {
		t.Fatalf("Applications frame allocates %d bytes, budget is %d (GH #454)", allocBest, applicationsFrameAllocBudget)
	}
}
