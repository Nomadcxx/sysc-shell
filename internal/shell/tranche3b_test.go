package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/render"
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
		if !reg.Metrics().SourceLeased(src) {
			t.Fatalf("source %v is used by a widget but not leased", src)
		}
	}
	for _, src := range []services.Source{services.SourceFilesystem, services.SourceBlock} {
		if reg.Metrics().SourceLeased(src) {
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
	if got := bar.left[0].inner.Text; got != string(render.MetricIconRune("cpu"))+" 42%" {
		t.Fatalf("healthy source rendered %q, want 42%%", got)
	}
	// A failed source keeps its icon: the field holds its width and still says
	// what it measures, while the placeholder says there is no reading.
	if got := bar.left[1].inner.Text; got != string(render.MetricIconRune("memory"))+" "+noWorkspace {
		t.Fatalf("failed source rendered %q, want the icon and the placeholder", got)
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

	if got := reg.bars[1].left[1].inner.Value; got != 0.25 {
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

// D8's requirement, measured against the face actually in use: a percentage
// must not change its node's width as it crosses from one digit to three. An
// unmeasured pixel constant cannot satisfy this, because the width it has to
// clear depends on the resolved font.
func TestAPercentageKeepsOneWidthFromNineToOneHundred(t *testing.T) {
	t.Parallel()
	cfg := metricConfig()
	bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "DP-9")
	if err != nil {
		t.Fatalf("NewWithTheme: %v", err)
	}

	width := func(fraction float64) int {
		t.Helper()
		bar.apply(barView{Metrics: services.Snapshot{
			CPU: &metrics.CPUSnapshot{Usage: metrics.CPUUsage{Fraction: fraction, Valid: true}},
		}})
		if err := bar.Layout(600, BarHeight); err != nil {
			t.Fatalf("Layout: %v", err)
		}
		return bar.left[0].node.Bounds.W
	}

	narrow, wide := width(0.09), width(1)
	if narrow != wide {
		t.Fatalf("9%% laid out %d wide and 100%% laid out %d; the floor must hold the field",
			narrow, wide)
	}
}

// The end of finding 1: a graph must read the history of the subject its
// widget names. The service keys rings per subject; this is the shell half —
// that each widget looks its own key up rather than its source's.
func TestAGraphPlotsItsOwnSubject(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Bar.Left = []config.Item{
		{ID: "network", Display: "graph", Interval: time.Second,
			Interface: "eth9", Direction: "rx"},
		{ID: "network", Display: "graph", Interval: time.Second,
			Interface: "eth8", Direction: "rx"},
	}
	cfg.Bar.Center, cfg.Bar.Right = nil, nil

	bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "DP-9")
	if err != nil {
		t.Fatalf("NewWithTheme: %v", err)
	}

	busy := services.Selector{Source: services.SourceNetwork, Subject: "eth9", Direction: "rx"}
	quiet := services.Selector{Source: services.SourceNetwork, Subject: "eth8", Direction: "rx"}
	bar.apply(barView{
		Metrics: services.Snapshot{Network: &metrics.NetworkSnapshot{
			Interfaces: []metrics.NetworkInterface{
				{Name: "eth9", Rates: metrics.NetworkRates{ReceiveBytesPerSecond: 1000, Valid: true}},
				{Name: "eth8", Rates: metrics.NetworkRates{ReceiveBytesPerSecond: 50, Valid: true}},
			},
		}},
		History: map[services.Selector][]float64{
			busy:  {1000, 1000},
			quiet: {100, 50},
		},
	})

	// The steady interface plots flat; the halving one falls. An aggregate
	// ring would have given both widgets the same shape.
	if got := bar.left[0].inner.Values; len(got) != 2 || got[0] != 1 || got[1] != 1 {
		t.Fatalf("the steady interface plotted %v, want a flat window", got)
	}
	if got := bar.left[1].inner.Values; len(got) != 2 || got[0] != 1 || got[1] != 0.5 {
		t.Fatalf("the halving interface plotted %v, want [1 0.5]", got)
	}
}

// The end of finding 2: a meter with no reading must not render as a genuine
// zero, because that is what an idle machine looks like.
func TestAMeterWithNoReadingIsAbsentRatherThanZero(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(mixedConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	meter := reg.bars[1].left[1].inner

	reg.UpdateMetrics(services.Snapshot{Memory: &metrics.MemorySnapshot{
		Memory: metrics.Capacity{TotalBytes: 1000, UsedBytes: 250},
	}})
	if meter.Absent || meter.Value != 0.25 {
		t.Fatalf("a read meter is absent=%v value=%v, want present at 0.25", meter.Absent, meter.Value)
	}

	// The collector fails: the source is nil this pass.
	reg.UpdateMetrics(services.Snapshot{})
	if !meter.Absent {
		t.Fatal("a meter with no reading is not marked absent, so it paints as 0%")
	}

	// And it recovers.
	reg.UpdateMetrics(services.Snapshot{Memory: &metrics.MemorySnapshot{
		Memory: metrics.Capacity{TotalBytes: 1000, UsedBytes: 500},
	}})
	if meter.Absent || meter.Value != 0.5 {
		t.Fatalf("a recovered meter is absent=%v value=%v, want present at 0.5", meter.Absent, meter.Value)
	}
}

// A graph must stop plotting when its source stops reporting, rather than
// showing a live line built from samples that are minutes old.
func TestAGraphStopsPlottingWhenItsSourceFails(t *testing.T) {
	t.Parallel()
	cfg := mixedConfig()
	bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "DP-9")
	if err != nil {
		t.Fatalf("NewWithTheme: %v", err)
	}

	sel := services.Selector{Source: services.SourceNetwork, Subject: "eth9", Direction: "rx"}
	history := map[services.Selector][]float64{sel: {500, 1000}}

	bar.apply(barView{
		Metrics: services.Snapshot{Network: &metrics.NetworkSnapshot{
			Interfaces: []metrics.NetworkInterface{{
				Name:  "eth9",
				Rates: metrics.NetworkRates{ReceiveBytesPerSecond: 1000, Valid: true},
			}},
		}},
		History: history,
	})
	graph := bar.left[2].inner
	if len(graph.Values) == 0 || graph.Absent {
		t.Fatal("a graph with a reading plotted nothing")
	}

	// The collector fails. The ring still holds its last good window, which is
	// exactly what must not be drawn.
	bar.apply(barView{History: history})
	if len(graph.Values) != 0 || !graph.Absent {
		t.Fatalf("a failed graph plotted %v (absent=%v), want an empty plot",
			graph.Values, graph.Absent)
	}
}

// The end of finding 3: a meter whose fraction is unchanged must not repaint.
// "No source change, no submitted frame" has to hold for every display mode,
// not only the one whose state happens to be text.
func TestAnUnchangedMeterChangesNothing(t *testing.T) {
	t.Parallel()
	cfg := metricConfig()
	cfg.Bar.Left = []config.Item{{
		ID: "memory", Display: "meter", Interval: time.Second,
	}}

	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	snap := services.Snapshot{Memory: &metrics.MemorySnapshot{
		Memory: metrics.Capacity{TotalBytes: 1000, UsedBytes: 250},
	}}
	if changed := reg.UpdateMetrics(snap); len(changed) != 1 {
		t.Fatalf("first sample changed %v, want global 1", changed)
	}
	if changed := reg.UpdateMetrics(snap); len(changed) != 0 {
		t.Fatalf("an identical meter sample changed %v, want nothing", changed)
	}

	moved := services.Snapshot{Memory: &metrics.MemorySnapshot{
		Memory: metrics.Capacity{TotalBytes: 1000, UsedBytes: 500},
	}}
	if changed := reg.UpdateMetrics(moved); len(changed) != 1 {
		t.Fatalf("a moved meter changed %v, want global 1", changed)
	}
}

// A graph whose window is identical must not repaint either. Its values do
// change on almost every real tick, which is why the design expects it to
// repaint often — but "often" must follow from the data, not from its kind.
func TestAnUnchangedGraphChangesNothing(t *testing.T) {
	t.Parallel()
	cfg := metricConfig()
	cfg.Bar.Left = []config.Item{{
		ID: "cpu", Display: "graph", Interval: time.Second,
	}}

	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	snap := services.Snapshot{CPU: &metrics.CPUSnapshot{
		Usage: metrics.CPUUsage{Fraction: 0.42, Valid: true},
	}}
	reg.UpdateMetrics(snap)
	if changed := reg.UpdateMetrics(snap); len(changed) != 0 {
		t.Fatalf("an identical graph window changed %v, want nothing", changed)
	}
}
