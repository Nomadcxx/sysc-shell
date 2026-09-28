package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// newTooltipFixture is a registry with one bar on a 1536x960 logical output,
// the laptop's, and a tooltip host whose requests are captured.
func newTooltipFixture(t *testing.T, blur bool) (*Registry, *tooltipHost, *hostHarness) {
	t.Helper()
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	cfg.Theme.BlurBehind = true
	cfg.Theme.PanelOpacity = 65
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	r.caps = wayland.Capabilities{Blur: blur}
	newHosts(t, r, map[uint32]string{1: "eDP-1"})
	r.bars[1].setOutputSize(1536, 960)
	hh := &hostHarness{}
	return r, newTooltipHost(r, hh), hh
}

func onlyOpen(t *testing.T, hh *hostHarness) *wayland.AuxSpec {
	t.Helper()
	if len(hh.opens) != 1 {
		t.Fatalf("opens = %d, want 1", len(hh.opens))
	}
	return hh.opens[0]
}

func TestATooltipUnderCompositorBlurIsAPanelGlassCard(t *testing.T) {
	t.Parallel()
	r, h, hh := newTooltipFixture(t, true)
	h.show(tooltipRequest{Global: 1, Anchor: ui.Rect{X: 700, W: 30, H: 38}, Text: "Volume 40%"})

	spec := onlyOpen(t, hh)
	if spec.BlurRegion != nil {
		t.Fatalf("spec captures %+v, want the compositor to blur instead", *spec.BlurRegion)
	}
	r.mu.Lock()
	style := h.cardStyle()
	panel := r.panelThemeFor(1).Surfaces.Panel
	r.mu.Unlock()
	if style.SurfaceOpacity != panel {
		t.Fatalf("ground alpha = %d, want the panel alpha %d", style.SurfaceOpacity, panel)
	}
	shape := spec.Callbacks.BlurShape()
	want := ui.BlurStrips(ui.SurfaceShape{Body: ui.Rect{W: int(spec.Width), H: int(spec.Height)}, Radius: style.Radius})
	if len(shape) == 0 || len(shape) != len(want) {
		t.Fatalf("blur shape = %+v, want the card's rounded strips %+v", shape, want)
	}
	for i := range want {
		if shape[i] != want[i] {
			t.Fatalf("blur strip %d = %+v, want %+v", i, shape[i], want[i])
		}
	}
}

func TestATooltipWithoutCompositorBlurCapturesAndKeepsTheOverlayGround(t *testing.T) {
	t.Parallel()
	r, h, hh := newTooltipFixture(t, false)
	h.show(tooltipRequest{Global: 1, Anchor: ui.Rect{X: 700, W: 30, H: 38}, Text: "Volume 40%"})

	spec := onlyOpen(t, hh)
	if spec.BlurRegion == nil {
		t.Fatal("spec carries no backdrop capture, want one with blur-behind on")
	}
	if got := *spec.BlurRegion; got.W != int(spec.Width) || got.H != int(spec.Height) ||
		got.X != int(spec.MarginLeft) || got.Y != int(spec.MarginTop) {
		t.Fatalf("capture %+v, want the card's own output rect", got)
	}
	if shape := spec.Callbacks.BlurShape(); len(shape) != 0 {
		t.Fatalf("blur shape = %+v, want none without compositor blur", shape)
	}
	r.mu.Lock()
	style := h.cardStyle()
	overlay := r.panelThemeFor(1).Surfaces.Overlay
	r.mu.Unlock()
	if style.SurfaceOpacity != overlay {
		t.Fatalf("ground alpha = %d, want the overlay alpha %d", style.SurfaceOpacity, overlay)
	}
}

func TestATooltipHasAQuietRimAndASmallCorner(t *testing.T) {
	t.Parallel()
	r, h, _ := newTooltipFixture(t, true)
	h.show(tooltipRequest{Global: 1, Anchor: ui.Rect{X: 700, W: 30, H: 38}, Text: "Volume 40%"})

	r.mu.Lock()
	style := h.cardStyle()
	th := r.panelThemeFor(1)
	r.mu.Unlock()
	if style.Rim != th.Palette.OutlineVariant {
		t.Fatalf("rim = %+v, want OutlineVariant %+v", style.Rim, th.Palette.OutlineVariant)
	}
	if style.Radius != th.Shapes.Small || th.Shapes.Small >= th.Radius {
		t.Fatalf("radius = %d, want ShapeSmall %d (theme radius %d)", style.Radius, th.Shapes.Small, th.Radius)
	}
}

// At 1.25 the physical inset is fractional. No glyph may land in it: every
// inset pixel clear of the rounded corners is the ground.
func TestPaintingATooltipAtFractionalScaleKeepsTheInsetClear(t *testing.T) {
	t.Parallel()
	_, h, hh := newTooltipFixture(t, true)
	h.show(tooltipRequest{Global: 1, Anchor: ui.Rect{X: 700, W: 30, H: 38}, Text: "Wg Volume 40% Qy"})
	spec := onlyOpen(t, hh)

	const scale120 = 150
	s := ui.Scale120(scale120)
	w, hgt := s.Physical(int(spec.Width)), s.Physical(int(spec.Height))
	if err := spec.Callbacks.Configure(int(spec.Width), int(spec.Height), scale120); err != nil {
		t.Fatal(err)
	}
	pix := make([]byte, w*hgt*4)
	if err := spec.Callbacks.Render(pix, w, hgt, w*4); err != nil {
		t.Fatal(err)
	}
	at := func(x, y int) [4]byte {
		i := y*w*4 + x*4
		return [4]byte{pix[i], pix[i+1], pix[i+2], pix[i+3]}
	}
	ground := at(w/2, 3)
	if ground[3] == 0 {
		t.Fatal("the ground is transparent; nothing was painted")
	}
	insetX := s.Physical(theme.MarginM) - 1
	insetY := s.Physical(theme.MarginS) - 1
	corner := s.Physical(8) // past the small radius and its rim
	edge := 2               // the rim and its antialiasing
	for y := edge; y < hgt-edge; y++ {
		for x := edge; x < w-edge; x++ {
			inInset := x < insetX || x >= w-insetX || y < insetY || y >= hgt-insetY
			nearCorner := (x < corner || x >= w-corner) && (y < corner || y >= hgt-corner)
			if !inInset || nearCorner {
				continue
			}
			if got := at(x, y); got != ground {
				t.Fatalf("inset pixel (%d,%d) = %v, want the ground %v", x, y, got, ground)
			}
		}
	}
}
