package shell

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/onboarding"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// openOnboardingAt opens the wizard on a w×h output and configures it at
// scale120, the way the compositor's first configure does.
func openOnboardingAt(t *testing.T, w, h, scale120 int) (*Registry, *PanelHost) {
	t.Helper()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: w, OutH: h}); err != nil {
		t.Fatal(err)
	}
	panel := drainAux(t, reg, 2)[1].Open
	if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), scale120); err != nil {
		t.Fatal(err)
	}
	return reg, reg.panelHosts[PanelOnboarding]
}

func TestOnboardingWordmarkIsCachedRasterAtPanelScale(t *testing.T) {
	for _, tc := range []struct {
		name     string
		scale120 int
	}{
		{"scale 1.0", 120},
		{"scale 1.25", 150},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg, h := openOnboardingAt(t, 3440, 1440, tc.scale120)
			marks := findAllKind(h.root, ui.KindImage)
			var mark *ui.Node
			for _, n := range marks {
				if n.Key == "onb-wordmark" {
					if mark != nil {
						t.Fatal("onboarding tree holds more than one wordmark")
					}
					mark = n
				}
			}
			if mark == nil {
				t.Fatal("onboarding tree has no onb-wordmark image")
			}
			if mark.Name != "SYSC" || mark.Role != "img" || mark.ImageW != 235 || mark.ImageH != 80 {
				t.Fatalf("wordmark = name %q role %q box %dx%d, want SYSC/img 235x80", mark.Name, mark.Role, mark.ImageW, mark.ImageH)
			}
			scale := ui.Scale120(tc.scale120)
			if mark.Image == nil || mark.Image.Width != scale.Physical(235) || mark.Image.Height != scale.Physical(80) {
				t.Fatalf("wordmark raster = %+v, want %dx%d", mark.Image, scale.Physical(235), scale.Physical(80))
			}
			first := mark.Image
			reg.mu.Lock()
			reg.rebuildPanel(h)
			reg.mu.Unlock()
			if again := onboardingNodeByKey(h.root, "onb-wordmark"); again == nil || again.Image != first {
				t.Fatal("a rebuild at the same scale and theme did not reuse the wordmark raster")
			}
		})
	}
}

func TestOnboardingWordmarkFollowsThemeColours(t *testing.T) {
	reg, h := openOnboardingAt(t, 3440, 1440, 120)
	first := onboardingNodeByKey(h.root, "onb-wordmark").Image
	tokens := reg.tokens
	tokens.OnSurface = "#ff4040"
	reg.mu.Lock()
	reg.retheThemeOpenSurfacesLocked(reg.cfg, tokens)
	reg.mu.Unlock()
	if h.theme.Palette.OnSurface == (Color{}) {
		t.Fatal("retheme left no palette")
	}
	if got := onboardingNodeByKey(h.root, "onb-wordmark").Image; got == first {
		t.Fatal("wordmark raster was not repainted after the palette changed")
	}
}

func TestOnboardingFrameIsRailBesideContent(t *testing.T) {
	reg, h := openOnboardingAt(t, 3440, 1440, 120)
	if h.root.Kind != ui.KindRow || len(h.root.Children) != 2 {
		t.Fatalf("root is kind %s with %d children, want a row of rail and content", h.root.Kind, len(h.root.Children))
	}
	rail, content := h.root.Children[0], h.root.Children[1]
	if rail.Key != "onb-rail" || rail.Width != onbRailWidth || onbRailWidth != 280 || rail.Fill != ui.FillContainerHigh {
		t.Fatalf("rail = key %q width %d fill %d, want onb-rail 280 FillContainerHigh", rail.Key, rail.Width, rail.Fill)
	}
	if content.Key != "onb-content" {
		t.Fatalf("second column key = %q, want onb-content", content.Key)
	}
	titleY := -1
	for page := onboardingPage(0); page < onbPages; page++ {
		reg.mu.Lock()
		h.onbPage = page
		reg.rebuildPanel(h)
		reg.mu.Unlock()
		content := onboardingNodeByKey(h.root, "onb-content")
		title := onboardingNodeByKey(h.root, "onb-title")
		footer := onboardingNodeByKey(h.root, "onb-footer")
		if content == nil || title == nil || footer == nil {
			t.Fatalf("page %d is missing content, title or footer", page)
		}
		if title.CenterY {
			t.Fatalf("page %d centres its title", page)
		}
		if titleY < 0 {
			titleY = title.Bounds.Y
		} else if title.Bounds.Y != titleY {
			t.Errorf("page %d title at y=%d, want %d as on the first page", page, title.Bounds.Y, titleY)
		}
		if got, want := footer.Bounds.Y+footer.Bounds.H, content.Bounds.Y+content.Bounds.H-content.Padding; got != want {
			t.Errorf("page %d footer ends at %d, want content end %d", page, got, want)
		}
		if got, want := rail.Bounds.H, h.root.Bounds.H-2*h.root.Padding; got != want {
			t.Errorf("rail height = %d, want the frame's inner height %d", got, want)
		}
	}
	for _, gone := range []string{"onb-brand-pill", "onb-setup-label", "onb-progress-label", "onb-stage"} {
		if onboardingNodeByKey(h.root, gone) != nil {
			t.Errorf("old frame node %q is still in the tree", gone)
		}
	}
}

func TestOnboardingNarrowFrameFoldsRailIntoHeader(t *testing.T) {
	_, h := openOnboardingAt(t, 800, 500, 120)
	if onboardingNodeByKey(h.root, "onb-rail") != nil {
		t.Fatal("narrow frame kept the rail")
	}
	header := onboardingNodeByKey(h.root, "onb-header")
	if header == nil || header.Kind != ui.KindRow {
		t.Fatal("narrow frame has no header row")
	}
	mark := onboardingNodeByKey(header, "onb-wordmark")
	if mark == nil || mark.ImageW != (onbWordmarkW+1)/2 {
		t.Fatal("narrow header lacks the half-size wordmark")
	}
	if eyebrow := onboardingNodeByKey(header, "onb-eyebrow"); eyebrow == nil || eyebrow.Text != "STEP 01 OF 06" {
		t.Fatalf("narrow header eyebrow = %+v, want STEP 01 OF 06", eyebrow)
	}
	if byAction(header, "onb-skip") == nil {
		t.Fatal("narrow header lacks Not now")
	}
	content := onboardingNodeByKey(h.root, "onb-content")
	footer := onboardingNodeByKey(h.root, "onb-footer")
	if content == nil || footer == nil {
		t.Fatal("narrow frame lacks content or footer")
	}
	if got, want := footer.Bounds.Y+footer.Bounds.H, content.Bounds.Y+content.Bounds.H-content.Padding; got != want {
		t.Errorf("narrow footer ends at %d, want content end %d", got, want)
	}
}

func TestOnboardingStepperMarksProgress(t *testing.T) {
	reg, h := openOnboardingAt(t, 3440, 1440, 120)
	reg.mu.Lock()
	h.onbPage = onbRegion
	reg.rebuildPanel(h)
	reg.mu.Unlock()
	labels := []string{"Welcome", "Appearance", "Region and language", "When idle", "Plugins", "Review"}
	for page := onboardingPage(0); page < onbPages; page++ {
		step := byAction(h.root, fmt.Sprintf("onb-step:%d", page))
		if step == nil || !step.Focusable || step.Role != "button" || step.Name != labels[page] {
			t.Fatalf("step %d = %+v, want a focusable button named %q", page, step, labels[page])
		}
		marker := onboardingNodeByKey(step, fmt.Sprintf("onb-step-marker:%d", page))
		if marker == nil || marker.Shape != ui.ShapeCircle {
			t.Fatalf("step %d marker = %+v, want a circle", page, marker)
		}
		check := findNode(marker, func(n *ui.Node) bool { return n.Kind == ui.KindIcon && n.Icon == "check" })
		switch {
		case page < h.onbPage:
			if marker.Fill != ui.FillAccent || check == nil {
				t.Errorf("done step %d marker fill %d check %v, want an accent disc with a check", page, marker.Fill, check != nil)
			}
		case page == h.onbPage:
			if marker.Fill != ui.FillSoft || marker.StrokeFill != ui.FillAccent || marker.Stroke != 2 || check != nil {
				t.Errorf("current step marker = fill %d stroke %d/%d, want FillSoft with a 2 px accent ring", marker.Fill, marker.Stroke, marker.StrokeFill)
			}
		default:
			if marker.StrokeFill != ui.FillOutline || marker.Stroke == 0 || marker.Fill == ui.FillAccent || check != nil {
				t.Errorf("ahead step %d marker = fill %d stroke %d/%d, want an outline ring", page, marker.Fill, marker.Stroke, marker.StrokeFill)
			}
		}
	}
}

func TestOnboardingStepperJumpsToAnyStep(t *testing.T) {
	reg, h := openOnboardingAt(t, 3440, 1440, 120)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h.onbPage = onbAppearance
	h.onbScroll = 40
	reg.rebuildPanel(h)
	h.setFocus(byAction(h.root, "onb-step:4"))
	if !h.activate(reg) {
		t.Fatal("step 5 rejected activation")
	}
	if h.onbPage != onbPlugins || h.onbScroll != 0 {
		t.Fatalf("after jump page=%d scroll=%d, want page %d and scroll 0", h.onbPage, h.onbScroll, onbPlugins)
	}
}

func TestOnboardingStepperSummarisesEarlierSteps(t *testing.T) {
	reg, h := openOnboardingAt(t, 3440, 1440, 120)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h.draft.Weather.City = "Melbourne"
	h.draft.Weather.Unit = "celsius"
	h.onbPage = onbReady
	reg.rebuildPanel(h)
	want := map[onboardingPage]string{
		onbAppearance: "Dark",
		onbRegion:     "Melbourne · °C",
		onbIdle:       "Nothing",
		onbPlugins:    "0 selected",
	}
	for page, text := range want {
		summary := onboardingNodeByKey(h.root, fmt.Sprintf("onb-step-summary:%d", page))
		if summary == nil || !strings.Contains(treeText(summary), text) {
			t.Errorf("step %d summary = %q, want it to contain %q", page, treeText(summary), text)
		}
	}
	h.onbPage = onbRegion
	reg.rebuildPanel(h)
	for page := onbRegion; page < onbPages; page++ {
		if onboardingNodeByKey(h.root, fmt.Sprintf("onb-step-summary:%d", page)) != nil {
			t.Errorf("step %d shows a summary before the user reached it", page)
		}
	}
}

func TestOnboardingStepperFitsAboveRailFoot(t *testing.T) {
	for _, size := range [][2]int{{1536, 864}, {1920, 1080}, {3440, 1440}} {
		reg, h := openOnboardingAt(t, size[0], size[1], 120)
		for _, page := range []onboardingPage{onbWelcome, onbReady} {
			reg.mu.Lock()
			h.onbPage = page
			reg.rebuildPanel(h)
			reg.mu.Unlock()
			last := byAction(h.root, fmt.Sprintf("onb-step:%d", onbPages-1))
			foot := onboardingNodeByKey(h.root, "onb-rail-foot")
			if last == nil || foot == nil || last.Bounds.Y+last.Bounds.H > foot.Bounds.Y {
				t.Errorf("%dx%d page %d: last step %+v runs into the rail foot %+v", size[0], size[1], page, last.Bounds, foot.Bounds)
			}
		}
	}
}

func TestOnboardingFooterLabelsAndStatus(t *testing.T) {
	reg, h := openOnboardingAt(t, 3440, 1440, 120)
	attachOnboardingTestPlugins(t, reg)
	want := []string{"Get started", "Continue", "Continue", "Continue", "Continue", "Finish"}
	for page := onboardingPage(0); page < onbPages; page++ {
		reg.mu.Lock()
		h.onbPage = page
		reg.rebuildPanel(h)
		reg.mu.Unlock()
		action := "onb-next"
		if page == onbReady {
			action = "onb-finish"
		}
		main := byAction(h.root, action)
		if main == nil || main.Name != want[page] || main.Fill != ui.FillAccent {
			t.Errorf("page %d primary = %+v, want accent %q", page, main, want[page])
		}
		if onbMainLabel(page) != want[page] {
			t.Errorf("onbMainLabel(%d) = %q, want %q", page, onbMainLabel(page), want[page])
		}
		back := byAction(h.root, "onb-back")
		if disabled := back.State.Has(ui.StateDisabled); disabled != (page == onbWelcome) {
			t.Errorf("page %d Back disabled = %t", page, disabled)
		}
		eyebrow := onboardingNodeByKey(h.root, "onb-eyebrow")
		if wantText := fmt.Sprintf("STEP %02d OF 06", page+1); eyebrow == nil || eyebrow.Text != wantText {
			t.Errorf("page %d eyebrow = %+v, want %q", page, eyebrow, wantText)
		}
		status := onboardingNodeByKey(h.root, "onb-status")
		if page == onbPlugins {
			if status == nil || status.Text != "0 selected" {
				t.Errorf("Plugins footer status = %+v, want 0 selected", status)
			}
		} else if status != nil && status.Text != "" {
			t.Errorf("page %d footer status = %q, want none", page, status.Text)
		}
	}
}

func TestOnboardingWelcomeIsAnOverview(t *testing.T) {
	_, h := openOnboardingAt(t, 3440, 1440, 120)
	for i, part := range []string{"look", "weather", "idle", "plugins"} {
		card := onboardingNodeByKey(h.root, "onb-welcome-"+part)
		if card == nil {
			t.Fatalf("Welcome lacks the %s card", part)
		}
		badge := findNode(card, func(n *ui.Node) bool {
			return n.Kind == ui.KindText && n.TextRole == theme.RoleMono && n.Text == fmt.Sprint(i+1)
		})
		if badge == nil {
			t.Errorf("%s card has no mono number badge %d", part, i+1)
		}
		if lines := findAllKind(card, ui.KindText); len(lines) < 3 {
			t.Errorf("%s card holds %d text nodes, want number, title and one line", part, len(lines))
		}
	}
	text := renderText(h.root)
	for _, summary := range []string{"Interface language:", "Theme:", "Weather location:", "Startup plugins:"} {
		if strings.Contains(text, summary) {
			t.Errorf("Welcome still renders the settings summary %q", summary)
		}
	}
	if byAction(h.root, "onb-docs") != nil {
		t.Error("Welcome still offers the documentation action")
	}
}

func TestOnboardingAppearanceChoosesModeByCard(t *testing.T) {
	reg, h := openOnboardingAt(t, 3440, 1440, 120)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h.draft.ThemeGen.Mode = "dark"
	h.onbPage = onbAppearance
	reg.rebuildPanel(h)
	dark, light := onboardingNodeByKey(h.root, "onb-mode:dark"), onboardingNodeByKey(h.root, "onb-mode:light")
	if dark == nil || light == nil {
		t.Fatal("Appearance lacks the Dark and Light cards")
	}
	if dark.Fill != ui.FillSoft || dark.StrokeFill != ui.FillAccent || dark.State.Has(ui.StateSelected) {
		t.Errorf("current mode card = state %v fill %d stroke %d, want FillSoft with an accent ring", dark.State, dark.Fill, dark.StrokeFill)
	}
	if light.Fill == ui.FillSoft || light.StrokeFill == ui.FillAccent {
		t.Error("the other mode card is marked selected")
	}
	if dark.Role != "radio" || !dark.Focusable || dark.Bounds.H <= 0 {
		t.Errorf("mode card = role %q focusable %t bounds %+v, want a laid-out radio", dark.Role, dark.Focusable, dark.Bounds)
	}
	h.setFocus(light)
	if !h.activate(reg) {
		t.Fatal("Light card rejected activation")
	}
	if h.draft.ThemeGen.Mode != "light" {
		t.Fatalf("draft mode = %q, want light written through the setting", h.draft.ThemeGen.Mode)
	}
	if light = onboardingNodeByKey(h.root, "onb-mode:light"); light.Fill != ui.FillSoft {
		t.Error("Light card is not selected after choosing it")
	}
	row := onboardingNodeByKey(h.root, "onb-wallpaper-row")
	change := byAction(h.root, "onb-wallpaper")
	if row == nil || change == nil || change.Name != "Change" || onboardingNodeByKey(row, "") == nil {
		t.Fatalf("wallpaper row = %v, change = %+v, want a row holding a Change action", row != nil, change)
	}
	if !strings.Contains(treeText(row), "Wallpaper") {
		t.Errorf("wallpaper row text = %q, want it to name the wallpaper", treeText(row))
	}
}

func openOnboardingRegion(t *testing.T) (*Registry, *PanelHost) {
	t.Helper()
	reg, h := openOnboardingAt(t, 3440, 1440, 120)
	reg.mu.Lock()
	h.onbPage = onbRegion
	reg.rebuildPanel(h)
	reg.mu.Unlock()
	return reg, h
}

func TestOnboardingRegionStacksLabelsOverControls(t *testing.T) {
	reg, h := openOnboardingRegion(t)
	for _, path := range []string{"session.language", "weather.unit", "weather.city"} {
		label := onboardingNodeByKey(h.root, "onb-label:"+path)
		control := onboardingNodeByKey(h.root, "onb-control:"+path)
		if label == nil || control == nil {
			t.Fatalf("%s lacks a stacked label or control", path)
		}
		if label.Bounds.Y+label.Bounds.H > control.Bounds.Y {
			t.Errorf("%s label %+v is not above its control %+v", path, label.Bounds, control.Bounds)
		}
	}
	city := onboardingNodeByKey(h.root, "onb-control:weather.city")
	if city.Bounds.W != onbContentWidth(h) {
		t.Errorf("city field width = %d, want the content width %d", city.Bounds.W, onbContentWidth(h))
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	for _, unit := range []string{"celsius", "fahrenheit"} {
		button := onboardingNodeByKey(h.root, "onb-unit:"+unit)
		if button == nil || button.Action != "pick:weather.unit="+unit || button.Role != "radio" {
			t.Fatalf("unit %s = %+v, want a radio picking weather.unit", unit, button)
		}
	}
	h.setFocus(onboardingNodeByKey(h.root, "onb-unit:fahrenheit"))
	if !h.activate(reg) || h.draft.Weather.Unit != "fahrenheit" {
		t.Fatalf("draft unit = %q after choosing Fahrenheit", h.draft.Weather.Unit)
	}
	if onboardingNodeByKey(h.root, "onb-unit:fahrenheit").Fill != ui.FillSoft {
		t.Error("the chosen unit is not marked")
	}
}

func TestOnboardingRegionPlaceCardFollowsWeather(t *testing.T) {
	reg, h := openOnboardingRegion(t)
	reg.mu.Lock()
	h.draft.Weather.City = "Melbourne"
	reg.rebuildPanel(h)
	place := onboardingNodeByKey(h.root, "onb-place")
	if place == nil || !strings.Contains(treeText(place), "looked up when setup finishes") {
		reg.mu.Unlock()
		t.Fatalf("place card before lookup = %q, want the lookup line", treeText(place))
	}
	reg.cfg.Weather.City = "melbourne "
	reg.mu.Unlock()
	reg.UpdateWeather(services.Reading{Observed: true, Location: "Melbourne, Victoria, Australia", Temperature: 14, Unit: services.UnitCelsius})
	reg.mu.Lock()
	defer reg.mu.Unlock()
	text := treeText(onboardingNodeByKey(h.root, "onb-place"))
	if !strings.Contains(text, "Melbourne, Victoria, Australia") || !strings.Contains(text, "14") {
		t.Fatalf("place card after the reading = %q, want the resolved place and reading", text)
	}
	h.draft.Weather.City = "Sydney"
	reg.rebuildPanel(h)
	if text := treeText(onboardingNodeByKey(h.root, "onb-place")); strings.Contains(text, "Melbourne") {
		t.Fatalf("place card for a new city = %q, still shows the old place", text)
	}
}

func TestOnboardingIdleOffersCardsWithReasons(t *testing.T) {
	reg, h := openOnboardingAt(t, 3440, 1440, 120)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.wallsSnapshot.ServiceAvailable = true
	h.draft.Session.Locker = ""
	h.onbPage = onbIdle
	reg.rebuildPanel(h)
	for _, mode := range []string{"nothing", "screensaver", "lock"} {
		card := onboardingNodeByKey(h.root, "onb-idle:"+mode)
		if card == nil || card.Action != "pick:idle.after="+mode {
			t.Fatalf("idle %s card = %+v, want one picking idle.after", mode, card)
		}
	}
	lock := onboardingNodeByKey(h.root, "onb-idle:lock")
	if !lock.State.Has(ui.StateDisabled) || lock.Focusable {
		t.Error("Lock without a locker is not dimmed and unreachable")
	}
	if chip := onboardingNodeByKey(lock, "onb-idle-reason:lock"); chip == nil || treeText(chip) == "" {
		t.Error("the dimmed Lock card gives no reason")
	}
	if onboardingNodeByKey(h.root, "onb-idle-reason:screensaver") != nil {
		t.Error("an available action shows a reason chip")
	}
	if nothing := onboardingNodeByKey(h.root, "onb-idle:nothing"); nothing.Fill != ui.FillSoft {
		t.Error("the current idle action is not marked")
	}
	h.setFocus(lock)
	h.activate(reg)
	if settings.WhenIdleMode(h.draft.Idle.Lock, reg.wallsSnapshot.EnabledAtLogin()) == "lock" {
		t.Fatal("a dimmed card changed the idle action")
	}
	reg.idleApplying = true
	h.onbPage = onbReady
	reg.rebuildPanel(h)
	if finish := byAction(h.root, "onb-finish"); finish == nil || !finish.State.Has(ui.StateDisabled) {
		t.Error("Finish is not held while the idle action applies")
	}
	h.onbPage = onbIdle
	reg.rebuildPanel(h)
	if screensaver := onboardingNodeByKey(h.root, "onb-idle:screensaver"); !screensaver.State.Has(ui.StateDisabled) {
		t.Error("idle cards stay live while the idle action applies")
	}
}

func TestOnboardingPluginsAreTilesFourPerRow(t *testing.T) {
	reg, h := openOnboardingAt(t, 3440, 1440, 120)
	var candidates []plugin.Candidate
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("org.example.p%d", i)
		candidates = append(candidates, plugin.Candidate{Manifest: plugin.Manifest{
			ID: id, Name: fmt.Sprintf("Plugin %d", i), Widgets: []plugin.Widget{{ID: "w", Label: "Widget"}},
		}})
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	previous := reg.plugins
	reg.plugins = &pluginHost{r: reg, catalog: plugin.Catalog{Plugins: candidates}}
	t.Cleanup(func() { reg.plugins = previous })
	h.onbPluginSelections = map[string]bool{"org.example.p1": true}
	h.onbPage = onbPlugins
	reg.rebuildPanel(h)
	var tiles []*ui.Node
	for _, c := range candidates {
		tile := byAction(h.root, "onb-plugin:"+c.Manifest.ID)
		if tile == nil || tile.Kind != ui.KindButton || tile.Role != "checkbox" || !tile.Focusable {
			t.Fatalf("%s tile = %+v, want a focusable checkbox button", c.Manifest.ID, tile)
		}
		tiles = append(tiles, tile)
	}
	for i := 1; i < 4; i++ {
		if tiles[i].Bounds.Y != tiles[0].Bounds.Y || tiles[i].Bounds.W != tiles[0].Bounds.W {
			t.Errorf("tile %d bounds %+v do not share the first row with %+v", i, tiles[i].Bounds, tiles[0].Bounds)
		}
	}
	if tiles[4].Bounds.Y <= tiles[0].Bounds.Y {
		t.Errorf("fifth tile %+v did not wrap to a second row", tiles[4].Bounds)
	}
	checked := onboardingNodeByKey(tiles[1], "onb-plugin-check:org.example.p1")
	if tiles[1].Value != 1 || checked == nil || findNode(checked, func(n *ui.Node) bool { return n.Icon == "check" }) == nil {
		t.Error("the selected tile shows no check mark")
	}
	if findNode(tiles[0], func(n *ui.Node) bool { return n.Icon == "check" }) != nil || tiles[0].Value != 0 {
		t.Error("an unselected tile shows a check mark")
	}
	h.setFocus(tiles[0])
	if !h.activate(reg) || !h.onbPluginSelections["org.example.p0"] {
		t.Fatal("activating a tile did not stage its selection")
	}
}

func TestOnboardingPluginStoreEmptyStateFinishesSetup(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	reg := newPanelRegistry(t)
	setOnboardingTestConfig(t, reg, reg.cfg.Clone())
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbPlugins
	reg.rebuildPanel(h)
	open := byAction(h.root, "onb-plugin-store")
	if open == nil || !strings.Contains(treeText(h.root), "No installed bar plugins") {
		reg.mu.Unlock()
		t.Fatalf("empty Plugins step = %q, want one line and the store action", treeText(h.root))
	}
	if status := onboardingNodeByKey(h.root, "onb-status"); status != nil && status.Text != "" {
		reg.mu.Unlock()
		t.Fatalf("empty Plugins step footer = %q, want no count", status.Text)
	}
	h.setFocus(open)
	if !h.activate(reg) {
		reg.mu.Unlock()
		t.Fatal("the store action was not accepted")
	}
	if reg.panelHosts[PanelOnboarding] != nil {
		reg.mu.Unlock()
		t.Fatal("onboarding stayed open")
	}
	store := reg.panelHosts[PanelPluginStore]
	if store == nil || store.output != 7 {
		reg.mu.Unlock()
		t.Fatalf("plugin store host = %+v, want it open on output 7", store)
	}
	reg.mu.Unlock()
	state, err := onboarding.Load(onboarding.StatePath())
	if err != nil || state.Outcome != onboarding.OutcomeCompleted {
		t.Fatalf("outcome = %q (%v), want completed", state.Outcome, err)
	}
}

func TestOnboardingReviewCardsJumpToTheirStep(t *testing.T) {
	reg, h := openOnboardingAt(t, 3440, 1440, 120)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h.draft.Weather.City = "Melbourne"
	for _, tc := range []struct {
		key  string
		page onboardingPage
		text string
	}{
		{"appearance", onbAppearance, "Dark"},
		{"language", onbRegion, "Follow system"},
		{"weather", onbRegion, "Melbourne"},
		{"idle", onbIdle, "Nothing"},
		{"plugins", onbPlugins, "None"},
	} {
		h.onbPage = onbReady
		reg.rebuildPanel(h)
		card := onboardingNodeByKey(h.root, "onb-review:"+tc.key)
		if card == nil || !card.Focusable || card.Role != "button" {
			t.Fatalf("Review lacks a focusable %s card", tc.key)
		}
		if !strings.Contains(treeText(card), tc.text) {
			t.Errorf("%s card = %q, want it to show %q", tc.key, treeText(card), tc.text)
		}
		h.setFocus(card)
		if !h.activate(reg) || h.onbPage != tc.page {
			t.Errorf("%s card opened page %d, want %d", tc.key, h.onbPage, tc.page)
		}
	}
	h.onbPage = onbReady
	reg.rebuildPanel(h)
	cards := []*ui.Node{onboardingNodeByKey(h.root, "onb-review:appearance"), onboardingNodeByKey(h.root, "onb-review:language")}
	if cards[0].Bounds.Y != cards[1].Bounds.Y || cards[0].Bounds.X == cards[1].Bounds.X {
		t.Errorf("review cards %+v and %+v do not share a row", cards[0].Bounds, cards[1].Bounds)
	}
	for _, action := range []string{"onb-settings", "onb-launcher", "onb-docs"} {
		if n := byAction(h.root, action); n == nil || n.Fill != ui.FillOutline {
			t.Errorf("Review lacks the outlined %s action", action)
		}
	}
	for _, gone := range []string{"Interface language:", "Theme:", "Startup plugins:"} {
		if strings.Contains(renderText(h.root), gone) {
			t.Errorf("Review still renders the summary line %q", gone)
		}
	}
}
