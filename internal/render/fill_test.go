package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// fillBandGeometries is the shape matrix the coverage bands must hold. It
// spans the shipped sizes (the panel body fills the whole surface at
// 2240x1640 and 1280x1440), the small control sizes, squares, thin rectangles,
// sizes whose radius is capped to half the shorter side, and odd dimensions
// where the integer corner maths could drift.
var fillBandGeometries = []struct {
	name   string
	w, h   int
	radius int
	square Corners
}{
	{"panel body", 440, 800, 24, 0},
	{"double-scale panel body", 880, 1600, 24, 0},
	{"wide panel body", 1280, 1440, 24, 0},
	{"settings body at scale 2", 2240, 1640, 24, 0},
	{"bar fill", 2880, 120, 16, 0},
	{"card", 300, 40, 20, 0},
	{"square", 64, 64, 0, 0},
	{"small capsule", 100, 30, 12, 0},
	{"odd size", 33, 77, 2, 0},
	{"odd size large radius", 41, 41, 3, 0},
	{"radius above half height", 60, 20, 40, 0},
	{"full circle", 50, 50, 25, 0},
	{"tall and narrow", 20, 400, 6, 0},
	{"wide and short", 400, 20, 6, 0},
	{"squared top edge", 2240, 1640, 24, SquareTL | SquareTR},
	{"squared corner", 880, 1600, 24, SquareTL | SquareBR},
}

// TestFillBandsCoverTheMask pins the band geometry to the mask it is derived
// from: every pixel CornerMask gives less than full coverage must fall inside
// the frame fillBands measures. A miss would leave an edge pixel blended at
// the wrong coverage, and because the miss depends on the radius and on the
// parity of the dimensions it would show up on some node sizes and not others.
// The bands are also the reason the fill can skip its mask everywhere else at
// all, so this is the check that keeps the optimisation honest (GH #455).
func TestFillBandsCoverTheMask(t *testing.T) {
	t.Parallel()
	for _, tc := range fillBandGeometries {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mask := CornerMask(tc.radius, tc.w, tc.h, tc.square)
			band := fillBands(tc.radius, tc.w, tc.h)
			if band <= 0 {
				t.Fatalf("fillBands(%d, %d, %d) = %d, want a positive band",
					tc.radius, tc.w, tc.h, band)
			}
			for y := 0; y < tc.h; y++ {
				for x := 0; x < tc.w; x++ {
					if mask.Pix[y*mask.Stride+x] == 255 {
						continue
					}
					frame := x < band || x >= tc.w-band || y < band || y >= tc.h-band
					if !frame {
						t.Fatalf("coverage %d at (%d, %d) lies outside the frame band=%d",
							mask.Pix[y*mask.Stride+x], x, y, band)
					}
				}
			}
		})
	}
}

// TestFillBandsCoverSmallShapes sweeps every radius and every box up to 32
// pixels, the sizes where the clamp to half the shorter side and the arc seam
// land differently on each geometry. It is the exhaustive half of the pin: the
// table above covers the shipped shapes, this covers the shapes a future
// control could take.
func TestFillBandsCoverSmallShapes(t *testing.T) {
	t.Parallel()
	for radius := 0; radius <= 8; radius++ {
		for w := 1; w <= 32; w++ {
			for h := 1; h <= 32; h++ {
				mask := RoundedMask(radius, w, h)
				band := fillBands(radius, w, h)
				for y := 0; y < h; y++ {
					for x := 0; x < w; x++ {
						if mask.Pix[y*mask.Stride+x] == 255 {
							continue
						}
						frame := x < band || x >= w-band || y < band || y >= h-band
						if !frame {
							t.Fatalf("radius %d %dx%d: coverage %d at (%d, %d) lies outside band=%d",
								radius, w, h, mask.Pix[y*mask.Stride+x], x, y, band)
						}
					}
				}
			}
		}
	}
}

// fillBlendCases place a fill over a non-blank destination, which is what the
// panel body does: the surface already holds the shadow and the wallpaper. The
// banded blend must land on the same bytes as the full-mask blend it replaces,
// including translucent colours, where a double-blended pixel would show as a
// denser band, and where the fill straddles the canvas edge or a restrict
// rectangle clips it to a partial repaint.
var fillBlendCases = []struct {
	name     string
	cw, ch   int
	rect     ui.Rect
	radius   int
	square   Corners
	col      Color
	restrict ui.Rect
}{
	{"panel body", 440, 400, ui.Rect{W: 440, H: 400}, 24, 0,
		Color{R: 0x30, G: 0x80, B: 0xd0, A: 0xc0}, ui.Rect{}},
	{"settings body at scale 2", 2240, 1640, ui.Rect{W: 2240, H: 1640}, 24, 0,
		Color{R: 0x20, G: 0x24, B: 0x2c, A: 0xe6}, ui.Rect{}},
	{"opaque body", 880, 800, ui.Rect{W: 880, H: 800}, 24, 0,
		Color{R: 0x18, G: 0x1c, B: 0x24, A: 0xff}, ui.Rect{}},
	{"bar fill", 2880, 120, ui.Rect{W: 2880, H: 120}, 16, 0,
		Color{R: 0x10, G: 0x14, B: 0x18, A: 0xe6}, ui.Rect{}},
	{"inset card", 200, 120, ui.Rect{X: 12, Y: 8, W: 160, H: 90}, 12, 0,
		Color{R: 0xff, G: 0xff, B: 0xff, A: 0x40}, ui.Rect{}},
	{"square radius", 120, 90, ui.Rect{X: 5, Y: 5, W: 100, H: 70}, 0, 0,
		Color{R: 0x40, G: 0x90, B: 0x40, A: 0xff}, ui.Rect{}},
	{"stadium", 300, 40, ui.Rect{X: 4, Y: 4, W: 292, H: 32}, 16, 0,
		Color{G: 0x80, A: 0xaa}, ui.Rect{}},
	{"squared top edge", 300, 200, ui.Rect{W: 300, H: 200}, 20, SquareTL | SquareTR,
		Color{R: 0x3a, G: 0x41, B: 0x49, A: 0xd0}, ui.Rect{}},
	{"odd size", 100, 30, ui.Rect{X: 3, Y: 2, W: 88, H: 22}, 12, 0,
		Color{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xc0}, ui.Rect{}},
	{"negative origin", 80, 60, ui.Rect{X: -14, Y: -9, W: 70, H: 50}, 10, 0,
		Color{R: 0xee, G: 0x99, B: 0x22, A: 0xdd}, ui.Rect{}},
	{"straddling the far edge", 80, 60, ui.Rect{X: 46, Y: 30, W: 70, H: 50}, 10, 0,
		Color{R: 0x22, G: 0x99, B: 0xee, A: 0xdd}, ui.Rect{}},
	{"past the far corner", 80, 60, ui.Rect{X: 100, Y: 90, W: 40, H: 30}, 8, 0,
		Color{A: 0xff}, ui.Rect{}},
	{"restricted", 200, 160, ui.Rect{X: 10, Y: 10, W: 180, H: 140}, 16, 0,
		Color{R: 0x77, G: 0x55, B: 0x33, A: 0x90}, ui.Rect{X: 30, Y: 40, W: 90, H: 60}},
	{"restricted past the fill", 200, 160, ui.Rect{X: 0, Y: 0, W: 80, H: 60}, 12, 0,
		Color{R: 0x77, G: 0x55, B: 0x33, A: 0xb0}, ui.Rect{X: 120, Y: 120, W: 40, H: 30}},
	{"fully transparent", 120, 90, ui.Rect{W: 120, H: 90}, 12, 0,
		Color{R: 0xff, G: 0xff, B: 0xff}, ui.Rect{}},
}

// TestFillRoundedMatchesFullBlend holds the banded fill to a byte-for-byte
// match with the full-mask blend, over a non-blank destination. It compares
// against the generic-accessor reference rather than against blendMask, so a
// coverage or offset mistake shows up here too. FillRounded draws the plain
// rounded mask, so the reference squares nothing; the squared-corner cases
// exercise the same geometry on that path (GH #455).
func TestFillRoundedMatchesFullBlend(t *testing.T) {
	t.Parallel()
	for i, tc := range fillBlendCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, want := seededCanvas(t, tc.cw, tc.ch, i), seededCanvas(t, tc.cw, tc.ch, i)
			if tc.restrict.W > 0 {
				got.restrict, want.restrict = tc.restrict, tc.restrict
			}
			got.FillRounded(tc.rect, tc.radius, tc.col)
			referenceBlendMask(want, RoundedMask(tc.radius, tc.rect.W, tc.rect.H),
				tc.rect.X, tc.rect.Y, tc.col)
			for j := range want.Pix {
				if got.Pix[j] != want.Pix[j] {
					t.Fatalf("byte %d = %d, want %d", j, got.Pix[j], want.Pix[j])
				}
			}
		})
	}
}

// TestBlendFillMatchesFullBlend does the same for the corner-masked fill the
// panel root draws, where a squared corner changes the mask the bands are
// measured from.
func TestBlendFillMatchesFullBlend(t *testing.T) {
	t.Parallel()
	for i, tc := range fillBlendCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, want := seededCanvas(t, tc.cw, tc.ch, i), seededCanvas(t, tc.cw, tc.ch, i)
			if tc.restrict.W > 0 {
				got.restrict, want.restrict = tc.restrict, tc.restrict
			}
			blendFill(got, tc.rect, tc.radius, tc.square, tc.col)
			referenceBlendMask(want, CornerMask(tc.radius, tc.rect.W, tc.rect.H, tc.square),
				tc.rect.X, tc.rect.Y, tc.col)
			for j := range want.Pix {
				if got.Pix[j] != want.Pix[j] {
					t.Fatalf("byte %d = %d, want %d", j, got.Pix[j], want.Pix[j])
				}
			}
		})
	}
}

// TestBlendFillMatchesFullBlendOverClear repeats the identity check against
// destinations whose runs are clear, either wholly or in part. The interior
// takes the colour outright when a run is empty instead of blending it, and a
// destination that changes from one run to the next has to switch back mid-box
// without landing on different bytes (GH #455).
func TestBlendFillMatchesFullBlendOverClear(t *testing.T) {
	t.Parallel()
	for i, tc := range fillBlendCases {
		for _, dest := range []string{"clear", "half and half"} {
			t.Run(tc.name+"/"+dest, func(t *testing.T) {
				t.Parallel()
				got, want := seededCanvas(t, tc.cw, tc.ch, i), seededCanvas(t, tc.cw, tc.ch, i)
				for _, c := range []*Canvas{got, want} {
					if dest == "clear" {
						clear(c.Pix)
					} else {
						clear(c.Pix[len(c.Pix)/2:])
					}
				}
				if tc.restrict.W > 0 {
					got.restrict, want.restrict = tc.restrict, tc.restrict
				}
				blendFill(got, tc.rect, tc.radius, tc.square, tc.col)
				referenceBlendMask(want, CornerMask(tc.radius, tc.rect.W, tc.rect.H, tc.square),
					tc.rect.X, tc.rect.Y, tc.col)
				for j := range want.Pix {
					if got.Pix[j] != want.Pix[j] {
						t.Fatalf("byte %d = %d, want %d", j, got.Pix[j], want.Pix[j])
					}
				}
			})
		}
	}
}
