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

func TestWordmarkRightClickOpensControlCentre(t *testing.T) {
	widgets := buildWidgets([]config.Item{{ID: "wordmark"}}, 6)
	if len(widgets) != 1 {
		t.Fatalf("buildWidgets = %d widgets, want 1", len(widgets))
	}
	mark := widgets[0].node
	if mark.Kind != ui.KindWordmark || mark.Action != panelControlCenterAction ||
		mark.Name != "Control centre" || mark.Role != "button" {
		t.Fatalf("wordmark = %+v, want the accessible control-centre action", mark)
	}
	if got := buildWidgets([]config.Item{{ID: "control-center"}}, 6); len(got) != 0 {
		t.Fatalf("standalone control-center built %d widgets, want none", len(got))
	}
	for _, id := range config.KnownItemIDs() {
		if id == "control-center" {
			t.Fatal("standalone control-center remains in the configuration vocabulary")
		}
	}
	for _, item := range config.Default().Bar.Right {
		if item.ID == "control-center" {
			t.Fatal("default right section still carries the redundant trigger")
		}
	}

	r := newPanelRegistry(t)
	cb, err := r.NewHost(7, "DP-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := cb.Configure(1536, 44, 120); err != nil {
		t.Fatal(err)
	}
	bar := r.bars[7]
	if err := bar.Layout(1536, 44); err != nil {
		t.Fatal(err)
	}
	target := bar.actionBounds(panelControlCenterAction)
	if target.W == 0 {
		t.Fatal("laid-out wordmark has no control-centre action bounds")
	}
	drainAuxQueue(r)
	if clickButton(bar, target.X+target.W/2, target.Y+target.H/2, buttonLeft) {
		t.Fatal("left-click on the wordmark must stay inert")
	}
	if !clickButton(bar, target.X+target.W/2, target.Y+target.H/2, buttonRight) {
		t.Fatal("right-click on the wordmark did not activate")
	}
	_ = drainAux(t, r, 2)
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	r.mu.Unlock()
	if h == nil {
		t.Fatal("right-click did not open the control centre")
	}
	if want := target.X + target.W/2; h.place.AnchorX != want {
		t.Errorf("anchor = %d, want wordmark centre %d", h.place.AnchorX, want)
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
