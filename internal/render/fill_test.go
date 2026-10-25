package render

import (
	"fmt"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// roundedFillGeometries spans the shipped shapes plus the degenerate ones the
// band split has to survive: a square fill, radii that clamp to half the
// shorter side, odd dimensions where the half-pixel geometry could drift, and
// a radius larger than the box.
var roundedFillGeometries = []struct {
	name         string
	w, h, radius int
}{
	{"square", 64, 64, 0},
	{"one pixel", 1, 1, 8},
	{"hairline wide", 400, 3, 24},
	{"panel body", 2240, 1640, 48},
	{"panel at 1x", 1120, 820, 24},
	{"card", 300, 40, 20},
	{"odd size", 33, 77, 2},
	{"odd size odd radius", 41, 41, 7},
	{"full circle", 50, 50, 25},
	{"radius above half", 60, 20, 40},
	{"stadium", 200, 30, 15},
	{"tall and narrow", 20, 400, 6},
	{"radius one", 90, 40, 1},
}

// TestRoundedCoverageIsOpaqueInsideTheInterior is the premise the banded fill
// stands on: a rounded rectangle's coverage is exactly 255 throughout the
// interior the band split fills directly. The interior is the box minus the
// radius-wide corner columns and, on an odd-sided box, the top and bottom
// antialiased edge rows - everything left sits at least half a pixel inside the
// silhouette, so its coverage is exactly 255. If this ever stops holding,
// filling the interior as a plain rectangle would clip or shadow its edge, so
// it is pinned rather than assumed (GH #455).
//
// Only this direction is required. Some pixels outside the interior are opaque
// too (the inner quarter-discs under the corner arcs, and the even-sided edge
// rows), and those go through the mask where its coverage is 255, which blends
// to the same bytes. Claiming the converse would pin an optimally tight
// interior the fill does not need.
func TestRoundedCoverageIsOpaqueInsideTheInterior(t *testing.T) {
	t.Parallel()
	for _, tc := range roundedFillGeometries {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mask := RoundedMask(tc.radius, tc.w, tc.h)
			_, interior := roundedFillBands(tc.w, tc.h, tc.radius)
			b := mask.Bounds()
			for y := interior.Y; y < interior.Y+interior.H; y++ {
				for x := interior.X; x < interior.X+interior.W; x++ {
					if got := mask.AlphaAt(b.Min.X+x, b.Min.Y+y).A; got != 255 {
						t.Fatalf("coverage at (%d, %d) = %d, want 255 inside the interior %v",
							x, y, got, interior)
					}
				}
			}
		})
	}
}

// TestRoundedFillBandsCoverTheBox pins the band split to the box it fills:
// every pixel is claimed exactly once by the masked frame or the opaque
// interior, so the banded fill cannot leave a hole or blend a pixel twice. The
// degenerate sizes produce empty rectangles rather than negative ones.
func TestRoundedFillBandsCoverTheBox(t *testing.T) {
	t.Parallel()
	for _, tc := range roundedFillGeometries {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			masked, opaque := roundedFillBands(tc.w, tc.h, tc.radius)
			seen := make([]int, tc.w*tc.h)
			claim := func(band ui.Rect) {
				if band.W < 0 || band.H < 0 {
					t.Fatalf("band %v has a negative extent", band)
				}
				for y := band.Y; y < band.Y+band.H; y++ {
					for x := band.X; x < band.X+band.W; x++ {
						if x < 0 || y < 0 || x >= tc.w || y >= tc.h {
							t.Fatalf("band %v claims (%d, %d) outside the box", band, x, y)
						}
						seen[y*tc.w+x]++
					}
				}
			}
			for _, band := range masked {
				claim(band)
			}
			claim(opaque)
			for y := 0; y < tc.h; y++ {
				for x := 0; x < tc.w; x++ {
					if got := seen[y*tc.w+x]; got != 1 {
						t.Fatalf("pixel (%d, %d) claimed %d times, want exactly once", x, y, got)
					}
				}
			}
		})
	}
}

// roundedFillCases place a fill over a non-blank destination, which is what a
// panel body, a card and a bar fill all do: the surface already holds the
// shadow and the backdrop. The banded fill must land on the same bytes as the
// full-mask blend it replaces, including where it straddles the canvas edge,
// where a restrict rectangle clips it to a partial repaint, and where the
// destination is translucent so the straight bands cannot take a copy shortcut.
var roundedFillCases = []struct {
	name     string
	cw, ch   int
	rect     ui.Rect
	radius   int
	col      Color
	restrict ui.Rect
	blank    bool
}{
	{"opaque body", 320, 240, ui.Rect{W: 320, H: 240}, 24, Color{A: 0xff}, ui.Rect{}, false},
	{"translucent body", 320, 240, ui.Rect{W: 320, H: 240}, 24,
		Color{R: 0x3a, G: 0x41, B: 0x49, A: 0x80}, ui.Rect{}, false},
	{"translucent over a clear surface", 200, 160, ui.Rect{X: 10, Y: 10, W: 180, H: 140}, 16,
		Color{R: 0x11, G: 0x22, B: 0x33, A: 0x40}, ui.Rect{}, true},
	{"inset card", 200, 120, ui.Rect{X: 12, Y: 8, W: 160, H: 90}, 12,
		Color{R: 0xff, G: 0xff, B: 0xff, A: 0x40}, ui.Rect{}, false},
	{"square fill", 64, 64, ui.Rect{W: 64, H: 64}, 0, Color{R: 0x10, A: 0xff}, ui.Rect{}, false},
	{"square translucent", 96, 48, ui.Rect{X: 4, Y: 4, W: 88, H: 40}, 0,
		Color{R: 0xaa, G: 0xbb, B: 0xcc, A: 0x7f}, ui.Rect{}, false},
	{"stadium", 300, 40, ui.Rect{X: 4, Y: 4, W: 292, H: 32}, 16,
		Color{G: 0x80, A: 0xaa}, ui.Rect{}, false},
	{"full circle", 60, 60, ui.Rect{X: 2, Y: 2, W: 56, H: 56}, 28,
		Color{R: 0x30, G: 0x80, B: 0xd0, A: 0xc0}, ui.Rect{}, false},
	{"odd size", 100, 30, ui.Rect{X: 3, Y: 2, W: 88, H: 22}, 12,
		Color{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xc0}, ui.Rect{}, false},
	{"odd dimensions", 120, 90, ui.Rect{X: 2, Y: 3, W: 87, H: 59}, 24,
		Color{R: 0x40, G: 0x60, B: 0x80, A: 0xd0}, ui.Rect{}, false},
	{"hairline", 200, 40, ui.Rect{X: 5, Y: 18, W: 190, H: 3}, 24,
		Color{R: 0x20, A: 0xc0}, ui.Rect{}, false},
	{"straddling the near edge", 80, 60, ui.Rect{X: -14, Y: -9, W: 70, H: 50}, 10,
		Color{R: 0xee, G: 0x99, B: 0x22, A: 0xdd}, ui.Rect{}, false},
	{"straddling the far edge", 80, 60, ui.Rect{X: 46, Y: 30, W: 70, H: 50}, 10,
		Color{R: 0x22, G: 0x99, B: 0xee, A: 0xdd}, ui.Rect{}, false},
	{"past the far corner", 80, 60, ui.Rect{X: 100, Y: 90, W: 40, H: 30}, 8,
		Color{A: 0xff}, ui.Rect{}, false},
	{"restricted", 200, 160, ui.Rect{X: 10, Y: 10, W: 180, H: 140}, 16,
		Color{R: 0x77, G: 0x55, B: 0x33, A: 0x90}, ui.Rect{X: 30, Y: 40, W: 90, H: 60}, false},
	{"restricted across a corner", 200, 160, ui.Rect{X: 10, Y: 10, W: 180, H: 140}, 16,
		Color{R: 0x77, G: 0x55, B: 0x33, A: 0xb0}, ui.Rect{X: 12, Y: 12, W: 24, H: 24}, false},
	{"fully transparent", 40, 40, ui.Rect{X: 2, Y: 2, W: 36, H: 36}, 8,
		Color{R: 0xff, G: 0xff, B: 0xff}, ui.Rect{}, false},
}

func TestFillRoundedMatchesFullBlend(t *testing.T) {
	t.Parallel()
	for i, tc := range roundedFillCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, want := seededCanvas(t, tc.cw, tc.ch, i), seededCanvas(t, tc.cw, tc.ch, i)
			if tc.blank {
				clear(got.Pix)
				clear(want.Pix)
			}
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

// TestFillRoundedLeavesTheStraightInteriorUntouched pins the point of the
// banding with the arithmetic that motivates it: the opaque interior is the
// bulk of a box wider and taller than its radius, so the fill writes those rows
// directly instead of reading a coverage plane that is 255 everywhere there.
func TestFillRoundedLeavesTheStraightInteriorUntouched(t *testing.T) {
	t.Parallel()
	const w, h, radius = 480, 320, 24
	masked, opaque := roundedFillBands(w, h, radius)
	area := 0
	for _, band := range masked {
		area += band.W * band.H
	}
	if opaque.W*opaque.H*10 < w*h*9 {
		t.Fatalf("the opaque interior is %dx%d of %dx%d, want the bulk of the box",
			opaque.W, opaque.H, w, h)
	}
	if area*4 > w*h {
		t.Fatalf("the masked frame covers %d pixels, want a thin border", area)
	}
}

// TestFillRoundedMaskFullCoverageFillsTheBox covers a mask passed with a radius
// it was not built for. image.Alpha records no radius, so FillRoundedMask cannot
// detect a mismatch and bands against the radius it is handed; the caller is
// expected to pass the two together. What must hold regardless is that a
// full-coverage mask - a square - fills its whole box: the radius hint only
// picks which pixels read the mask, never whether a pixel is written. The fill
// is compared with the reference blend so the assertion is about bytes, not one
// channel's offset in the canvas's memory order.
func TestFillRoundedMaskFullCoverageFillsTheBox(t *testing.T) {
	t.Parallel()
	for _, hint := range []int{0, 4, 8, 20, 40} {
		t.Run(fmt.Sprintf("radius hint %d", hint), func(t *testing.T) {
			t.Parallel()
			const s = 30
			col := Color{R: 0xff, A: 0xff}
			mask := RoundedMask(0, s, s) // a square: coverage 255 at every pixel
			got, want := seededCanvas(t, 40, 40, 2), seededCanvas(t, 40, 40, 2)
			got.FillRoundedMask(mask, hint, 0, 0, col)
			referenceBlendMask(want, mask, 0, 0, col)
			for j := range want.Pix {
				if got.Pix[j] != want.Pix[j] {
					t.Fatalf("byte %d = %d, want %d (radius hint %d)", j, got.Pix[j], want.Pix[j], hint)
				}
			}
		})
	}
}

// BenchmarkFillRounded measures the banded fill over the shapes a Settings body
// and a bar hit: opaque and translucent, radius 0 and radius > 0, over a full
// and a restricted (partially repainted) canvas. GH #455's budget is to bring a
// 2x body fill from about 33 ms to under 8 ms a frame; this reports the per-op
// cost the banded fill actually pays.
func BenchmarkFillRounded(b *testing.B) {
	cases := []struct {
		name     string
		w, h     int
		radius   int
		col      Color
		restrict ui.Rect
	}{
		{"opaque body r48", 2240, 1640, 48, Color{A: 0xff}, ui.Rect{}},
		{"translucent body r48", 2240, 1640, 48, Color{R: 0x3a, G: 0x41, B: 0x49, A: 0x80}, ui.Rect{}},
		{"square r0", 2240, 1640, 0, Color{A: 0xff}, ui.Rect{}},
		{"card r20", 300, 40, 20, Color{R: 0xff, A: 0xff}, ui.Rect{}},
		{"restricted r48", 2240, 1640, 48, Color{R: 0x77, A: 0x90}, ui.Rect{X: 30, Y: 40, W: 900, H: 600}},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			c, err := NewCanvas(make([]byte, tc.w*4*tc.h), tc.w, tc.h, tc.w*4)
			if err != nil {
				b.Fatal(err)
			}
			if tc.restrict.W > 0 {
				c.restrict = tc.restrict
			}
			r := ui.Rect{W: tc.w, H: tc.h}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c.FillRounded(r, tc.radius, tc.col)
			}
		})
	}
}
