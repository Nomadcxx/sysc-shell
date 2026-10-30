package shell

import (
	"testing"

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
	if rail.Key != "onb-rail" || rail.Width != onbRailWidth || onbRailWidth != 280 || rail.Fill != ui.FillContainer {
		t.Fatalf("rail = key %q width %d fill %d, want onb-rail 280 FillContainer", rail.Key, rail.Width, rail.Fill)
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
