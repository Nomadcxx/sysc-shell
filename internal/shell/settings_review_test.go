package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The whole-branch review's Important findings, one test each.

// An output with its own bar override kept the preview on that override, so
// the draft's Style changed nothing in the picture.
func TestPreviewFollowsTheDraftOnAnOverriddenOutput(t *testing.T) {
	reg, h := openSettingsForPreview(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h.draft.Outputs = []config.OutputOverride{{Connector: "DP-1", Bar: h.draft.Bar}}
	solid, islands := h.draft, h.draft
	solid.Bar.Style, islands.Bar.Style = "solid", "islands"
	a, b := reg.settingsBarImage(h, solid, 600), reg.settingsBarImage(h, islands, 600)
	if a == nil || b == nil || opaquePixels(a) == opaquePixels(b) {
		t.Fatal("the preview ignores the draft's Style on an output with an override")
	}
}

// Each bar picture scanned system fonts; the Appearance page cost 38 ms a
// rebuild on the Wayland owner.
func TestBarPicturesAreCachedByTheirInputs(t *testing.T) {
	reg, h := openSettingsForPreview(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	first := reg.settingsBarImage(h, h.draft, 600)
	if again := reg.settingsBarImage(h, h.draft, 600); again != first {
		t.Error("the same draft painted a second picture")
	}
	other := h.draft
	other.Bar.Style = "islands"
	if reg.settingsBarImage(h, other, 600) == first {
		t.Error("a different draft reused the cached picture")
	}
}

// Turning the bar off committed the draft without rebuilding, so nothing
// dimmed until the next unrelated rebuild.
func TestTurningTheBarOffDimsAppearanceAtOnce(t *testing.T) {
	reg, h := openSettingsForPreview(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h.applySetting(reg, &ui.Node{Kind: ui.KindToggle, Action: "set:bar.enabled", Value: 0})
	if h.draft.Bar.Enabled {
		t.Fatal("the toggle did not reach the draft")
	}
	if n := findAction(h.root, "pick:bar.style=solid"); n == nil || n.State&ui.StateDisabled == 0 {
		t.Fatal("the Style cards are live with the bar off")
	}
	if findNode(h.root, func(n *ui.Node) bool { return n.Text == "The bar is off. Turn it on to change this." }) == nil {
		t.Error("no row says why it is dimmed")
	}
}

// Sliders, toggles and menus painted no disabled look.
func TestDimmedRowsMuteTheirLabelAndSayWhy(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.draft.Bar.Style = "islands"
	h.root = settingsTree(nil, h)
	frost := findAction(h.root, "set:bar.frost-opacity")
	if frost == nil || frost.State&ui.StateDisabled == 0 {
		t.Fatal("frost opacity is live under Islands, where the ground is zero")
	}
	if findNode(h.root, func(n *ui.Node) bool { return n.Text == "Applies to the Frosted style." && n.Tone == ui.ToneSubtle }) == nil {
		t.Error("the frost row does not say why it is dimmed")
	}
	if label := findNode(h.root, func(n *ui.Node) bool { return n.Text == "Frost opacity" }); label == nil || label.Tone != ui.ToneSubtle {
		t.Error("the dimmed row's label is not muted")
	}
	if pill := findAction(h.root, "set:bar.pill-opacity"); pill == nil || pill.State&ui.StateDisabled != 0 {
		t.Error("pill opacity dimmed under Islands, where it applies")
	}
}

// Switching page restored the old page's scroll offset.
func TestSwitchingPageOpensAtTheTop(t *testing.T) {
	reg, h := openSettingsForPreview(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	findSettingsBody(h.root).ScrollOffset = 300
	h.focus = []*ui.Node{{Kind: ui.KindButton, Action: "page:Layout", Focusable: true}}
	h.roving = ui.Roving{Count: 1}
	h.activate(reg)
	if h.settingsScroll != 0 || findSettingsBody(h.root).ScrollOffset != 0 {
		t.Fatalf("Layout opened at %d", findSettingsBody(h.root).ScrollOffset)
	}
}

// At spacious density the rail ran past the bottom of a 1280x720 pane. The
// rail is a bounded scroll column now: its section list may outgrow the pane
// (a tall font's labels, a long list), and what has to stay inside the pane
// is the rail itself, which is what the pane draws.
func TestRailFitsThePaneAtEveryDensity(t *testing.T) {
	t.Parallel()
	for _, d := range []theme.Density{theme.DensityStandard, theme.DensityComfortable, theme.DensitySpacious} {
		m, ok := theme.MetricsFor(d)
		if !ok {
			t.Fatalf("no metrics for %s", d)
		}
		h := newSettingsHost()
		h.theme = DefaultTheme()
		h.theme.Metrics = m
		h.place.Panel = settingsPanelSize(1280, 720)
		h.root = settingsTree(nil, h)
		if err := ui.LayoutColumn(h.root, h.place.Panel, settingsMeasure(h)); err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		rail := railScroll(h.root)
		if rail == nil {
			t.Fatalf("%s: the section list is not a bounded scroll column", d)
		}
		if bottom := h.place.Panel.H - m.PanelPadding; rail.Bounds.Y+rail.Bounds.H > bottom {
			t.Errorf("%s: the rail ends at %d, the pane's content ends at %d", d, rail.Bounds.Y+rail.Bounds.H, bottom)
		}
	}
}

// railScroll is the scroll column holding the section tabs.
func railScroll(n *ui.Node) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Kind == ui.KindScroll && n.Width == settingsRailWidth && len(byRole(n, "tab")) > 0 {
		return n
	}
	for _, c := range n.Children {
		if got := railScroll(c); got != nil {
			return got
		}
	}
	return nil
}

// The tree is built before the first configure; a configure at another scale
// has to rebuild it, or the preview stays upscaled.
func TestAConfigureAtANewScaleRebuildsSettings(t *testing.T) {
	reg, _ := openSettingsForPreview(t)
	reg.mu.Lock()
	h := reg.panelHosts[PanelSettings]
	w, hgt := h.surfaceSize()
	cb := h.configureLocking(reg)
	reg.mu.Unlock()
	if err := cb(w, hgt, 150); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if h.settingsTreeScale != 150 {
		t.Fatalf("tree built at %d after a configure at 150", h.settingsTreeScale)
	}
}

// Opening Settings from the control centre passed an empty trigger, so it
// took the 1920x1080 size on every output.
func TestSettingsOpenedByShortcutTakesTheOutputsSize(t *testing.T) {
	reg := newPanelRegistry(t)
	bar := withTestBar(t, reg, 7, reg.cfg)
	bar.setOutputSize(1536, 864)
	if err := bar.Configure(1536, reg.cfg.Bar.SurfaceExtent(), 150); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	reg.openSettingsAtLocked(7, "Bar")
	h := reg.panelHosts[PanelSettings]
	reg.mu.Unlock()
	_ = drainAux(t, reg, 2)
	if h == nil {
		t.Fatal("settings did not open")
	}
	if want := settingsPanelSize(1536, 864); h.place.Panel.W != want.W {
		t.Errorf("opened %dx%d, want %d wide", h.place.Panel.W, h.place.Panel.H, want.W)
	}
}

// findSettingsBody is the pane's content scroll. The settings pane has two
// scrolls now — the section rail and the body — so a bare "first scroll" walk
// finds the rail and every offset and block assertion lands on the wrong one.
func findSettingsBody(n *ui.Node) *ui.Node {
	var out *ui.Node
	walkNodes(n, func(node *ui.Node) {
		if out == nil && node.Kind == ui.KindScroll && node.Key == settingsBodyKey {
			out = node
		}
	})
	return out
}
