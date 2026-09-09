package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestControlCentreNameAndFlushPlacement(t *testing.T) {
	id, err := parsePanelName("control-center")
	if err != nil {
		t.Fatalf("parsePanelName(control-center): %v", err)
	}
	if got := id.String(); got != "control-center" {
		t.Fatalf("String() = %q, want control-center", got)
	}

	cfg := config.Default()
	cfg.Panels.Gap = 19 // The fused panel ignores a configured floating gap.
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	if err := r.OpenPanelByName("control-center"); err != nil {
		t.Fatal(err)
	}

	r.mu.Lock()
	h := r.panelHosts[id]
	if h == nil {
		r.mu.Unlock()
		t.Fatal("control centre did not create a panel host")
	}
	place := h.place
	section := h.section
	rootKind := h.root.Kind
	leaseCount := len(h.leases)
	fillet := h.filletMargin()
	spec := r.panelSpec(h, place.Margins())
	r.mu.Unlock()

	if place.Panel != (ui.Rect{W: 700, H: 564}) {
		t.Errorf("panel size = %+v, want 700x564", place.Panel)
	}
	if place.Gap != 0 {
		t.Errorf("gap = %d, want 0", place.Gap)
	}
	if got := place.Margins(); got.Top != place.BarZone || got.Left != (place.Output.W-place.Panel.W)/2 {
		t.Errorf("margins = %+v, want flush and centred", got)
	}
	if section != "home" {
		t.Errorf("initial section = %q, want home", section)
	}
	if rootKind != ui.KindRow {
		t.Errorf("root kind = %v, want row", rootKind)
	}
	if leaseCount != 4 {
		t.Errorf("leases = %d, want CPU, memory, battery and clock", leaseCount)
	}
	if fillet != 12 || spec.Width != 724 {
		t.Errorf("fillet = %d, drawn width = %d, want 12 and 724", fillet, spec.Width)
	}
}

func TestControlCentreBarWidgetSatisfiesApply(t *testing.T) {
	widgets := buildWidgets([]config.Item{{ID: "control-center"}}, 6)
	if len(widgets) != 1 {
		t.Fatalf("buildWidgets = %d widgets, want 1", len(widgets))
	}
	w := widgets[0]
	if w.inner == nil || w.inner.Kind != ui.KindIcon || w.inner.Icon != "tune" {
		t.Fatalf("inner node = %+v, want the tune icon", w.inner)
	}
	if w.inner.Action != "panel:control-center" || w.tooltip != "Control centre" {
		t.Errorf("action = %q, tooltip = %q", w.inner.Action, w.tooltip)
	}
	(&Bar{left: widgets}).apply(barView{})

	known := false
	for _, id := range config.KnownItemIDs() {
		known = known || id == "control-center"
	}
	if !known {
		t.Error("control-center is absent from the configuration vocabulary")
	}
	right := config.Default().Bar.Right
	if len(right) < 2 || right[len(right)-2].ID != "control-center" || right[len(right)-1].ID != "notifications" {
		t.Errorf("default right section = %+v, want control-center before notifications", right)
	}

	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	bar := &Bar{}
	r.bindBarPanelActionsLocked(7, bar)
	if !bar.onAction("panel:control-center", buttonLeft) {
		t.Fatal("left-click did not activate the control centre")
	}
	r.mu.Lock()
	_, opened := r.panelHosts[PanelControlCenter]
	r.mu.Unlock()
	if !opened {
		t.Error("left-click activated without opening the control centre")
	}
}

func TestPanelSectionValidationPrecedesMutation(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	call := r.HandlePanelByName

	if err := r.OpenPanelByName("control-center"); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	original := r.panelHosts[PanelControlCenter]
	r.mu.Unlock()
	for _, section := range []string{"network", "nope"} {
		if err := call("open", "control-center", section); err == nil {
			t.Errorf("section %q was accepted", section)
		}
		r.mu.Lock()
		got := r.panelHosts[PanelControlCenter]
		selected := got.section
		r.mu.Unlock()
		if got != original || selected != "home" {
			t.Fatalf("rejected section %q changed host or selection", section)
		}
	}
	if err := call("open", "control-center", "audio"); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	selected := r.panelHosts[PanelControlCenter].section
	r.mu.Unlock()
	if selected != "audio" {
		t.Errorf("selected section = %q, want audio", selected)
	}
	if err := call("open", "control-center", ""); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	selected = r.panelHosts[PanelControlCenter].section
	r.mu.Unlock()
	if selected != "home" {
		t.Errorf("omitted section selected %q, want home", selected)
	}

	if err := call("open", "settings", "Appearance"); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	settingsSection := r.panelHosts[PanelSettings].section
	r.mu.Unlock()
	if settingsSection != "Appearance" {
		t.Errorf("settings section = %q, want Appearance", settingsSection)
	}
}
