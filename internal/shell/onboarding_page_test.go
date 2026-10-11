package shell

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/onboarding"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestOnboardingPageNavigation(t *testing.T) {
	seq := []onboardingPage{onbWelcome, onbAppearance, onbRegion, onbIdle, onbReady}
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

func TestOnboardingHasBrandedHeaderAndUniformPillActions(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	h := reg.panelHosts[PanelOnboarding]
	brandPill := onboardingNodeByKey(h.root, "onb-brand-pill")
	if brandPill == nil || brandPill.Stroke == 0 || brandPill.Shape != ui.ShapeStadium {
		t.Fatalf("wordmark header pill = %+v, want a visibly stroked stadium capsule", brandPill)
	}
	if brandPill.Padding < h.metrics().PanelPadding {
		t.Errorf("wordmark pill padding = %d, want at least panel padding %d", brandPill.Padding, h.metrics().PanelPadding)
	}
	if brandPill.PaddingX < brandPill.Padding+theme.MarginS {
		t.Errorf("wordmark pill horizontal padding = %d, want at least %d for a little more room at the sides", brandPill.PaddingX, brandPill.Padding+theme.MarginS)
	}
	mark := onboardingNodeByKey(h.root, "onb-ascii-wordmark")
	if mark == nil || mark.Kind != ui.KindColumn || len(mark.Children) != 9 {
		t.Fatalf("ASCII wordmark = %+v, want eight mark rows and its tagline", mark)
	}
	if len(brandPill.Children) != 1 || brandPill.Children[0] != mark {
		t.Fatalf("wordmark pill contents = %+v, want only the complete ASCII mark", brandPill.Children)
	}
	if mark.Name != "SYSC" || mark.Role != "img" {
		t.Fatalf("ASCII wordmark semantics = %q/%q, want image named SYSC", mark.Name, mark.Role)
	}
	for i, row := range mark.Children {
		if got := len([]rune(row.Text)); got != 69 {
			t.Errorf("ASCII wordmark row %d has %d cells, want 69", i, got)
		}
		if row.Kind != ui.KindText || row.TextRole != theme.RoleMono || row.Tone != ui.ToneAccent || row.Bold || !row.CenterX {
			t.Errorf("ASCII wordmark row %d = %+v, want centred regular-weight mono accent text", i, row)
		}
	}
	if !strings.Contains(mark.Children[8].Text, "SEE YOU IN SPACE COWBOY") {
		t.Fatalf("ASCII wordmark lost its tagline: %q", mark.Children[8].Text)
	}
	if strings.Contains(strings.ToLower(onbIntroWelcome), "sysc-shell provides") {
		t.Fatalf("welcome copy still explains shell architecture: %q", onbIntroWelcome)
	}
	setup := onboardingNodeByKey(h.root, "onb-setup-label")
	if setup == nil || setup.Text != "SETUP" || setup.Role != "heading" || setup.TextRole != theme.RoleTitle || !setup.CenterX {
		t.Fatalf("setup heading = %+v, want a centred heading beneath the wordmark", setup)
	}
	if onboardingNodeByKey(h.root, "onb-move-hint") == nil {
		t.Fatal("the footer does not explain how to move the setup window")
	}
	if h.rootStyle(h.theme, reg.cfg.Theme.BlurBehind).Rim != h.theme.Outline {
		t.Fatalf("floating onboarding rim = %+v, want the shell's neutral outline %+v", h.rootStyle(h.theme, reg.cfg.Theme.BlurBehind).Rim, h.theme.Outline)
	}
	card := onboardingNodeByKey(h.root, "onb-content-card")
	if card == nil || card.Stroke == 0 {
		t.Fatalf("content card has no visible outline: %+v", card)
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
	if actions[0].Fill != ui.FillOutlineVariant || actions[0].Stroke != 1 || actions[0].StrokeFill != ui.FillAccent {
		t.Errorf("Not now accent = fill %d stroke %d/%d; want existing quiet fill with a one-pixel accent outline", actions[0].Fill, actions[0].Stroke, actions[0].StrokeFill)
	}
	if actions[2].Fill != ui.FillAccent {
		t.Fatalf("Next fill = %d, want the accented primary action", actions[2].Fill)
	}

	h.onbPage = onbReady
	reg.rebuildPanel(h)
	for _, action := range []string{"onb-settings", "onb-launcher", "onb-wallpaper", "onb-docs"} {
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

func TestOnboardingBrandAndMoveHintComposition(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	panel := drainAux(t, reg, 2)[1].Open
	if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), 120); err != nil {
		t.Fatal(err)
	}
	h := reg.panelHosts[PanelOnboarding]
	header := onboardingNodeByKey(h.root, "onb-header")
	pill := onboardingNodeByKey(h.root, "onb-brand-pill")
	mark := onboardingNodeByKey(h.root, "onb-ascii-wordmark")
	setup := onboardingNodeByKey(h.root, "onb-setup-label")
	footer := onboardingNodeByKey(h.root, "onb-footer")
	hint := onboardingNodeByKey(h.root, "onb-move-hint")
	move := byAction(h.root, "onb-move")
	card := onboardingNodeByKey(h.root, "onb-content-card")
	if header == nil || pill == nil || mark == nil || setup == nil || footer == nil || hint == nil || move == nil || card == nil {
		t.Fatalf("missing composition nodes: header=%+v pill=%+v mark=%+v setup=%+v footer=%+v hint=%+v move=%+v card=%+v",
			header, pill, mark, setup, footer, hint, move, card)
	}
	center := h.root.Bounds.X + h.root.Bounds.W/2
	for name, node := range map[string]*ui.Node{"pill": pill, "setup": setup} {
		if got := node.Bounds.X + node.Bounds.W/2; abs(got-center) > 1 {
			t.Errorf("%s center = %d, want panel center %d", name, got, center)
		}
	}
	for i, row := range mark.Children {
		if got := row.Bounds.X + row.Bounds.W/2; abs(got-(pill.Bounds.X+pill.Bounds.W/2)) > 1 {
			t.Errorf("wordmark row %d center = %d, want pill center %d", i, got, pill.Bounds.X+pill.Bounds.W/2)
		}
	}
	if got := pill.Bounds.Y - (h.root.Bounds.Y + h.root.Padding); got < h.metrics().PanelPadding {
		t.Errorf("brand top inset = %d, want at least %d", got, h.metrics().PanelPadding)
	}
	if setup.Bounds.Y < pill.Bounds.Y+pill.Bounds.H {
		t.Errorf("setup heading bounds %+v overlap the wordmark pill %+v", setup.Bounds, pill.Bounds)
	}
	if hint.Bounds.Y < footer.Bounds.Y || hint.Bounds.Y+hint.Bounds.H > footer.Bounds.Y+footer.Bounds.H {
		t.Errorf("move hint bounds %+v are outside the bottom footer %+v", hint.Bounds, footer.Bounds)
	}
	if move.Bounds.Y < footer.Bounds.Y || move.Bounds.Y+move.Bounds.H > footer.Bounds.Y+footer.Bounds.H {
		t.Errorf("move action bounds %+v are outside the bottom footer %+v", move.Bounds, footer.Bounds)
	}
	if got, want := footer.Bounds.Y+footer.Bounds.H, h.root.Bounds.Y+h.root.Bounds.H-h.root.Padding; got != want {
		t.Errorf("footer ends at %d, want panel content bottom %d", got, want)
	}
	if card.Bounds.Y < setup.Bounds.Y+setup.Bounds.H {
		t.Errorf("main content bounds %+v overlap the brand block ending at %d", card.Bounds, setup.Bounds.Y+setup.Bounds.H)
	}
	if gap := card.Bounds.Y - (setup.Bounds.Y + setup.Bounds.H); gap > h.metrics().PanelPadding+theme.MarginM {
		t.Errorf("brand-to-content gap = %d, want at most header bottom inset plus panel gap (%d)", gap, h.metrics().PanelPadding+theme.MarginM)
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
	if !strings.Contains(appearance, "Colour mode") || !strings.Contains(appearance, "Choose wallpaper") {
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

func TestOnboardingBrandPillFitsResponsivePanel(t *testing.T) {
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
			panel := drainAux(t, reg, 2)[1].Open
			if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), 120); err != nil {
				t.Fatal(err)
			}
			root := reg.panelHosts[PanelOnboarding].root
			mark := onboardingNodeByKey(root, "onb-ascii-wordmark")
			if tc.name == "compact" {
				if mark == nil || len(mark.Children) != 1 || mark.Children[0].Text != "SYSC" {
					t.Fatalf("compact wordmark = %+v, want the compact SYSC row", mark)
				}
			} else if mark == nil || len(mark.Children) != 9 {
				t.Fatalf("full wordmark = %+v, want the complete ASCII mark", mark)
			}
			pill := onboardingNodeByKey(root, "onb-brand-pill")
			left := root.Bounds.X + root.Padding
			right := root.Bounds.X + root.Bounds.W - root.Padding
			if pill.Bounds.X < left || pill.Bounds.X+pill.Bounds.W > right {
				t.Fatalf("brand pill bounds %+v exceed panel content [%d,%d]", pill.Bounds, left, right)
			}
			footer := onboardingNodeByKey(root, "onb-footer")
			hint := onboardingNodeByKey(root, "onb-move-hint")
			if footer == nil || hint == nil {
				t.Fatalf("missing responsive footer: footer=%+v hint=%+v", footer, hint)
			}
			if hint.Bounds.X < left || hint.Bounds.X+hint.Bounds.W > right || hint.Bounds.Y < footer.Bounds.Y || hint.Bounds.Y+hint.Bounds.H > footer.Bounds.Y+footer.Bounds.H {
				t.Errorf("move hint bounds %+v exceed panel content or footer %+v", hint.Bounds, footer.Bounds)
			}
			wantHintWidth, _ := reg.panelHosts[PanelOnboarding].measureText()(hint.Text, ui.TextAttrsOf(hint))
			if hint.Bounds.W < wantHintWidth {
				t.Errorf("move hint width = %d, want at least %d for %q", hint.Bounds.W, wantHintWidth, hint.Text)
			}
			markWidth, _ := reg.panelHosts[PanelOnboarding].measureText()(mark.Children[0].Text, ui.TextAttrsOf(mark.Children[0]))
			if pill.Bounds.W < markWidth+2*pill.Padding {
				t.Errorf("brand pill width = %d, want full wordmark width %d plus %d padding on both sides", pill.Bounds.W, markWidth, pill.Padding)
			}
		})
	}
}

func TestOnboardingWrapsJapaneseCopyWithConfiguredShaper(t *testing.T) {
	cfg := config.Default()
	cfg.Session.Language = "ja"
	cfg.Accessibility.ReducedMotion = true
	reg := NewRegistry(cfg)
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

func TestOnboardingStepRailAndStageLayout(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 2560, OutH: 1440}); err != nil {
		t.Fatal(err)
	}
	panel := drainAux(t, reg, 2)[1].Open
	h := reg.panelHosts[PanelOnboarding]
	assertPinnedActions := func() {
		t.Helper()
		if len(h.root.Children) != 3 {
			t.Fatalf("onboarding root has %d children, want header, body and action row", len(h.root.Children))
		}
		body, actions := h.root.Children[1], h.root.Children[2]
		if body.Bounds.H <= 0 {
			t.Fatalf("scroll viewport height = %d, want positive height", body.Bounds.H)
		}
		wantBottom := h.root.Bounds.Y + h.root.Bounds.H - h.root.Padding
		if got := actions.Bounds.Y + actions.Bounds.H; got != wantBottom {
			t.Fatalf("action row ends at %d, want panel interior bottom %d", got, wantBottom)
		}
	}
	if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), 120); err != nil {
		t.Fatal(err)
	}
	assertPinnedActions()
	h.onbPage = onbAppearance
	reg.rebuildPanel(h)
	assertPinnedActions()
	active := onboardingNodeByKey(h.root, "onb-step-appearance")
	if active == nil || active.Fill != ui.FillAccent {
		t.Fatalf("active step = %+v, want the Appearance step selected", active)
	}
	if got := len(onboardingStepNodes(h.root)); got != int(onbPages) {
		t.Fatalf("step rail has %d items, want %d", got, onbPages)
	}
	for i, item := range onboardingStepNodes(h.root) {
		want := ui.FillOutline
		if onboardingPage(i) == h.onbPage {
			want = ui.FillAccent
		}
		if item.Fill != want || item.Stroke == 0 {
			t.Errorf("step %d fill/stroke = %d/%d, want fill %d with a visible outline", i+1, item.Fill, item.Stroke, want)
		}
	}
	h.onbPage = onbRegion
	reg.rebuildPanel(h)
	assertPinnedActions()
	intro := onboardingNodeByKey(h.root, "onb-intro")
	if intro == nil || intro.Kind != ui.KindColumn || intro.Width != onbContentWidth(h) || len(intro.Children) == 0 {
		t.Fatalf("page intro = %+v, want measured text within the content width", intro)
	}
	for _, line := range intro.Children {
		if line.Kind != ui.KindText || line.MaxWidth != onbContentWidth(h) {
			t.Errorf("intro line = %+v, want a text line bounded by the content width", line)
		}
	}
	card := onboardingNodeByKey(h.root, "onb-content-card")
	body := onboardingNodeByKey(h.root, onbBodyKey)
	frame := onboardingNodeByKey(h.root, "onb-content-frame")
	main := onboardingNodeByKey(h.root, "onb-content-main")
	stage := onboardingNodeByKey(h.root, "onb-stage")
	if card == nil || body == nil || frame == nil || main == nil || stage == nil {
		t.Fatalf("missing card/body/frame/main/stage geometry: card=%+v body=%+v frame=%+v main=%+v stage=%+v", card, body, frame, main, stage)
	}
	if card.Bounds.H >= body.Bounds.H {
		t.Fatalf("content card fills the whole body (%d >= %d); sparse setup pages need breathing room", card.Bounds.H, body.Bounds.H)
	}
	if topGap := card.Bounds.Y - body.Bounds.Y; topGap > 1 {
		t.Fatalf("content card starts %d below its scroll viewport; want it top-aligned to tighten the header gap", topGap)
	}
	upper := stage.Bounds.Y - main.Bounds.Y
	lower := main.Bounds.Y + main.Bounds.H - stage.Bounds.Y - stage.Bounds.H
	if abs(upper-lower) > 2 {
		t.Fatalf("page stage is not balanced in the body: upper=%d lower=%d main=%+v stage=%+v", upper, lower, main.Bounds, stage.Bounds)
	}
	if card.Bounds.H < main.Bounds.H {
		t.Fatalf("content card height %d is shorter than its main pane %d", card.Bounds.H, main.Bounds.H)
	}
	rail := onboardingStepNodes(h.root)
	if len(rail) != int(onbPages) {
		t.Fatal("missing step rail")
	}
	railCenter := (rail[0].Bounds.Y + rail[len(rail)-1].Bounds.Y + rail[len(rail)-1].Bounds.H) / 2
	mainCenter := main.Bounds.Y + main.Bounds.H/2
	if abs(railCenter-mainCenter) > 2 {
		t.Fatalf("step rail center %d is not aligned with page center %d", railCenter, mainCenter)
	}

	narrow := newPanelRegistry(t)
	if err := narrow.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 800, OutH: 500}); err != nil {
		t.Fatal(err)
	}
	if len(onboardingStepNodes(narrow.panelHosts[PanelOnboarding].root)) != 0 {
		t.Fatal("narrow onboarding layout kept the desktop step rail")
	}
}

func TestOnboardingPaintsItsCapturedBackdrop(t *testing.T) {
	cfg := config.Default()
	cfg.Theme.BlurBehind = true
	cfg.Theme.PanelOpacity = 60
	cfg.Accessibility.ReducedMotion = true
	reg := NewRegistry(cfg)
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
	panel.Callbacks.Backdrop(shot)
	withBackdrop := paint()
	root := reg.panelHosts[PanelOnboarding].root
	card := onboardingNodeByKey(root, "onb-content-card")
	if card == nil {
		t.Fatal("onboarding render has no content card")
	}
	// Sample the top interior gutter. The taller ASCII wordmark occupies the
	// old card.Bounds.Y/2 point, where the opaque brand pill correctly hides blur.
	sampleX, sampleY := width/2, max(root.Padding/2, 1)
	index := (sampleY*width + sampleX) * 4
	if plain[index] == withBackdrop[index] && plain[index+1] == withBackdrop[index+1] &&
		plain[index+2] == withBackdrop[index+2] && plain[index+3] == withBackdrop[index+3] {
		t.Fatal("captured backdrop did not reach the clear onboarding frame gutter")
	}
}

func TestOnboardingSummaryIncludesLanguageWithReadableText(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	h := reg.panelHosts[PanelOnboarding]
	for _, page := range []onboardingPage{onbWelcome, onbReady} {
		h.onbPage = page
		reg.rebuildPanel(h)
		texts := findAllKind(h.root, ui.KindText)
		languageFound, themeFound := false, false
		for _, n := range texts {
			languageFound = languageFound || strings.Contains(n.Text, "Interface language:")
			if strings.HasPrefix(n.Text, "Theme:") {
				themeFound = true
				if n.Tone == ui.ToneSubtle {
					t.Errorf("%s theme summary uses low-emphasis text", onbTitles[page])
				}
			}
		}
		if !languageFound || !themeFound {
			t.Errorf("%s summary missing interface language or theme: %s", onbTitles[page], renderText(h.root))
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
		{onbReady, []string{"onb-settings", "onb-launcher", "onb-wallpaper", "onb-docs"}},
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
	if err.Bounds.Y+err.Bounds.H > finish.Bounds.Y {
		t.Fatalf("error at %+v is not above Finish at %+v", err.Bounds, finish.Bounds)
	}
	footer := root.Children[2]
	footerBottom := root.Bounds.Y + root.Bounds.H - root.Padding
	if footer.Bounds.Y+footer.Bounds.H != footerBottom {
		t.Fatalf("error footer ends at %d, want pinned bottom %d", footer.Bounds.Y+footer.Bounds.H, footerBottom)
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
			if got := len(onboardingStepNodes(h.root)); got != 0 {
				t.Fatalf("narrow page retained %d desktop rail items", got)
			}
			footerBottom := h.root.Bounds.Y + h.root.Bounds.H - h.root.Padding
			footer := h.root.Children[2]
			if footer.Bounds.Y+footer.Bounds.H != footerBottom {
				t.Fatalf("narrow page footer ends at %d, want pinned bottom %d", footer.Bounds.Y+footer.Bounds.H, footerBottom)
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
		h.set = reg.settingsForLocked(h.draft)
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
			card := onboardingNodeByKey(h.root, "onb-content-card")
			if card == nil {
				t.Fatal("narrow localized page has no content card")
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
			if len(root.Children) != 3 {
				reg.mu.Unlock()
				t.Fatalf("onboarding root has %d children, want header, body, actions", len(root.Children))
			}
			scroll, actions := root.Children[1], root.Children[2]
			if scroll.Bounds.H < 300 {
				reg.mu.Unlock()
				t.Fatalf("page body height = %d, want it to use the available frame space", scroll.Bounds.H)
			}
			footerBottom := root.Bounds.Y + root.Bounds.H - root.Padding
			if actions.Bounds.Y+actions.Bounds.H != footerBottom {
				reg.mu.Unlock()
				t.Fatalf("action row bottom = %d, want pinned frame bottom %d", actions.Bounds.Y+actions.Bounds.H, footerBottom)
			}
			if page == onbWelcome || page == onbReady {
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
	reg.mu.Lock()
	reg.onboardingPending = false
	reg.mu.Unlock()
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
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
	if f == nil || f.Name != onbNextLabel {
		t.Fatalf("initial focus name=%v, want %q", f, onbNextLabel)
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
	if skip.Fill != ui.FillOutlineVariant || skip.Stroke != 1 || skip.StrokeFill != ui.FillAccent || back.Fill != ui.FillOutline {
		t.Fatal("secondary actions are not visually subordinate")
	}
	if next.Fill != ui.FillAccent {
		t.Fatal("the primary action lost its accent fill")
	}
	for _, p := range []onboardingPage{onbAppearance, onbRegion, onbIdle, onbReady} {
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
	progress := findText(h.root, "// 01 / 05")
	if progress == nil || progress.TextRole != theme.RoleMono {
		t.Fatalf("progress = %+v, want compact mono // label", progress)
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
		progress := findNode(h.root, func(n *ui.Node) bool { return n.Name == "Setup progress" || n.Text == "// 03 / 05" })
		if progress == nil || progress.Role != "status" || progress.Name == "Setup progress" {
			t.Errorf("progress accessibility for %s = %+v; want localized status", locale, progress)
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
	if got := selectedIdleAfter(h); got != "screensaver" {
		t.Fatalf("selected idle action = %q, want screensaver from live walls state", got)
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

func TestOnboardingReadyOffersDirectSettingsAndWallpaperActions(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbReady
	reg.rebuildPanel(h)
	if byAction(h.root, "onb-settings") == nil || byAction(h.root, "onb-wallpaper") == nil ||
		byAction(h.root, "onb-launcher") == nil || byAction(h.root, "onb-docs") == nil {
		t.Fatalf("Ready tree lacks direct Settings/wallpaper actions: %q", treeText(h.root))
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
