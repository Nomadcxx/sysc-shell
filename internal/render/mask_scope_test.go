package render

import (
	"image"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The scoped builders compute the same coverage plane the full builders do,
// restricted to a rectangle, so a tall surface never needs a mask bigger than
// the frame can see (GH #454). These checks pin every scoped pixel to the full
// mask's value at the same box coordinate, and pin the restricted render path
// to the unclipped one, so a scoping bug shows up as a byte difference.

// expectedMaskRect is the normalisation CornerMaskIn must perform, written out
// independently: the whole-box sentinel means the whole box, anything else is
// clipped to it. A divergence between this and the implementation shifts which
// coverage lands under a pixel and fails the pixel comparison below.
func expectedMaskRect(w, h int, in image.Rectangle) image.Rectangle {
	full := image.Rect(0, 0, max(w, 0), max(h, 0))
	if in == (image.Rectangle{}) {
		return full
	}
	return in.Intersect(full)
}

var scopedMaskGeometries = []struct {
	name      string
	w, h      int
	radius    int
	square    Corners
	ringWidth int
}{
	{"panel body at 2x", 2240, 1640, 24, 0, 3},
	{"tall settings card", 1736, 2400, 24, 0, 2},
	{"odd width and height", 33, 77, 10, 0, 2},
	{"radius past half", 41, 41, 40, 0, 2},
	{"radius zero", 100, 60, 0, 0, 2},
	{"square corners", 60, 200, 16, SquareTL | SquareBR, 2},
	{"tiny", 3, 3, 2, 0, 1},
}

func scopedInRects(w, h int) []struct {
	name string
	in   image.Rectangle
} {
	return []struct {
		name string
		in   image.Rectangle
	}{
		{"whole box sentinel", image.Rectangle{}},
		{"explicit whole box", image.Rect(0, 0, w, h)},
		{"top left quarter", image.Rect(0, 0, w/2, h/2)},
		{"corner strip", image.Rect(0, 0, min(w, 40), min(h, 40))},
		{"right edge band", image.Rect(max(w-40, 0), 0, w, h)},
		{"off edge overlap", image.Rect(max(w-20, 0), max(h-20, 0), w+50, h+50)},
		{"negative min overlap", image.Rect(-30, -30, min(w, 20), min(h, 20))},
		{"tiny centre", image.Rect(w/2, h/2, w/2+min(w, 2), h/2+min(h, 2))},
		{"fully disjoint", image.Rect(w+10, h+10, w+40, h+40)},
	}
}

func TestCornerMaskInMatchesCornerMask(t *testing.T) {
	for _, geo := range scopedMaskGeometries {
		t.Run(geo.name, func(t *testing.T) {
			want := CornerMask(geo.radius, geo.w, geo.h, geo.square)
			for _, tc := range scopedInRects(geo.w, geo.h) {
				t.Run(tc.name, func(t *testing.T) {
					exp := expectedMaskRect(geo.w, geo.h, tc.in)
					got := CornerMaskIn(geo.radius, geo.w, geo.h, geo.square, tc.in)
					if exp.Empty() {
						if got.Bounds().Dx() != 0 || got.Bounds().Dy() != 0 {
							t.Fatalf("disjoint rect built a %vx%v mask", got.Bounds().Dx(), got.Bounds().Dy())
						}
						return
					}
					if got.Bounds().Dx() != exp.Dx() || got.Bounds().Dy() != exp.Dy() {
						t.Fatalf("mask is %vx%v, want %vx%v", got.Bounds().Dx(), got.Bounds().Dy(), exp.Dx(), exp.Dy())
					}
					for iy := 0; iy < exp.Dy(); iy++ {
						for ix := 0; ix < exp.Dx(); ix++ {
							g := got.AlphaAt(got.Bounds().Min.X+ix, got.Bounds().Min.Y+iy).A
							wv := want.AlphaAt(exp.Min.X+ix, exp.Min.Y+iy).A
							if g != wv {
								t.Fatalf("coverage at box (%d,%d): got %d, want %d", exp.Min.X+ix, exp.Min.Y+iy, g, wv)
							}
						}
					}
				})
			}
		})
	}
}

func TestRingMaskInMatchesRingMask(t *testing.T) {
	for _, geo := range scopedMaskGeometries {
		t.Run(geo.name, func(t *testing.T) {
			width := min(geo.ringWidth, min(geo.w, geo.h)/2)
			want := RingMask(geo.radius, geo.w, geo.h, width)
			for _, tc := range scopedInRects(geo.w, geo.h) {
				t.Run(tc.name, func(t *testing.T) {
					exp := expectedMaskRect(geo.w, geo.h, tc.in)
					got := RingMaskIn(geo.radius, geo.w, geo.h, width, tc.in)
					if exp.Empty() {
						if got.Bounds().Dx() != 0 || got.Bounds().Dy() != 0 {
							t.Fatalf("disjoint rect built a %vx%v mask", got.Bounds().Dx(), got.Bounds().Dy())
						}
						return
					}
					if got.Bounds().Dx() != exp.Dx() || got.Bounds().Dy() != exp.Dy() {
						t.Fatalf("mask is %vx%v, want %vx%v", got.Bounds().Dx(), got.Bounds().Dy(), exp.Dx(), exp.Dy())
					}
					for iy := 0; iy < exp.Dy(); iy++ {
						for ix := 0; ix < exp.Dx(); ix++ {
							g := got.AlphaAt(got.Bounds().Min.X+ix, got.Bounds().Min.Y+iy).A
							wv := want.AlphaAt(exp.Min.X+ix, exp.Min.Y+iy).A
							if g != wv {
								t.Fatalf("ring coverage at box (%d,%d): got %d, want %d", exp.Min.X+ix, exp.Min.Y+iy, g, wv)
							}
						}
					}
				})
			}
		})
	}
}

func TestRippleMaskScopedMatchesFull(t *testing.T) {
	cases := []struct {
		name         string
		w, h, radius int
		cx, cy       int
		disc         float64
	}{
		{"disc over the middle", 200, 120, 16, 100, 60, 40},
		{"disc past the corner", 200, 120, 16, -20, -20, 25},
		{"tiny disc", 200, 120, 0, 30, 30, 0.5},
		{"disc beyond the box", 200, 120, 16, 400, 300, 80},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := RippleMask(tc.w, tc.h, tc.radius, tc.cx, tc.cy, tc.disc, image.Rectangle{})
			for _, in := range scopedInRects(tc.w, tc.h) {
				t.Run(in.name, func(t *testing.T) {
					exp := expectedMaskRect(tc.w, tc.h, in.in)
					got := RippleMask(tc.w, tc.h, tc.radius, tc.cx, tc.cy, tc.disc, in.in)
					if exp.Empty() {
						if got.Bounds().Dx() != 0 || got.Bounds().Dy() != 0 {
							t.Fatalf("disjoint rect built a %vx%v mask", got.Bounds().Dx(), got.Bounds().Dy())
						}
						return
					}
					for iy := 0; iy < exp.Dy(); iy++ {
						for ix := 0; ix < exp.Dx(); ix++ {
							g := got.AlphaAt(got.Bounds().Min.X+ix, got.Bounds().Min.Y+iy).A
							wv := want.AlphaAt(exp.Min.X+ix, exp.Min.Y+iy).A
							if g != wv {
								t.Fatalf("ripple coverage at box (%d,%d): got %d, want %d", exp.Min.X+ix, exp.Min.Y+iy, g, wv)
							}
						}
					}
				})
			}
		})
	}
}

// assertRestrictedEqualsUnclipped draws twice: once with the canvas free and
// once with c.restrict set. Inside the restrict rectangle the two canvases
// must be byte-identical; outside it the restricted canvas must still hold the
// seed, untouched.
func assertRestrictedEqualsUnclipped(t *testing.T, name string, seed int, restrict ui.Rect, draw func(*Canvas)) {
	t.Helper()
	free := seededCanvas(t, 260, 180, seed)
	rest := seededCanvas(t, 260, 180, seed)
	draw(free)
	rest.restrict = restrict
	draw(rest)
	rest.restrict = ui.Rect{}
	for y := 0; y < 180; y++ {
		for x := 0; x < 260; x++ {
			inside := x >= restrict.X && x < restrict.X+restrict.W && y >= restrict.Y && y < restrict.Y+restrict.H
			for i := 0; i < 4; i++ {
				r := rest.Pix[y*rest.Stride+x*4+i]
				if !inside {
					if want := byte(((y*rest.Stride+x*4+i)*7 + seed*13) % 251); r != want {
						t.Fatalf("%s: pixel (%d,%d) outside restrict changed: got %d, seed %d", name, x, y, r, want)
					}
					continue
				}
				f := free.Pix[y*free.Stride+x*4+i]
				if r != f {
					t.Fatalf("%s: pixel (%d,%d) inside restrict: restricted %d, free %d", name, x, y, r, f)
				}
			}
		}
	}
}

var scopedDrawRects = []struct {
	name   string
	box    ui.Rect
	radius int
	square Corners
	col    Color
}{
	{"translucent negative origin", ui.Rect{X: -30, Y: -20, W: 160, H: 120}, 24, 0, Color{R: 0x3a, G: 0x41, B: 0x49, A: 0x80}},
	{"opaque straddling edges", ui.Rect{X: 200, Y: 120, W: 120, H: 100}, 16, 0, Color{R: 0xff, G: 0x20, B: 0x40, A: 0xff}},
	{"odd dims", ui.Rect{X: 40, Y: 30, W: 33, H: 77}, 10, 0, Color{R: 0x12, G: 0xcd, B: 0x44, A: 0xc0}},
	{"square corners", ui.Rect{X: 10, Y: 10, W: 60, H: 60}, 16, SquareTL | SquareBR, Color{R: 0xaa, G: 0xbb, B: 0xcc, A: 0x90}},
	{"radius past half", ui.Rect{X: 50, Y: 50, W: 41, H: 41}, 40, 0, Color{R: 0x80, G: 0x80, B: 0x80, A: 0xd0}},
	{"radius zero", ui.Rect{X: 0, Y: 0, W: 120, H: 90}, 0, 0, Color{R: 0x20, G: 0x40, B: 0x80, A: 0x60}},
	{"fully transparent", ui.Rect{X: 20, Y: 20, W: 60, H: 60}, 12, 0, Color{R: 0xff, G: 0xff, B: 0xff, A: 0}},
}

var scopedRestricts = []struct {
	name     string
	restrict ui.Rect
}{
	{"whole canvas", ui.Rect{X: 0, Y: 0, W: 260, H: 180}},
	{"left half", ui.Rect{X: 0, Y: 0, W: 130, H: 180}},
	{"strip through the box", ui.Rect{X: 60, Y: 40, W: 40, H: 120}},
	{"corner sliver", ui.Rect{X: 230, Y: 150, W: 30, H: 30}},
	{"disjoint from the canvas", ui.Rect{X: 400, Y: 400, W: 50, H: 50}},
}

func TestBlendFillRestrictedMatchesUnclipped(t *testing.T) {
	for di, draw := range scopedDrawRects {
		for _, re := range scopedRestricts {
			t.Run(draw.name+"/"+re.name, func(t *testing.T) {
				assertRestrictedEqualsUnclipped(t, draw.name, di+1, re.restrict, func(c *Canvas) {
					blendFill(c, draw.box, draw.radius, draw.square, draw.col)
				})
			})
		}
	}
}

func TestBlendRingRestrictedMatchesUnclipped(t *testing.T) {
	for di, draw := range scopedDrawRects {
		for _, re := range scopedRestricts {
			t.Run(draw.name+"/"+re.name, func(t *testing.T) {
				assertRestrictedEqualsUnclipped(t, draw.name, di+2, re.restrict, func(c *Canvas) {
					blendRing(c, draw.box, draw.radius, 2, draw.col)
				})
			})
		}
	}
}

// TestScopedGradientMaskMatchesFullMask pins the paintChrome gradient path:
// a mask scoped to the visible rect, placed at the clip origin and coloured
// against the full surface must paint exactly what the full-box mask painted.
func TestScopedGradientMaskMatchesFullMask(t *testing.T) {
	stops := []gradientStop{{at: 0, c: Color{R: 0x20, G: 0x30, B: 0x60, A: 0xf0}}, {at: 1, c: Color{R: 0x10, G: 0x10, B: 0x20, A: 0x20}}}
	boxes := []ui.Rect{
		{X: -30, Y: -20, W: 300, H: 220},
		{X: 40, Y: 30, W: 33, H: 77},
		{X: 200, Y: 120, W: 120, H: 100},
	}
	for bi, box := range boxes {
		for _, re := range scopedRestricts {
			t.Run(re.name, func(t *testing.T) {
				free := seededCanvas(t, 260, 180, bi+1)
				rest := seededCanvas(t, 260, 180, bi+1)
				radius := 24
				full := RoundedMask(radius, box.W, box.H)
				blendMaskGradient(free, full, box.X, box.Y, stops, 30, 0, false)
				rest.restrict = re.restrict
				mask, mx, my := visibleRoundedMask(rest, radius, box)
				blendMaskGradientIn(rest, mask, mx, my, box, stops, 30, 0, false)
				rest.restrict = ui.Rect{}
				for y := 0; y < 180; y++ {
					for x := 0; x < 260; x++ {
						inside := x >= re.restrict.X && x < re.restrict.X+re.restrict.W && y >= re.restrict.Y && y < re.restrict.Y+re.restrict.H
						for i := 0; i < 4; i++ {
							r := rest.Pix[y*rest.Stride+x*4+i]
							if !inside {
								if want := byte(((y*rest.Stride+x*4+i)*7 + (bi+1)*13) % 251); r != want {
									t.Fatalf("pixel (%d,%d) outside restrict changed: got %d, seed %d", x, y, r, want)
								}
								continue
							}
							if f := free.Pix[y*free.Stride+x*4+i]; r != f {
								t.Fatalf("pixel (%d,%d): scoped %d, full %d", x, y, r, f)
							}
						}
					}
				}
			})
		}
	}
}
