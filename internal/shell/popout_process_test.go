package shell

import (
	"errors"
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

func TestProcessesAreTheDefaultMonitorPage(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelMonitor]
	for _, label := range []string{"System Processes", "System Monitor", "Search", "All", "User", "System"} {
		if !treeHasNameOrText(h.root, label) {
			t.Errorf("default process page missing %q", label)
		}
	}
	if list := findKind(h.root, ui.KindVirtualList); list == nil {
		t.Fatal("default process page has no virtual list")
	}
	if findKind(h.root, ui.KindGraph) != nil {
		t.Fatal("monitor cards rendered on the default process page")
	}
}

func TestProjectProcessesFiltersSearchesAndSortsStably(t *testing.T) {
	procs := processFixture()
	tests := []struct {
		name, query, filter, sort string
		desc                      bool
		want                      []int
	}{
		{name: "all by pid", filter: "all", sort: "pid", want: []int{10, 20, 30, 40}},
		{name: "user", filter: "user", sort: "pid", want: []int{20, 30}},
		{name: "system", filter: "system", sort: "pid", want: []int{10}},
		{name: "case insensitive args search", query: "chrome", filter: "all", sort: "pid", want: []int{20}},
		{name: "cpu descending", filter: "all", sort: "cpu", desc: true, want: []int{30, 20, 10, 40}},
		{name: "memory ascending", filter: "all", sort: "memory", want: []int{10, 20, 30, 40}},
		{name: "name ascending", filter: "all", sort: "name", want: []int{10, 20, 30, 40}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := projectProcesses(procs, tt.query, tt.filter, tt.sort, tt.desc, 1000)
			if len(got) != len(tt.want) {
				t.Fatalf("PIDs = %v, want %v", processPIDs(got), tt.want)
			}
			for i, pid := range tt.want {
				if got[i].Identity.PID != pid {
					t.Fatalf("PIDs = %v, want %v", processPIDs(got), tt.want)
				}
			}
		})
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
		procs[i] = services.Process{Identity: services.ProcessIdentity{PID: i + 2, StartTimeTicks: uint64(i + 1)}, Name: "worker"}
	}
	tree := processMonitorTree(h, services.ProcessSnapshot{Processes: procs}, 1000)
	size := panelTargetSize(PanelMonitor)
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 8, 16 }
	if err := ui.LayoutColumn(tree, size, measure); err != nil {
		t.Fatal(err)
	}
	list := findKind(tree, ui.KindVirtualList)
	if list == nil || list.ItemCount != 500 {
		t.Fatalf("virtual list = %#v", list)
	}
	if bottom := list.Bounds.Y + list.Bounds.H; bottom > size.H {
		t.Fatalf("list bottom %d exceeds panel %d", bottom, size.H)
	}
}

func TestProcessTableUsesCompactChromeAndOneKillAction(t *testing.T) {
	h := &PanelHost{
		place: Placement{Panel: panelTargetSize(PanelMonitor)}, theme: Theme{Metrics: standardMetrics()},
		monitorPage: monitorPageProcesses, processFilter: "all", processSort: "pid", search: ui.NewField(""),
	}
	tree := processMonitorTree(h, services.ProcessSnapshot{Processes: processFixture()[:1]}, 1000)
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 8, 16 }
	if err := ui.LayoutColumn(tree, panelTargetSize(PanelMonitor), measure); err != nil {
		t.Fatal(err)
	}

	page := findNodeKey(tree, "monitor-page")
	if page == nil || page.Height != 32 || page.Gap != 0 {
		t.Fatalf("page switch = %+v, want a joined 32px segmented control", page)
	}
	for _, segment := range page.Children {
		if segment.Fill != ui.FillOutline {
			t.Fatalf("page segment %q fill = %v, want outline", segment.Name, segment.Fill)
		}
	}
	field := findKind(tree, ui.KindTextField)
	if field == nil || field.Height != 32 || field.Bounds.H != 32 {
		t.Fatalf("search field = %+v, want a laid-out 32px control", field)
	}
	filters := findNodeKey(tree, "process-filter")
	if filters == nil || filters.Height != 32 {
		t.Fatalf("filter switch = %+v, want 32px", filters)
	}

	list := findKind(tree, ui.KindVirtualList)
	if list == nil || list.ItemHeight != 44 {
		t.Fatalf("process list = %+v, want 44px row pitch", list)
	}
	row := list.Item(0)
	if row == nil || row.Height != 40 {
		t.Fatalf("process row = %+v, want a 40px card with a visible gap", row)
	}
	if selectNode := findAction(row, "monitor:select:30:300"); selectNode == nil || !selectNode.Focusable {
		t.Fatalf("process data has no selectable row action: %+v", selectNode)
	}
	if end := findText(row, "End"); end != nil {
		t.Fatalf("redundant End action remains: %+v", end)
	}
	kill := findAction(row, "process:term:30:300")
	if kill == nil || kill.Text != "Kill" || kill.State.Has(ui.StateDisabled) {
		t.Fatalf("single TERM-backed Kill action = %+v", kill)
	}
	if legacy := findAction(row, "process:kill:30:300"); legacy != nil {
		t.Fatalf("SIGKILL action remains: %+v", legacy)
	}
}

func TestSelectedProcessRowKeepsAVisibleHighlight(t *testing.T) {
	process := processFixture()[0]
	h := &PanelHost{
		place: Placement{Panel: panelTargetSize(PanelMonitor)}, theme: Theme{Metrics: standardMetrics()},
		processSelected: process.Identity,
	}
	row := processRow(h, process)
	if row.Fill != ui.FillSoft || row.Stroke != 1 || row.StrokeFill != ui.FillAccent {
		t.Fatalf("selected row chrome = fill %v stroke %d/%v, want soft accent highlight", row.Fill, row.Stroke, row.StrokeFill)
	}
}

func TestProcessIdentityActionsAcceptSelectionAndTermOnly(t *testing.T) {
	want := services.ProcessIdentity{PID: 30, StartTimeTicks: 300}
	if got, ok := parseProcessIdentityAction("monitor:select:30:300", "monitor:select"); !ok || got != want {
		t.Fatalf("selection parsed as %+v/%v, want %+v", got, ok, want)
	}
	if got, signal, ok := parseProcessAction("process:term:30:300"); !ok || got != want || signal != syscall.SIGTERM {
		t.Fatalf("TERM parsed as %+v/%v/%v", got, signal, ok)
	}
	if _, _, ok := parseProcessAction("process:kill:30:300"); ok {
		t.Fatal("SIGKILL action is still accepted")
	}
}

func TestProcessRowPointerActionUsesLaidOutControl(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.sample.Processes = &services.ProcessSnapshot{Processes: processFixture()[:1]}
	signaled := make(chan services.ProcessIdentity, 1)
	reg.signalProcess = func(id services.ProcessIdentity, signal syscall.Signal) error {
		if signal != syscall.SIGTERM {
			t.Errorf("signal = %v, want SIGTERM", signal)
		}
		signaled <- id
		return nil
	}
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	h := reg.panelHosts[PanelMonitor]
	if err := h.configure(h.place.Panel.W, h.place.Panel.H, int(ui.ScaleUnit)); err != nil {
		t.Fatal(err)
	}
	button := findAction(h.root, "process:term:30:300")
	if button == nil || button.Bounds.W == 0 || button.Bounds.H == 0 {
		t.Fatalf("laid-out Kill control = %+v", button)
	}
	x := float64(button.Bounds.X + button.Bounds.W/2)
	y := float64(button.Bounds.Y + button.Bounds.H/2)
	handle := reqs[1].Open.Callbacks.Handle
	if !handle(wayland.Event{Kind: wayland.EventPointerPress, X: x, Y: y}) {
		t.Fatal("pointer press did not resolve the laid-out Kill control")
	}
	if !handle(wayland.Event{Kind: wayland.EventPointerRelease, X: x, Y: y}) {
		t.Fatal("pointer release did not activate the laid-out Kill control")
	}
	select {
	case id := <-signaled:
		if id != (services.ProcessIdentity{PID: 30, StartTimeTicks: 300}) {
			t.Fatalf("signaled process = %+v", id)
		}
	case <-time.After(time.Second):
		t.Fatal("pointer action did not signal the process")
	}
}

func TestScrolledOutProcessButtonCannotActivateAtItsOldPosition(t *testing.T) {
	reg := newPanelRegistry(t)
	processes := make([]services.Process, 30)
	for i := range processes {
		processes[i] = services.Process{
			Identity: services.ProcessIdentity{PID: 100 + i, StartTimeTicks: uint64(1000 + i)},
			Name:     "worker",
		}
	}
	reg.sample.Processes = &services.ProcessSnapshot{Processes: processes}
	signaled := make(chan services.ProcessIdentity, 1)
	reg.signalProcess = func(id services.ProcessIdentity, _ syscall.Signal) error {
		signaled <- id
		return nil
	}
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	h := reg.panelHosts[PanelMonitor]
	if err := h.configure(h.place.Panel.W, h.place.Panel.H, int(ui.ScaleUnit)); err != nil {
		t.Fatal(err)
	}
	old := findAction(h.root, "process:term:100:1000")
	if old == nil || old.Bounds.W == 0 || old.Bounds.H == 0 {
		t.Fatalf("initial Kill control = %+v", old)
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
		t.Fatal("old row position did not resolve the currently visible Kill control")
	}
	select {
	case id := <-signaled:
		if id == (services.ProcessIdentity{PID: 100, StartTimeTicks: 1000}) {
			t.Fatalf("scrolled-out process %+v activated at its stale position", id)
		}
	case <-time.After(time.Second):
		t.Fatal("visible End control was not activated")
	}
}

func TestKeyboardFocusRevealsAProcessAction(t *testing.T) {
	reg := newPanelRegistry(t)
	processes := make([]services.Process, 30)
	for i := range processes {
		processes[i] = services.Process{
			Identity: services.ProcessIdentity{PID: 100 + i, StartTimeTicks: uint64(1000 + i)},
			Name:     "worker",
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
		if candidate.Action == "process:term:129:1029" {
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
	var n *ui.Node
	for _, candidate := range h.focus {
		if candidate.Action == "process:term:30:300" {
			n = candidate
			break
		}
	}
	if n == nil {
		t.Fatal("TERM action missing")
	}
	h.setFocus(n)
	reg.mu.Lock()
	if !h.activate(reg) {
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
