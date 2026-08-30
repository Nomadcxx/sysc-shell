package shell

import (
	"runtime"
	"testing"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// mixedConfig carries a text metric, a meter and a graph on one bar, which is
// the arrangement most likely to expose a change-detection defect.
func mixedConfig() config.Config {
	cfg := config.Default()
	cfg.Bar.Left = []config.Item{
		{ID: "cpu", Display: "text", Interval: time.Second},
		{ID: "memory", Display: "meter", Interval: time.Second},
		{ID: "network", Display: "graph", Interval: time.Second,
			Interface: "eth9", Direction: "rx"},
	}
	cfg.Bar.Center = nil
	cfg.Bar.Right = nil
	return cfg
}

func TestAMixedBarLeasesEverySourceItUses(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(mixedConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	for _, src := range []services.Source{
		services.SourceCPU, services.SourceMemory, services.SourceNetwork,
	} {
		if !reg.Metrics().Leased(src) {
			t.Fatalf("source %v is used by a widget but not leased", src)
		}
	}
	for _, src := range []services.Source{services.SourceFilesystem, services.SourceBlock} {
		if reg.Metrics().Leased(src) {
			t.Fatalf("source %v leased with no widget", src)
		}
	}
}

// A partial failure must isolate: one unreadable source renders the
// placeholder while its neighbours keep rendering.
func TestOneFailingSourceDoesNotSuppressAnother(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Bar.Left = []config.Item{
		{ID: "cpu", Display: "text", Interval: time.Second},
		{ID: "memory", Display: "text", Interval: time.Second},
	}
	cfg.Bar.Center, cfg.Bar.Right = nil, nil

	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	// CPU present, memory absent — as when one collector fails.
	reg.UpdateMetrics(services.Snapshot{
		CPU: &metrics.CPUSnapshot{Usage: metrics.CPUUsage{Fraction: 0.42, Valid: true}},
	})

	bar := reg.bars[1]
	if got := bar.left[0].node.Text; got != "42%" {
		t.Fatalf("healthy source rendered %q, want 42%%", got)
	}
	if got := bar.left[1].node.Text; got != noWorkspace {
		t.Fatalf("failed source rendered %q, want the placeholder", got)
	}
}

// A meter's fraction reaches its node, which is how a meter renders at all.
func TestAMeterCarriesItsFractionOnTheNode(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(mixedConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	reg.UpdateMetrics(services.Snapshot{
		Memory: &metrics.MemorySnapshot{
			Memory: metrics.Capacity{TotalBytes: 1000, UsedBytes: 250},
		},
	})

	if got := reg.bars[1].left[1].node.Value; got != 0.25 {
		t.Fatalf("meter node value = %v, want 0.25", got)
	}
}

// A graph normalises against its own window, which is what lets a rate with no
// natural full scale be plotted at all.
func TestAGraphNormalisesAgainstItsWindow(t *testing.T) {
	t.Parallel()
	got := normalise([]float64{1_000_000, 2_000_000, 4_000_000})

	if len(got) != 3 {
		t.Fatalf("normalised %d values, want 3", len(got))
	}
	if got[2] != 1 {
		t.Fatalf("window maximum normalised to %v, want 1", got[2])
	}
	if got[0] != 0.25 || got[1] != 0.5 {
		t.Fatalf("normalised = %v, want [0.25 0.5 1]", got)
	}
}

// An all-zero window must plot flat rather than divide by zero.
func TestAnAllZeroWindowNormalisesFlat(t *testing.T) {
	t.Parallel()
	for _, v := range normalise([]float64{0, 0, 0}) {
		if v != 0 {
			t.Fatalf("all-zero window normalised to %v, want flat zero", v)
		}
	}
}

// An accepted reload must not restart a sampling service still in use, and a
// changed interval must re-arm rather than cycle the goroutine.
func TestAnAcceptedReloadDoesNotRestartTheSamplingService(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(metricConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	if got := reg.Metrics().Starts(); got != 1 {
		t.Fatalf("starts = %d before reload, want 1", got)
	}

	candidate := metricConfig()
	candidate.Bar.Left[0].Interval = time.Second
	prepared, err := reg.PrepareConfig(candidate, identities(map[uint32]string{1: "DP-9"}))
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	prepared.Commit()

	if got := reg.Metrics().Starts(); got != 1 {
		t.Fatalf("starts = %d after reload, want 1; the service restarted", got)
	}
	if !reg.Metrics().Running() {
		t.Fatal("the sampling service stopped across a reload that still uses it")
	}
}

// A rejected reload must leave the sampling service exactly as it was.
func TestARejectedReloadLeavesTheSamplingServiceUnchanged(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(metricConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})
	before := reg.Metrics().Starts()

	broken := metricConfig()
	broken.Bar.Height = 4
	broken.Bar.Gap = 4
	if _, err := reg.PrepareConfig(broken, identities(map[uint32]string{1: "DP-9"})); err == nil {
		t.Fatal("an unbuildable candidate was prepared")
	}

	if got := reg.Metrics().Starts(); got != before {
		t.Fatalf("starts = %d, want the unchanged %d", got, before)
	}
	if !reg.Metrics().Running() {
		t.Fatal("a rejected reload stopped the sampling service")
	}
}

// Every goroutine the tranche starts must be gone once the registry closes.
func TestClosingTheRegistryStopsTheSamplingGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()

	reg := NewRegistry(mixedConfig())
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})
	reg.Close()

	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > before {
		t.Fatalf("goroutines = %d after Close, want at most the starting %d", got, before)
	}
}
