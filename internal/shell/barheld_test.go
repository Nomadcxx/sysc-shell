package shell

import "testing"

func TestRegistryBarHeld(t *testing.T) {
	reg := newPanelRegistry(t)
	if reg.barHeld(7) {
		t.Fatal("held with nothing open")
	}
	if err := reg.OpenPanel(PanelNotifications, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if !reg.barHeld(7) {
		t.Fatal("not held with a panel open on the bar's output")
	}
	// The probe reaches the bar through the panel-action binding, which a
	// reload repeats for every rebuilt bar.
	bar := &Bar{conn: "DP-1"}
	reg.mu.Lock()
	reg.bindBarPanelActionsLocked(7, bar)
	reg.mu.Unlock()
	if bar.holdProbe == nil || !bar.holdProbe() {
		t.Fatal("the bound bar's hold probe does not see the open panel")
	}
	if reg.barHeld(8) {
		t.Fatal("held for another output")
	}
	reg.mu.Lock()
	reg.closeAllPanelsLocked()
	if reg.trayMenu != nil {
		reg.trayMenu.open_, reg.trayMenu.output = true, 7
	}
	reg.mu.Unlock()
	if reg.trayMenu != nil && !reg.barHeld(7) {
		t.Fatal("not held with the tray menu open on the bar's output")
	}
}
