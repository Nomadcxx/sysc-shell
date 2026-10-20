package shell

import (
	"strings"
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

// The rail stays inside the pane and every tab remains reachable when density,
// output size, scale and text size change.
func TestRailFitsThePaneAtEveryDensity(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	for _, d := range theme.Densities() {
		for _, out := range [][2]int{{1280, 720}, {1536, 864}, {3440, 1440}} {
			for _, scale := range []int{120, 150, 180} {
				for _, fontScale := range []int{100, theme.FontScaleMax} {
					m, ok := theme.MetricsFor(d)
					if !ok {
						t.Fatalf("no metrics for %s", d)
					}
					h.theme = settingsRailTheme(d, fontScale)
					h.scale120 = scale
					h.place.Panel = settingsPanelSize(out[0], out[1])
					h.settingsRailScroll = 0
					h.section = "Sounds"
					measure := settingsMeasure(h)
					head := []*ui.Node{settingsSectionHeading(h, h.section)}
					railNode := settingsRail(h, h.section, head)
					h.root = &ui.Node{Kind: ui.KindColumn, Padding: m.PanelPadding, Children: []*ui.Node{railNode}}
					if err := ui.LayoutColumn(h.root, h.place.Panel, measure); err != nil {
						t.Fatalf("%s %dx%d @%d font %d: %v", d, out[0], out[1], scale, fontScale, err)
					}
					rail := keyedScroll(h.root, settingsRailKey)
					if rail == nil {
						t.Fatalf("%s %dx%d @%d font %d: section list is not a scroll column", d, out[0], out[1], scale, fontScale)
					}
					if bottom := h.place.Panel.H - m.PanelPadding; rail.Bounds.Y+rail.Bounds.H > bottom {
						t.Errorf("%s %dx%d @%d font %d: rail ends at %d, pane content ends at %d", d, out[0], out[1], scale, fontScale, rail.Bounds.Y+rail.Bounds.H, bottom)
						continue
					}
					tabs := byRole(h.root, "tab")
					if len(tabs) == 0 {
						t.Fatalf("%s %dx%d @%d font %d: section tabs are missing", d, out[0], out[1], scale, fontScale)
					}
					viewportH := rail.Bounds.H - 2*rail.Padding
					maxOffset := max(rail.ContentH-viewportH, 0)
					for _, tab := range tabs {
						tabTop := tab.Bounds.Y - rail.Bounds.Y - rail.Padding + rail.ScrollOffset
						tabBottom := tabTop + tab.Bounds.H
						targetOffset := min(max(tabTop, 0), maxOffset)
						if tabTop < 0 || tabBottom > rail.ContentH || tabTop < targetOffset || tabBottom > targetOffset+viewportH {
							t.Errorf("%s %dx%d @%d font %d: tab %q at content y=%d..%d is unreachable in viewport %d at offset %d (content %d)", d, out[0], out[1], scale, fontScale, tab.Name, tabTop, tabBottom, viewportH, targetOffset, rail.ContentH)
						}
					}
					ui.ScrollBy(rail, maxOffset-rail.ScrollOffset)
					if err := ui.LayoutColumn(h.root, h.place.Panel, measure); err != nil {
						t.Fatalf("%s %dx%d @%d font %d: end-of-rail layout: %v", d, out[0], out[1], scale, fontScale, err)
					}
					last := tabs[len(tabs)-1]
					if last.Bounds.Y < rail.Bounds.Y+rail.Padding || last.Bounds.Y+last.Bounds.H > rail.Bounds.Y+rail.Bounds.H-rail.Padding {
						t.Errorf("%s %dx%d @%d font %d: last tab %q at %+v is clipped at maximum offset %d", d, out[0], out[1], scale, fontScale, last.Name, last.Bounds, rail.ScrollOffset)
					}
				}
			}
		}
	}
}

func TestSettingsRailKeepsTheDeepLinkedSectionVisible(t *testing.T) {
	reg, h := openSettingsForPreview(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	prepareOverflowSettingsRail(t, reg, h)
	rail := keyedScroll(h.root, settingsRailKey)
	if rail == nil || rail.ContentH <= rail.Bounds.H {
		t.Fatalf("fixture does not overflow the rail: %+v", rail)
	}
	if err := reg.selectPanelSectionLocked(PanelSettings, "Sounds"); err != nil {
		t.Fatal(err)
	}
	assertSettingsRailSectionVisible(t, h, "Sounds")
}

func TestSettingsRailRevealsAnAlreadySelectedSection(t *testing.T) {
	reg, h := openSettingsForPreview(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	prepareOverflowSettingsRail(t, reg, h)
	if err := reg.selectPanelSectionLocked(PanelSettings, "Sounds"); err != nil {
		t.Fatal(err)
	}

	const section = "Sounds"
	rail := keyedScroll(h.root, settingsRailKey)
	if rail == nil {
		t.Fatal("Settings rail is missing")
	}
	wasOffscreen := false
	for _, delta := range []int{rail.ContentH, -rail.ContentH} {
		ui.ScrollBy(rail, delta)
		if err := h.configure(h.logicalW, h.logicalH, h.scale120); err != nil {
			t.Fatal(err)
		}
		tab := findAction(h.root, "section:"+section)
		if tab == nil {
			t.Fatalf("section tab %q is missing", section)
		}
		if tab.Bounds.Y < rail.Bounds.Y || tab.Bounds.Y+tab.Bounds.H > rail.Bounds.Y+rail.Bounds.H {
			wasOffscreen = true
			break
		}
	}
	if !wasOffscreen {
		t.Fatalf("fixture could not scroll selected section %q out of view", section)
	}
	if err := reg.selectPanelSectionLocked(PanelSettings, section); err != nil {
		t.Fatal(err)
	}
	assertSettingsRailSectionVisible(t, h, section)
}

func TestSettingsRailRevealsAlreadySelectedSectionOnFirstConfigure(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1280, OutH: 720}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelSettings]
	h.theme = settingsRailTheme(theme.DensitySpacious, theme.FontScaleMax)
	h.place.Panel = settingsPanelSize(1280, 720)
	h.logicalW, h.logicalH, h.scale120 = 0, 0, 180
	h.settingsRailScroll = 0
	h.settingsRailRevealPending = ""
	h.root = settingsTree(reg, h)
	tabs := byRole(h.root, "tab")
	if len(tabs) == 0 {
		t.Fatal("Settings rail has no tabs")
	}
	section := tabs[len(tabs)-1].Name
	_, page, ok := settingsAddress(section)
	if !ok {
		t.Fatalf("last Settings tab %q has no address", section)
	}
	h.section, h.settingsPage = section, page
	h.root = settingsTree(reg, h)
	if err := reg.selectPanelSectionLocked(PanelSettings, section); err != nil {
		t.Fatal(err)
	}
	w, height := h.surfaceSize()
	if err := h.configure(w, height, h.scale120); err != nil {
		t.Fatal(err)
	}
	assertSettingsRailSectionVisible(t, h, section)
}

func TestSettingsRailRevealsDeepLinkOnFirstConfigure(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1280, OutH: 720}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelSettings]
	h.theme = settingsRailTheme(theme.DensitySpacious, theme.FontScaleMax)
	h.place.Panel = settingsPanelSize(1280, 720)
	h.logicalW, h.logicalH, h.scale120 = 0, 0, 180
	h.root = settingsTree(reg, h)
	if err := reg.selectPanelSectionLocked(PanelSettings, "Sounds"); err != nil {
		t.Fatal(err)
	}
	w, height := h.surfaceSize()
	if err := h.configure(w, height, h.scale120); err != nil {
		t.Fatal(err)
	}
	assertSettingsRailSectionVisible(t, h, "Sounds")
}

func TestSettingsRailKeepsKeyboardFocusVisible(t *testing.T) {
	reg, h := openSettingsForPreview(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	prepareOverflowSettingsRail(t, reg, h)
	idx := -1
	for i, n := range h.focus {
		if n != nil && n.Action == "section:Sounds" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("Sounds section is not keyboard focusable")
	}
	h.roving.Set(idx)
	h.afterFocusChange(reg)
	assertSettingsRailSectionVisible(t, h, "Sounds")
}

func TestSettingsRailRetainsScrollWhenSelectingASection(t *testing.T) {
	reg, h := openSettingsForPreview(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	prepareOverflowSettingsRail(t, reg, h)
	rail := keyedScroll(h.root, settingsRailKey)
	ui.ScrollBy(rail, rail.ContentH)
	if rail.ScrollOffset == 0 {
		t.Fatal("fixture did not scroll the rail")
	}
	if err := h.configure(h.logicalW, h.logicalH, h.scale120); err != nil {
		t.Fatal(err)
	}
	idx, selected := -1, ""
	for i, n := range h.focus {
		if n == nil || !strings.HasPrefix(n.Action, "section:") {
			continue
		}
		if n.Bounds.Y >= rail.Bounds.Y && n.Bounds.Y+n.Bounds.H <= rail.Bounds.Y+rail.Bounds.H {
			idx = i
			selected = strings.TrimPrefix(n.Action, "section:")
		}
	}
	if idx < 0 {
		t.Fatal("no section is visible after scrolling the rail")
	}
	before := rail.ScrollOffset
	h.roving.Set(idx)
	h.activate(reg)
	assertSettingsRailSectionVisible(t, h, selected)
	if got := keyedScroll(h.root, settingsRailKey).ScrollOffset; got != before {
		t.Fatalf("selecting visible section %q changed rail offset from %d to %d", selected, before, got)
	}
}

func prepareOverflowSettingsRail(t *testing.T, reg *Registry, h *PanelHost) {
	t.Helper()
	h.theme = settingsRailTheme(theme.DensitySpacious, theme.FontScaleMax)
	h.place.Panel = settingsPanelSize(1280, 720)
	h.logicalW, h.logicalH, h.scale120 = 0, 0, 180
	h.root = settingsTree(reg, h)
	w, height := h.surfaceSize()
	if err := h.configure(w, height, h.scale120); err != nil {
		t.Fatal(err)
	}
	h.focus = ui.Focusables(h.root)
	h.roving = ui.Roving{Count: len(h.focus)}
}

func settingsRailTheme(d theme.Density, fontScale int) Theme {
	resolved := DefaultTheme()
	if m, ok := theme.MetricsFor(d); ok {
		resolved.Metrics = m
	}
	for i, spec := range resolved.Type.Roles {
		spec.Size = max((spec.Size*fontScale+50)/100, 1)
		resolved.Type.Roles[i] = spec
	}
	return resolved
}

func assertSettingsRailSectionVisible(t *testing.T, h *PanelHost, section string) {
	t.Helper()
	rail := keyedScroll(h.root, settingsRailKey)
	tab := findAction(h.root, "section:"+section)
	if rail == nil || tab == nil {
		t.Fatalf("rail or section tab %q is missing", section)
	}
	if tab.Bounds.Y < rail.Bounds.Y || tab.Bounds.Y+tab.Bounds.H > rail.Bounds.Y+rail.Bounds.H {
		t.Fatalf("selected %q tab at %+v is outside rail viewport %+v (offset %d)", section, tab.Bounds, rail.Bounds, rail.ScrollOffset)
	}
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
