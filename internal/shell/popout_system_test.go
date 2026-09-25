package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func systemHost() *PanelHost {
	h := processHost()
	h.monitorPage = monitorPageMetrics
	return h
}

func TestSystemPageHasThreeSectionsOfRows(t *testing.T) {
	root := monitorPanelTree(systemHost(), testMonitorView())
	for _, text := range []string{"Compute", "Memory", "Storage & network", "CPU", "GPU", "RAM", "Swap", "/", "Disk I/O", "Network"} {
		if !treeHasNameOrText(root, text) {
			t.Errorf("system page missing %q", text)
		}
	}
	if findByName(root, "Search") != nil {
		t.Fatal("system page shows the process search")
	}
	if findKind(root, ui.KindVirtualList) != nil {
		t.Fatal("system page shows the process table")
	}
}

func TestSystemPageDashesAnAbsentSourceAndPaintsNoMark(t *testing.T) {
	root := monitorPanelTree(systemHost(), testMonitorView()) // no GPU, memory or network in the view
	gpu := findByName(root, "GPU usage")
	if gpu == nil || gpu.Text != ccDash {
		t.Fatalf("GPU value = %+v", gpu)
	}
	walkNodes(root, func(n *ui.Node) {
		if n.Kind == ui.KindGraph && !n.Absent && len(n.Values) == 0 {
			t.Errorf("graph with no values is not absent: %+v", n)
		}
	})
}

func TestSystemPageSectionsCollapse(t *testing.T) {
	h := systemHost()
	h.processCollapsed["section:compute"] = true
	root := monitorPanelTree(h, testMonitorView())
	if findByName(root, "CPU usage") != nil {
		t.Fatal("collapsed Compute still shows CPU")
	}
	if findAction(root, "monitor:toggle:section:compute") == nil {
		t.Fatal("Compute header has no toggle")
	}
}

func TestMonitorLeasesMatchTheControlCentreSet(t *testing.T) {
	want := map[services.Selector]bool{
		{Source: services.SourceCPU}: true, {Source: services.SourceMemory}: true,
		{Source: services.SourceCPU, Subject: "temperature"}: true, {Source: services.SourceGPU}: true,
		{Source: services.SourceFilesystem, Subject: "/"}: true,
		{Source: services.SourceNetwork}:                  true, {Source: services.SourceBlock}: true,
	}
	for _, sel := range monitorLeaseSelectors() {
		delete(want, sel)
	}
	if len(want) != 0 {
		t.Fatalf("missing leases: %v", want)
	}
}

func TestMonitorLeaseIntervalFollowsRefresh(t *testing.T) {
	m := config.Default().Monitor
	if got := monitorLeaseInterval(m); got != time.Second {
		t.Fatalf("default interval = %v", got)
	}
	m.Refresh = 3
	if got := monitorLeaseInterval(m); got != 3*time.Second {
		t.Fatalf("refresh 3 interval = %v", got)
	}
	m.Refresh = 0
	if got := monitorLeaseInterval(m); got != time.Second {
		t.Fatalf("zero refresh interval = %v, want the one-second floor", got)
	}
}
