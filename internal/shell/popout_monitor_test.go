package shell

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestMonitorSelectorsUseLandedMetricVocabulary(t *testing.T) {
	t.Parallel()
	bar := config.Default().Bar
	bar.Right = append(bar.Right,
		config.Item{ID: "filesystem", Path: "/"},
		config.Item{ID: "network", Interface: "eth0", Direction: "rx"},
		config.Item{ID: "filesystem", Path: "/home"},
	)
	got := monitorSelectors(bar)
	want := []services.Selector{
		{Source: services.SourceCPU},
		{Source: services.SourceMemory},
		{Source: services.SourceGPU},
		{Source: services.SourceFilesystem, Subject: "/"},
		{Source: services.SourceNetwork, Subject: "eth0", Direction: "rx"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selector %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestMonitorUsesRegistrySnapshotAndHistory(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	snap := fixtureSnapshot()
	reg.UpdateMetrics(snap)
	h := reg.panelHosts[PanelMonitor]
	sel := services.Selector{Source: services.SourceCPU}
	label, _ := formatMonitorMetric(sel, snap)
	if !treeHasText(h.root, label) {
		t.Fatalf("active tab missing %q in tree", label)
	}
	graph := findKind(h.root, ui.KindGraph)
	if graph == nil {
		t.Fatal("missing graph")
	}
	want := normalise(reg.Metrics().History(sel))
	if !floatSliceEqual(graph.Values, want) {
		t.Fatalf("graph values = %v, want %v", graph.Values, want)
	}
}

func TestMonitorAbsentSampleShowsCollecting(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.UpdateMetrics(services.Snapshot{})
	h := reg.panelHosts[PanelMonitor]
	if !treeHasText(h.root, "collecting") {
		t.Fatal("absent sample did not render collecting")
	}
	graph := findKind(h.root, ui.KindGraph)
	if graph == nil || !graph.Absent {
		t.Fatal("absent sample did not mark the graph absent")
	}
}

func TestMonitorConfigureAcceptsTabsAndGraph(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelMonitor]
	if err := h.configure(640, 480, 120); err != nil {
		t.Fatal(err)
	}
}

func TestMonitorLeaseReusesM3Service(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("popout_monitor.go")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(src, []byte("sysc-metrics")) {
		t.Fatal("popout_monitor.go must not import sysc-metrics")
	}
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if !reg.Metrics().Running() {
		t.Fatal("opening the monitor did not lease Registry.Metrics")
	}
	starts := reg.Metrics().Starts()
	reg.ClosePanel(PanelMonitor)
	if reg.Metrics().Running() {
		t.Fatal("closing left a second sampler running")
	}
	if starts != 1 {
		t.Fatalf("starts = %d, want 1 shared service", starts)
	}
}

func treeHasText(n *ui.Node, text string) bool {
	if n == nil {
		return false
	}
	if n.Text == text {
		return true
	}
	for _, c := range n.Children {
		if treeHasText(c, text) {
			return true
		}
	}
	return false
}

func findKind(n *ui.Node, k ui.Kind) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Kind == k {
		return n
	}
	for _, c := range n.Children {
		if got := findKind(c, k); got != nil {
			return got
		}
	}
	return nil
}

func floatSliceEqual(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The panel shows every metric at once in a titled card, not one at a time
// behind a tab. A card carries its own heading, so a reader never has to infer
// which metric a bare number belongs to.
func TestMonitorBuildsOneTitledCardPerMetric(t *testing.T) {
	t.Parallel()
	sels := []services.Selector{
		{Source: services.SourceCPU},
		{Source: services.SourceMemory},
		{Source: services.SourceNetwork, Subject: "eth9", Direction: "rx"},
	}
	tree := monitorTree(sels, fixtureSnapshot(), map[services.Selector][]float64{}, machineFacts{})

	cards := findAllKind(tree, ui.KindCapsule)
	if len(cards) < len(sels) {
		t.Fatalf("cards = %d, want one per metric plus the resources card", len(cards))
	}
	for _, sel := range sels {
		// The accessible name, not the painted text: the title carries an icon
		// glyph from a private-use range that means nothing read aloud.
		if !treeHasName(tree, selectorLabel(sel)) {
			t.Fatalf("no card titled %q", selectorLabel(sel))
		}
	}
	if graphs := findAllKind(tree, ui.KindGraph); len(graphs) != len(sels) {
		t.Fatalf("graphs = %d, want one per metric visible at once", len(graphs))
	}
	if tabs := findAllKind(tree, ui.KindTab); len(tabs) != 0 {
		t.Fatalf("tabs = %d, want none: every metric is visible", len(tabs))
	}
}

func TestMonitorSystemCardProjectsStaticFacts(t *testing.T) {
	t.Parallel()
	facts := machineFacts{
		CPU:    "Intel Core i7-8665U @ 1.90GHz",
		GPU:    "Intel UHD Graphics 620",
		OS:     "Arch Linux",
		Kernel: "Linux 7.1.9-arch1-2",
		WM:     "niri",
		Uptime: "4 hours 59 minutes",
	}
	tree := monitorSystemCard(facts)
	for _, want := range []string{
		"System",
		"CPU", "Intel Core i7-8665U @ 1.90GHz",
		"GPU", "Intel UHD Graphics 620",
		"OS", "Arch Linux",
		"Kernel", "Linux 7.1.9-arch1-2",
		"WM", "niri",
		"Uptime", "4 hours 59 minutes",
	} {
		if !treeHasText(tree, want) {
			t.Fatalf("system card missing %q", want)
		}
	}
}

func TestMonitorOmitsEmptySystemCard(t *testing.T) {
	t.Parallel()
	if n := monitorSystemCard(machineFacts{}); n != nil {
		t.Fatal("empty facts still built a card")
	}
}

func TestMonitorSystemCardOmitsEmptyGPU(t *testing.T) {
	t.Parallel()
	tree := monitorSystemCard(machineFacts{CPU: "x"})
	if treeHasText(tree, "GPU") {
		t.Fatal("an empty GPU row was rendered")
	}
}

func TestMonitorTreeKeepsASystemCard(t *testing.T) {
	t.Parallel()
	tree := monitorTree(nil, services.Snapshot{}, nil, machineFacts{CPU: "box"})
	if !treeHasText(tree, "System") || !treeHasText(tree, "box") {
		t.Fatal("monitor tree dropped the system card")
	}
}

// The reference is CPU|Memory, then GPU|Network, then System|Resources.
// Identity cards sit on the last row, not above the graphs.
func TestMonitorPutsSystemBesideResources(t *testing.T) {
	t.Parallel()
	tree := monitorTree([]services.Selector{
		{Source: services.SourceCPU},
		{Source: services.SourceMemory},
	}, fixtureSnapshot(), map[services.Selector][]float64{}, machineFacts{CPU: "box"})
	var last *ui.Node
	for _, child := range tree.Children {
		if child.Kind == ui.KindRow {
			last = child
		}
	}
	if last == nil || len(last.Children) != 2 {
		t.Fatal("System and Resources are not a two-up row")
	}
	if !treeHasName(last.Children[0], "System") {
		t.Fatal("left of the last row is not System")
	}
	if !treeHasName(last.Children[1], "Resources") {
		t.Fatal("right of the last row is not Resources")
	}
}

func TestParseCPUModel(t *testing.T) {
	t.Parallel()
	got := parseCPUModel("processor\t: 0\nvendor_id\t: AuthenticAMD\nmodel name\t: AMD Ryzen 9 9950X 16-Core Processor\n")
	if got != "AMD Ryzen 9 9950X 16-Core Processor" {
		t.Fatalf("got %q", got)
	}
}

func TestParseOSReleasePrettyName(t *testing.T) {
	t.Parallel()
	got := parseOSRelease(`NAME="Arch Linux"
PRETTY_NAME="Arch Linux"
ID=arch
`)
	if got != "Arch Linux" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUptime(t *testing.T) {
	t.Parallel()
	cases := []struct {
		d    time.Duration
		want string
	}{
		{3 * time.Minute, "3 minutes"},
		{2*time.Hour + 15*time.Minute, "2 hours 15 minutes"},
		{50*time.Hour + 10*time.Minute, "2 days 2 hours"},
		{4*time.Hour + 59*time.Minute, "4 hours 59 minutes"},
	}
	for _, tc := range cases {
		if got := formatUptime(tc.d); got != tc.want {
			t.Fatalf("formatUptime(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// A value without a unit is a number a reader has to guess at. Every card
// states what its number measures.
func TestMonitorCardsCarryUnits(t *testing.T) {
	t.Parallel()
	snap := fixtureSnapshot()
	tree := monitorTree([]services.Selector{
		{Source: services.SourceCPU},
		{Source: services.SourceNetwork, Subject: "eth9", Direction: "rx"},
	}, snap, map[services.Selector][]float64{}, machineFacts{})

	if !treeHasText(tree, "42%") {
		t.Fatal("the cpu card does not state its percentage")
	}
	if !treeHasText(tree, formatRate(1_500_000)) {
		t.Fatal("the network card does not state its rate")
	}
}

// The resources card projects what the metrics release already supplies and
// no widget shows: load average, swap, and memory as bytes rather than a bare
// percentage.
func TestMonitorResourcesCardProjectsLoadAndSwap(t *testing.T) {
	t.Parallel()
	snap := fixtureSnapshot()
	snap.CPU.Load1, snap.CPU.Load5, snap.CPU.Load15, snap.CPU.LoadValid = 0.56, 0.59, 0.57, true
	snap.Memory.Swap = metrics.Capacity{TotalBytes: 4 << 30, UsedBytes: 1 << 30}

	tree := monitorTree([]services.Selector{{Source: services.SourceCPU}}, snap,
		map[services.Selector][]float64{}, machineFacts{})

	for _, want := range []string{"Resources", "Load", "0.56 / 0.59 / 0.57", "Swap"} {
		if !treeHasText(tree, want) {
			t.Fatalf("resources card is missing %q", want)
		}
	}
}

// A load average the kernel did not report is omitted rather than rendered as
// a row of zeroes, which reads as an idle machine.
func TestMonitorResourcesOmitsAnInvalidLoad(t *testing.T) {
	t.Parallel()
	snap := fixtureSnapshot()
	snap.CPU.LoadValid = false
	tree := monitorTree([]services.Selector{{Source: services.SourceCPU}}, snap,
		map[services.Selector][]float64{}, machineFacts{})
	if treeHasText(tree, "Load") {
		t.Fatal("an invalid load average was rendered anyway")
	}
}

func TestMonitorCPUCardShowsPackageTemp(t *testing.T) {
	t.Parallel()
	snap := fixtureSnapshot()
	snap.Thermal = &metrics.ThermalSnapshot{Celsius: 50, Valid: true}
	tree := monitorTree([]services.Selector{{Source: services.SourceCPU}}, snap,
		map[services.Selector][]float64{}, machineFacts{})
	if !treeHasText(tree, "50°C") {
		t.Fatal("CPU card missing package temperature")
	}
	if !legendRowHolds(tree, "50°C") {
		t.Fatal("package temperature is not in the CPU legend")
	}
}

func TestMonitorGPUCardProjectsUsageAndTemp(t *testing.T) {
	t.Parallel()
	snap := fixtureSnapshot()
	snap.GPU = &metrics.GPUSnapshot{GPUs: []metrics.GPU{{
		PCIID: "1002:67df", Name: "AMD Radeon",
		Usage:   metrics.GPUUsage{Fraction: 0.14, Valid: true},
		Celsius: 45, TempValid: true,
	}}}
	tree := monitorTree([]services.Selector{{Source: services.SourceGPU}}, snap,
		map[services.Selector][]float64{}, machineFacts{})
	for _, want := range []string{"GPU", "14%", "45°C"} {
		if !treeHasText(tree, want) {
			t.Fatalf("GPU card missing %q", want)
		}
	}
}

func TestMonitorGPUWithoutUsageShowsAnEmDash(t *testing.T) {
	t.Parallel()
	snap := fixtureSnapshot()
	snap.GPU = &metrics.GPUSnapshot{GPUs: []metrics.GPU{{
		Name: "Intel UHD Graphics 620",
	}}}
	tree := monitorTree([]services.Selector{{Source: services.SourceGPU}}, snap,
		map[services.Selector][]float64{}, machineFacts{})
	if !treeHasText(tree, "--") {
		t.Fatal("an iGPU with no usage still said collecting")
	}
	if treeHasText(tree, "collecting") {
		t.Fatal("known GPU still rendered collecting")
	}
}

func TestMonitorSystemCardUsesGPUNameFromSnapshot(t *testing.T) {
	t.Parallel()
	snap := services.Snapshot{GPU: &metrics.GPUSnapshot{GPUs: []metrics.GPU{
		{Name: "Intel UHD Graphics 620"},
	}}}
	tree := monitorTree(nil, snap, nil, machineFacts{CPU: "x"})
	if !treeHasText(tree, "Intel UHD Graphics 620") {
		t.Fatal("system card did not take the GPU name from the snapshot")
	}
}

func legendRowHolds(n *ui.Node, text string) bool {
	for _, row := range findAllKind(n, ui.KindRow) {
		if len(row.Children) == 0 {
			continue
		}
		// A legend sits under a graph: every child is a figure, none a card.
		if row.Children[0].Kind == ui.KindCapsule {
			continue
		}
		if treeHasText(row, text) {
			return true
		}
	}
	return false
}

func treeHasName(n *ui.Node, name string) bool {
	if n == nil {
		return false
	}
	if n.Name == name {
		return true
	}
	for _, c := range n.Children {
		if treeHasName(c, name) {
			return true
		}
	}
	return false
}

func findAllKind(n *ui.Node, kind ui.Kind) []*ui.Node {
	if n == nil {
		return nil
	}
	var out []*ui.Node
	if n.Kind == kind {
		out = append(out, n)
	}
	for _, c := range n.Children {
		out = append(out, findAllKind(c, kind)...)
	}
	for i := 0; n.Item != nil && i < n.ItemCount; i++ {
		out = append(out, findAllKind(n.Item(i), kind)...)
	}
	return out
}

// A fraction is already zero through one, so it graphs against that scale.
// Normalising it against its own maximum makes a flat series fill the card:
// steady 31% memory painted as a solid block, which reads as a machine out of
// memory. A rate has no ceiling and still normalises.
func TestMonitorGraphsFractionsAgainstFullScale(t *testing.T) {
	t.Parallel()
	steady := []float64{0.31, 0.31, 0.31, 0.31}
	history := map[services.Selector][]float64{
		{Source: services.SourceMemory}:                                    steady,
		{Source: services.SourceNetwork, Subject: "eth9", Direction: "rx"}: {1000, 2000},
	}
	tree := monitorTree([]services.Selector{
		{Source: services.SourceMemory},
		{Source: services.SourceNetwork, Subject: "eth9", Direction: "rx"},
	}, fixtureSnapshot(), history, machineFacts{})

	graphs := findAllKind(tree, ui.KindGraph)
	if len(graphs) != 2 {
		t.Fatalf("graphs = %d, want two", len(graphs))
	}
	for i, v := range graphs[0].Values {
		if v != steady[i] {
			t.Fatalf("memory graph value %d = %v, want the fraction %v unscaled", i, v, steady[i])
		}
	}
	// The rate keeps its own scaling: without a ceiling there is nothing else
	// to draw it against.
	if got := graphs[1].Values; len(got) != 2 || got[1] != 1 {
		t.Fatalf("rate graph = %v, want its own maximum at full height", got)
	}
}

// The reference lays cards two to a row. Proving it needs a real layout at the
// panel's own size: the tree alone cannot show that two cards share a row, sit
// at the same height, and stay inside the panel.
func TestMonitorCardsLayOutTwoToARow(t *testing.T) {
	t.Parallel()
	sels := []services.Selector{
		{Source: services.SourceCPU},
		{Source: services.SourceMemory},
		{Source: services.SourceNetwork, Subject: "eth9", Direction: "rx"},
	}
	tree := monitorTree(sels, fixtureSnapshot(), map[services.Selector][]float64{}, machineFacts{})
	size := panelTargetSize(PanelMonitor)
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 8, 16 }
	if err := ui.LayoutColumn(tree, ui.Rect{W: size.W, H: size.H}, measure); err != nil {
		t.Fatalf("LayoutColumn: %v", err)
	}

	rows := 0
	for _, child := range tree.Children {
		if child.Kind != ui.KindRow {
			continue
		}
		rows++
		if len(child.Children) != 2 {
			t.Fatalf("row holds %d cards, want 2", len(child.Children))
		}
		a, b := child.Children[0], child.Children[1]
		if a.Bounds.W != b.Bounds.W {
			t.Fatalf("cards in a row are %d and %d wide, want equal", a.Bounds.W, b.Bounds.W)
		}
		if a.Bounds.H != b.Bounds.H {
			t.Fatalf("cards in a row are %d and %d tall, want equal", a.Bounds.H, b.Bounds.H)
		}
		if a.Bounds.X == b.Bounds.X {
			t.Fatalf("both cards start at x=%d, want side by side", a.Bounds.X)
		}
		if right := b.Bounds.X + b.Bounds.W; right > size.W {
			t.Fatalf("row overflows the panel: right edge %d > %d", right, size.W)
		}
	}
	if rows == 0 {
		t.Fatal("no card laid out two to a row")
	}
}

func TestMonitorSurfaceHeightCoversATallTree(t *testing.T) {
	t.Parallel()
	tree := monitorTree([]services.Selector{
		{Source: services.SourceCPU},
		{Source: services.SourceMemory},
		{Source: services.SourceGPU},
	}, fixtureSnapshot(), map[services.Selector][]float64{}, machineFacts{
		CPU: "Intel(R) Core(TM) i7-8665U CPU @ 1.90GHz",
		GPU: "WhiskeyLake-U GT2 [UHD Graphics 620]",
		OS:  "Arch Linux", Kernel: "Linux 7.2.2-arch1-1",
		WM: "niri", Uptime: "1 hour 1 minute",
	})
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 8, 20 }
	content, err := ui.ContentHeight(tree, 640, measure)
	if err != nil {
		t.Fatal(err)
	}
	if content <= 480 {
		t.Fatalf("content height %d still fits the old 480 guess, fixture is too short", content)
	}
	got := monitorSurfaceHeight(tree, 640, 12, measure)
	if got < content+24 {
		t.Fatalf("surface height %d, want at least content %d plus two radii", got, content)
	}
}

// Every card must carry a real height. The measure path used to hand a column
// a sentinel band, so a card in a row reported 1048576 tall.
func TestMonitorCardsHaveSaneHeights(t *testing.T) {
	t.Parallel()
	tree := monitorTree([]services.Selector{
		{Source: services.SourceCPU},
		{Source: services.SourceMemory},
	}, fixtureSnapshot(), map[services.Selector][]float64{}, machineFacts{})
	size := panelTargetSize(PanelMonitor)
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 8, 16 }
	if err := ui.LayoutColumn(tree, ui.Rect{W: size.W, H: size.H}, measure); err != nil {
		t.Fatalf("LayoutColumn: %v", err)
	}
	for _, card := range findAllKind(tree, ui.KindCapsule) {
		if card.Bounds.H <= 0 || card.Bounds.H > size.H {
			t.Fatalf("card height %d is outside the panel's %d", card.Bounds.H, size.H)
		}
	}
}

// The System card sits in a two-up cell. A long CPU string used to make the
// nested key/value row refuse layout and close the panel.
func TestMonitorSystemCardWithLongCPUFitsPanel(t *testing.T) {
	t.Parallel()
	tree := monitorTree([]services.Selector{
		{Source: services.SourceCPU},
	}, fixtureSnapshot(), map[services.Selector][]float64{}, machineFacts{
		CPU: "AMD Ryzen 7 8845HS w/ Radeon 780M Graphics",
	})
	size := panelTargetSize(PanelMonitor)
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 8, 16 }
	if err := ui.LayoutColumn(tree, ui.Rect{W: size.W, H: size.H}, measure); err != nil {
		t.Fatalf("LayoutColumn: %v", err)
	}
}
