package shell

import (
	"net/url"
	"syscall"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestProcessActionsParseIntAndKillOnly(t *testing.T) {
	cases := []struct {
		action string
		sig    syscall.Signal
		ok     bool
	}{
		{"process:int:20:200", syscall.SIGINT, true},
		{"process:kill:20:200", syscall.SIGKILL, true},
		{"process:term:20:200", 0, false},
		{"process:kill:0:200", 0, false},
		{"process:kill:20", 0, false},
	}
	for _, c := range cases {
		id, sig, ok := parseProcessAction(c.action)
		if ok != c.ok || ok && (sig != c.sig || id != (services.ProcessIdentity{PID: 20, StartTimeTicks: 200})) {
			t.Errorf("%s = %v %v %v", c.action, id, sig, ok)
		}
	}
}

func TestDetailViewShowsTheSelectedProcessAndItsActions(t *testing.T) {
	h := processHost()
	h.processSelected = services.ProcessIdentity{PID: 20, StartTimeTicks: 200}
	v := processView(processFixture())
	card := processDetailCard(h, v, *v.Snap.Processes)
	if card == nil {
		t.Fatal("no detail card")
	}
	for _, text := range []string{"comm:", "beta", "PID:", "20", "cmdline:", "beta --Chrome-Helper"} {
		if !treeHasNameOrText(card, text) {
			t.Errorf("detail missing %q", text)
		}
	}
	kill := findAction(card, "process:int:20:200")
	force := findAction(card, "process:kill:20:200")
	if kill == nil || force == nil || findAction(card, "monitor:detail:close") == nil {
		t.Fatal("detail lacks its three actions")
	}
	if kill.AriaDisabled || force.AriaDisabled {
		t.Fatal("own process actions are disabled")
	}
}

func TestDetailViewShowsProcessIORates(t *testing.T) {
	h := processHost()
	id := services.ProcessIdentity{PID: 20, StartTimeTicks: 200}
	h.processSelected = id
	v := processView(processFixture())
	for i := range v.Snap.Processes.Processes {
		if v.Snap.Processes.Processes[i].Identity == id {
			p := &v.Snap.Processes.Processes[i]
			p.Executable, p.ExecutableValid = "/usr/lib/firefox/firefox", true
			p.SwapBytes, p.SwapValid = 11, true
			p.IO.ReadBytesPerSecond, p.IO.WriteBytesPerSecond, p.IO.Valid = 5, 7, true
		}
	}
	card := processDetailCard(h, v, *v.Snap.Processes)
	for _, value := range []string{"/usr/lib/firefox/firefox", "11B", "5B/s", "7B/s"} {
		if !treeHasNameOrText(card, value) {
			t.Errorf("detail missing process value %q", value)
		}
	}
}

func TestDetailViewShowsMemoryOnlyForSelectedProcess(t *testing.T) {
	h := processHost()
	id := services.ProcessIdentity{PID: 20, StartTimeTicks: 200}
	h.processSelected = id
	h.processMemory = services.ProcessMemory{PrivateBytes: 17, SharedBytes: 19}
	h.processMemoryOf = id
	v := processView(processFixture())
	card := processDetailCard(h, v, *v.Snap.Processes)
	for _, value := range []string{"private:", "shared:", "17B", "19B"} {
		if !treeHasNameOrText(card, value) {
			t.Errorf("detail missing selected memory value %q", value)
		}
	}

	h.processMemoryOf = services.ProcessIdentity{PID: 10, StartTimeTicks: 100}
	card = processDetailCard(h, v, *v.Snap.Processes)
	for _, value := range []string{"17B", "19B"} {
		if treeHasNameOrText(card, value) {
			t.Errorf("detail showed stale memory value %q for another process", value)
		}
	}
}

func TestSelectingAProcessReadsMemoryOffRegistryLock(t *testing.T) {
	reg := newPanelRegistryWithoutBridge(t)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	id := services.ProcessIdentity{PID: 20, StartTimeTicks: 200}
	type observation struct {
		identity services.ProcessIdentity
		locked   bool
	}
	read := make(chan observation, 1)
	reg.mu.Lock()
	reg.readProcessMemory = func(got services.ProcessIdentity) (services.ProcessMemory, error) {
		acquired := make(chan struct{})
		go func() {
			reg.mu.Lock()
			reg.mu.Unlock()
			close(acquired)
		}()
		locked := false
		select {
		case <-acquired:
		case <-time.After(5 * time.Second):
			locked = true
		}
		read <- observation{identity: got, locked: locked}
		return services.ProcessMemory{PrivateBytes: 23, SharedBytes: 29}, nil
	}
	h := reg.panelHosts[PanelMonitor]
	reg.mu.Unlock()
	handled := make(chan struct{})
	go func() {
		reg.mu.Lock()
		h.activateMonitor(reg, &ui.Node{Action: "monitor:select:20:200"})
		reg.mu.Unlock()
		close(handled)
	}()

	select {
	case got := <-read:
		if got.identity != id {
			t.Fatalf("reader identity = %+v, want %+v", got.identity, id)
		}
		if got.locked {
			t.Fatal("smaps_rollup read ran while Registry.mu was held")
		}
	case <-time.After(6 * time.Second):
		t.Fatal("process memory reader was not called")
	}
	select {
	case <-handled:
	case <-time.After(2 * time.Second):
		t.Fatal("process selection did not finish after scheduling its memory read")
	}
	waitFor(t, func() bool {
		reg.mu.Lock()
		defer reg.mu.Unlock()
		return h.processMemoryOf == id && h.processMemory == (services.ProcessMemory{PrivateBytes: 23, SharedBytes: 29})
	})
}

func TestProcessMemoryReadIsIgnoredAfterSelectionChanges(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	h := reg.panelHosts[PanelMonitor]
	selected := services.ProcessIdentity{PID: 30, StartTimeTicks: 300}
	stale := services.ProcessIdentity{PID: 20, StartTimeTicks: 200}
	reg.mu.Lock()
	h.processSelected = selected
	reg.mu.Unlock()

	reg.applyProcessMemory(h, stale, services.ProcessMemory{PrivateBytes: 23, SharedBytes: 29})
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if h.processMemoryOf != (services.ProcessIdentity{}) || h.processMemory != (services.ProcessMemory{}) {
		t.Fatalf("stale read changed memory state: %v / %+v", h.processMemoryOf, h.processMemory)
	}
}

func TestDetailValuesAreCopyButtonsExceptMissingValues(t *testing.T) {
	values := detailPairs([][2]string{{"PID:", "27"}, {"exe:", ccDash}}, detailWideValueW)
	for i, want := range []struct {
		text   string
		action string
	}{{"27", "monitor:copy:" + url.QueryEscape("27")}, {ccDash, ""}} {
		value := values.Children[i].Children[1]
		if value.Text != want.text || value.Action != want.action {
			t.Errorf("value %d = text %q action %q, want %q / %q", i, value.Text, value.Action, want.text, want.action)
		}
		if want.action != "" && (value.Role != "button" || !value.Focusable) {
			t.Errorf("value %d is not an accessible button: %+v", i, value)
		}
		if want.action == "" && value.Focusable {
			t.Errorf("missing value %d is focusable: %+v", i, value)
		}
	}
}

func TestDetailActionsDisableForAForeignProcess(t *testing.T) {
	h := processHost()
	h.processSelected = services.ProcessIdentity{PID: 10, StartTimeTicks: 100} // uid 0
	v := processView(processFixture())
	card := processDetailCard(h, v, *v.Snap.Processes)
	for _, a := range []string{"process:int:10:100", "process:kill:10:100"} {
		n := findAction(card, a)
		if n == nil || !n.AriaDisabled || n.State&ui.StateDisabled == 0 {
			t.Errorf("%s not disabled: %+v", a, n)
		}
	}
}

func TestDetailViewReportsAnExitedProcess(t *testing.T) {
	h := processHost()
	h.processSelected = services.ProcessIdentity{PID: 99, StartTimeTicks: 990}
	v := processView(processFixture())
	card := processDetailCard(h, v, *v.Snap.Processes)
	if card == nil || !treeHasNameOrText(card, "Process exited") {
		t.Fatal("exited process not reported")
	}
	if findAction(card, "process:int:99:990") != nil && !findAction(card, "process:int:99:990").AriaDisabled {
		t.Fatal("exited process can still be signalled")
	}
}

func TestNoSelectionMeansNoDetail(t *testing.T) {
	v := processView(processFixture())
	if processDetailCard(processHost(), v, *v.Snap.Processes) != nil {
		t.Fatal("detail without a selection")
	}
}

func TestEscapeClosesTheDetailBeforeThePanel(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelMonitor]
	press := func() {
		// keyPress runs under Registry.mu, as the Wayland handler takes it.
		reg.mu.Lock()
		defer reg.mu.Unlock()
		h.keyPress(reg, keyEsc)
	}
	reg.mu.Lock()
	h.processSelected = services.ProcessIdentity{PID: 20, StartTimeTicks: 200}
	reg.mu.Unlock()
	press()
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.panelHosts[PanelMonitor] == nil {
		t.Fatal("Escape closed the panel while the detail was open")
	}
	if h.processSelected != (services.ProcessIdentity{}) {
		t.Fatal("Escape left the detail open")
	}
	reg.mu.Unlock()
	press()
	reg.mu.Lock()
	if reg.panelHosts[PanelMonitor] != nil {
		t.Fatal("second Escape did not close the panel")
	}
}
