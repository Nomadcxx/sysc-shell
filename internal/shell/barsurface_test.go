package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// TestBarStyleResolvesGroundAndPillAlpha is design D1-D3 as a table: style,
// compositor blur and high contrast decide the bar's ground and pill alpha.
func TestBarStyleResolvesGroundAndPillAlpha(t *testing.T) {
	t.Parallel()
	pct := func(p int) uint8 { return uint8((p*255 + 50) / 100) }
	solid := opacityAlpha(90, false)
	for _, tc := range []struct {
		style        string
		blur, hc     bool
		ground, pill uint8
		frosted      bool
	}{
		{"solid", true, false, solid, 0xff, false},
		{"solid", false, false, solid, 0xff, false},
		{"frosted", true, false, pct(65), pct(70), true},
		{"frosted", false, false, solid, 0xff, false},
		{"islands", true, false, 0, pct(70), true},
		{"islands", false, false, 0, pct(theme.OpacityMin), false},
		{"frosted", true, true, 0xff, 0xff, false},
		{"islands", true, true, 0xff, 0xff, false},
		// A literal bar that names no style paints as it always has.
		{"", true, false, solid, 0xff, false},
	} {
		cfg := config.Default()
		cfg.Theme.BarOpacity = 90
		cfg.Accessibility.HighContrast = tc.hc
		bar := cfg.Bar
		bar.Style = tc.style
		got := ThemeFrom(cfg, bar).WithCompositor(tc.blur)
		if got.Surfaces.Bar != tc.ground || got.PillAlpha != tc.pill || got.Blur != tc.frosted {
			t.Errorf("%q blur=%v hc=%v: ground %#x pill %#x blur %v, want %#x %#x %v",
				tc.style, tc.blur, tc.hc, got.Surfaces.Bar, got.PillAlpha, got.Blur,
				tc.ground, tc.pill, tc.frosted)
		}
		if err := got.Valid(); err != nil {
			t.Errorf("%q blur=%v hc=%v: %v", tc.style, tc.blur, tc.hc, err)
		}
		if back := got.WithCompositor(!tc.blur).WithCompositor(tc.blur); back.Surfaces.Bar != got.Surfaces.Bar || back.PillAlpha != got.PillAlpha {
			t.Errorf("%q: WithCompositor does not round-trip", tc.style)
		}
	}
}

func TestFrostFloorIsForty(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	bar := cfg.Bar
	bar.FrostOpacity, bar.PillOpacity = 10, 10 // below what config accepts
	got := ThemeFrom(cfg, bar).WithCompositor(true)
	want := uint8((theme.OpacityMinFrost*255 + 50) / 100)
	if got.Surfaces.Bar != want || got.PillAlpha != want {
		t.Errorf("ground %#x pill %#x, want both at the %d%% floor %#x",
			got.Surfaces.Bar, got.PillAlpha, theme.OpacityMinFrost, want)
	}
}

// TestPillLiftsTowardTheForeground is D3's lift: a translucent pill moves
// (1 - alpha) x 0.3 of the way to the foreground so it stays distinct from a
// translucent ground; an opaque one does not move.
func TestPillLiftsTowardTheForeground(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	base := ThemeFrom(cfg, cfg.Bar)
	for _, tc := range []struct {
		pill int
		lift float64
	}{{70, 0.3 * (1 - float64(uint8((70*255+50)/100))/255)}, {100, 0}} {
		bar := cfg.Bar
		bar.PillOpacity = tc.pill
		th := ThemeFrom(cfg, bar).WithCompositor(true)
		got := barStyle(th).Capsule
		want := render.LerpColor(base.Capsule, base.Foreground, tc.lift)
		want.A = th.PillAlpha
		if got != want {
			t.Errorf("pill %d%%: capsule %+v, want %+v", tc.pill, got, want)
		}
	}
	solid := cfg.Bar
	solid.Style = "solid"
	if got := barStyle(ThemeFrom(cfg, solid).WithCompositor(true)).Capsule; got != base.Capsule {
		t.Errorf("solid capsule %+v moved from %+v", got, base.Capsule)
	}
}
