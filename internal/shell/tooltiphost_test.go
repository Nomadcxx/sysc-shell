package shell

import (
	"testing"
	"time"

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
	// Configured at the laptop's 1.25, so cards measure at the scale they
	// paint at, as they do on a real output.
	if err := r.bars[1].Configure(1536, 38, 150); err != nil {
		t.Fatal(err)
	}
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
	assertInsetClear(t, pix, w, hgt, s)
}

// Sweeping along the bar moves the card; it does not close and reopen it.
func TestASecondWidgetUpdatesTheCardInPlace(t *testing.T) {
	t.Parallel()
	_, h, hh := newTooltipFixture(t, true)
	h.show(tooltipRequest{Global: 1, Anchor: ui.Rect{X: 700, W: 30, H: 38}, Text: "Volume 40%"})
	first := onlyOpen(t, hh)
	h.show(tooltipRequest{Global: 1, Anchor: ui.Rect{X: 300, W: 30, H: 38}, Text: "Wi-Fi: home network, strong signal"})

	if len(hh.opens) != 1 || len(hh.closes) != 0 {
		t.Fatalf("opens %d closes %d, want the one surface kept", len(hh.opens), len(hh.closes))
	}
	if len(hh.updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(hh.updates))
	}
	u := hh.updates[0]
	if u.Width == nil || u.Height == nil || u.MarginTop == nil || u.MarginLeft == nil {
		t.Fatalf("update %+v, want size and margins", u)
	}
	if int32(*u.Width) <= first.Width {
		t.Fatalf("width %d, want wider than the first card's %d", *u.Width, first.Width)
	}
	if *u.MarginLeft >= first.MarginLeft {
		t.Fatalf("left margin %d, want the card moved left of %d", *u.MarginLeft, first.MarginLeft)
	}
	if shape := first.Callbacks.BlurShape(); len(shape) == 0 || shape[len(shape)-1].W > int(*u.Width) {
		t.Fatalf("blur shape %+v does not follow the new %d width", shape, *u.Width)
	}
}

func TestLeavingClosesTheCard(t *testing.T) {
	t.Parallel()
	r, h, _ := newTooltipFixture(t, true)
	// The relay sends off the registry lock, so capture on a channel.
	sent := make(chan wayland.AuxRequest, 8)
	h.request = func(q wayland.AuxRequest) { sent <- q }
	r.tooltips = h
	d := newDwell(time.Millisecond)
	t.Cleanup(d.stop)
	go r.relayTooltips(d)

	d.enter(1, ui.Rect{X: 700, W: 30, H: 38}, "Volume 40%")
	open := nextAux(t, sent)
	if open.Open == nil {
		t.Fatalf("first request %+v, want an open", open)
	}
	d.leave()
	closed := nextAux(t, sent)
	if closed.Open != nil || closed.Update != nil || closed.ID != open.Open.ID {
		t.Fatalf("request %+v, want the close of %q", closed, open.Open.ID)
	}
}

func nextAux(t *testing.T, ch <-chan wayland.AuxRequest) wayland.AuxRequest {
	t.Helper()
	select {
	case q := <-ch:
		return q
	case <-time.After(2 * time.Second):
		t.Fatal("no aux request arrived")
		return wayland.AuxRequest{}
	}
}

// The owner closed the surface on its own -- the compositor, or the output
// leaving. The host forgets it rather than updating a surface that is gone.
func TestACompositorCloseIsForgotten(t *testing.T) {
	t.Parallel()
	r, h, hh := newTooltipFixture(t, true)
	r.tooltips = h
	h.show(tooltipRequest{Global: 1, Anchor: ui.Rect{X: 700, W: 30, H: 38}, Text: "Volume 40%"})
	r.DropAux(1, hh.opens[0].ID)
	h.show(tooltipRequest{Global: 1, Anchor: ui.Rect{X: 300, W: 30, H: 38}, Text: "Battery 80%"})

	if len(hh.opens) != 2 || len(hh.updates) != 0 {
		t.Fatalf("opens %d updates %d, want the next hover to open afresh", len(hh.opens), len(hh.updates))
	}
	if hh.opens[1].ID == hh.opens[0].ID {
		t.Fatalf("reopened under the dropped id %q", hh.opens[0].ID)
	}
}

// A late report for a card already replaced must not forget its successor.
func TestAStaleCloseReportKeepsTheNewCard(t *testing.T) {
	t.Parallel()
	r, h, hh := newTooltipFixture(t, false) // capture: a move reopens
	r.tooltips = h
	h.show(tooltipRequest{Global: 1, Anchor: ui.Rect{X: 700, W: 30, H: 38}, Text: "Volume 40%"})
	h.show(tooltipRequest{Global: 1, Anchor: ui.Rect{X: 300, W: 30, H: 38}, Text: "Battery 80%"})
	if len(hh.closes) != 1 || len(hh.opens) != 2 {
		t.Fatalf("closes %d opens %d, want a move over a capture to reopen", len(hh.closes), len(hh.opens))
	}
	r.DropAux(1, hh.opens[0].ID)
	h.hide()
	if len(hh.closes) != 2 || hh.closes[1] != hh.opens[1].ID {
		t.Fatalf("closes %v, want the second card closed on hide", hh.closes)
	}
}

func TestLosingTheOutputForgetsTheCard(t *testing.T) {
	t.Parallel()
	r, h, hh := newTooltipFixture(t, true)
	r.tooltips = h
	newHosts(t, r, map[uint32]string{2: "HDMI-A-1"})
	h.show(tooltipRequest{Global: 1, Anchor: ui.Rect{X: 700, W: 30, H: 38}, Text: "Volume 40%"})
	r.DropHost(1)
	h.show(tooltipRequest{Global: 2, Anchor: ui.Rect{X: 700, W: 30, H: 38}, Text: "Volume 40%"})

	if len(hh.closes) != 0 {
		t.Fatalf("closes %v, want none sent to an output that is gone", hh.closes)
	}
	if len(hh.opens) != 2 || hh.opens[1].ID == hh.opens[0].ID {
		t.Fatalf("opens %d, want a fresh card on the remaining output", len(hh.opens))
	}
}

// Without compositor blur the captured backdrop is what shows through the
// translucent ground, so it has to reach the paint.
func TestATooltipPaintsItsCapturedBackdrop(t *testing.T) {
	t.Parallel()
	r, h, hh := newTooltipFixture(t, false)
	// The default overlay is opaque, which hides any backdrop by design.
	r.mu.Lock()
	r.cfg.Theme.OverlayOpacity = 60
	r.mu.Unlock()
	h.show(tooltipRequest{Global: 1, Anchor: ui.Rect{X: 700, W: 30, H: 38}, Text: "Volume 40%"})
	spec := onlyOpen(t, hh)
	w, hgt := int(spec.Width), int(spec.Height)
	paint := func() []byte {
		pix := make([]byte, w*hgt*4)
		if err := spec.Callbacks.Render(pix, w, hgt, w*4); err != nil {
			t.Fatal(err)
		}
		return pix
	}
	plain := paint()
	shot := &ui.Image{Width: w, Height: hgt, Stride: w * 4, Pix: make([]byte, w*hgt*4)}
	for i := 0; i < len(shot.Pix); i += 4 {
		shot.Pix[i], shot.Pix[i+1], shot.Pix[i+2], shot.Pix[i+3] = 0, 0xff, 0, 0xff
	}
	spec.Callbacks.Backdrop(shot)
	withBackdrop := paint()
	i := (hgt/2*w + 2) * 4 // inside the ground, clear of glyphs
	if string(plain[i:i+4]) == string(withBackdrop[i:i+4]) {
		t.Fatalf("pixel %v is unchanged by the backdrop", plain[i:i+4])
	}
}

// assertInsetClear checks that every inset pixel clear of the rounded corners
// and the rim is the ground: no glyph reached into the host inset.
func assertInsetClear(t *testing.T, pix []byte, w, hgt int, s ui.Scale120) {
	t.Helper()
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

func TestBarAnchorOutputCoordinates(t *testing.T) {
	for _, edge := range []string{"top", "bottom", "left", "right"} {
		t.Run(edge, func(t *testing.T) {
			r, h, _ := newTooltipFixture(t, false)
			cfg := r.cfg
			cfg.Bar.Edge = edge
			r.mu.Lock()
			r.cfg = cfg
			r.mu.Unlock()
			bar := r.bars[1]
			barW, barH := 1536, cfg.Bar.SurfaceExtent()
			local := ui.Rect{X: 18, Y: 300, W: 24, H: 24}
			wantAnchor := local
			switch edge {
			case "bottom":
				wantAnchor.Y += 960 - barH
			case "left":
				barW, barH = cfg.Bar.SurfaceExtent(), 960
			case "right":
				barW, barH = cfg.Bar.SurfaceExtent(), 960
				wantAnchor.X += 1536 - barW
			}
			bar.setOutputSize(1536, 960)
			if err := bar.Configure(barW, barH, 150); err != nil {
				t.Fatal(err)
			}
			h.show(tooltipRequest{Global: 1, Anchor: local, Text: "Volume"})
			want := tooltipPlacement(edge, wantAnchor, h.open.place.W, h.open.place.H, 1536, 960)
			if h.open.place != want {
				t.Fatalf("bar-local placement = %+v, want %+v from output anchor %+v", h.open.place, want, wantAnchor)
			}

			// Panel tooltip anchors already use output coordinates. Passing the
			// same rectangle through that path must not apply the bar offset again.
			h.hide()
			h.show(tooltipRequest{Global: 1, Anchor: wantAnchor, Text: "Volume", OnOutput: true})
			if got := h.open.place; got != want {
				t.Fatalf("output-local placement = %+v, want %+v", got, want)
			}
		})
	}
}
