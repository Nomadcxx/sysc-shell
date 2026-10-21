package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// strokeBandGeometries is the shape matrix the coverage bands must hold. It
// spans the shipped sizes (a panel rim strokes the whole surface at 880x1600
// and 1280x1440), the small control sizes, square rings, thin rectangles, sizes
// whose radius or stroke is capped to half the shorter side, and odd dimensions
// where the integer corner maths could drift.
var strokeBandGeometries = []struct {
	name                string
	w, h, radius, width int
}{
	{"panel rim", 440, 800, 24, 2},
	{"double-scale panel rim", 880, 1600, 24, 2},
	{"wide panel rim", 1280, 1440, 24, 2},
	{"hairline card", 300, 40, 20, 1},
	{"square ring", 64, 64, 0, 1},
	{"square ring wide", 64, 64, 0, 6},
	{"small capsule", 100, 30, 12, 1},
	{"odd size", 33, 77, 2, 1},
	{"odd size wide", 41, 41, 3, 5},
	{"radius above half height", 60, 20, 40, 2},
	{"width above half height", 60, 20, 2, 40},
	{"full circle", 50, 50, 25, 3},
	{"tall and narrow", 20, 400, 6, 2},
	{"thick stroke", 200, 120, 8, 20},
	{"radius equals width", 120, 60, 9, 9},
}

// TestStrokeBandsCoverTheRing pins the band geometry to the mask it is derived
// from: every pixel RingMask gives coverage must fall in the top or bottom band
// or, between them, in the left or right band. A miss would leave a hole in a
// stroke, and because the miss depends on the radius and the width it would
// show up on some node sizes and not others. The bands are also the reason the
// blend can skip the rectangle's empty interior at all, so this is the check
// that keeps the optimisation honest (GH #373).
func TestStrokeBandsCoverTheRing(t *testing.T) {
	t.Parallel()
	for _, tc := range strokeBandGeometries {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mask := RingMask(tc.radius, tc.w, tc.h, tc.width)
			top, side := strokeBands(tc.radius, tc.w, tc.h, tc.width)
			if top <= 0 || side <= 0 {
				t.Fatalf("strokeBands(%d, %d, %d, %d) = %d, %d, want positive bands",
					tc.radius, tc.w, tc.h, tc.width, top, side)
			}
			for y := 0; y < tc.h; y++ {
				for x := 0; x < tc.w; x++ {
					if mask.Pix[y*mask.Stride+x] == 0 {
						continue
					}
					middle := y >= top && y < tc.h-top
					covered := !middle || x < side || x >= tc.w-side
					if !covered {
						t.Fatalf("coverage at (%d, %d) lies outside the bands top=%d side=%d",
							x, y, top, side)
					}
				}
			}
		})
	}
}

// strokeBlendCases place a stroke over a non-blank destination, which is what a
// panel rim does: the surface already holds the body fill and the shadow. The
// banded blend must land on the same bytes as the full-mask blend it replaces,
// including where the stroke straddles the canvas edge and where a restrict
// rectangle clips it to a partial repaint.
var strokeBlendCases = []struct {
	name          string
	cw, ch        int
	rect          ui.Rect
	radius, width int
	col           Color
	restrict      ui.Rect
}{
	{"panel rim", 440, 400, ui.Rect{W: 440, H: 400}, 24, 2,
		Color{R: 0x30, G: 0x80, B: 0xd0, A: 0xc0}, ui.Rect{}},
	{"inset card", 200, 120, ui.Rect{X: 12, Y: 8, W: 160, H: 90}, 12, 2,
		Color{R: 0xff, G: 0xff, B: 0xff, A: 0x40}, ui.Rect{}},
	{"hairline capsule", 300, 40, ui.Rect{X: 4, Y: 4, W: 292, H: 32}, 20, 1,
		Color{G: 0x80, A: 0xaa}, ui.Rect{}},
	{"square ring", 64, 64, ui.Rect{W: 64, H: 64}, 0, 1,
		Color{R: 0x10, G: 0x20, B: 0x30, A: 0xff}, ui.Rect{}},
	{"odd size", 100, 30, ui.Rect{X: 3, Y: 2, W: 88, H: 22}, 12, 1,
		Color{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xc0}, ui.Rect{}},
	{"opaque stroke", 120, 90, ui.Rect{X: 5, Y: 5, W: 100, H: 70}, 10, 3,
		Color{R: 0x40, G: 0x90, B: 0x40, A: 0xff}, ui.Rect{}},
	{"straddling the near edge", 80, 60, ui.Rect{X: -14, Y: -9, W: 70, H: 50}, 10, 2,
		Color{R: 0xee, G: 0x99, B: 0x22, A: 0xdd}, ui.Rect{}},
	{"straddling the far edge", 80, 60, ui.Rect{X: 46, Y: 30, W: 70, H: 50}, 10, 2,
		Color{R: 0x22, G: 0x99, B: 0xee, A: 0xdd}, ui.Rect{}},
	{"past the far corner", 80, 60, ui.Rect{X: 100, Y: 90, W: 40, H: 30}, 8, 2,
		Color{A: 0xff}, ui.Rect{}},
	{"restricted", 200, 160, ui.Rect{X: 10, Y: 10, W: 180, H: 140}, 16, 2,
		Color{R: 0x77, G: 0x55, B: 0x33, A: 0x90}, ui.Rect{X: 30, Y: 40, W: 90, H: 60}},
	{"restricted past the stroke", 200, 160, ui.Rect{X: 0, Y: 0, W: 80, H: 60}, 12, 2,
		Color{R: 0x77, G: 0x55, B: 0x33, A: 0xb0}, ui.Rect{X: 120, Y: 120, W: 40, H: 30}},
}

// TestStrokeRoundedMatchesFullBlend holds the banded stroke to a byte-for-byte
// match with the full-mask blend, over a non-blank destination and a translucent
// colour. It compares against the generic-accessor reference rather than against
// blendMask, so a coverage or offset mistake shows up here too.
func TestStrokeRoundedMatchesFullBlend(t *testing.T) {
	t.Parallel()
	for i, tc := range strokeBlendCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, want := seededCanvas(t, tc.cw, tc.ch, i), seededCanvas(t, tc.cw, tc.ch, i)
			if tc.restrict.W > 0 {
				got.restrict, want.restrict = tc.restrict, tc.restrict
			}
			got.StrokeRounded(tc.rect, tc.radius, tc.width, tc.col)
			referenceBlendMask(want, RingMask(tc.radius, tc.rect.W, tc.rect.H, tc.width),
				tc.rect.X, tc.rect.Y, tc.col)
			for j := range want.Pix {
				if got.Pix[j] != want.Pix[j] {
					t.Fatalf("byte %d = %d, want %d", j, got.Pix[j], want.Pix[j])
				}
			}
		})
	}
}

// TestStrokeRoundedLeavesPixelsOutsideTheBands pins the point of the banded
// blend: pixels strictly inside the rectangle, past the bands, keep their bytes
// and are never visited. The seeded canvas would show any stray blend as a
// changed byte, and the counter of strokeBands' area makes the saving explicit.
func TestStrokeRoundedLeavesPixelsOutsideTheBands(t *testing.T) {
	t.Parallel()
	const w, h, radius, width = 440, 800, 24, 2
	c := seededCanvas(t, w, h, 7)
	before := append([]byte(nil), c.Pix...)
	c.StrokeRounded(ui.Rect{W: w, H: h}, radius, width, Color{R: 0xff, A: 0xff})
	top, side := strokeBands(radius, w, h, width)
	visited := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			middle := y >= top && y < h-top
			inBand := !middle || x < side || x >= w-side
			i := y*c.Stride + x*4
			changed := before[i] != c.Pix[i] || before[i+1] != c.Pix[i+1] ||
				before[i+2] != c.Pix[i+2] || before[i+3] != c.Pix[i+3]
			if changed && !inBand {
				t.Fatalf("pixel (%d, %d) changed outside the bands", x, y)
			}
			if inBand {
				visited++
			}
		}
	}
	if visited*10 >= w*h {
		t.Fatalf("the bands visit %d of %d pixels, want under a tenth of the rectangle",
			visited, w*h)
	}
}
