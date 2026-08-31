package shell

import (
	"bytes"
	"os"
	"testing"

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
