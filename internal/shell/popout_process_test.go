package shell

import (
	"errors"
	"fmt"
	"syscall"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func processFixture() []services.Process {
	return []services.Process{
		{Identity: services.ProcessIdentity{PID: 30, StartTimeTicks: 300}, Name: "gamma", UID: 1000, UIDValid: true, ResidentBytes: 300, ResidentValid: true, CPU: services.ProcessCPU{Fraction: .3, Valid: true}},
		{Identity: services.ProcessIdentity{PID: 10, StartTimeTicks: 100}, Name: "Alpha", UID: 0, UIDValid: true, ResidentBytes: 100, ResidentValid: true, CPU: services.ProcessCPU{Fraction: .1, Valid: true}},
		{Identity: services.ProcessIdentity{PID: 20, StartTimeTicks: 200}, Name: "beta", UID: 1000, UIDValid: true, ResidentBytes: 200, ResidentValid: true, Args: []string{"beta", "--Chrome-Helper"}, CPU: services.ProcessCPU{Fraction: .2, Valid: true}},
		{Identity: services.ProcessIdentity{PID: 40, StartTimeTicks: 400}, Name: "unknown"},
	}
}

func TestProcessListStaysInsideItsViewport(t *testing.T) {
	h := &PanelHost{
		place:         Placement{Panel: panelTargetSize(PanelMonitor)},
		theme:         Theme{Metrics: standardMetrics()},
		monitorPage:   monitorPageProcesses,
		processFilter: "all",
		processSort:   "pid",
		search:        ui.NewField(""),
	}
	procs := make([]services.Process, 500)
	for i := range procs {
		procs[i] = services.Process{Identity: services.ProcessIdentity{PID: i + 2, StartTimeTicks: uint64(i + 1)}, Name: fmt.Sprintf("worker%d", i)}
	}
	h.processExpanded, h.processCollapsed = map[string]bool{}, map[string]bool{}
	tree := processTableTree(h, processView(procs))
	size := panelTargetSize(PanelMonitor)
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 8, 16 }
	if err := ui.LayoutColumn(tree, size, measure); err != nil {
		t.Fatal(err)
	}
	list := findKind(tree, ui.KindVirtualList)
	if list == nil || list.ItemCount != 501 { // the Processes section header, then 500 rows
		t.Fatalf("virtual list = %#v", list)
	}
	if bottom := list.Bounds.Y + list.Bounds.H; bottom > size.H {
		t.Fatalf("list bottom %d exceeds panel %d", bottom, size.H)
	}
}

func TestHiddenProcessScrollbarKeepsWheelAndKeyboardPaging(t *testing.T) {
	reg := newPanelRegistry(t)
	processes := make([]services.Process, 80)
	for i := range processes {
		processes[i] = services.Process{
			Identity: services.ProcessIdentity{PID: 100 + i, StartTimeTicks: uint64(1000 + i)},
			Name:     fmt.Sprintf("worker%02d", i),
		}
	}
	reg.sample.Processes = &services.ProcessSnapshot{Processes: processes}
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelMonitor]
	if err := h.configure(h.place.Panel.W, h.place.Panel.H, int(ui.ScaleUnit)); err != nil {
		t.Fatal(err)
	}
	list := findKind(h.root, ui.KindVirtualList)
	if list == nil || !list.HideScrollbar {
		t.Fatalf("process list = %+v, want hidden scrollbar", list)
	}
	h.hoverX, h.hoverY = list.Bounds.X+1, list.Bounds.Y+1
	if !h.scrollAxis(reg, wayland.Event{Kind: wayland.EventPointerAxis, AxisValue120: 120}) || list.ScrollOffset == 0 {
		t.Fatalf("wheel left hidden-scrollbar list at offset %d", list.ScrollOffset)
	}
	list.ScrollOffset = 0
	if !h.keyPress(reg, keyPageDown) || list.ScrollOffset == 0 {
		t.Fatalf("Page Down left hidden-scrollbar list at offset %d", list.ScrollOffset)
	}
}

func TestSelectedProcessRowKeepsAVisibleHighlight(t *testing.T) {
	process := processFixture()[0]
	h := &PanelHost{
		place: Placement{Panel: panelTargetSize(PanelMonitor)}, theme: Theme{Metrics: standardMetrics()},
		processSelected: process.Identity,
	}
	row := processLineRow(h, processView(processFixture()), processLine{Kind: lineProcess, Identity: process.Identity, Name: process.Name})
	if row.Kind != ui.KindRow || row.Fill != ui.FillSoft || row.Stroke != 0 {
		t.Fatalf("selected row = kind %v fill %v stroke %d, want one flat soft wash", row.Kind, row.Fill, row.Stroke)
	}
}

func TestProcessRowPointerActionUsesLaidOutControl(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.sample.Processes = &services.ProcessSnapshot{Processes: processFixture()[:1]}
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	h := reg.panelHosts[PanelMonitor]
	if err := h.configure(h.place.Panel.W, h.place.Panel.H, int(ui.ScaleUnit)); err != nil {
		t.Fatal(err)
	}
	button := findAction(h.root, "monitor:select:30:300")
	if button == nil || button.Bounds.W == 0 || button.Bounds.H == 0 {
		t.Fatalf("laid-out process row = %+v", button)
	}
	x := float64(button.Bounds.X + button.Bounds.W/2)
	y := float64(button.Bounds.Y + button.Bounds.H/2)
	handle := reqs[1].Open.Callbacks.Handle
	if !handle(wayland.Event{Kind: wayland.EventPointerPress, X: x, Y: y}) {
		t.Fatal("pointer press did not resolve the laid-out process row")
	}
	if !handle(wayland.Event{Kind: wayland.EventPointerRelease, X: x, Y: y}) {
		t.Fatal("pointer release did not activate the laid-out process row")
	}
	reg.mu.Lock()
	selected := h.processSelected
	reg.mu.Unlock()
	if selected != (services.ProcessIdentity{PID: 30, StartTimeTicks: 300}) {
		t.Fatalf("selected process = %+v", selected)
	}
}

func TestScrolledOutProcessButtonCannotActivateAtItsOldPosition(t *testing.T) {
	reg := newPanelRegistry(t)
	processes := make([]services.Process, 30)
	for i := range processes {
		processes[i] = services.Process{
			Identity: services.ProcessIdentity{PID: 100 + i, StartTimeTicks: uint64(1000 + i)},
			Name:     fmt.Sprintf("worker%02d", i),
		}
	}
	reg.sample.Processes = &services.ProcessSnapshot{Processes: processes}
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	h := reg.panelHosts[PanelMonitor]
	if err := h.configure(h.place.Panel.W, h.place.Panel.H, int(ui.ScaleUnit)); err != nil {
		t.Fatal(err)
	}
	h.processSort, h.processDesc = "pid", false
	reg.mu.Lock()
	reg.rebuildPanel(h)
	reg.mu.Unlock()
	if err := h.configure(h.place.Panel.W, h.place.Panel.H, int(ui.ScaleUnit)); err != nil {
		t.Fatal(err)
	}
	old := findAction(h.root, "monitor:select:100:1000")
	if old == nil || old.Bounds.W == 0 || old.Bounds.H == 0 {
		t.Fatalf("initial process row = %+v", old)
	}
	x := float64(old.Bounds.X + old.Bounds.W/2)
	y := float64(old.Bounds.Y + old.Bounds.H/2)
	list := findKind(h.root, ui.KindVirtualList)
	ui.ScrollBy(list, 8*processRowPitch)
	if err := h.configure(h.place.Panel.W, h.place.Panel.H, int(ui.ScaleUnit)); err != nil {
		t.Fatal(err)
	}
	handle := reqs[1].Open.Callbacks.Handle
	if !handle(wayland.Event{Kind: wayland.EventPointerPress, X: x, Y: y}) ||
		!handle(wayland.Event{Kind: wayland.EventPointerRelease, X: x, Y: y}) {
		t.Fatal("old row position did not resolve the currently visible process row")
	}
	reg.mu.Lock()
	selected := h.processSelected
	reg.mu.Unlock()
	if selected == (services.ProcessIdentity{PID: 100, StartTimeTicks: 1000}) || selected == (services.ProcessIdentity{}) {
		t.Fatalf("selection after scrolling = %+v, want the row now under the pointer", selected)
	}
}

func TestKeyboardFocusRevealsAProcessAction(t *testing.T) {
	reg := newPanelRegistry(t)
	processes := make([]services.Process, 30)
	for i := range processes {
		processes[i] = services.Process{
			Identity: services.ProcessIdentity{PID: 100 + i, StartTimeTicks: uint64(1000 + i)},
			Name:     fmt.Sprintf("worker%02d", i),
		}
	}
	reg.sample.Processes = &services.ProcessSnapshot{Processes: processes}
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelMonitor]
	if err := h.configure(h.place.Panel.W, h.place.Panel.H, int(ui.ScaleUnit)); err != nil {
		t.Fatal(err)
	}
	var target *ui.Node
	for _, candidate := range h.focus {
		if candidate.Action == "monitor:select:129:1029" {
			target = candidate
			break
		}
	}
	if target == nil {
		t.Fatal("last process action is missing from keyboard focus")
	}
	h.setFocus(target)
	reg.mu.Lock()
	h.afterFocusChange(reg)
	reg.mu.Unlock()
	focused := h.focused()
	list := findKind(h.root, ui.KindVirtualList)
	if focused == nil || list == nil || focused.Bounds.W == 0 || !list.Bounds.Contains(focused.Bounds.X+focused.Bounds.W/2, focused.Bounds.Y+focused.Bounds.H/2) {
		t.Fatalf("focused process action %+v is outside list %+v", focused, list)
	}
}

func TestProcessSignalRunsUnlockedAndReportsIdentityFailure(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.sample.Processes = &services.ProcessSnapshot{Processes: processFixture()[:1]}
	called := make(chan struct{}, 1)
	reg.signalProcess = func(id services.ProcessIdentity, sig syscall.Signal) error {
		if !reg.mu.TryLock() {
			t.Error("process signal ran while Registry.mu was held")
		} else {
			reg.mu.Unlock()
		}
		if id.PID != 30 || sig != syscall.SIGTERM {
			t.Errorf("signal request = %#v, %v", id, sig)
		}
		called <- struct{}{}
		return services.ErrProcessIdentityChanged
	}
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelMonitor]
	n := &ui.Node{Action: "process:term:30:300"}
	reg.mu.Lock()
	if !h.activateMonitor(reg, n) {
		reg.mu.Unlock()
		t.Fatal("TERM action was not handled")
	}
	reg.mu.Unlock()
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("process signal did not run")
	}
	deadline := time.Now().Add(time.Second)
	for {
		reg.mu.Lock()
		status := h.processStatus
		reg.mu.Unlock()
		if status != "" {
			if !errors.Is(h.processStatusErr, services.ErrProcessIdentityChanged) {
				t.Fatalf("status error = %v", h.processStatusErr)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("identity failure was not shown inline")
		}
		time.Sleep(time.Millisecond)
	}
}

func processPIDs(processes []services.Process) []int {
	out := make([]int, len(processes))
	for i := range processes {
		out[i] = processes[i].Identity.PID
	}
	return out
}

func treeHasNameOrText(n *ui.Node, value string) bool {
	if n == nil {
		return false
	}
	if n.Name == value || n.Text == value {
		return true
	}
	for _, child := range n.Children {
		if treeHasNameOrText(child, value) {
			return true
		}
	}
	return false
}

func findNodeKey(n *ui.Node, key string) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Key == key {
		return n
	}
	for _, child := range n.Children {
		if got := findNodeKey(child, key); got != nil {
			return got
		}
	}
	return nil
}

func findText(n *ui.Node, value string) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Text == value {
		return n
	}
	for _, child := range n.Children {
		if got := findText(child, value); got != nil {
			return got
		}
	}
	return nil
}

func processView(procs []services.Process) monitorView {
	v := testMonitorView()
	v.Snap.Processes = &services.ProcessSnapshot{Processes: procs}
	v.UID = 1000
	return v
}

func processHost() *PanelHost {
	return &PanelHost{
		place: Placement{Panel: panelTargetSize(PanelMonitor)}, theme: Theme{Metrics: standardMetrics()},
		monitorPage: monitorPageProcesses, processFilter: "all", processSort: "mem", processDesc: true,
		search: ui.NewField(""), processExpanded: map[string]bool{}, processCollapsed: map[string]bool{},
	}
}

func TestProcessPageMatchesTheReferenceLayout(t *testing.T) {
	root := monitorPanelTree(processHost(), processView(processFixture()))
	for _, label := range []string{"Processes", "System", "Search"} {
		if !treeHasNameOrText(root, label) {
			t.Errorf("processes page missing %q", label)
		}
	}
	for _, col := range []string{"Name", "CPU", "MEM", "SWAP", "DISK", "PID", "USER"} {
		if findByName(root, "Sort by "+col) == nil {
			t.Errorf("table header missing %q", col)
		}
	}
	if findKind(root, ui.KindVirtualList) == nil {
		t.Fatal("no virtual list")
	}
	if treeHasNameOrText(root, "Kill") {
		t.Fatal("per-row Kill pill is still drawn")
	}
	if panelTargetSize(PanelMonitor) != (ui.Rect{W: 800, H: 650}) {
		t.Fatalf("panel size = %+v", panelTargetSize(PanelMonitor))
	}
}

func TestSortedColumnUsesTheConfiguredRoles(t *testing.T) {
	h := processHost()
	v := processView(processFixture())
	v.Config.SortBackground, v.Config.SortColor = "primary_container", "on_primary_container"
	root := monitorPanelTree(h, v)
	head := findByName(root, "Sort by MEM")
	if head == nil || head.Fill != ui.FillRole || head.FillRole != ui.PaintPrimaryContainer || head.InkRole != ui.PaintOnPrimaryContainer {
		t.Fatalf("MEM header = %+v", head)
	}
	if !treeHasNameOrText(head, "▼ MEM") {
		t.Fatal("descending MEM header lacks ▼")
	}
	list := findKind(root, ui.KindVirtualList)
	row := list.Item(1) // 0 is the Processes section header
	cell := findByName(row, "MEM value")
	if cell == nil || cell.Fill != ui.FillRole || cell.FillRole != ui.PaintPrimaryContainer {
		t.Fatalf("MEM cell = %+v", cell)
	}
}

func TestRowsHoverWithTheConfiguredRoles(t *testing.T) {
	v := processView(processFixture())
	v.Config.HoverBackground, v.Config.HoverColor = "secondary_container", "on_secondary_container"
	list := findKind(monitorPanelTree(processHost(), v), ui.KindVirtualList)
	row := list.Item(1)
	if row.HoverFill != ui.PaintSecondaryContainer || row.HoverInk != ui.PaintOnSecondaryContainer || row.Shape == ui.ShapeInherit {
		t.Fatalf("row hover = fill %v ink %v shape %v", row.HoverFill, row.HoverInk, row.Shape)
	}
}

func TestGroupRowHasNoPIDAndTogglesByKey(t *testing.T) {
	procs := append(processFixture(),
		services.Process{Identity: services.ProcessIdentity{PID: 31, StartTimeTicks: 310}, Name: "gamma", UID: 1000, UIDValid: true, ResidentBytes: 1, ResidentValid: true})
	list := findKind(monitorPanelTree(processHost(), processView(procs)), ui.KindVirtualList)
	var group *ui.Node
	for i := 0; i < list.ItemCount && group == nil; i++ {
		group = findAction(list.Item(i), "monitor:toggle:exe:name:gamma")
	}
	if group == nil {
		t.Fatal("gamma group has no toggle action")
	}
	if treeHasNameOrText(group, "30") || treeHasNameOrText(group, "31") {
		t.Fatal("group row shows a PID")
	}
}

func TestFormatProcessCell(t *testing.T) {
	cases := []struct {
		kind string
		t    processTotals
		want string
	}{
		{"cpu", processTotals{CPU: .002, CPUValid: true}, "0.2%"},
		{"cpu", processTotals{CPU: .01, CPUValid: true}, "1%"},
		{"cpu", processTotals{CPU: 0, CPUValid: true}, "—"},
		{"cpu", processTotals{}, "—"},
		{"mem", processTotals{Resident: 2576980377, ResidentValid: true}, "2.4 G"},
		{"mem", processTotals{Resident: 682699161, ResidentValid: true}, "651.1 M"},
		{"swap", processTotals{}, "—"},
		{"io", processTotals{IO: 1536, IOValid: true}, "1.5 K/s"},
	}
	for _, c := range cases {
		if got := formatProcessCell(c.kind, c.t); got != c.want {
			t.Errorf("%s %+v = %q, want %q", c.kind, c.t, got, c.want)
		}
	}
}
