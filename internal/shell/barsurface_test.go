package shell

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	shellplugin "github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
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
// declares it, for every shape and edge.
func TestBarBodyMatchesThePlatform(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		style string
		hc    bool
	}{{"frosted", false}, {"islands", false}, {"islands", true}} {
		for _, shape := range config.BarShapes {
			for _, edge := range []string{"top", "bottom", "left", "right"} {
				checkBarBodyMatchesThePlatform(t, tc.style, tc.hc, shape, edge)
			}
		}
	}
}

// checkBarBodyMatchesThePlatform compares one combination. High contrast
// paints islands as solid, but the surface is still sized from the configured
// style, so the painted body has to follow that too.
func checkBarBodyMatchesThePlatform(t *testing.T, style string, hc bool, shape, edge string) {
	t.Helper()
	cfg := config.Default()
	cfg.Accessibility.HighContrast = hc
	policy := cfg.Bar
	policy.Style, policy.Shape, policy.Edge = style, shape, edge
	policy.Left, policy.Center, policy.Right = nil, nil, nil
	bar, err := NewWithTheme(ThemeFrom(cfg, policy), policy, "DP-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bar.stopAnimation)
	name := fmt.Sprintf("%s hc=%v %s %s", style, hc, shape, edge)
	width, height := 1200, policy.SurfaceExtent()
	if edge == "left" || edge == "right" {
		width, height = policy.SurfaceExtent(), 800
	}
	var want ui.Rect
	want.X, want.Y, want.W, want.H = policy.BodyIn(width, height)
	if got := bar.bodyLocked(width, height); got != want {
		t.Errorf("%s: shell body %+v, platform body %+v", name, got, want)
	}
	if surface, _, _ := bar.themeSnapshot().Geometry(); surface != policy.Extent() {
		t.Errorf("%s: theme extent %d, want %d", name, surface, policy.Extent())
	}
	if g := barStyle(bar.themeSnapshot()); (g.AttachEdge != "") != policy.Attached() {
		t.Errorf("%s: painter attach edge %q, platform attached %v", name, g.AttachEdge, policy.Attached())
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

func TestSideAttachedBarRendersItsScreenFillets(t *testing.T) {
	t.Parallel()
	for _, edge := range []string{"left", "right"} {
		for _, scale := range []ui.Scale120{120, 150} {
			cfg := config.Default()
			policy := cfg.Bar
			policy.Edge, policy.Style, policy.Shape = edge, "frosted", "attached"
			policy.Left, policy.Center, policy.Right = nil, nil, nil
			bar, err := NewWithTheme(ThemeFrom(cfg, policy).WithCompositor(true), policy, "DP-1")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(bar.stopAnimation)
			const outH = 160
			if err := bar.Configure(policy.SurfaceExtent(), outH, int(scale)); err != nil {
				t.Fatal(err)
			}
			w, h := scale.Physical(policy.SurfaceExtent()), scale.Physical(outH)
			pixels := make([]byte, w*h*4)
			if err := bar.Render(pixels, w, h, w*4); err != nil {
				t.Fatal(err)
			}
			alpha := func(x, y int) byte { return pixels[(y*w+x)*4+3] }
			body := scale.PhysicalRect(bar.bodyLocked(policy.SurfaceExtent(), outH))
			farX := body.X + body.W
			if edge == "right" {
				farX = body.X - 1
			}
			if got := alpha(farX, body.Y); got == 0 {
				t.Errorf("%s scale %d: leading screen-edge wedge is empty", edge, scale)
			}
			midY := body.Y + body.H/2
			if got := alpha(farX, midY); got != 0 {
				t.Errorf("%s scale %d: mid overhang alpha %d", edge, scale, got)
			}
			for _, strip := range bar.blurShape() {
				physical := scale.PhysicalRect(strip)
				for y := physical.Y; y < physical.Y+physical.H; y++ {
					for x := physical.X; x < physical.X+physical.W; x++ {
						if alpha(x, y) == 0 {
							t.Fatalf("%s scale %d: blurred pixel (%d,%d) is transparent", edge, scale, x, y)
						}
					}
				}
			}
		}
	}
}

// TestIslandsPaintNoGround renders an islands bar: the capsules paint, and the
// bar between them stays transparent whether or not the compositor blurs.
func TestIslandsPaintNoGround(t *testing.T) {
	t.Parallel()
	for _, blur := range []bool{true, false} {
		bar := blurBar(t, "islands", "attached", blur, config.Default().Bar.Center)
		const w = 1200
		h := bar.themeSnapshot().barGeometry().SurfaceExtent()
		if err := bar.Configure(w, h, 120); err != nil {
			t.Fatal(err)
		}
		pix := make([]byte, w*h*4)
		if err := bar.Render(pix, w, h, w*4); err != nil {
			t.Fatal(err)
		}
		alpha := func(x, y int) byte { return pix[(y*w+x)*4+3] }
		var pill *ui.Node
		for _, section := range bar.sections() {
			for _, n := range section {
				if n.Kind == ui.KindCapsule && n.Bounds.W > 0 && pill == nil {
					pill = n
				}
			}
		}
		if pill == nil {
			t.Fatalf("blur=%v: no capsule was arranged", blur)
		}
		cx, cy := pill.Bounds.X+pill.Bounds.W/2, pill.Bounds.Y+pill.Bounds.H/2
		if alpha(cx, cy) == 0 {
			t.Errorf("blur=%v: the capsule at %+v is unpainted", blur, pill.Bounds)
		}
		for _, x := range []int{4, w / 4, w - 5} {
			if a := alpha(x, cy); a != 0 {
				t.Errorf("blur=%v: ground at (%d,%d) alpha %#x, want transparent", blur, x, cy, a)
			}
		}
	}
}

func TestSideBarRenderMatrix(t *testing.T) {
	plugin := matrixPluginFrame(t)
	for _, edge := range []string{"top", "bottom", "left", "right"} {
		for _, style := range config.BarStyles {
			for _, shape := range config.BarShapes {
				t.Run(fmt.Sprintf("%s/%s/%s", edge, style, shape), func(t *testing.T) {
					cfg, policy := renderMatrixPolicy(edge, style, shape, "compact")
					bar := newRenderMatrixBar(t, cfg, policy, plugin, false)
					for _, output := range []struct {
						name string
						w, h int
					}{{"desktop", 3440, 1440}, {"laptop", 1536, 864}} {
						for _, scale := range []int{120, 150} {
							t.Run(fmt.Sprintf("%s/%d", output.name, scale), func(t *testing.T) {
								w, h := barSurfaceSize(policy, output.w, output.h)
								renderMatrixSurface(t, bar, policy, w, h, scale, true, true)
							})
						}
					}
				})
			}
		}
	}

	for _, edge := range []string{"top", "bottom", "left", "right"} {
		t.Run("wide text/"+edge, func(t *testing.T) {
			cfg, policy := renderMatrixPolicy(edge, "frosted", "floating", "wide")
			bar := newRenderMatrixBar(t, cfg, policy, plugin, false)
			for _, output := range []struct {
				name string
				w, h int
			}{{"desktop", 3440, 1440}, {"laptop", 1536, 864}} {
				for _, scale := range []int{120, 150} {
					t.Run(fmt.Sprintf("%s/%d", output.name, scale), func(t *testing.T) {
						w, h := barSurfaceSize(policy, output.w, output.h)
						renderMatrixSurface(t, bar, policy, w, h, scale, true, true)
					})
				}
			}
		})
	}

	for _, edge := range []string{"top", "bottom", "left", "right"} {
		for _, scale := range []int{120, 150} {
			t.Run(fmt.Sprintf("constrained/%s/%d", edge, scale), func(t *testing.T) {
				cfg, policy := renderMatrixPolicy(edge, "frosted", "attached", "dense")
				bar := newRenderMatrixBar(t, cfg, policy, plugin, true)
				fullW, fullH := barSurfaceSize(policy, 1536, 864)
				renderMatrixSurface(t, bar, policy, fullW, fullH, scale, true, true)
				bar.mu.Lock()
				old, ok := matrixActionBounds(bar, "plugin:matrix-view:run")
				bar.mu.Unlock()
				if !ok || old.W <= 0 || old.H <= 0 {
					t.Fatalf("dense plugin bounds = %+v, visible %v before constraint", old, ok)
				}

				shortOutputW, shortOutputH := 1536, 180
				if edge == "top" || edge == "bottom" {
					shortOutputW, shortOutputH = 180, 864
				}
				shortW, shortH := barSurfaceSize(policy, shortOutputW, shortOutputH)
				renderMatrixSurface(t, bar, policy, shortW, shortH, scale, false, false)
				bar.mu.Lock()
				got, visible := matrixActionBounds(bar, "plugin:matrix-view:run")
				hit, hitOK := bar.hitLocked(old.X+old.W/2, old.Y+old.H/2)
				overflow := bar.overflow
				bar.mu.Unlock()
				if !visible || got != (ui.Rect{}) {
					t.Fatalf("dropped plugin bounds = %+v, visible %v; want cleared bounds", got, visible)
				}
				if hitOK {
					t.Fatalf("old plugin point still hits %q after the item was dropped", hit)
				}
				if !overflow.Any() || overflow.Left == 0 {
					t.Fatalf("constrained overflow = %+v, want a dropped left-lane tail", overflow)
				}
			})
		}
	}
}

func TestSideAuxiliarySurfaceRenderUsesSpecBody(t *testing.T) {
	for _, edge := range []string{"left", "right"} {
		for _, scale := range []int{120, 150} {
			t.Run(fmt.Sprintf("%s/%d", edge, scale), func(t *testing.T) {
				cfg := config.Default()
				cfg.Bar.Edge = edge
				cfg.Theme.BlurBehind = false
				cfg.Accessibility.ReducedMotion = true
				reg := NewRegistry(cfg)
				reg.lookPath = func(string) (string, error) { return "", fmt.Errorf("not installed") }
				t.Cleanup(reg.Close)
				bar := withTestBar(t, reg, 7, cfg)
				bar.setOutputSize(1536, 864)
				if err := bar.Configure(cfg.Bar.SurfaceExtent(), 864, scale); err != nil {
					t.Fatal(err)
				}
				reg.mu.Lock()
				trigger := reg.triggerLocked(7, "DP-1")
				reg.mu.Unlock()
				if err := reg.OpenPanel(PanelClock, 7, trigger); err != nil {
					t.Fatal(err)
				}
				specs := drainAux(t, reg, 2)
				var spec *wayland.AuxSpec
				for _, request := range specs {
					if request.Open != nil && request.Open.ID == panelSurfaceID(PanelClock) {
						spec = request.Open
						break
					}
				}
				if spec == nil || spec.Width <= 0 || spec.Height <= 0 {
					t.Fatalf("side panel spec = %+v, want nonempty actual surface geometry", spec)
				}
				w, h := int(spec.Width), int(spec.Height)
				if err := spec.Callbacks.Configure(w, h, scale); err != nil {
					t.Fatalf("configure actual auxiliary spec %dx%d: %v", w, h, err)
				}
				physicalW, physicalH := ui.Scale120(scale).Physical(w), ui.Scale120(scale).Physical(h)
				pixels := make([]byte, physicalW*physicalH*4)
				if err := spec.Callbacks.Render(pixels, physicalW, physicalH, physicalW*4); err != nil {
					t.Fatalf("render actual auxiliary spec: %v", err)
				}
				reg.mu.Lock()
				host := reg.panelHosts[PanelClock]
				body := host.surfaceBody(w, h)
				place := host.place
				reg.mu.Unlock()
				if place.BarEdge != edge || body.W <= 0 || body.H <= 0 || body.X < 0 || body.Y < 0 || body.X+body.W > w || body.Y+body.H > h {
					t.Fatalf("side panel place edge=%s body=%+v surface=%dx%d", place.BarEdge, body, w, h)
				}
				if len(spec.InputRects) > 0 && spec.InputRects[0] != body {
					t.Fatalf("panel input body = %+v, rendered body = %+v", spec.InputRects[0], body)
				}
				painted := false
				for i := 3; i < len(pixels); i += 4 {
					if pixels[i] != 0 {
						painted = true
						break
					}
				}
				if !painted {
					t.Fatal("actual side panel spec rendered no visible pixels")
				}
			})
		}
	}
}

func matrixPluginFrame(t *testing.T) pluginFrame {
	t.Helper()
	wire := &v1.Node{Kind: v1.KindRow, Children: []*v1.Node{{
		Kind: v1.KindButton, ID: "run", Text: "Scan", Name: "Scan devices", Role: "button",
		Events: []v1.EventKind{v1.EventActivate},
	}}}
	root, err := shellplugin.Convert(wire, v1.ViewBar)
	if err != nil {
		t.Fatalf("convert validated bar plugin tree: %v", err)
	}
	stampPluginActions(root, "matrix-view")
	return pluginFrame{Root: root, Revision: 1, ViewID: "matrix-view"}
}

func renderMatrixPolicy(edge, style, shape, profile string) (config.Config, config.Bar) {
	cfg := config.Default()
	policy := cfg.Bar
	policy.Edge, policy.Style, policy.Shape = edge, style, shape
	policy.Center = config.Default().Bar.Center
	plugin := config.Item{ID: "plugin", Plugin: "org.sysc.matrix", Entry: "bar", Instance: "matrix"}
	switch profile {
	case "wide":
		policy.Height = 80
		policy.Left = []config.Item{
			{ID: "launcher"}, {ID: "workspace"},
			{ID: "group", Items: []config.Item{{ID: "cpu", Display: "text"}, {ID: "memory", Display: "text"}}},
			{ID: "window-title", MaxWidth: 280},
		}
		policy.Right = []config.Item{{ID: "running-apps"}, plugin}
	case "dense":
		policy.Left = []config.Item{
			{ID: "launcher"}, {ID: "workspace"}, {ID: "window-title", MaxWidth: 120}, {ID: "volume"}, plugin,
		}
		policy.Right = []config.Item{{ID: "running-apps"}}
	default:
		policy.Left = []config.Item{{ID: "launcher"}, {ID: "workspace"}, {ID: "window-title", MaxWidth: 160}}
		policy.Right = []config.Item{{ID: "running-apps"}, plugin}
	}
	return cfg, policy
}

func newRenderMatrixBar(t *testing.T, cfg config.Config, policy config.Bar, plugin pluginFrame, dense bool) *Bar {
	t.Helper()
	bar, err := NewWithTheme(ThemeFrom(cfg, policy).WithCompositor(true), policy, "DP-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bar.stopAnimation)
	count := 4
	if dense {
		count = 18
	}
	items := make([]tray.Item, 0, count)
	images := make(map[tray.ItemKey]*ui.Image, count)
	for i := 0; i < count; i++ {
		item := trayItem(uint64(i+1), fmt.Sprintf("matrix-%d", i))
		items = append(items, item)
		images[item.Key] = &ui.Image{Width: 2, Height: 2, Stride: 8,
			Pix: []byte{0x30, 0x90, 0xf0, 0xff, 0x30, 0x90, 0xf0, 0xff,
				0x30, 0x90, 0xf0, 0xff, 0x30, 0x90, 0xf0, 0xff}}
	}
	bar.setTray(items, config.TrayPreferences{Enabled: true}, images)
	running := []runningAppSlot{{Key: "firefox"}, {Key: "terminal"}, {Key: "editor"}}
	if dense {
		running = append(running, runningAppSlot{Key: "browser"}, runningAppSlot{Key: "music"})
	}
	bar.apply(barView{
		Now: time.Date(2026, time.October, 3, 9, 7, 0, 0, time.UTC), Title: "A long focused window title for side bar geometry",
		Pills:   []workspacePill{{ID: 11, Index: 1, Name: "one", Focused: true}, {ID: 12, Index: 2, Name: "two", Occupied: true}, {ID: 13, Index: 3, Name: "three"}},
		Running: running, Plugins: map[string]pluginFrame{"matrix": plugin},
	})
	return bar
}

func barSurfaceSize(policy config.Bar, outputW, outputH int) (int, int) {
	if policy.Edge == "left" || policy.Edge == "right" {
		return policy.SurfaceExtent(), outputH
	}
	return outputW, policy.SurfaceExtent()
}

func renderMatrixSurface(t *testing.T, bar *Bar, policy config.Bar, width, height, scale120 int, checkCenter, checkHits bool) []byte {
	t.Helper()
	scale := ui.Scale120(scale120)
	if err := bar.Configure(width, height, scale120); err != nil {
		t.Fatalf("configure %dx%d scale %d: %v", width, height, scale120, err)
	}
	pw, ph := scale.Physical(width), scale.Physical(height)
	pixels := make([]byte, pw*ph*4)
	if err := bar.Render(pixels, pw, ph, pw*4); err != nil {
		t.Fatalf("render %dx%d scale %d: %v", width, height, scale120, err)
	}
	painted := false
	for i := 3; i < len(pixels); i += 4 {
		if pixels[i] != 0 {
			painted = true
			break
		}
	}
	if !painted {
		t.Fatal("bar render has no surviving pixels")
	}

	bar.mu.Lock()
	body := bar.bodyLocked(width, height)
	content := bar.contentLocked(width, height)
	axis := bar.barAxis()
	sections := bar.sections()
	counts := [3]int{bar.overflow.Left, bar.overflow.Center, bar.overflow.Right}
	wantBodyX, wantBodyY, wantBodyW, wantBodyH := policy.BodyIn(width, height)
	if body != (ui.Rect{X: wantBodyX, Y: wantBodyY, W: wantBodyW, H: wantBodyH}) {
		bar.mu.Unlock()
		t.Fatalf("rendered body %+v differs from policy geometry %+v", body,
			ui.Rect{X: wantBodyX, Y: wantBodyY, W: wantBodyW, H: wantBodyH})
	}
	for _, section := range sections {
		var checkCross func(*ui.Node, bool)
		checkCross = func(n *ui.Node, clipped bool) {
			if n == nil {
				return
			}
			if !clipped && n.Bounds.W > 0 && n.Bounds.H > 0 {
				if axis == ui.Vertical && (n.Bounds.X < content.X || n.Bounds.X+n.Bounds.W > content.X+content.W) {
					t.Errorf("side node kind=%d text=%q action=%q bounds=%+v escape content %+v", n.Kind, n.Text, n.Action, n.Bounds, content)
				}
				if axis == ui.Horizontal && (n.Bounds.Y < content.Y || n.Bounds.Y+n.Bounds.H > content.Y+content.H) {
					t.Errorf("horizontal node cross bounds %+v escape content %+v", n.Bounds, content)
				}
			}
			for _, child := range n.Children {
				checkCross(child, clipped || n.ClipBounds)
			}
		}
		for _, node := range section {
			checkCross(node, false)
		}
	}
	center := ui.Rect{}
	if len(bar.center) > 0 && bar.center[0].node != nil {
		center = bar.center[0].node.Bounds
	}
	if checkCenter {
		if center.W <= 0 || center.H <= 0 {
			bar.mu.Unlock()
			t.Fatal("centre pill has no rendered bounds")
		}
		got, want := center.X+center.W/2, content.X+content.W/2
		if axis == ui.Vertical {
			got, want = center.Y+center.H/2, content.Y+content.H/2
		}
		if abs(got-want) > 1 {
			bar.mu.Unlock()
			t.Fatalf("centre anchor = %d, want %d within content %+v", got, want, content)
		}
	}
	wantFades := 0
	for i, section := range sections {
		if counts[i] > 0 && lastPlaced(section) != nil {
			wantFades++
		}
	}
	fades := overflowFades(sections, bar.overflow, bar.theme.Metrics.StandardControl, axis)
	if len(fades) != wantFades {
		bar.mu.Unlock()
		t.Fatalf("overflow counts %v produced %d fades, want %d", counts, len(fades), wantFades)
	}
	for _, fade := range fades {
		if fade.Action != "" || fade.Focusable || fade.Tooltip != "" {
			bar.mu.Unlock()
			t.Fatalf("overflow fade is interactive: %+v", fade)
		}
	}
	if checkHits {
		for _, action := range []string{"workspace:11", panelControlCenterAction, "running-app:firefox", "plugin:matrix-view:run", "tray-item:0"} {
			bounds, ok := matrixActionBounds(bar, action)
			if !ok || bounds.W <= 0 || bounds.H <= 0 {
				if action == "plugin:matrix-view:run" {
					for i, widget := range bar.left {
						node := widget.node
						t.Logf("left[%d]: kind=%d text=%q action=%q bounds=%+v children=%d", i, node.Kind, node.Text, node.Action, node.Bounds, len(node.Children))
					}
					t.Logf("overflow=%+v", bar.overflow)
				}
				bar.mu.Unlock()
				t.Fatalf("action %q has no visible bounds: %+v (%v)", action, bounds, ok)
			}
			got, hit := bar.hitLocked(bounds.X+bounds.W/2, bounds.Y+bounds.H/2)
			if !hit || got != action {
				bar.mu.Unlock()
				t.Fatalf("hit %q bounds %+v returned %q/%v", action, bounds, got, hit)
			}
		}
	}
	bar.mu.Unlock()

	assertBlurPaintCoverage(t, bar, scale, pixels, pw, ph)
	return pixels
}

func assertBlurPaintCoverage(t *testing.T, bar *Bar, scale ui.Scale120, pixels []byte, width, height int) {
	t.Helper()
	// At fractional scale, edge conversion can round one boundary pixel onto
	// the transparent side; tolerate it only when the adjacent pixel is painted.
	alpha := func(x, y int) byte {
		if x < 0 || y < 0 || x >= width || y >= height {
			return 0
		}
		return pixels[(y*width+x)*4+3]
	}
	for _, strip := range bar.blurShape() {
		physical := scale.PhysicalRect(strip)
		for y := physical.Y; y < physical.Y+physical.H; y++ {
			for x := physical.X; x < physical.X+physical.W; x++ {
				if alpha(x, y) > 0 {
					continue
				}
				edge := x == physical.X || x == physical.X+physical.W-1 ||
					y == physical.Y || y == physical.Y+physical.H-1
				adjacentPaint := alpha(x-1, y) > 0 || alpha(x+1, y) > 0 || alpha(x, y-1) > 0 || alpha(x, y+1) > 0
				if edge && adjacentPaint {
					continue
				}
				t.Fatalf("blur strip %+v includes unpainted pixel (%d,%d)", strip, x, y)
			}
		}
	}
}

func matrixActionBounds(bar *Bar, action string) (ui.Rect, bool) {
	for _, section := range bar.sections() {
		for _, node := range section {
			if bounds, ok := nodeActionBounds(node, action); ok {
				return bounds, true
			}
		}
	}
	return ui.Rect{}, false
}
