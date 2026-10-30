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
