package shell

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func renderText(n *ui.Node) string {
	if n == nil {
		return ""
	}
	parts := []string{n.Text}
	for _, child := range n.Children {
		parts = append(parts, renderText(child))
	}
	return strings.Join(parts, " ")
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

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

func TestControlCentreRailKeepsDisabledDestinationsAddressable(t *testing.T) {
	root := controlCentreTree(nil, &PanelHost{section: "home"})
	if len(root.Children) != 2 {
		t.Fatalf("control centre has %d regions, want rail and body", len(root.Children))
	}
	rail := root.Children[0]
	var entries []*ui.Node
	for _, n := range rail.Children {
		if n.Kind == ui.KindButton {
			entries = append(entries, n)
		}
	}
	if len(entries) != 10 {
		t.Fatalf("rail has %d entries, want 10", len(entries))
	}
	if entries[0].Name != "Home" || !entries[0].State.Has(ui.StateSelected) {
		t.Errorf("first entry = %+v, want selected Home", entries[0])
	}
	media := entries[1]
	if media.Name != "Media — not available yet" || media.Action != "" ||
		!media.Focusable || !media.State.Has(ui.StateDisabled) {
		t.Errorf("disabled Media entry = %+v", media)
	}
	if got := len(ui.Focusables(rail)); got != 10 {
		t.Errorf("focusable rail entries = %d, want all 10 including unavailable destinations", got)
	}
}

func TestControlCentreHeaderAndBodyComposition(t *testing.T) {
	root := controlCentreTree(nil, &PanelHost{section: "audio"})
	if len(root.Children) != 2 || len(root.Children[1].Children) != 2 {
		t.Fatalf("control centre composition = %+v, want rail beside header and body", root)
	}
	header := root.Children[1].Children[0]
	if header.Height != 40 || len(header.Children) != 2 || header.Children[0].Text != "Audio" {
		t.Fatalf("header = %+v, want a 40px Audio header", header)
	}
	want := map[string]string{
		"Settings": "cc:settings",
		"Power":    "cc:power",
		"Close":    "cc:close",
	}
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if action, ok := want[n.Name]; ok {
			if n.Action != action || !n.Focusable {
				t.Errorf("%s control = %+v", n.Name, n)
			}
			delete(want, n.Name)
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(header)
	if len(want) != 0 {
		t.Errorf("header is missing controls: %v", want)
	}
	if body := root.Children[1].Children[1]; body.Kind != ui.KindScroll || len(body.Children) != 1 {
		t.Errorf("body = %+v, want one scroll viewport", body)
	}
}

func TestControlCentreHomeFillsTheBodyContract(t *testing.T) {
	h := &PanelHost{id: PanelControlCenter, section: "home", theme: DefaultTheme()}
	root := controlCentreTree(&Registry{}, h)
	body := root.Children[1].Children[1]
	if body.Height != 480 {
		t.Fatalf("body height = %d, want 480", body.Height)
	}
	home := body.Children[0]
	if home.Gap != 12 || len(home.Children) != 4 {
		t.Fatalf("Home composition = %+v, want four blocks separated by 12px", home)
	}
	want := []int{96, 48, 184, 116}
	for i, child := range home.Children {
		if child.Height != want[i] {
			t.Errorf("Home block %d height = %d, want %d", i, child.Height, want[i])
		}
	}
}

func TestHomeShowsDashesBeforeTheFirstSample(t *testing.T) {
	h := &PanelHost{id: PanelControlCenter, section: "home", theme: DefaultTheme()}
	got := renderText(ccHome(&Registry{}, h))
	if strings.Contains(got, "0%") {
		t.Error("Home rendered 0% before a sample landed: stale must read as a dash")
	}
	if !strings.Contains(got, "—") {
		t.Error("Home rendered no dash for an unsampled value")
	}
}

func TestHomeUsesTheRegistryIdentitySnapshot(t *testing.T) {
	r := &Registry{controlIdentity: ccIdentity{
		Name: "Nomad", Account: "nomadx@pony", Uptime: "2 hours",
	}}
	h := &PanelHost{id: PanelControlCenter, section: "home", theme: DefaultTheme()}
	got := renderText(ccHome(r, h))
	for _, want := range []string{"Nomad", "nomadx@pony", "2 hours"} {
		if !strings.Contains(got, want) {
			t.Errorf("Home text %q is missing cached identity %q", got, want)
		}
	}
}

func TestHomeUnavailableControlsAreDisabledAndBatteryIsAReadout(t *testing.T) {
	h := &PanelHost{id: PanelControlCenter, section: "home", theme: DefaultTheme()}
	home := ccHome(&Registry{}, h)
	for _, name := range []string{"Mute", "Power profile", "Volume", "Brightness"} {
		n := findByName(home, name)
		if n == nil || !n.State.Has(ui.StateDisabled) {
			t.Errorf("unavailable %s = %+v, want disabled", name, n)
		}
	}
	battery := findNode(home, func(n *ui.Node) bool {
		return n.Kind == ui.KindCapsule && strings.Contains(renderText(n), "Battery")
	})
	if battery == nil || battery.Focusable || battery.Action != "" || battery.Stroke == 0 {
		t.Errorf("battery = %+v, want an outlined non-actionable readout", battery)
	}
}

func TestHomeDNDControlUpdatesMemorySynchronously(t *testing.T) {
	r := &Registry{notify: newNotifyState()}
	h := &PanelHost{id: PanelControlCenter, section: "home", theme: DefaultTheme()}
	n := findByName(ccHome(r, h), "Do not disturb")
	if !h.activateControlCentre(r, n) {
		t.Fatal("DND control was not handled")
	}
	if _, on := r.notify.dndState(time.Time{}); !on {
		t.Fatal("DND control did not update the in-memory state")
	}
}

func TestHomeWallpaperControlOpensTheExistingPanel(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	n := findByName(h.root, "Wallpaper")
	if !h.activateControlCentre(r, n) {
		r.mu.Unlock()
		t.Fatal("Wallpaper control was not handled")
	}
	_, controlOpen := r.panelHosts[PanelControlCenter]
	_, wallpaperOpen := r.panelHosts[PanelWallpaper]
	r.mu.Unlock()
	if controlOpen || !wallpaperOpen {
		t.Fatalf("after Wallpaper: control open=%v wallpaper open=%v", controlOpen, wallpaperOpen)
	}
}

func TestControlCentreHeaderRoutesPanelsAndClose(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action string
		want   PanelID
		closed bool
	}{
		{name: "settings", action: "cc:settings", want: PanelSettings},
		{name: "power", action: "cc:power", want: PanelSession},
		{name: "close", action: "cc:close", closed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newPanelRegistry(t)
			if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
				t.Fatal(err)
			}
			r.mu.Lock()
			h := r.panelHosts[PanelControlCenter]
			for i, n := range h.focus {
				if n.Action == tc.action {
					h.roving.Set(i)
					break
				}
			}
			if !h.activate(r) {
				r.mu.Unlock()
				t.Fatalf("%s did not activate", tc.action)
			}
			_, controlOpen := r.panelHosts[PanelControlCenter]
			_, targetOpen := r.panelHosts[tc.want]
			r.mu.Unlock()
			if controlOpen || (!tc.closed && !targetOpen) {
				t.Errorf("after %s: control open=%v target open=%v", tc.action, controlOpen, targetOpen)
			}
		})
	}
}

func TestCaffeineTogglesThroughTheRegistryHook(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	started := make(chan struct{})
	released := make(chan struct{})
	r.startInhibit = func() (io.Closer, error) {
		close(started)
		return closerFunc(func() error { close(released); return nil }), nil
	}
	r.setCaffeine(h, true)
	r.mu.Unlock()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("caffeine never started the idle inhibit")
	}
	deadline := time.Now().Add(time.Second)
	for {
		r.mu.Lock()
		on := r.inhibit != nil
		r.mu.Unlock()
		if on {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("started inhibit was not retained")
		}
		time.Sleep(time.Millisecond)
	}
	r.mu.Lock()
	r.setCaffeine(h, false)
	r.mu.Unlock()
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("turning caffeine off did not release the inhibit")
	}
}

func TestCaffeineHoldIsReleasedOnClose(t *testing.T) {
	r := NewRegistry(config.Default())
	released := false
	r.mu.Lock()
	r.inhibit = closerFunc(func() error { released = true; return nil })
	r.mu.Unlock()
	r.Close()
	if !released {
		t.Fatal("the idle inhibit outlived the shell")
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
