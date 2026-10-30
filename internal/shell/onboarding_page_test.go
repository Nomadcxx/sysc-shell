package shell

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/onboarding"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestOnboardingPageNavigation(t *testing.T) {
	seq := []onboardingPage{onbWelcome, onbAppearance, onbRegion, onbIdle, onbPlugins, onbReady}
	for i, p := range seq {
		if i < len(seq)-1 && p.next() != seq[i+1] {
			t.Errorf("page %d next()=%d want %d", i, p.next(), seq[i+1])
		}
		if i > 0 && p.prev() != seq[i-1] {
			t.Errorf("page %d prev()=%d want %d", i, p.prev(), seq[i-1])
		}
	}
	if onbWelcome.prev() != onbWelcome {
		t.Errorf("welcome prev escaped clamp")
	}
	if onbReady.next() != onbReady {
		t.Errorf("ready next escaped clamp")
	}
	if len(onbTitles) != int(onbPages) || len(onbIntros) != int(onbPages) {
		t.Fatalf("copy arrays must hold one entry per page")
	}
	for i, title := range onbTitles {
		if title == "" || onbIntros[i] == "" {
			t.Errorf("page %d has empty copy", i)
		}
	}
}

func TestOnboardingUsesResponsiveFullOutputFrame(t *testing.T) {
	reg := newPanelRegistry(t)
	trigger := Trigger{BarEdge: "top", BarZone: 64, Align: "right", AnchorX: 1200, AnchorY: 90, OutW: 1536, OutH: 864}
	if err := reg.OpenPanel(PanelOnboarding, 7, trigger); err != nil {
		t.Fatal(err)
	}
	h := reg.panelHosts[PanelOnboarding]
	wantSize := onboardingPanelSize(trigger.OutW, trigger.OutH)
	if h.place.Panel != wantSize {
		t.Fatalf("panel size = %+v, want responsive settings size %+v", h.place.Panel, wantSize)
	}
	if !h.place.CenterY || h.place.Align != "center" || h.place.BarZone != 0 || h.place.Gap != 0 || h.place.AnchorX != 0 || h.place.AnchorY != 0 {
		t.Fatalf("onboarding kept a bar-relative anchor: %+v", h.place)
	}
	if abs(h.rect.X+h.rect.W/2-trigger.OutW/2) > 1 || abs(h.rect.Y+h.rect.H/2-trigger.OutH/2) > 1 {
		t.Fatalf("frame centre = (%d,%d), want output centre (%d,%d)", h.rect.X+h.rect.W/2, h.rect.Y+h.rect.H/2, trigger.OutW/2, trigger.OutH/2)
	}
}

func TestOnboardingPanelHeightIsCompactOnLargeOutputs(t *testing.T) {
	for _, tc := range []struct {
		name string
		w, h int
	}{
		{"compact", 800, 500},
		{"standard", 1536, 864},
		{"wide", 2560, 1440},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := newPanelRegistry(t)
			if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: tc.w, OutH: tc.h}); err != nil {
				t.Fatal(err)
			}
			want := settingsPanelSize(tc.w, tc.h)
			want.H = min(want.H, 640)
			if got := reg.panelHosts[PanelOnboarding].place.Panel; got != want {
				t.Fatalf("onboarding panel size = %+v, want compact responsive size %+v", got, want)
			}
		})
	}
}

func TestOnboardingHasUniformPillActions(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	h := reg.panelHosts[PanelOnboarding]
	if strings.Contains(strings.ToLower(onbIntroWelcome), "sysc-shell provides") {
		t.Fatalf("welcome copy still explains shell architecture: %q", onbIntroWelcome)
	}
	if onboardingNodeByKey(h.root, "onb-move-hint") == nil {
		t.Fatal("the rail does not explain how to move the setup window")
	}
	if h.rootStyle(h.theme, reg.cfg.Theme.BlurBehind).Rim != h.theme.Outline {
		t.Fatalf("floating onboarding rim = %+v, want the shell's neutral outline %+v", h.rootStyle(h.theme, reg.cfg.Theme.BlurBehind).Rim, h.theme.Outline)
	}
	var actions []*ui.Node
	for _, action := range []string{"onb-skip", "onb-back", "onb-next"} {
		n := byAction(h.root, action)
		if n == nil {
			t.Fatalf("missing navigation action %q", action)
		}
		actions = append(actions, n)
	}
	for _, n := range actions {
		if n.Shape != ui.ShapeStadium || n.Padding != h.metrics().ButtonPadding || n.Height != h.metrics().StandardControl {
			t.Errorf("%s geometry = shape %d padding %d height %d; want shared stadium/button metrics", n.Action, n.Shape, n.Padding, n.Height)
		}
	}
	if actions[0].Fill != ui.FillOutline || actions[1].Fill != ui.FillOutline {
		t.Errorf("Not now/Back fills = %d/%d, want the outlined secondary style", actions[0].Fill, actions[1].Fill)
	}
	if actions[2].Fill != ui.FillAccent {
		t.Fatalf("Next fill = %d, want the accented primary action", actions[2].Fill)
	}

	h.onbPage = onbReady
	reg.rebuildPanel(h)
	for _, action := range []string{"onb-settings", "onb-launcher", "onb-docs"} {
		n := byAction(h.root, action)
		if n == nil {
			t.Fatalf("missing ready-page action %q", action)
		}
		if n.Width != 0 {
			t.Errorf("%s explicit width = %d, want natural-width pill action", action, n.Width)
		}
		if n.Padding != h.metrics().ButtonPadding || n.Height != h.metrics().StandardControl {
			t.Errorf("%s geometry = padding %d height %d; want shared button metrics", action, n.Padding, n.Height)
		}
	}
}

func TestOnboardingRailFootHoldsMoveControls(t *testing.T) {
	_, h := openOnboardingAt(t, 1536, 864, 120)
	rail := onboardingNodeByKey(h.root, "onb-rail")
	foot := onboardingNodeByKey(h.root, "onb-rail-foot")
	hint := onboardingNodeByKey(h.root, "onb-move-hint")
	move := byAction(h.root, "onb-move")
	skip := byAction(h.root, "onb-skip")
	if rail == nil || foot == nil || hint == nil || move == nil || skip == nil {
		t.Fatal("rail is missing its foot, move hint, move action or Not now")
	}
	if move.Name != "Move window" || len(move.Children) != 1 || move.Children[0].Kind != ui.KindIcon {
		t.Fatalf("move control = name %q with %d children, want an icon button named Move window", move.Name, len(move.Children))
	}
	if got, want := foot.Bounds.Y+foot.Bounds.H, rail.Bounds.Y+rail.Bounds.H-rail.Padding; got != want {
		t.Errorf("rail foot ends at %d, want the rail's inner foot %d", got, want)
	}
	for name, n := range map[string]*ui.Node{"hint": hint, "move": move, "not now": skip} {
		if n.Bounds.Y < foot.Bounds.Y || n.Bounds.Y+n.Bounds.H > foot.Bounds.Y+foot.Bounds.H ||
			n.Bounds.X < rail.Bounds.X || n.Bounds.X+n.Bounds.W > rail.Bounds.X+rail.Bounds.W {
			t.Errorf("%s bounds %+v are outside the rail foot %+v", name, n.Bounds, foot.Bounds)
		}
	}
}

func TestOnboardingKeepsAdvancedSettingsOutOfSetup(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbAppearance
	reg.rebuildPanel(h)
	appearance := renderText(h.root)
	for _, advanced := range []string{"Theme source", "Colour palette", "Colour scheme"} {
		if strings.Contains(appearance, advanced) {
			t.Errorf("Appearance exposes advanced Settings control %q: %s", advanced, appearance)
		}
	}
	if !strings.Contains(appearance, "Dark") || !strings.Contains(appearance, "Light") || !strings.Contains(appearance, "Change") {
		t.Fatalf("Appearance is missing its basic setup controls: %s", appearance)
	}

	h.onbPage = onbIdle
	reg.rebuildPanel(h)
	idle := renderText(h.root)
	for _, advanced := range []string{"Delay", "Locker"} {
		if strings.Contains(idle, advanced) {
			t.Errorf("idle setup asks for raw configuration %q: %s", advanced, idle)
		}
	}
	if !strings.Contains(idle, "Settings") || !strings.Contains(idle, "Session") {
		t.Fatalf("idle guidance does not explain where advanced setup lives: %s", idle)
	}

	h.onbPage = onbRegion
	reg.rebuildPanel(h)
	region := renderText(h.root)
	for _, advanced := range []string{"Latitude", "Longitude"} {
		if strings.Contains(region, advanced) {
			t.Errorf("region setup exposes raw coordinates %q: %s", advanced, region)
		}
	}
	for _, basic := range []string{"Language", "City", "Unit"} {
		if !strings.Contains(region, basic) {
			t.Errorf("region setup is missing %q: %s", basic, region)
		}
	}
}

func TestOnboardingBrandFitsResponsivePanel(t *testing.T) {
	for _, tc := range []struct {
		name string
		w, h int
	}{
		{"compact", 800, 500},
		{"standard", 1536, 864},
		{"wide", 2560, 1440},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, h := openOnboardingAt(t, tc.w, tc.h, 120)
			root := h.root
			mark := onboardingNodeByKey(root, "onb-wordmark")
			container := onboardingNodeByKey(root, "onb-rail")
			wantW := onbWordmarkW
			if tc.name == "compact" {
				container = onboardingNodeByKey(root, "onb-header")
				wantW = (onbWordmarkW + 1) / 2
			}
			if mark == nil || container == nil || mark.ImageW != wantW {
				t.Fatalf("%s frame lacks a %d-wide wordmark in its rail or header", tc.name, wantW)
			}
			if mark.Bounds.X < container.Bounds.X || mark.Bounds.X+mark.Bounds.W > container.Bounds.X+container.Bounds.W {
				t.Fatalf("wordmark bounds %+v exceed %s %+v", mark.Bounds, container.Key, container.Bounds)
			}
			hint := onboardingNodeByKey(root, "onb-move-hint")
			if tc.name == "compact" {
				if hint != nil {
					t.Fatal("compact frame shows the Super-drag hint outside move mode")
				}
				return
			}
			if hint == nil || hint.Bounds.X+hint.Bounds.W > container.Bounds.X+container.Bounds.W {
				t.Fatalf("move hint %+v escapes the rail %+v", hint, container.Bounds)
			}
		})
	}
}

func TestOnboardingWrapsJapaneseCopyWithConfiguredShaper(t *testing.T) {
	cfg := config.Default()
	cfg.Session.Language = "ja"
	cfg.Accessibility.ReducedMotion = true
	reg := NewRegistry(cfg)
	keepInvalidationsDrained(t, reg)
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(reg.Close)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 800, OutH: 500}); err != nil {
		t.Fatal(err)
	}
	panel := drainAux(t, reg, 2)[1].Open
	h := reg.panelHosts[PanelOnboarding]
	if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), 150); err != nil {
		t.Fatal(err)
	}
	if h.onboardingTreeScale != 150 {
		t.Fatalf("onboarding tree scale = %d, want configured 150", h.onboardingTreeScale)
	}
	intro := onboardingNodeByKey(h.root, "onb-intro")
	if intro == nil || len(intro.Children) == 0 {
		t.Fatalf("Japanese intro = %+v, want wrapped text", intro)
	}
	if treeText(intro) == onbIntroWelcome {
		t.Fatal("onboarding intro did not use the configured Japanese locale")
	}
	measure := h.measureText()
	width := onbContentWidth(h)
	for _, line := range intro.Children {
		got, _ := measure(line.Text, ui.TextAttrsOf(line))
		if got > width {
			t.Errorf("wrapped Japanese line width = %d, want at most %d: %q", got, width, line.Text)
		}
	}
}

func TestOnboardingIntroWrapsToContentWidth(t *testing.T) {
	reg, h := openOnboardingAt(t, 2560, 1440, 120)
	reg.mu.Lock()
	h.onbPage = onbRegion
	reg.rebuildPanel(h)
	reg.mu.Unlock()
	intro := onboardingNodeByKey(h.root, "onb-intro")
	if intro == nil || intro.Kind != ui.KindColumn || intro.Width != onbContentWidth(h) || len(intro.Children) == 0 {
		t.Fatalf("page intro = %+v, want measured text within the content width", intro)
	}
	for _, line := range intro.Children {
		if line.Kind != ui.KindText || line.MaxWidth != onbContentWidth(h) {
			t.Errorf("intro line = %+v, want a text line bounded by the content width", line)
		}
	}
	content := onboardingNodeByKey(h.root, "onb-content")
	if got, want := onbContentWidth(h), content.Bounds.W-2*content.Padding; got != want {
		t.Fatalf("onbContentWidth = %d, want the laid-out content column's inner width %d", got, want)
	}
}

func TestOnboardingPaintsItsCapturedBackdrop(t *testing.T) {
	cfg := config.Default()
	cfg.Theme.BlurBehind = true
	cfg.Theme.PanelOpacity = 60
	cfg.Accessibility.ReducedMotion = true
	reg := NewRegistry(cfg)
	keepInvalidationsDrained(t, reg)
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(reg.Close)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	panel := drainAux(t, reg, 2)[1].Open
	settleHostAnimation(reg, reg.panelHosts[PanelOnboarding])
	width, height := int(panel.Width), int(panel.Height)
	if err := panel.Callbacks.Configure(width, height, 120); err != nil {
		t.Fatal(err)
	}
	paint := func() []byte {
		pixels := make([]byte, width*height*4)
		if err := panel.Callbacks.Render(pixels, width, height, width*4); err != nil {
			t.Fatal(err)
		}
		return pixels
	}
	plain := paint()
	shot := &ui.Image{Width: width, Height: height, Stride: width * 4, Pix: make([]byte, width*height*4)}
	for i := 0; i < len(shot.Pix); i += 4 {
		shot.Pix[i], shot.Pix[i+1], shot.Pix[i+2], shot.Pix[i+3] = 0, 0xff, 0, 0xff
	}
	panel.Callbacks.Backdrop(nil, shot)
	withBackdrop := paint()
	root := reg.panelHosts[PanelOnboarding].root
	if onboardingNodeByKey(root, "onb-content") == nil {
		t.Fatal("onboarding render has no content column")
	}
	// Sample the top interior gutter, above the rail card and the content.
	sampleX, sampleY := width/2, max(root.Padding/2, 1)
	index := (sampleY*width + sampleX) * 4
	if plain[index] == withBackdrop[index] && plain[index+1] == withBackdrop[index+1] &&
		plain[index+2] == withBackdrop[index+2] && plain[index+3] == withBackdrop[index+3] {
		t.Fatal("captured backdrop did not reach the clear onboarding frame gutter")
	}
}

func TestOnboardingReviewShowsLanguageWithReadableText(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbReady
	reg.rebuildPanel(h)
	language := onboardingNodeByKey(h.root, "onb-review:language")
	if language == nil || !strings.Contains(treeText(language), onboardingLocale(h)) {
		t.Fatalf("Review language card = %q, want %q", treeText(language), onboardingLocale(h))
	}
	for _, n := range findAllKind(language, ui.KindText) {
		if strings.Contains(n.Text, "Follow system") && n.Tone == ui.ToneSubtle {
			t.Error("the chosen language uses low-emphasis text")
		}
	}
}

func TestOnboardingSecondaryActionsStayCompact(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	panel := drainAux(t, reg, 2)[1].Open
	h := reg.panelHosts[PanelOnboarding]
	for _, tc := range []struct {
		page    onboardingPage
		actions []string
	}{
		{onbAppearance, []string{"onb-wallpaper"}},
		{onbReady, []string{"onb-settings", "onb-launcher", "onb-docs"}},
	} {
		h.onbPage = tc.page
		reg.rebuildPanel(h)
		if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), 120); err != nil {
			t.Fatal(err)
		}
		for _, action := range tc.actions {
			n := byAction(h.root, action)
			if n == nil {
				t.Fatalf("%s page is missing action %q", onbTitles[tc.page], action)
			}
			if n.Bounds.W >= onbContentWidth(h)/2 {
				t.Errorf("%s action width = %d, want a compact pill under half the content width", action, n.Bounds.W)
			}
		}
	}
}

func onboardingNodeByKey(root *ui.Node, key string) *ui.Node {
	if root == nil {
		return nil
	}
	if root.Key == key {
		return root
	}
	for _, child := range root.Children {
		if node := onboardingNodeByKey(child, key); node != nil {
			return node
		}
	}
	return nil
}

func onboardingStepNodes(root *ui.Node) []*ui.Node {
	var out []*ui.Node
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if strings.HasPrefix(n.Key, "onb-step-") {
			out = append(out, n)
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	return out
}

func TestOnboardingErrorStaysBesidePinnedActions(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	panel := drainAux(t, reg, 2)[1].Open
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbReady
	h.errLabel = "Settings could not be saved."
	reg.rebuildPanel(h)
	if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), 120); err != nil {
		t.Fatal(err)
	}
	root, finish := h.root, byAction(h.root, "onb-finish")
	err := onboardingNodeByKey(root, "onb-error")
	if err == nil || finish == nil {
		t.Fatalf("error or Finish action is missing: error=%+v finish=%+v", err, finish)
	}
	if got := strings.Count(treeText(root), h.errLabel); got != 1 {
		t.Fatalf("save error appears %d times; want one message beside the pinned actions", got)
	}
	footer := onboardingNodeByKey(root, "onb-footer")
	if err.Bounds.Y+err.Bounds.H > footer.Bounds.Y {
		t.Fatalf("error at %+v is not above the footer at %+v", err.Bounds, footer.Bounds)
	}
	content := onboardingNodeByKey(root, "onb-content")
	if got, want := footer.Bounds.Y+footer.Bounds.H, content.Bounds.Y+content.Bounds.H-content.Padding; got != want {
		t.Fatalf("error footer ends at %d, want pinned content end %d", got, want)
	}
}

func TestOnboardingNarrowPagesRender(t *testing.T) {
	for page := onboardingPage(0); page < onbPages; page++ {
		t.Run(onbTitles[page], func(t *testing.T) {
			reg := newPanelRegistry(t)
			if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 800, OutH: 500}); err != nil {
				t.Fatal(err)
			}
			panel := drainAux(t, reg, 2)[1].Open
			h := reg.panelHosts[PanelOnboarding]
			h.onbPage = page
			reg.rebuildPanel(h)
			if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), 120); err != nil {
				t.Fatal(err)
			}
			pixels := make([]byte, int(panel.Width*panel.Height*4))
			if err := panel.Callbacks.Render(pixels, int(panel.Width), int(panel.Height), int(panel.Width*4)); err != nil {
				t.Fatalf("render narrow page %d: %v", page, err)
			}
			if onboardingNodeByKey(h.root, "onb-rail") != nil {
				t.Fatal("narrow page retained the desktop rail")
			}
			content, footer := onboardingNodeByKey(h.root, "onb-content"), onboardingNodeByKey(h.root, "onb-footer")
			if got, want := footer.Bounds.Y+footer.Bounds.H, content.Bounds.Y+content.Bounds.H-content.Padding; got != want {
				t.Fatalf("narrow page footer ends at %d, want pinned content end %d", got, want)
			}
			if got, want := content.Bounds.Y+content.Bounds.H, h.root.Bounds.Y+h.root.Bounds.H-h.root.Padding; got != want {
				t.Fatalf("narrow content ends at %d, want the frame's inner end %d", got, want)
			}
		})
	}
}

func TestOnboardingLocalizedNarrowPagesFit(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 800, OutH: 500}); err != nil {
		t.Fatal(err)
	}
	panel := drainAux(t, reg, 2)[1].Open
	h := reg.panelHosts[PanelOnboarding]
	for _, locale := range []string{"es", "pt", "ja", "ko", "ru"} {
		h.draft.Session.Language = locale
		reg.mu.Lock()
		h.set = reg.settingsForLocked(h.draft)
		reg.mu.Unlock()
		for page := onboardingPage(0); page < onbPages; page++ {
			h.onbPage = page
			h.onbFontLocale, h.onbFontState = "", onboardingFontOff
			if page == onbRegion && (locale == "ja" || locale == "ko") {
				h.onbFontLocale, h.onbFontState = locale, onboardingFontUnavailable
			}
			reg.rebuildPanel(h)
			if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), 120); err != nil {
				t.Fatalf("configure %s page %d: %v", locale, page, err)
			}
			card := onboardingNodeByKey(h.root, "onb-content")
			if card == nil {
				t.Fatal("narrow localized page has no content column")
			}
			for _, n := range findAllKind(card, ui.KindText) {
				if n.Text == "" || n.Bounds.W == 0 {
					continue
				}
				if n.Bounds.X < card.Bounds.X || n.Bounds.X+n.Bounds.W > card.Bounds.X+card.Bounds.W {
					t.Errorf("%s page %d text %q escapes card: text=%+v card=%+v", locale, page, n.Text, n.Bounds, card.Bounds)
				}
			}
		}
		h.onbPage = onbWelcome
		h.onbMoveMode = true
		reg.rebuildPanel(h)
		if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), 120); err != nil {
			t.Fatalf("configure %s narrow move mode: %v", locale, err)
		}
		h.onbMoveMode = false
	}
}

func TestOnboardingUsesConfiguredBlurPath(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	if !reg.cfg.Theme.BlurBehind {
		t.Fatal("test configuration must enable blur-behind")
	}
	if alpha := h.rootStyle(h.theme, reg.cfg.Theme.BlurBehind).RootFill().A; alpha == 0xff {
		t.Fatalf("default onboarding root alpha = %#x; want translucent glass so blur remains visible", alpha)
	}
	moved := h.rect
	moved.X += theme.MarginL
	reg.updatePanelPlacementLocked(h, moved)
	reg.caps.Blur = false
	spec := reg.panelSpec(h, marginsFor(h.rect, h.place))
	if spec.BlurRegion == nil || *spec.BlurRegion != h.rect || spec.BlurRadius != reg.cfg.Theme.BlurRadius {
		t.Fatalf("software blur spec = %+v, want the onboarding frame and configured radius", spec)
	}
	if spec.Callbacks.OpaqueBackground {
		t.Fatal("translucent onboarding frame is advertised as opaque, hiding its captured blur")
	}
	reg.caps.Blur = true
	if shape := h.blurShape(reg); len(shape) == 0 {
		t.Fatal("compositor blur shape is empty while blur-behind is enabled")
	}
}

func TestOnboardingGlassRespectsBlurAndHighContrastSettings(t *testing.T) {
	for _, tc := range []struct {
		name            string
		blurBehind      bool
		highContrast    bool
		wantTranslucent bool
	}{
		{name: "blur enabled", blurBehind: true, wantTranslucent: true},
		{name: "blur disabled", blurBehind: false},
		{name: "high contrast", blurBehind: true, highContrast: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Theme.BlurBehind = tc.blurBehind
			cfg.Accessibility.HighContrast = tc.highContrast
			cfg.Accessibility.ReducedMotion = true
			reg := NewRegistry(cfg)
			keepInvalidationsDrained(t, reg)
			reg.caps.Blur = true
			reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
			t.Cleanup(reg.Close)
			if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
				t.Fatal(err)
			}
			h := reg.panelHosts[PanelOnboarding]
			style := h.rootStyle(h.theme, cfg.Theme.BlurBehind).WithPaper(h.paper)
			gotTranslucent := style.RootFill().A != 0xff
			if gotTranslucent != tc.wantTranslucent {
				t.Errorf("root alpha = %#x, translucent = %t; want translucent = %t", style.RootFill().A, gotTranslucent, tc.wantTranslucent)
			}
			spec := reg.panelSpec(h, marginsFor(h.rect, h.place))
			wantOpaque := !tc.wantTranslucent
			if spec.Callbacks.OpaqueBackground != wantOpaque {
				t.Errorf("opaque-region hint = %t, want %t to match the rendered root", spec.Callbacks.OpaqueBackground, wantOpaque)
			}
		})
	}
}

func TestOnboardingPagesRender(t *testing.T) {
	for page := onboardingPage(0); page < onbPages; page++ {
		t.Run(onbTitles[page], func(t *testing.T) {
			reg := newPanelRegistry(t)
			if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
				t.Fatal(err)
			}
			panel := drainAux(t, reg, 2)[1].Open
			h := reg.panelHosts[PanelOnboarding]
			settleHostAnimation(reg, h)
			reg.mu.Lock()
			h.onbPage = page
			reg.rebuildPanel(h)
			reg.mu.Unlock()
			width, height := int(panel.Width), int(panel.Height)
			if err := panel.Callbacks.Configure(width, height, 120); err != nil {
				t.Fatal(err)
			}
			reg.mu.Lock()
			root := h.root
			content, scroll, footer := onboardingNodeByKey(root, "onb-content"), onboardingNodeByKey(root, onbBodyKey), onboardingNodeByKey(root, "onb-footer")
			if content == nil || scroll == nil || footer == nil {
				reg.mu.Unlock()
				t.Fatal("onboarding page lacks its content column, body or footer")
			}
			if scroll.Bounds.H <= 0 || scroll.Bounds.Y+scroll.Bounds.H > footer.Bounds.Y {
				reg.mu.Unlock()
				t.Fatalf("page body %+v is empty or runs under the footer %+v", scroll.Bounds, footer.Bounds)
			}
			if got, want := footer.Bounds.Y+footer.Bounds.H, content.Bounds.Y+content.Bounds.H-content.Padding; got != want {
				reg.mu.Unlock()
				t.Fatalf("footer bottom = %d, want pinned content end %d", got, want)
			}
			if page == onbReady {
				docs := byAction(root, "onb-docs")
				if docs == nil || docs.Bounds.W == 0 || docs.Bounds.H == 0 {
					reg.mu.Unlock()
					t.Fatalf("documentation action bounds = %+v, want the card to lay out all children", docs)
				}
			}
			reg.mu.Unlock()
			pixels := make([]byte, width*height*4)
			if err := panel.Callbacks.Render(pixels, width, height, width*4); err != nil {
				t.Fatalf("render page %d: %v", page, err)
			}
		})
	}
}

func TestOnboardingSuperDragMovesAndClampsSurface(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	h := reg.panelHosts[PanelOnboarding]
	requests := drainAux(t, reg, 2)
	start := h.rect
	h.handle(reg)(wayland.Event{Kind: wayland.EventPointerPress, Button: 272, X: 100, Y: 120, Mods: ui.ModSuper})
	if !h.onbMoving {
		t.Fatal("Super+left press did not start moving the onboarding window")
	}
	h.handle(reg)(wayland.Event{Kind: wayland.EventPointerMotion, X: 140, Y: 150, Mods: ui.ModSuper})
	want := ui.Rect{X: start.X + 40, Y: start.Y + 30, W: start.W, H: start.H}
	if h.rect != want {
		t.Fatalf("drag moved frame to %+v, want %+v", h.rect, want)
	}
	request := <-reg.aux
	if request.Update == nil || request.ID != panelSurfaceID(PanelOnboarding) {
		t.Fatalf("drag emitted aux request %+v; want an owner-thread panel placement update", request)
	}
	if request.Update.MarginTop == nil || *request.Update.MarginTop != int32(want.Y) ||
		request.Update.MarginLeft == nil || *request.Update.MarginLeft != int32(want.X) {
		t.Fatalf("drag margins = %+v, want position (%d,%d)", request.Update, want.X, want.Y)
	}
	reg.mu.Lock()
	h.shieldQuiet = time.Time{}
	reg.mu.Unlock()
	insideMovedFrame := wayland.Event{Kind: wayland.EventPointerPress, Button: 272,
		X: float64(want.X + want.W/2), Y: float64(want.Y + want.H - 2)}
	if requests[0].Open.Callbacks.Handle(insideMovedFrame) {
		t.Fatal("shield treated a click inside the moved onboarding frame as an outside dismissal")
	}
	h.handle(reg)(wayland.Event{Kind: wayland.EventPointerMotion, X: 700, Y: 700})
	request = <-reg.aux
	if request.Update == nil {
		t.Fatalf("clamped drag emitted %+v; want another owner-thread placement update", request)
	}
	want.X = h.place.Output.W - h.place.Padding - want.W
	want.Y = h.place.Output.H - h.place.Padding - want.H
	if h.rect != want {
		t.Fatalf("clamped drag frame = %+v, want %+v", h.rect, want)
	}
	h.handle(reg)(wayland.Event{Kind: wayland.EventPointerRelease, Button: 272, X: 700, Y: 700})
	if h.onbMoving {
		t.Fatal("left release did not end the drag")
	}
	if h.rect.X < h.place.Padding || h.rect.Y < h.place.Padding ||
		h.rect.X+h.rect.W > h.place.Output.W-h.place.Padding || h.rect.Y+h.rect.H > h.place.Output.H-h.place.Padding {
		t.Fatalf("dragged frame escaped output padding: rect=%+v output=%+v", h.rect, h.place.Output)
	}
}

func TestOnboardingPlainPointerPressDoesNotStartMove(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	h := reg.panelHosts[PanelOnboarding]
	start := h.rect
	h.handle(reg)(wayland.Event{Kind: wayland.EventPointerPress, Button: 272, X: 100, Y: 120})
	h.handle(reg)(wayland.Event{Kind: wayland.EventPointerMotion, X: 140, Y: 150})
	if h.rect != start {
		t.Fatalf("plain pointer motion moved onboarding from %+v to %+v", start, h.rect)
	}
}

func TestOnboardingMoveModeSupportsKeyboardNudges(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
	if !h.activateOnboarding(reg, &ui.Node{Action: "onb-move"}) {
		reg.mu.Unlock()
		t.Fatal("Move action was not handled")
	}
	start := h.rect
	if !h.keyInput(reg, ui.KeyInput{Code: keyRight, Sym: ui.SymRight}) {
		reg.mu.Unlock()
		t.Fatal("right arrow was not handled in move mode")
	}
	want := start
	want.X += theme.MarginL
	if h.rect != want {
		reg.mu.Unlock()
		t.Fatalf("right arrow moved frame to %+v, want %+v", h.rect, want)
	}
	if !h.keyInput(reg, ui.KeyInput{Code: keyEnter, Sym: ui.SymReturn}) {
		reg.mu.Unlock()
		t.Fatal("Enter did not end move mode")
	}
	if reg.panelHosts[PanelOnboarding] != h {
		reg.mu.Unlock()
		t.Fatal("ending move mode closed onboarding")
	}
	reg.mu.Unlock()
}

func TestOnboardingMoveModePlacesBySingleClick(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 3440, OutH: 1440}); err != nil {
		t.Fatal(err)
	}
	requests := drainAux(t, reg, 2)
	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
	if !h.activateOnboarding(reg, &ui.Node{Action: "onb-move"}) {
		reg.mu.Unlock()
		t.Fatal("Move action was not handled")
	}
	h.shieldQuiet = time.Time{}
	reg.mu.Unlock()

	if !requests[0].Open.Callbacks.Handle(wayland.Event{
		Kind: wayland.EventPointerPress, Button: 272, X: 800, Y: 700,
	}) {
		t.Fatal("clicking a destination outside the frame was not handled")
	}
	reg.mu.Lock()
	moved := reg.panelHosts[PanelOnboarding]
	if moved == nil {
		reg.mu.Unlock()
		t.Fatal("destination click dismissed onboarding")
	}
	want := moved.rect
	want.X = 800 - moved.place.Panel.W/2
	want.Y = 700 - moved.place.Panel.H/2
	if moved.rect != want {
		reg.mu.Unlock()
		t.Fatalf("destination click left frame at %+v, want %+v", moved.rect, want)
	}
	reg.mu.Unlock()
}

func TestOnboardingOfferOpensOnlyOnce(t *testing.T) {
	reg := newPanelRegistry(t)
	bar := withTestBar(t, reg, 7, reg.cfg)
	bar.setOutputSize(1536, 864)
	reg.mu.Lock()
	reg.onboardingPending = true
	reg.tryOpenOnboardingLocked(7)
	first := reg.panelHosts[PanelOnboarding]
	reg.tryOpenOnboardingLocked(8)
	second := reg.panelHosts[PanelOnboarding]
	reg.mu.Unlock()
	if first == nil || first != second || first.output != 7 {
		t.Fatalf("offer opened first=%+v second=%+v; want one wizard on output 7", first, second)
	}
}

func TestOnboardingOfferWaitsForInteractiveShell(t *testing.T) {
	for _, blockedBy := range []string{"panel", "lock", "authentication"} {
		t.Run(blockedBy, func(t *testing.T) {
			reg := newPanelRegistry(t)
			switch blockedBy {
			case "panel":
				if err := reg.OpenPanel(PanelSettings, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
					t.Fatal(err)
				}
			case "lock":
				reg.lockerRunning = true
			case "authentication":
				reg.polkitHost.open_ = true
			}
			reg.mu.Lock()
			reg.onboardingPending = true
			reg.tryOpenOnboardingLocked(7)
			pending := reg.onboardingPending
			wizard := reg.panelHosts[PanelOnboarding]
			reg.mu.Unlock()
			if !pending || wizard != nil {
				t.Fatalf("pending=%v wizard=%v while blocked by %s", pending, wizard != nil, blockedBy)
			}
		})
	}
}

func TestOnboardingOfferRetriesAfterAnExistingPanelCloses(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	reg := newPanelRegistry(t)
	bar := withTestBar(t, reg, 7, reg.cfg)
	bar.setOutputSize(1536, 864)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	reg.onboardingPending = true
	reg.tryOpenOnboardingLocked(7)
	deferred := reg.onboardingPending && reg.panelHosts[PanelOnboarding] == nil
	reg.mu.Unlock()
	if !deferred {
		t.Fatal("the offer opened on top of an existing panel")
	}
	reg.ClosePanel(PanelSettings)
	reg.mu.Lock()
	wizard := reg.panelHosts[PanelOnboarding]
	reg.mu.Unlock()
	if wizard == nil || wizard.output != 7 {
		t.Fatalf("wizard after Settings closed = %+v; want it on the ready output", wizard)
	}
}

func TestOnboardingTargetWaitsForAndUsesFocusedOutput(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.mu.Lock()
	reg.focused = "DP-2"
	if output, _, ready := reg.onboardingTargetLocked(7); ready || output != 0 {
		reg.mu.Unlock()
		t.Fatalf("target without focused output = %d, ready=%v; want wait", output, ready)
	}
	reg.bars[8] = &Bar{conn: "DP-2"}
	if output, _, ready := reg.onboardingTargetLocked(7); ready || output != 0 {
		reg.mu.Unlock()
		t.Fatalf("target before focused output mode = %d, ready=%v; want wait", output, ready)
	}
	reg.bars[8].setOutputSize(1600, 900)
	output, _, ready := reg.onboardingTargetLocked(7)
	reg.mu.Unlock()
	if !ready || output != 8 {
		t.Fatalf("target with focused output = %d, ready=%v; want 8", output, ready)
	}
}

func TestOnboardingTargetUsesLowestReadyOutputWhenFocusIsUnknown(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.mu.Lock()
	reg.bars[8] = &Bar{conn: "DP-2"}
	reg.bars[8].setOutputSize(1600, 900)
	reg.bars[7] = &Bar{conn: "DP-1"}
	reg.bars[7].setOutputSize(3440, 1440)
	output, trigger, ready := reg.onboardingTargetLocked(0)
	reg.mu.Unlock()
	if !ready || output != 7 || trigger.OutW != 3440 || trigger.OutH != 1440 {
		t.Fatalf("fallback target = %d, %+v, ready=%v; want lowest ready output 7 at 3440x1440", output, trigger, ready)
	}
}

func TestOnboardingOfferOpensOnReadyFocusedOutput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	reg := newPanelRegistry(t)
	reg.UpdateNiri(niri.Snapshot{FocusedOutput: "DP-2"})
	if _, err := reg.NewHost(7, "DP-1"); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	if reg.panelHosts[PanelOnboarding] != nil || !reg.onboardingPending {
		reg.mu.Unlock()
		t.Fatal("the offer opened before the focused output was ready")
	}
	reg.mu.Unlock()
	host, err := reg.NewHost(8, "DP-2")
	if err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
	if h != nil || !reg.onboardingPending {
		reg.mu.Unlock()
		t.Fatalf("wizard opened before output dimensions arrived: host=%+v pending=%v", h, reg.onboardingPending)
	}
	reg.mu.Unlock()
	if host.OutputSize == nil {
		t.Fatal("output host has no size callback")
	}
	host.OutputSize(3440, 1440)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h = reg.panelHosts[PanelOnboarding]
	if h == nil || h.output != 8 {
		t.Fatalf("wizard output = %+v; want focused output 8", h)
	}
	if h.place.Output.W != 3440 || h.place.Output.H != 1440 {
		t.Fatalf("wizard output size = %+v; want the real 3440x1440 mode", h.place.Output)
	}
	if abs(h.rect.X+h.rect.W/2-1720) > 1 || abs(h.rect.Y+h.rect.H/2-720) > 1 {
		t.Fatalf("wizard frame = %+v; want centred in the 3440x1440 output", h.rect)
	}
}

func TestOnboardingOfferCentersOnActualFallbackOutput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	reg := newPanelRegistry(t)
	host, err := reg.NewHost(7, "DP-1")
	if err != nil {
		t.Fatal(err)
	}
	if host.OutputSize == nil {
		t.Fatal("output host has no size callback")
	}
	host.OutputSize(3440, 1440)

	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	if h == nil || h.output != 7 {
		t.Fatalf("wizard output = %+v; want fallback output 7", h)
	}
	if h.place.Output.W != 3440 || h.place.Output.H != 1440 {
		t.Fatalf("wizard output size = %+v; want the real 3440x1440 mode", h.place.Output)
	}
	if abs(h.rect.X+h.rect.W/2-1720) > 1 || abs(h.rect.Y+h.rect.H/2-720) > 1 {
		t.Fatalf("wizard frame = %+v; want centred in the 3440x1440 output", h.rect)
	}
}

func TestOnboardingOutputLossRequeuesOfferAndPage(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	reg := newPanelRegistry(t)
	attachOnboardingTestPlugins(t, reg)
	reg.mu.Lock()
	reg.onboardingPending = false
	reg.mu.Unlock()
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbPlugins
	reg.rebuildPanel(h)
	toggle := byAction(h.root, "onb-plugin:org.example.timer")
	if toggle == nil {
		reg.mu.Unlock()
		t.Fatal("Startup plugins page did not render the installed bar plugin")
	}
	h.setFocus(toggle)
	if !h.activate(reg) {
		reg.mu.Unlock()
		t.Fatal("plugin selection was not accepted")
	}
	h.onbPage = onbRegion
	h.draft.Weather.City = "Melbourne"
	reg.closePanelsOnOutputLocked(7)
	pending := reg.onboardingPending
	reg.mu.Unlock()
	if !pending {
		t.Fatal("output loss consumed the first-start offer")
	}
	state, err := onboarding.Load(onboarding.StatePath())
	if err != nil || !state.Unseen() {
		t.Fatalf("output loss recorded state %+v, err %v; want unseen", state, err)
	}

	if err := reg.OpenPanel(PanelOnboarding, 8, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	resumed := reg.panelHosts[PanelOnboarding]
	if resumed.onbPage != onbRegion || resumed.draft.Weather.City != "Melbourne" {
		t.Fatalf("resumed page=%d city=%q; want region and preserved draft", resumed.onbPage, resumed.draft.Weather.City)
	}
	if onboardingContainsString(resumed.draft.Plugins.Enabled, "org.example.timer") ||
		hasPluginPlacement(resumed.draft.Bar, "org.example.timer", "clock") {
		t.Fatal("resuming onboarding copied the staged plugin selection into the config draft")
	}
	resumed.onbPage = onbPlugins
	reg.rebuildPanel(resumed)
	if toggle := byAction(resumed.root, "onb-plugin:org.example.timer"); toggle == nil || toggle.Value == 0 {
		t.Fatal("resuming onboarding lost the staged plugin selection")
	}
}

func TestOnboardingResumesAfterAnotherModalReplacesItsRoot(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	reg := newPanelRegistry(t)
	bar := withTestBar(t, reg, 7, reg.cfg)
	bar.setOutputSize(1536, 864)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}

	reg.mu.Lock()
	wizard := reg.panelHosts[PanelOnboarding]
	wizard.onbPage = onbRegion
	wizard.draft.Weather.City = "Melbourne"
	reg.windowSwitcher = newWindowSwitcherHost(reg)
	reg.windowSwitcher.openLocked(7, "DP-1", windowSwitcherModel{})
	if reg.panelHosts[PanelOnboarding] != nil {
		reg.mu.Unlock()
		t.Fatal("replacing the root left onboarding's panel host open")
	}
	reg.windowSwitcher.closeLocked()
	resumed := reg.panelHosts[PanelOnboarding]
	if resumed == nil {
		reg.mu.Unlock()
		t.Fatal("onboarding did not resume when the replacing modal closed")
	}
	if resumed.onbPage != onbRegion || resumed.draft.Weather.City != "Melbourne" {
		reg.mu.Unlock()
		t.Fatalf("resumed page=%d city=%q; want region and preserved draft", resumed.onbPage, resumed.draft.Weather.City)
	}
	reg.mu.Unlock()
}

func TestOnboardingIdlePageUsesSharedIdleEntries(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	if h == nil {
		t.Fatal("the wizard did not host")
	}
	h.onbPage = onbIdle
	reg.rebuildPanel(h)
	if byAction(h.root, "pick:idle.after=nothing") == nil {
		t.Fatal("the idle page lost the shared idle.after control")
	}
	lock := byAction(h.root, "pick:idle.after=lock")
	if lock == nil {
		t.Fatal("the idle page lost the lock option")
	}
	if lock.State&ui.StateDisabled == 0 {
		t.Fatal("lock stayed selectable with no locker command")
	}
}

func TestOnboardingReopensFromSessionSettings(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.mu.Lock()
	reg.cfg.Session.Language = "ja"
	reg.mu.Unlock()
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if err := reg.selectPanelSectionLocked(PanelSettings, "Session"); err != nil {
		t.Fatal(err)
	}
	h := reg.panelHosts[PanelSettings]
	reopen := byAction(h.root, "onboarding:open")
	if reopen == nil {
		t.Fatal("the Session section lost the reopen action")
	}
	if reopen.Name == "Run setup again" {
		t.Fatal("the Settings reopen action did not follow the selected UI language")
	}
	h.setFocus(reopen)
	if !h.activate(reg) {
		t.Fatal("the reopen action was not accepted")
	}
	if !reg.panelOpenLocked(PanelOnboarding) {
		t.Fatal("reopen did not open the wizard")
	}
	if reg.panelHosts[PanelSettings] != nil {
		t.Fatal("the wizard left Settings open")
	}
}

func TestOnboardingOpensOnPrimaryAndReusesWallpaperPicker(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	if h == nil {
		t.Fatal("the wizard did not host")
	}
	f := h.focused()
	if f == nil || f.Name != onbStartLabel {
		t.Fatalf("initial focus = %v, want %q", f, onbStartLabel)
	}
	h.onbPage = onbAppearance
	reg.rebuildPanel(h)
	pick := byAction(h.root, "onb-wallpaper")
	if pick == nil {
		t.Fatal("the appearance page lost the wallpaper picker action")
	}
	h.setFocus(pick)
	if !h.activate(reg) {
		t.Fatal("the picker action was not accepted")
	}
	if !reg.panelOpenLocked(PanelWallpaper) {
		t.Fatal("the shared picker did not open")
	}
	if !reg.panelOpenLocked(PanelOnboarding) {
		t.Fatal("the wizard closed underneath the picker")
	}
}

func TestOnboardingActionRowHierarchyAndKeptFocus(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	skip := byAction(h.root, "onb-skip")
	back := byAction(h.root, "onb-back")
	next := byAction(h.root, "onb-next")
	if skip == nil || back == nil || next == nil {
		t.Fatal("the action row lost a control")
	}
	if skip.Fill != ui.FillOutline || back.Fill != ui.FillOutline {
		t.Fatal("secondary actions are not visually subordinate")
	}
	if next.Fill != ui.FillAccent {
		t.Fatal("the primary action lost its accent fill")
	}
	for _, p := range []onboardingPage{onbAppearance, onbRegion, onbIdle, onbPlugins, onbReady} {
		nav := byAction(h.root, "onb-next")
		if nav == nil {
			t.Fatalf("the Next action vanished before page %d", p)
		}
		h.setFocus(nav)
		if !h.activate(reg) {
			t.Fatalf("Next rejected on page %d", p)
		}
		if h.onbPage != p {
			t.Fatalf("advanced to %d, want %d", h.onbPage, p)
		}
		if f := h.focused(); f == nil || f.Name != onbMainLabel(p) {
			t.Errorf("page %d focus=%v, want %q", p, f, onbMainLabel(p))
		}
	}
}

func TestOnboardingProgressAndDismissalStayClear(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	progress := onboardingNodeByKey(h.root, "onb-eyebrow")
	if progress == nil || progress.Text != "STEP 01 OF 06" || progress.TextRole != theme.RoleMono || progress.Tone != ui.ToneAccent {
		t.Fatalf("progress = %+v, want the mono accent STEP 01 OF 06 eyebrow", progress)
	}
	for p := onboardingPage(0); p < onbPages; p++ {
		h.onbPage = p
		reg.rebuildPanel(h)
		skip := byAction(h.root, "onb-skip")
		if skip == nil || skip.Name != "Not now" || skip.Tone == ui.ToneSubtle {
			t.Fatalf("page %d dismissal = %+v, want a clearly visible Not now action", p, skip)
		}
	}
}

func TestOnboardingRegionLocalizesAndOffersLanguagePreference(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbRegion
	for _, locale := range []string{"es", "pt", "ja", "ko", "ru"} {
		h.draft.Session.Language = locale
		h.set = reg.settingsForLocked(h.draft)
		reg.rebuildPanel(h)
		wantTitle := h.tr("onboarding.title.region", onbTitleRegion)
		if wantTitle == onbTitleRegion || findText(h.root, wantTitle) == nil {
			t.Errorf("region title for %s = %q in tree %q, want translated title", locale, wantTitle, treeText(h.root))
		}
		progress := onboardingNodeByKey(h.root, "onb-eyebrow")
		if progress == nil || progress.Role != "status" || progress.Name != progress.Text || !strings.Contains(progress.Text, "03") || progress.Text == "STEP 03 OF 06" {
			t.Errorf("progress accessibility for %s = %+v; want a status naming step 03", locale, progress)
		}
		language := h.set.ByPath("session.language")
		if language == nil || !strings.Contains(treeText(h.root), language.Label) {
			t.Errorf("Region tree for %s does not expose the localized language preference: %q", locale, treeText(h.root))
			continue
		}
		city := h.set.ByPath("weather.city")
		if city != nil && strings.Index(treeText(h.root), language.Label) > strings.Index(treeText(h.root), city.Label) {
			t.Errorf("language preference for %s appears after weather location", locale)
		}
	}
}

func TestOnboardingIdlePageUsesEffectiveWallsValues(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.mu.Lock()
	reg.cfg.Session.Locker = "sysc-lock"
	reg.wallsSnapshot = enabledAtLoginSnapshot()
	reg.mu.Unlock()
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	h.draft.Session.Locker = "sysc-lock"
	h.set = reg.settingsForLocked(h.draft)
	h.onbPage = onbIdle
	reg.rebuildPanel(h)
	if card := onboardingNodeByKey(h.root, "onb-idle:screensaver"); card == nil || card.Fill != ui.FillSoft {
		t.Fatal("the Screensaver card is not marked current from live walls state")
	}
}

func TestOnboardingShowsPersistenceError(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	h.errLabel = "configuration write failed"
	reg.rebuildPanel(h)
	if !strings.Contains(treeText(h.root), h.errLabel) {
		t.Fatalf("onboarding tree %q hides persistence error %q", treeText(h.root), h.errLabel)
	}
}

func TestOnboardingFinishDoesNotCompleteWhenDraftWriteFails(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	reg := newPanelRegistry(t)
	badPath := t.TempDir()
	reg.BindPersist(badPath, nil) // A directory cannot be replaced by the config file.
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbReady
	h.draft.Weather.City = "Updated city"
	h.writeTimer = time.AfterFunc(time.Hour, func() {})
	reg.rebuildPanel(h)
	finish := byAction(h.root, "onb-finish")
	if finish == nil {
		reg.mu.Unlock()
		t.Fatal("Ready page lost Finish")
	}
	h.setFocus(finish)
	if !h.activate(reg) {
		reg.mu.Unlock()
		t.Fatal("Finish was not accepted")
	}
	open := reg.panelOpenLocked(PanelOnboarding)
	errText := h.errLabel
	reg.mu.Unlock()
	state, err := onboarding.Load(onboarding.StatePath())
	if err != nil {
		t.Fatal(err)
	}
	if !open || !state.Unseen() || errText == "" {
		t.Fatalf("failed finish closed=%v state=%+v error=%q; want open, unseen, visible error", !open, state, errText)
	}
}

func TestOnboardingReadyOffersDirectSettingsActions(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbReady
	reg.rebuildPanel(h)
	if byAction(h.root, "onb-settings") == nil || onboardingNodeByKey(h.root, "onb-review:appearance") == nil ||
		byAction(h.root, "onb-launcher") == nil || byAction(h.root, "onb-docs") == nil {
		t.Fatalf("Ready tree lacks direct Settings or appearance actions: %q", treeText(h.root))
	}
	if got := byAction(h.root, "onb-settings").Width; got != 0 {
		t.Fatalf("Ready action width = %d, want a natural-width pill", got)
	}
}

func TestOnboardingDismissalPreservesPriorCompletion(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	statePath := onboarding.StatePath()
	if err := onboarding.Mark(statePath, onboarding.OutcomeCompleted); err != nil {
		t.Fatal(err)
	}
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
	h.setFocus(byAction(h.root, "onb-skip"))
	if !h.activate(reg) {
		reg.mu.Unlock()
		t.Fatal("Not now was not accepted")
	}
	reg.mu.Unlock()
	state, err := onboarding.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.Outcome != onboarding.OutcomeCompleted {
		t.Fatalf("dismissal changed prior completion to %q", state.Outcome)
	}
}

func TestOnboardingEscapeAndOutsideClickDismiss(t *testing.T) {
	for _, method := range []string{"escape", "outside click"} {
		t.Run(method, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			reg := newPanelRegistry(t)
			if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
				t.Fatal(err)
			}
			switch method {
			case "escape":
				reg.mu.Lock()
				closed := reg.panelHosts[PanelOnboarding].keyInput(reg, ui.KeyInput{Code: keyEsc, Sym: ui.SymEscape})
				open := reg.panelHosts[PanelOnboarding] != nil
				reg.mu.Unlock()
				if !closed || open {
					t.Fatalf("Escape handled=%v panel-open=%v; want handled and closed", closed, open)
				}
			case "outside click":
				requests := drainAux(t, reg, 2)
				reg.mu.Lock()
				h := reg.panelHosts[PanelOnboarding]
				h.shieldQuiet = time.Time{}
				body := h.rect
				reg.mu.Unlock()
				outside := wayland.Event{Kind: wayland.EventPointerPress, Button: 272,
					X: float64(body.X + body.W + 5), Y: float64(body.Y + body.H/2)}
				if !requests[0].Open.Callbacks.Handle(outside) {
					t.Fatal("outside click did not close onboarding")
				}
				reg.mu.Lock()
				open := reg.panelHosts[PanelOnboarding] != nil
				reg.mu.Unlock()
				if open {
					t.Fatal("outside click left onboarding open")
				}
			}
			state, err := onboarding.Load(onboarding.StatePath())
			if err != nil {
				t.Fatal(err)
			}
			if state.Outcome != onboarding.OutcomeDismissed {
				t.Fatalf("%s outcome = %q, want dismissed", method, state.Outcome)
			}
		})
	}
}

func TestOnboardingExplicitCloseRecordsDismissed(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.ClosePanel(PanelOnboarding)

	state, err := onboarding.Load(onboarding.StatePath())
	if err != nil {
		t.Fatal(err)
	}
	if state.Outcome != onboarding.OutcomeDismissed {
		t.Fatalf("explicit close outcome = %q, want dismissed", state.Outcome)
	}
}

func TestOnboardingStartupPluginsPageListsCurrentSelection(t *testing.T) {
	reg := newPanelRegistry(t)
	cfg := reg.cfg.Clone()
	pluginID := "org.example.timer"
	cfg.Plugins.Enabled = []string{pluginID}
	cfg.Bar.Right = append(cfg.Bar.Right, config.Item{ID: "plugin", Plugin: pluginID, Entry: "clock", Instance: "timer-1"})
	setOnboardingTestConfig(t, reg, cfg)
	attachOnboardingTestPlugins(t, reg)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbReady.prev()
	reg.rebuildPanel(h)
	if onbPages != 6 {
		t.Fatalf("page count = %d, want six including Startup plugins", onbPages)
	}
	toggle := byAction(h.root, "onb-plugin:org.example.timer")
	if toggle == nil || toggle.Kind != ui.KindButton || toggle.Role != "checkbox" || toggle.Value == 0 || !toggle.Focusable || toggle.Name != "Place Timer on the bar" {
		t.Fatalf("installed, enabled and placed plugin control = %+v", toggle)
	}
	if onboardingNodeByKey(toggle, "onb-plugin-check:org.example.timer") == nil || !strings.Contains(treeText(toggle), "Bar widget: Clock") {
		t.Fatalf("plugin tile did not show its check mark and default widget: %q", treeText(toggle))
	}
	if byAction(h.root, "onb-plugin:org.example.panel-only") != nil ||
		byAction(h.root, "onb-plugin:org.example.missing-command") != nil {
		t.Fatal("onboarding listed a plugin without a startable bar widget")
	}
}

func TestOnboardingPluginSelectionIsStagedUntilFinish(t *testing.T) {
	reg := newPanelRegistry(t)
	cfg := reg.cfg.Clone()
	cfg.Plugins = config.Plugins{
		Enabled:  []string{"org.example.other"},
		Settings: map[string]map[string]any{"org.example.other": {"theme": "blue"}},
	}
	cfg.Bar.Right = append(cfg.Bar.Right, config.Item{ID: "plugin", Plugin: "org.example.keep", Entry: "status", Instance: "keep-1"})
	cfg.Outputs = []config.OutputOverride{{Connector: "DP-2", Bar: cfg.Bar.Clone()}}
	configPath := setOnboardingTestConfig(t, reg, cfg)
	attachOnboardingTestPlugins(t, reg)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbReady.prev()
	reg.rebuildPanel(h)
	toggle := byAction(h.root, "onb-plugin:org.example.timer")
	if toggle == nil {
		reg.mu.Unlock()
		t.Fatal("Startup plugins page did not render the installed bar plugin")
	}
	h.setFocus(toggle)
	if !h.activate(reg) {
		reg.mu.Unlock()
		t.Fatal("plugin selection was not accepted")
	}
	if onboardingContainsString(h.draft.Plugins.Enabled, "org.example.timer") ||
		hasPluginPlacement(h.draft.Bar, "org.example.timer", "clock") ||
		hasPluginPlacement(h.draft.Outputs[0].Bar, "org.example.timer", "clock") {
		reg.mu.Unlock()
		t.Fatal("selecting a plugin mutated the config draft before Finish")
	}
	toggle = byAction(h.root, "onb-plugin:org.example.timer")
	if toggle == nil || toggle.Value == 0 || onboardingNodeByKey(toggle, "onb-plugin-check:org.example.timer").Fill != ui.FillAccent {
		reg.mu.Unlock()
		t.Fatal("staged plugin selection was not reflected in the page")
	}
	if h.draft.Plugins.Settings["org.example.other"]["theme"] != "blue" ||
		!reflect.DeepEqual(reg.cfg.Plugins.Enabled, cfg.Plugins.Enabled) {
		reg.mu.Unlock()
		t.Fatal("plugin selection changed unrelated plugin settings or live config")
	}
	if saved, err := config.LoadRequired(configPath); err != nil {
		reg.mu.Unlock()
		t.Fatal(err)
	} else if onboardingContainsString(saved.Plugins.Enabled, "org.example.timer") ||
		hasPluginPlacement(saved.Bar, "org.example.timer", "clock") {
		reg.mu.Unlock()
		t.Fatal("plugin selection was written before Finish")
	}
	reg.mu.Unlock()

	reg.mu.Lock()
	h = reg.panelHosts[PanelOnboarding]
	toggle = byAction(h.root, "onb-plugin:org.example.timer")
	if toggle == nil || toggle.Value == 0 {
		reg.mu.Unlock()
		t.Fatal("selected plugin toggle did not reflect the draft")
	}
	h.setFocus(toggle)
	if !h.activate(reg) {
		reg.mu.Unlock()
		t.Fatal("plugin deselection was not accepted")
	}
	if onboardingContainsString(h.draft.Plugins.Enabled, "org.example.timer") ||
		hasPluginPlacement(h.draft.Bar, "org.example.timer", "clock") ||
		hasPluginPlacement(h.draft.Outputs[0].Bar, "org.example.timer", "clock") {
		reg.mu.Unlock()
		t.Fatal("deselecting a plugin mutated the config draft before Finish")
	}
	toggle = byAction(h.root, "onb-plugin:org.example.timer")
	if toggle == nil || toggle.Value != 0 || onboardingNodeByKey(toggle, "onb-plugin-check:org.example.timer").Fill == ui.FillAccent {
		reg.mu.Unlock()
		t.Fatal("staged plugin deselection was not reflected in the page")
	}
	if !reflect.DeepEqual(h.draft.Bar.Right, cfg.Bar.Right) || !reflect.DeepEqual(h.draft.Outputs[0].Bar.Right, cfg.Outputs[0].Bar.Right) {
		reg.mu.Unlock()
		t.Fatal("deselecting a plugin changed unrelated bar items")
	}
	reg.mu.Unlock()
}

func TestOnboardingReadyActionsPersistOrDiscardPluginSelection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		action   string
		complete bool
		opens    bool
		target   PanelID
	}{
		{name: "finish", action: "onb-finish", complete: true},
		{name: "dismiss", action: "onb-skip"},
		{name: "settings", action: "onb-settings", complete: true, opens: true, target: PanelSettings},
		{name: "launcher", action: "onb-launcher", complete: true, opens: true, target: PanelLauncher},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			reg := newPanelRegistry(t)
			cfg := reg.cfg.Clone()
			cfg.Plugins.Settings = map[string]map[string]any{"org.example.keep": {"theme": "blue"}}
			configPath := setOnboardingTestConfig(t, reg, cfg)
			attachOnboardingTestPlugins(t, reg)
			if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
				t.Fatal(err)
			}
			reg.mu.Lock()
			h := reg.panelHosts[PanelOnboarding]
			h.onbPage = onbReady.prev()
			reg.rebuildPanel(h)
			toggle := byAction(h.root, "onb-plugin:org.example.timer")
			if toggle == nil {
				reg.mu.Unlock()
				t.Fatal("Startup plugins page did not render the installed bar plugin")
			}
			h.setFocus(toggle)
			if !h.activate(reg) {
				reg.mu.Unlock()
				t.Fatal("plugin selection was not accepted")
			}
			h.draft.Weather.Configured = true
			h.draft.Weather.City = "Pending city"
			timer := time.AfterFunc(time.Hour, func() {})
			h.writeTimer = timer
			t.Cleanup(func() { timer.Stop() })
			if tc.action != "onb-skip" {
				h.onbPage = onbReady
				reg.rebuildPanel(h)
			}
			h.setFocus(byAction(h.root, tc.action))
			if !h.activate(reg) {
				reg.mu.Unlock()
				t.Fatalf("%s was not accepted", tc.name)
			}
			if tc.opens && !reg.panelOpenLocked(tc.target) {
				reg.mu.Unlock()
				t.Fatalf("%s action did not open %s", tc.name, tc.target)
			}
			if tc.opens && reg.panelHosts[PanelOnboarding] != nil {
				reg.mu.Unlock()
				t.Fatalf("%s action left onboarding open", tc.name)
			}
			reg.mu.Unlock()

			saved, err := config.LoadRequired(configPath)
			if err != nil {
				t.Fatal(err)
			}
			gotEnabled := onboardingContainsString(saved.Plugins.Enabled, "org.example.timer")
			gotPlaced := hasPluginPlacement(saved.Bar, "org.example.timer", "clock")
			if gotEnabled != tc.complete || gotPlaced != tc.complete {
				t.Fatalf("%s saved plugin enabled=%v placed=%v, want both %v", tc.name, gotEnabled, gotPlaced, tc.complete)
			}
			if saved.Plugins.Settings["org.example.keep"]["theme"] != "blue" {
				t.Fatal("saving plugin selection changed unrelated plugin settings")
			}
			if saved.Weather.City != "Pending city" {
				t.Fatalf("%s saved weather city %q, want Pending city", tc.name, saved.Weather.City)
			}
			weatherCount := onboardingWeatherItemCount(saved.Bar)
			if weatherCount != 1 {
				t.Fatalf("%s saved weather placements = %d, want 1", tc.name, weatherCount)
			}
			state, err := onboarding.Load(onboarding.StatePath())
			if err != nil {
				t.Fatal(err)
			}
			wantOutcome := onboarding.OutcomeDismissed
			if tc.complete {
				wantOutcome = onboarding.OutcomeCompleted
			}
			if state.Outcome != wantOutcome {
				t.Fatalf("%s outcome = %q, want %q", tc.name, state.Outcome, wantOutcome)
			}
		})
	}
}

func TestOnboardingWeatherCityAddsBarPlacement(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	reg := newPanelRegistry(t)
	cfg := reg.cfg.Clone()
	cfg.Outputs = []config.OutputOverride{{Connector: "DP-2", Bar: cfg.Bar.Clone()}}
	configPath := setOnboardingTestConfig(t, reg, cfg)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}

	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
	if h == nil || h.set == nil {
		reg.mu.Unlock()
		t.Fatal("onboarding settings were not initialized")
	}
	city := h.set.ByPath("weather.city")
	if city == nil {
		reg.mu.Unlock()
		t.Fatal("weather city setting is missing")
	}
	h.commitSetting(reg, city, "Melbourne")
	h.commitSetting(reg, city, "Brisbane")
	if err := reg.finishOnboardingLocked(h); err != nil {
		reg.mu.Unlock()
		t.Fatal(err)
	}
	reg.mu.Unlock()

	saved, err := config.LoadRequired(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Weather.City != "Brisbane" {
		t.Fatalf("saved weather city = %q, want Brisbane", saved.Weather.City)
	}
	if len(saved.Outputs) != 1 {
		t.Fatalf("saved output overrides = %d, want 1", len(saved.Outputs))
	}
	for _, tc := range []struct {
		name string
		bar  config.Bar
	}{{name: "base", bar: saved.Bar}, {name: "output", bar: saved.Outputs[0].Bar}} {
		count := onboardingWeatherItemCount(tc.bar)
		if count != 1 {
			t.Errorf("%s bar weather placements = %d, want 1", tc.name, count)
		}
	}
}

func TestOnboardingWeatherPlacementKeepsNestedOutputItem(t *testing.T) {
	cfg := config.Default()
	cfg.Outputs = []config.OutputOverride{{
		Connector: "DP-2",
		Bar:       config.Bar{Center: []config.Item{{ID: "group", Items: []config.Item{{ID: "weather"}}}}},
	}}

	for range 2 {
		if err := onbEnsureWeatherPlacement(&cfg); err != nil {
			t.Fatal(err)
		}
	}
	if got := onboardingWeatherItemCount(cfg.Bar); got != 1 {
		t.Fatalf("base bar weather placements = %d, want 1", got)
	}
	if got := onboardingWeatherItemCount(cfg.Outputs[0].Bar); got != 1 {
		t.Fatalf("output bar weather placements = %d, want 1", got)
	}
}

func TestOnboardingClearedWeatherCityDoesNotAddBarPlacement(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	reg := newPanelRegistry(t)
	configPath := setOnboardingTestConfig(t, reg, reg.cfg.Clone())
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}

	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
	if h == nil || h.set == nil {
		reg.mu.Unlock()
		t.Fatal("onboarding settings were not initialized")
	}
	city := h.set.ByPath("weather.city")
	if city == nil {
		reg.mu.Unlock()
		t.Fatal("weather city setting is missing")
	}
	h.commitSetting(reg, city, "Melbourne")
	h.commitSetting(reg, h.set.ByPath("weather.city"), "")
	if err := reg.finishOnboardingLocked(h); err != nil {
		reg.mu.Unlock()
		t.Fatal(err)
	}
	reg.mu.Unlock()

	saved, err := config.LoadRequired(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Weather.City != "" {
		t.Fatalf("saved weather city = %q, want empty", saved.Weather.City)
	}
	if count := onboardingWeatherItemCount(saved.Bar); count != 0 {
		t.Fatalf("cleared city saved %d weather placements, want 0", count)
	}
}

func onboardingWeatherItemCount(bar config.Bar) int {
	count := 0
	for _, ref := range config.BarItemRefs(bar) {
		if item := bar.ItemAt(ref); item != nil && item.ID == "weather" {
			count++
		}
	}
	return count
}

func TestOnboardingPluginSelectionIsIdempotentAndRemovesNestedItems(t *testing.T) {
	pluginID := "org.example.timer"
	manifest := plugin.Manifest{ID: pluginID, Widgets: []plugin.Widget{{ID: "clock"}}}
	cfg := config.Default()
	cfg.Outputs = []config.OutputOverride{{Connector: "DP-2", Bar: cfg.Bar.Clone()}}
	selected, err := onbSetPluginSelected(cfg, manifest, true)
	if err != nil {
		t.Fatal(err)
	}
	for name, bar := range map[string]config.Bar{"shared": selected.Bar, "DP-2": selected.Outputs[0].Bar} {
		if !reflect.DeepEqual(bar.Right[:len(cfg.Bar.Right)], cfg.Bar.Right) ||
			countPluginPlacements(bar, pluginID) != countPluginPlacements(cfg.Bar, pluginID)+1 {
			t.Errorf("%s bar selection changed existing order or did not add one placement: %+v", name, bar.Right)
		}
	}
	repeated, err := onbSetPluginSelected(selected, manifest, true)
	if err != nil {
		t.Fatal(err)
	}
	for name, bar := range map[string]config.Bar{"shared": repeated.Bar, "DP-2": repeated.Outputs[0].Bar} {
		if got := countPluginPlacements(bar, pluginID); got != 1 {
			t.Errorf("%s bar has %d placements after repeated selection, want one", name, got)
		}
	}
	if !reflect.DeepEqual(selected.Bar.Right, repeated.Bar.Right) {
		t.Fatal("repeated selection added a duplicate bar placement")
	}

	nested := selected.Clone()
	nested.Bar.Center = []config.Item{{ID: "group", Items: []config.Item{
		{ID: "clock", Format: "15:04"},
		{ID: "plugin", Plugin: pluginID, Entry: "clock", Instance: "timer-2"},
	}}}
	nested.Bar.Left = []config.Item{{ID: "group", Items: []config.Item{
		{ID: "plugin", Plugin: pluginID, Entry: "clock", Instance: "timer-3"},
	}}}
	nested.Plugins.Enabled = append(nested.Plugins.Enabled, "org.example.other")
	cleared, err := onbSetPluginSelected(nested, manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	if onbBarHasPluginPlacement(cleared.Bar, pluginID) || onbBarHasPluginPlacement(cleared.Outputs[0].Bar, pluginID) {
		t.Fatal("deselection kept a nested plugin placement")
	}
	if len(cleared.Bar.Center) != 1 || cleared.Bar.Center[0].ID != "group" || len(cleared.Bar.Center[0].Items) != 1 || cleared.Bar.Center[0].Items[0].ID != "clock" {
		t.Fatalf("deselection did not preserve the other group member: %+v", cleared.Bar.Center)
	}
	if len(cleared.Bar.Left) != 0 {
		t.Fatalf("deselection kept an empty group: %+v", cleared.Bar.Left)
	}
	if !reflect.DeepEqual(nested.Bar.Center[0].Items[1], config.Item{ID: "plugin", Plugin: pluginID, Entry: "clock", Instance: "timer-2"}) {
		t.Fatal("deselection mutated its input config")
	}
	if len(cleared.Plugins.Enabled) != 1 || cleared.Plugins.Enabled[0] != "org.example.other" {
		t.Fatalf("enabled plugins after deselection = %v", cleared.Plugins.Enabled)
	}
}

func setOnboardingTestConfig(t *testing.T, reg *Registry, cfg config.Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	reg.cfg = cfg
	reg.BindPersist(path, nil)
	return path
}

func attachOnboardingTestPlugins(t *testing.T, reg *Registry) {
	t.Helper()
	previous := reg.plugins
	reg.plugins = &pluginHost{r: reg, catalog: plugin.Catalog{Plugins: []plugin.Candidate{
		{Manifest: plugin.Manifest{ID: "org.example.timer", Name: "Timer", Widgets: []plugin.Widget{
			{ID: "clock", Label: "Clock"}, {ID: "calendar", Label: "Calendar"},
		}}},
		{Manifest: plugin.Manifest{ID: "org.example.panel-only", Name: "Panel only", Panels: []plugin.Panel{{ID: "panel"}}}},
		{Manifest: plugin.Manifest{ID: "org.example.missing-command", Name: "Missing command", Widgets: []plugin.Widget{{ID: "widget"}}}, MissingCommands: []string{"missing-command"}},
	}}}
	t.Cleanup(func() {
		reg.mu.Lock()
		reg.plugins = previous
		reg.mu.Unlock()
	})
}

func onboardingContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasPluginPlacement(bar config.Bar, pluginID, entryID string) bool {
	var found bool
	var visit func([]config.Item)
	visit = func(items []config.Item) {
		for _, item := range items {
			if item.ID == "plugin" && item.Plugin == pluginID && item.Entry == entryID {
				found = true
			}
			visit(item.Items)
		}
	}
	visit(bar.Left)
	visit(bar.Center)
	visit(bar.Right)
	return found
}

func countPluginPlacements(bar config.Bar, pluginID string) int {
	count := 0
	var visit func([]config.Item)
	visit = func(items []config.Item) {
		for _, item := range items {
			if item.ID == "plugin" && item.Plugin == pluginID {
				count++
			}
			visit(item.Items)
		}
	}
	visit(bar.Left)
	visit(bar.Center)
	visit(bar.Right)
	return count
}
