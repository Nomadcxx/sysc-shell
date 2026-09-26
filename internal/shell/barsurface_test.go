package shell

import (
	"slices"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
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

// blurBar builds a bar in the given style and shape, resolved against blur,
// and configures it at 1200 logical pixels.
func blurBar(t *testing.T, style, shape string, blur bool, center []config.Item) *Bar {
	t.Helper()
	cfg := config.Default()
	policy := cfg.Bar
	policy.Style, policy.Shape = style, shape
	policy.Left, policy.Center, policy.Right = nil, center, nil
	bar, err := NewWithTheme(ThemeFrom(cfg, policy).WithCompositor(blur), policy, "DP-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bar.stopAnimation)
	if err := bar.Configure(1200, BarHeight, 120); err != nil {
		t.Fatal(err)
	}
	return bar
}

func TestBarBlurShapeFollowsStyleAndShape(t *testing.T) {
	t.Parallel()
	for _, style := range []string{"solid", "frosted", "islands"} {
		if got := blurBar(t, style, "floating", false, nil).blurShape(); got != nil {
			t.Errorf("%s without compositor blur published %v, want nil", style, got)
		}
	}
	if got := blurBar(t, "solid", "floating", true, nil).blurShape(); got != nil {
		t.Errorf("solid published %v, want nil", got)
	}

	floating := blurBar(t, "frosted", "floating", true, nil)
	body := floating.bodyLocked(1200, BarHeight)
	want := ui.BlurStrips(ui.SurfaceShape{Body: body, Radius: floating.theme.Radius})
	if got := floating.blurShape(); !slices.Equal(got, want) {
		t.Errorf("frosted floating = %v, want the rounded body %v", got, want)
	}

	attached := blurBar(t, "frosted", "attached", true, nil)
	body = attached.bodyLocked(1200, BarHeight)
	got := attached.blurShape()
	if len(got) == 0 || got[0] != (ui.Rect{X: body.X, Y: body.Y, W: body.W, H: got[0].H}) {
		t.Fatalf("attached = %v, want square corners on the attached edge of %+v", got, body)
	}
	var below []ui.Rect
	for _, r := range got {
		if r.Y >= body.Y+body.H {
			below = append(below, r)
		}
	}
	if len(below) == 0 || below[0].X != body.X || below[len(below)-1].X+below[len(below)-1].W != body.X+body.W {
		t.Errorf("attached published no end fillets past the body: %v", below)
	}
}

func TestIslandsBlurEachVisibleCapsule(t *testing.T) {
	t.Parallel()
	bar := blurBar(t, "islands", "floating", true, []config.Item{{ID: "media", MaxWidth: 120}})
	present := barView{Media: services.MediaState{
		Available: true, Status: services.PlaybackPlaying, Title: "Track",
	}}
	bar.apply(present)
	if err := bar.Configure(1200, BarHeight, 120); err != nil {
		t.Fatal(err)
	}
	media := bar.center[0].node
	if media.Kind != ui.KindCapsule || media.Bounds.W == 0 {
		t.Fatalf("media = kind %v bounds %+v, want an arranged capsule", media.Kind, media.Bounds)
	}
	got := bar.blurShape()
	if len(got) == 0 {
		t.Fatal("a visible media capsule published no blur")
	}
	for _, r := range got {
		if !media.Bounds.Contains(r.X, r.Y) || !media.Bounds.Contains(r.X+r.W-1, r.Y+r.H-1) {
			t.Errorf("strip %+v escapes the media capsule %+v", r, media.Bounds)
		}
	}

	bar.apply(barView{})
	if err := bar.Configure(1200, BarHeight, 120); err != nil {
		t.Fatal(err)
	}
	if got := bar.blurShape(); len(got) != 0 {
		t.Errorf("absent media still published %v", got)
	}
}

// TestBarBodyMatchesThePlatform: the shell paints its body where the platform
// declares it, for both shapes on both edges.
func TestBarBodyMatchesThePlatform(t *testing.T) {
	t.Parallel()
	for _, shape := range config.BarShapes {
		for _, edge := range []string{"top", "bottom"} {
			cfg := config.Default()
			policy := cfg.Bar
			policy.Shape, policy.Edge = shape, edge
			policy.Left, policy.Center, policy.Right = nil, nil, nil
			bar, err := NewWithTheme(ThemeFrom(cfg, policy), policy, "DP-1")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(bar.stopAnimation)
			height := policy.SurfaceExtent()
			var want ui.Rect
			want.X, want.Y, want.W, want.H = policy.BodyIn(1200, height)
			if got := bar.bodyLocked(1200, height); got != want {
				t.Errorf("%s %s: shell body %+v, platform body %+v", shape, edge, got, want)
			}
			if surface, _, _ := bar.themeSnapshot().Geometry(); surface != policy.Extent() {
				t.Errorf("%s %s: theme extent %d, want %d", shape, edge, surface, policy.Extent())
			}
		}
	}
}

// TestPanelsMeetTheAttachedBody: the bar's surface holds the overhang, but a
// panel attaches at the body's edge.
func TestPanelsMeetTheAttachedBody(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Bar.Left, cfg.Bar.Center, cfg.Bar.Right = nil, nil, nil
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	reg.tokens = theme.Fallback
	bar, leases, _, err := reg.buildBar(cfg, "DP-2", reg.tokens)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { releaseAll(leases) })
	reg.setTestBar(7, bar)
	if err := bar.Configure(1200, cfg.Bar.SurfaceExtent(), 120); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	trig := reg.triggerLocked(7, "DP-2")
	reg.mu.Unlock()
	if trig.BarZone != cfg.Bar.Extent() {
		t.Errorf("zone = %d, want the attached body %d", trig.BarZone, cfg.Bar.Extent())
	}
}

// TestAttachedBarRendersItsEndFillets renders the default attached bar: the
// wedges paint at both ends of the overhang, nothing paints between them, and
// every blurred pixel past the body is painted.
func TestAttachedBarRendersItsEndFillets(t *testing.T) {
	t.Parallel()
	bar := blurBar(t, "frosted", "attached", true, nil)
	const w = 1200
	h := config.Default().Bar.SurfaceExtent()
	if err := bar.Configure(w, h, 120); err != nil {
		t.Fatal(err)
	}
	pix := make([]byte, w*h*4)
	if err := bar.Render(pix, w, h, w*4); err != nil {
		t.Fatal(err)
	}
	alpha := func(x, y int) byte { return pix[(y*w+x)*4+3] }
	body := bar.bodyLocked(w, h)
	below := body.Y + body.H
	if alpha(0, below) == 0 || alpha(w-1, below) == 0 {
		t.Errorf("no wedge at the ends of the overhang: left %#x right %#x", alpha(0, below), alpha(w-1, below))
	}
	if a := alpha(w/2, below); a != 0 {
		t.Errorf("mid overhang alpha %#x, want transparent", a)
	}
	for _, r := range bar.blurShape() {
		for y := max(r.Y, below); y < r.Y+r.H; y++ {
			for x := r.X; x < r.X+r.W; x++ {
				if alpha(x, y) == 0 {
					t.Fatalf("blurred pixel (%d,%d) past the body is unpainted", x, y)
				}
			}
		}
	}
}
