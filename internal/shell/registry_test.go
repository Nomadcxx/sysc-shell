package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"os/exec"
	"testing"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// newHosts is the common setup: one registry with hosts at the given globals.
func newHosts(t *testing.T, reg *Registry, hosts map[uint32]string) {
	t.Helper()
	for global, connector := range hosts {
		if _, err := reg.NewHost(global, connector); err != nil {
			t.Fatalf("NewHost(%d, %s): %v", global, connector, err)
		}
	}
}

func TestTwoBarsShareOneClockServiceAndOneUpdate(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	if got := reg.Clock().Starts(); got != 1 {
		t.Fatalf("clock starts = %d, want 1 shared start for two bars", got)
	}

	changed := reg.UpdateClock(time.Date(2026, 8, 30, 15, 4, 5, 0, time.UTC))
	if len(changed) != 2 {
		t.Fatalf("one clock update changed %d bars, want 2", len(changed))
	}
}

func TestRemovingOneBarRetainsTheServiceForTheOther(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	reg.DropHost(1)
	if !reg.Clock().Running() {
		t.Fatal("dropping one of two bars stopped the clock")
	}

	reg.DropHost(2)
	if reg.Clock().Running() {
		t.Fatal("dropping the last bar left the clock running")
	}
}

// Reconnect overlap: two globals briefly carry the same connector. They must
// stay distinct instances with distinct leases.
func TestTwoGlobalsSharingAConnectorKeepDistinctInstances(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "DP-9"})

	if len(reg.bars) != 2 {
		t.Fatalf("bars = %d, want two distinct instances for one connector", len(reg.bars))
	}
	if reg.bars[1] == reg.bars[2] {
		t.Fatal("two globals share one bar instance")
	}

	// A projection for that connector must reach both.
	changed := reg.UpdateNiri(niri.Snapshot{
		Workspaces: []niri.Workspace{{ID: 5, Name: "code", Output: "DP-9", Active: true}},
	})
	if len(changed) != 2 {
		t.Fatalf("one connector's change reached %d bars, want 2", len(changed))
	}

	// Dropping the stale global must not remove the reconnected one.
	reg.DropHost(1)
	if _, ok := reg.bars[2]; !ok {
		t.Fatal("dropping one global removed the other sharing its connector")
	}
}

func TestOnlyTheAffectedOutputIsInvalidated(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	reg.UpdateNiri(niri.Snapshot{Workspaces: []niri.Workspace{
		{ID: 5, Name: "code", Output: "DP-9", Active: true},
		{ID: 6, Name: "chat", Output: "HDMI-A-9", Active: true},
	}})

	// Change one output only. The bar draws numbered pills, so a rename is
	// invisible to it; adding a workspace to DP-9 is what its pill row sees.
	changed := reg.UpdateNiri(niri.Snapshot{Workspaces: []niri.Workspace{
		{ID: 5, Index: 1, Name: "code", Output: "DP-9", Active: true},
		{ID: 7, Index: 2, Name: "notes", Output: "DP-9"},
		{ID: 6, Name: "chat", Output: "HDMI-A-9", Active: true},
	}})
	if len(changed) != 1 || changed[0] != 1 {
		t.Fatalf("changed = %v, want only global 1", changed)
	}
}

func TestAnIdenticalSnapshotChangesNothing(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	snap := niri.Snapshot{Workspaces: []niri.Workspace{
		{ID: 5, Name: "code", Output: "DP-9", Active: true},
	}}
	if changed := reg.UpdateNiri(snap); len(changed) != 1 {
		t.Fatalf("first update changed %v, want global 1", changed)
	}
	if changed := reg.UpdateNiri(snap); len(changed) != 0 {
		t.Fatalf("an identical snapshot changed %v", changed)
	}
}

// A clock tick inside the same minute renders identical text, so no bar
// repaints. This is the no-change-no-frame invariant.
func TestATickInsideTheSameBoundaryChangesNothing(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	base := time.Date(2026, 8, 30, 15, 4, 5, 0, time.UTC)
	if changed := reg.UpdateClock(base); len(changed) != 1 {
		t.Fatalf("first tick changed %v, want global 1", changed)
	}
	if changed := reg.UpdateClock(base.Add(20 * time.Second)); len(changed) != 0 {
		t.Fatalf("a tick inside the same minute changed %v", changed)
	}
	if changed := reg.UpdateClock(base.Add(time.Minute)); len(changed) != 1 {
		t.Fatalf("crossing a minute changed %v, want global 1", changed)
	}
}

// Niri state may name an output whose wl_output has not been announced yet.
// It must be held and applied when the host appears, and must never create one.
func TestNiriStateForAnUnknownOutputIsHeldNotDropped(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)

	reg.UpdateNiri(niri.Snapshot{Workspaces: []niri.Workspace{
		{ID: 5, Name: "later", Output: "DP-9", Active: true},
	}})
	if len(reg.bars) != 0 {
		t.Fatal("a Niri event created a bar")
	}

	newHosts(t, reg, map[uint32]string{1: "DP-9"})
	if got := pillIndices(reg.bars[1].left[0].node); len(got) == 0 {
		t.Fatal("new bar shows no workspace pills, so the held state was lost")
	}
}

func TestAConfigWithNoClockLeavesTheServiceStopped(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Bar.Left = []config.Item{{ID: "workspace"}}
	cfg.Bar.Center = nil
	cfg.Bar.Right = nil

	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	if reg.Clock().Running() {
		t.Fatal("a configuration with no clock started the clock service")
	}
}

func TestCloseReleasesEverything(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	reg.Close()
	if reg.Clock().Running() {
		t.Fatal("Close left the clock running")
	}
	if len(reg.bars) != 0 {
		t.Fatal("Close left bars behind")
	}
	reg.Close()
}

// identities is the host set a reload prepares for, in the shape the Wayland
// callbacks supply.
func identities(hosts map[uint32]string) []wayland.HostIdentity {
	out := make([]wayland.HostIdentity, 0, len(hosts))
	for global, connector := range hosts {
		out = append(out, wayland.HostIdentity{Global: global, Connector: connector})
	}
	return out
}

func TestAnAcceptedReloadDoesNotRestartAServiceStillInUse(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	before := reg.bars[1]
	if got := reg.Clock().Starts(); got != 1 {
		t.Fatalf("clock starts = %d before reload, want 1", got)
	}

	candidate := config.Default()
	candidate.Theme.Radius = 10
	prepared, err := reg.PrepareConfig(candidate, identities(map[uint32]string{1: "DP-9", 2: "HDMI-A-9"}))
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	// Prepare must not touch live state.
	if reg.bars[1] != before {
		t.Fatal("PrepareConfig replaced a live bar before commit")
	}

	prepared.Commit()
	if reg.bars[1] == before {
		t.Fatal("commit retained the old bar")
	}
	if got := reg.Clock().Starts(); got != 1 {
		t.Fatalf("clock starts = %d after reload, want 1; the service restarted", got)
	}
	if !reg.Clock().Running() {
		t.Fatal("the clock stopped across a reload that still uses it")
	}
}

// A format that coarsens or refines the tick must re-arm, not restart.
func TestAnAcceptedReloadDoesNotRestartWhenTheClockBoundaryChanges(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	if got := reg.Clock().Starts(); got != 1 {
		t.Fatalf("clock starts = %d before reload, want 1", got)
	}

	candidate := config.Default()
	candidate.Bar.Center = []config.Item{{
		ID: "clock", Format: "15:04:05", Boundary: time.Second,
	}}
	prepared, err := reg.PrepareConfig(candidate, identities(map[uint32]string{1: "DP-9"}))
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	prepared.Commit()

	if got := reg.Clock().Starts(); got != 1 {
		t.Fatalf("clock starts = %d after a boundary change, want 1; the service restarted", got)
	}
	if !reg.Clock().Running() {
		t.Fatal("the clock stopped across a reload that still uses it")
	}
}

func TestARejectedReloadLeavesServicesAndWidgetsUnchanged(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	reg.UpdateNiri(niri.Snapshot{Workspaces: []niri.Workspace{
		{ID: 5, Name: "code", Output: "DP-9", Active: true},
	}})
	before := reg.bars[1]
	beforeText := before.left[0].node.Text
	beforeStarts := reg.Clock().Starts()

	// A theme this bar cannot be built from: a gap that leaves no body.
	broken := config.Default()
	broken.Bar.Height = 4
	broken.Bar.Gap = 4
	if _, err := reg.PrepareConfig(broken, identities(map[uint32]string{1: "DP-9"})); err == nil {
		t.Fatal("an unbuildable candidate was prepared")
	}

	if reg.bars[1] != before {
		t.Fatal("a rejected reload replaced the live bar")
	}
	if got := reg.bars[1].left[0].node.Text; got != beforeText {
		t.Fatalf("visible text = %q, want the unchanged %q", got, beforeText)
	}
	if got := reg.Clock().Starts(); got != beforeStarts {
		t.Fatalf("clock starts = %d, want the unchanged %d", got, beforeStarts)
	}
	if !reg.Clock().Running() {
		t.Fatal("a rejected reload stopped the clock")
	}
}

// The owner may still reject after the shell prepared. Rollback must return
// lease counts exactly where they were.
func TestRollbackReleasesEverythingPrepareAcquired(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	prepared, err := reg.PrepareConfig(config.Default(), identities(map[uint32]string{1: "DP-9"}))
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	prepared.Rollback()

	if !reg.Clock().Running() {
		t.Fatal("rollback stopped a service the live bar still uses")
	}
	// The live bar must still hold exactly its own lease, so dropping it stops
	// the clock. A leaked prepared lease would keep it running.
	reg.DropHost(1)
	if reg.Clock().Running() {
		t.Fatal("rollback leaked a lease: the clock outlived its last consumer")
	}
}

func TestCommitAppliesHeldStateToTheReplacementBars(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})
	reg.UpdateNiri(niri.Snapshot{Workspaces: []niri.Workspace{
		{ID: 5, Name: "code", Output: "DP-9", Active: true},
	}})

	prepared, err := reg.PrepareConfig(config.Default(), identities(map[uint32]string{1: "DP-9"}))
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	prepared.Commit()

	if got := pillIndices(reg.bars[1].left[0].node); len(got) == 0 {
		t.Fatal("replacement bar shows no workspace pills, so the held state was lost")
	}
}

// metricConfig is a bar carrying one CPU text widget.
func metricConfig() config.Config {
	cfg := config.Default()
	cfg.Bar.Left = []config.Item{{
		ID: "cpu", Display: "text", Interval: 2 * time.Second,
	}}
	cfg.Bar.Center = nil
	cfg.Bar.Right = nil
	return cfg
}

func TestAMetricWidgetLeasesItsSource(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(metricConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	if !reg.Metrics().SourceLeased(services.SourceCPU) {
		t.Fatal("a CPU widget did not lease the CPU source")
	}
	for _, src := range []services.Source{
		services.SourceMemory, services.SourceFilesystem,
		services.SourceBlock, services.SourceNetwork,
	} {
		if reg.Metrics().SourceLeased(src) {
			t.Fatalf("source %v leased with no widget", src)
		}
	}
}

func TestTwoBarsShareOneMetricsServiceAndOneSample(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(metricConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	if got := reg.Metrics().Starts(); got != 1 {
		t.Fatalf("metrics starts = %d, want 1 shared start for two bars", got)
	}

	changed := reg.UpdateMetrics(services.Snapshot{
		CPU: &metrics.CPUSnapshot{Usage: metrics.CPUUsage{Fraction: 0.42, Valid: true}},
	})
	if len(changed) != 2 {
		t.Fatalf("one sample changed %d bars, want 2", len(changed))
	}
}

func TestDroppingTheLastMetricBarStopsTheService(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(metricConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	reg.DropHost(1)
	if !reg.Metrics().Running() {
		t.Fatal("dropping one of two bars stopped the metrics service")
	}
	reg.DropHost(2)
	if reg.Metrics().Running() {
		t.Fatal("dropping the last bar left the metrics service running")
	}
}

// An unchanged sample must not repaint: no source change, no submitted frame.
func TestAnUnchangedSampleChangesNothing(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(metricConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	snap := services.Snapshot{
		CPU: &metrics.CPUSnapshot{Usage: metrics.CPUUsage{Fraction: 0.42, Valid: true}},
	}
	if changed := reg.UpdateMetrics(snap); len(changed) != 1 {
		t.Fatalf("first sample changed %v, want global 1", changed)
	}
	if changed := reg.UpdateMetrics(snap); len(changed) != 0 {
		t.Fatalf("an identical sample changed %v", changed)
	}
}

// A configuration naming no metric leaves the service stopped, so a clock-only
// bar costs no sampling goroutine.
func TestAConfigWithNoMetricLeavesTheServiceStopped(t *testing.T) {
	t.Parallel()
	// The default bar now ships status widgets, so a no-metric configuration
	// has to be built rather than taken from the defaults.
	cfg := config.Default()
	cfg.Bar.Right = nil
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	if reg.Metrics().Running() {
		t.Fatal("a configuration with no metric started the sampling service")
	}
}

// A graph repaints when its window changes, and not otherwise. Its values do
// change on almost every real tick, so it will repaint often — but that has to
// follow from the data rather than from the widget's kind.
func TestAGraphRepaintsWhenItsWindowChanges(t *testing.T) {
	t.Parallel()
	cfg := metricConfig()
	cfg.Bar.Left = []config.Item{{
		ID: "cpu", Display: "graph", Interval: 2 * time.Second,
	}}

	bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "DP-9")
	if err != nil {
		t.Fatalf("NewWithTheme: %v", err)
	}

	sel := services.Selector{Source: services.SourceCPU}
	live := services.Snapshot{CPU: &metrics.CPUSnapshot{
		Usage: metrics.CPUUsage{Fraction: 0.42, Valid: true},
	}}
	view := func(samples ...float64) barView {
		return barView{Metrics: live, History: map[services.Selector][]float64{sel: samples}}
	}

	if !bar.apply(view(0.1, 0.2)) {
		t.Fatal("the first window did not mark the bar changed")
	}
	if bar.apply(view(0.1, 0.2)) {
		t.Fatal("an identical window marked the bar changed")
	}
	if !bar.apply(view(0.1, 0.9)) {
		t.Fatal("a moved window did not mark the bar changed")
	}
}

func weatherConfig() config.Config {
	cfg := config.Default()
	cfg.Weather = config.Weather{
		Latitude: 0, Longitude: 0, Unit: "celsius",
		Interval: 15 * time.Minute, Configured: true,
	}
	cfg.Bar.Left = []config.Item{{ID: "weather", MaxWidth: 160}}
	cfg.Bar.Center, cfg.Bar.Right = nil, nil
	return cfg
}

func TestTwoBarsShareOneWeatherServiceAndOneReading(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(weatherConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	if got := reg.Weather().Starts(); got != 1 {
		t.Fatalf("weather starts = %d, want 1 shared start for two bars", got)
	}
	changed := reg.UpdateWeather(services.Reading{
		Observed: true, Temperature: 18, Unit: services.UnitCelsius,
	})
	if len(changed) != 2 {
		t.Fatalf("one reading changed %d bars, want 2", len(changed))
	}
}

func TestAnUnchangedReadingChangesNothing(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(weatherConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	reading := services.Reading{Observed: true, Temperature: 18, Unit: services.UnitCelsius}
	if changed := reg.UpdateWeather(reading); len(changed) != 1 {
		t.Fatalf("first reading changed %v, want global 1", changed)
	}
	if changed := reg.UpdateWeather(reading); len(changed) != 0 {
		t.Fatalf("an identical reading changed %v", changed)
	}
}

func TestAConfigWithNoWeatherWidgetLeavesTheServiceStopped(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	if reg.Weather().Running() {
		t.Fatal("a configuration with no weather widget started the service")
	}
}

func batteryConfig() config.Config {
	cfg := config.Default()
	cfg.Bar.Left = []config.Item{{
		ID: "battery", Label: "percent", WarnBelow: 20, Interval: 30 * time.Second,
	}}
	cfg.Bar.Center, cfg.Bar.Right = nil, nil
	return cfg
}

func TestABatteryWidgetLeasesTheBatterySource(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(batteryConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	if !reg.Metrics().SourceLeased(services.SourceBattery) {
		t.Fatal("a battery widget did not lease the battery source")
	}
	if reg.Metrics().SourceLeased(services.SourceCPU) {
		t.Fatal("a battery-only configuration leased the CPU source")
	}
}

func TestTwoBarsShareOneBatteryLeaseSet(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(batteryConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	if got := reg.Metrics().Starts(); got != 1 {
		t.Fatalf("starts = %d, want 1 shared start for two bars", got)
	}
	reg.DropHost(1)
	if !reg.Metrics().Running() {
		t.Fatal("dropping one of two bars stopped the sampling service")
	}
	reg.DropHost(2)
	if reg.Metrics().Running() {
		t.Fatal("dropping the last bar left the sampling service running")
	}
}

// --- Palette transitions ----------------------------------------------------

// rethemeHost opens a session panel and returns it with its registry, ready to
// be moved onto another palette.
func rethemeHost(t *testing.T, reduced bool) (*Registry, *PanelHost) {
	t.Helper()
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = reduced
	reg := NewRegistry(cfg)
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(reg.Close)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelSession]
	if h == nil {
		t.Fatal("session host is missing")
	}
	return reg, h
}

func lightTheme() Theme {
	return ThemeFromTokens(lightPalette(), 12)
}

func TestPaletteTransitionStartsFromTheRenderedColours(t *testing.T) {
	t.Parallel()
	reg, h := rethemeHost(t, false)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	before := h.paintTheme()
	h.retheme(lightTheme())

	// The fade begins at the colours that were on screen, not at the incoming
	// palette, so nothing jumps on the frame the reload lands.
	if got := h.paintTheme().Surface; got != before.Surface {
		t.Errorf("first frame after reload = %+v, want the rendered %+v", got, before.Surface)
	}
	if h.anim.Settled() {
		t.Error("a palette change settled immediately without reduced motion")
	}
}

func TestPaletteTransitionRetargetsFromMidFade(t *testing.T) {
	t.Parallel()
	reg, h := rethemeHost(t, false)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	clock := &fakeClock{t: time.Unix(0, 0)}
	h.anim = newAnimator(clock.now, false)
	h.retheme(lightTheme())
	clock.add(animThemeDuration / 2)

	mid := h.paintTheme().Surface
	if mid == DefaultTheme().Surface || mid == lightTheme().Surface {
		t.Fatalf("mid-fade surface = %+v, want a blend", mid)
	}

	// A second reload mid-fade continues from what is rendering rather than
	// snapping back to the palette it was already leaving.
	h.retheme(DefaultTheme())
	if got := h.paintTheme().Surface; got != mid {
		t.Errorf("second reload started at %+v, want the rendered %+v", got, mid)
	}
	clock.add(animThemeDuration)
	if got := h.paintTheme().Surface; got != DefaultTheme().Surface {
		t.Errorf("settled surface = %+v, want the newest palette", got)
	}
}

func TestReducedMotionSnapsToTheNewPalette(t *testing.T) {
	t.Parallel()
	reg, h := rethemeHost(t, true)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	want := lightTheme()
	h.retheme(want)
	// A palette change is colour, not motion: there is nothing for reduced
	// motion to want held.
	if got := h.paintTheme().Surface; got != want.Surface {
		t.Errorf("surface = %+v, want an immediate %+v", got, want.Surface)
	}
	if got := h.anim.duration(animTheme, true); got != 0 {
		t.Errorf("palette duration = %v under reduced motion, want 0", got)
	}
}

func TestAnInvalidPaletteKeepsThePreviousCompleteTheme(t *testing.T) {
	t.Parallel()
	reg, h := rethemeHost(t, false)
	reg.mu.Lock()
	before := h.paintTheme()
	published := reg.tokens
	reg.mu.Unlock()

	// A generator that returns a partial palette must not reach a surface: the
	// shell would paint half in one theme and half in the other.
	reg.themeGen = theme.Generator{CacheDir: t.TempDir(), Matugen: "/nonexistent/matugen"}
	broken := config.Default()
	broken.ThemeGen.Source = "hex"
	broken.ThemeGen.Seed = "not-a-colour"
	got := reg.generateTheme(broken)

	if err := got.Complete(); err != nil {
		t.Fatalf("a rejected palette was published anyway: %v", err)
	}
	if got != published {
		t.Errorf("tokens changed on a failed reload:\n got %+v\nwant %+v", got, published)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if after := h.paintTheme(); after != before {
		t.Error("the open surface moved off its palette on a failed reload")
	}
}

func TestReloadMovesOpenSurfacesOntoTheNewPalette(t *testing.T) {
	t.Parallel()
	reg, h := rethemeHost(t, true)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	before := h.paintTheme().Surface
	reg.tokens = lightPalette()
	reg.retheThemeOpenSurfacesLocked()

	// An open panel used to keep the theme it was spawned with until it was
	// closed and reopened.
	if after := h.paintTheme().Surface; after == before {
		t.Errorf("open surface stayed on %+v after a reload", before)
	}
	// The panel's own corner radius is fixed, not themed.
	if h.theme.Radius != 12 {
		t.Errorf("panel radius = %d, want the fixed 12", h.theme.Radius)
	}
}
