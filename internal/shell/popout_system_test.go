package shell

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
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

// A reload that changes monitor.refresh re-leases an open monitor at the new
// interval rather than waiting for it to be reopened.
func TestAReloadedRefreshReachesAnOpenMonitor(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	before := slices.Clone(reg.panelHosts[PanelMonitor].leases)
	reg.mu.Unlock()

	next := config.Default()
	next.Monitor.Refresh = 3
	prepared, err := reg.PrepareConfig(next, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared.Commit()

	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelMonitor]
	if h == nil {
		t.Fatal("the reload closed the monitor")
	}
	if h.monitorInterval != 3*time.Second {
		t.Fatalf("monitor leased at %v, want 3s", h.monitorInterval)
	}
	if len(h.leases) != len(before) {
		t.Fatalf("leases = %d, want %d", len(h.leases), len(before))
	}
	for i := range before {
		if h.leases[i] == before[i] {
			t.Fatalf("lease %d was kept at the old interval", i)
		}
	}
}

// Usernames are resolved for the panel's lifetime: closing the monitor drops
// them, so a renamed account reads correctly the next time it opens.
func TestClosingTheMonitorForgetsUsernames(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	reg.usernameCache().Name(1000)
	reg.closePanelLocked(PanelMonitor)
	cache := reg.usernames
	reg.mu.Unlock()
	if cache != nil {
		t.Fatal("usernames outlived the monitor")
	}
}

// Icons arriving together rebuild the open monitor once, not once each.
func TestArrivingIconsRebuildTheMonitorOnce(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	surface := panelSurfaceID(PanelMonitor)
	count := func(wait time.Duration) int {
		n := 0
		deadline := time.After(wait)
		for {
			select {
			case inv := <-reg.Invalidations():
				if inv.SurfaceID == surface {
					n++
				}
			case <-deadline:
				return n
			}
		}
	}
	for count(100*time.Millisecond) > 0 { // let the open animation finish
	}
	img := &ui.Image{Width: 1, Height: 1, Stride: 4, Pix: make([]byte, 4)}
	for i := range 5 {
		reg.applyTrayIcon(icons.Key{Name: "icon-" + strconv.Itoa(i)}, img)
	}
	if got := count(300 * time.Millisecond); got != 1 {
		t.Fatalf("monitor published %d times for 5 icons, want 1", got)
	}
}
