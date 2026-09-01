package shell

import (
	"bytes"
	"os"
	"testing"

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
	tree := monitorTree(sels, fixtureSnapshot(), map[services.Selector][]float64{}, 0)

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

// A value without a unit is a number a reader has to guess at. Every card
// states what its number measures.
func TestMonitorCardsCarryUnits(t *testing.T) {
	t.Parallel()
	snap := fixtureSnapshot()
	tree := monitorTree([]services.Selector{
		{Source: services.SourceCPU},
		{Source: services.SourceNetwork, Subject: "eth9", Direction: "rx"},
	}, snap, map[services.Selector][]float64{}, 0)

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
		map[services.Selector][]float64{}, 0)

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
		map[services.Selector][]float64{}, 0)
	if treeHasText(tree, "Load") {
		t.Fatal("an invalid load average was rendered anyway")
	}
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
	}, fixtureSnapshot(), history, 0)

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
