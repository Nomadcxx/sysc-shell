package shell

import (
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// processCPUTime is the CPU time this process has consumed, user plus system.
func processCPUTime(t *testing.T) time.Duration {
	t.Helper()
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatalf("getrusage: %v", err)
	}
	return time.Duration(usage.Utime.Sec+usage.Stime.Sec)*time.Second +
		time.Duration(usage.Utime.Usec+usage.Stime.Usec)*time.Microsecond
}

// TestSettingsApplicationsCPUBudget is acceptance criterion 1: a frame of the
// Applications page over 1,000 installed apps must cost at most 16 ms of CPU at
// 2x. It measures the real render callback, so the mask cache, the layout and
// the row build all count.
func TestSettingsApplicationsCPUBudget(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector inflates frame CPU time; the allocation budget still runs")
	}
	_, spec := openSyntheticApps(t, syntheticAppCatalog(1000))
	scale := ui.Scale120(240)
	pw, ph := scale.Physical(int(spec.Width)), scale.Physical(int(spec.Height))
	pixels := make([]byte, pw*ph*4)
	render := func() {
		t.Helper()
		if err := spec.Callbacks.Render(pixels, pw, ph, pw*4); err != nil {
			t.Fatal(err)
		}
	}
	render() // warm the mask caches and the text shaper

	const frames = 50
	startCPU := processCPUTime(t)
	startWall := time.Now()
	for i := 0; i < frames; i++ {
		render()
	}
	perFrame := (processCPUTime(t) - startCPU) / time.Duration(frames)
	wall := time.Since(startWall) / frames
	const budget = 16 * time.Millisecond
	t.Logf("Applications frame at 2x over 1000 apps: %v CPU, %v wall", perFrame, wall)
	if perFrame > budget {
		t.Fatalf("frame costs %v CPU, over the %v budget", perFrame, budget)
	}
}

// TestSettingsApplicationsFrameAllocation guards the per-frame allocation the
// virtual list removed: copying every row used to cost 13 MB a frame and the
// uncached rounded mask 242 MB. A frame that allocates megabytes has brought
// one of those back.
func TestSettingsApplicationsFrameAllocation(t *testing.T) {
	_, spec := openSyntheticApps(t, syntheticAppCatalog(1000))
	scale := ui.Scale120(240)
	pw, ph := scale.Physical(int(spec.Width)), scale.Physical(int(spec.Height))
	pixels := make([]byte, pw*ph*4)
	render := func() {
		t.Helper()
		if err := spec.Callbacks.Render(pixels, pw, ph, pw*4); err != nil {
			t.Fatal(err)
		}
	}
	render()

	const frames = 20
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < frames; i++ {
		render()
	}
	runtime.ReadMemStats(&after)
	perFrame := (after.TotalAlloc - before.TotalAlloc) / frames
	const budget = 4 << 20
	t.Logf("Applications frame allocates %d bytes", perFrame)
	if perFrame > budget {
		t.Fatalf("frame allocates %d bytes, over the %d-byte budget", perFrame, budget)
	}
}
