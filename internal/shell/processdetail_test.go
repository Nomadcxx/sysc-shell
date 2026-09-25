package shell

import (
	"syscall"
	"testing"

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
	h.processSelected = services.ProcessIdentity{PID: 20, StartTimeTicks: 200}
	h.keyPress(reg, keyEsc)
	if reg.panelHosts[PanelMonitor] == nil {
		t.Fatal("Escape closed the panel while the detail was open")
	}
	if h.processSelected != (services.ProcessIdentity{}) {
		t.Fatal("Escape left the detail open")
	}
	h.keyPress(reg, keyEsc)
	if reg.panelHosts[PanelMonitor] != nil {
		t.Fatal("second Escape did not close the panel")
	}
}
