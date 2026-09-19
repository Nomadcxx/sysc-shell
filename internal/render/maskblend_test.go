package render

import (
	"image"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The three mask compositors index the coverage plane directly rather than
// asking image.Alpha for one pixel at a time. These checks pin them to the
// generic accessor they replaced: the reference below is the loop body as it
// read before, so a divergence in rounding, clipping or offset shows up as a
// byte difference rather than as a faint visual regression nobody bisects.

func referenceCoverage(mask *image.Alpha, px, py, x, y int) uint32 {
	b := mask.Bounds()
	return uint32(mask.AlphaAt(b.Min.X+px-x, b.Min.Y+py-y).A)
}

func referenceBlendMask(c *Canvas, mask *image.Alpha, x, y int, col Color) {
	if col.A == 0 || mask == nil {
		return
	}
	b := mask.Bounds()
	x0, y0, x1, y1 := c.clip(ui.Rect{X: x, Y: y, W: b.Dx(), H: b.Dy()})
	src := col.premultiply()
	for py := y0; py < y1; py++ {
		row := c.Pix[py*c.Stride:]
		for px := x0; px < x1; px++ {
			cov := referenceCoverage(mask, px, py, x, y)
			if cov == 0 {
				continue
			}
			alpha := uint32(col.A) * cov / 255
			var s [4]byte
			for i := range src {
				s[i] = byte(uint32(src[i]) * cov / 255)
			}
			blendPixel(row[px*4:px*4+4], s, alpha)
		}
	}
}

// seededCanvas gives every byte a non-uniform value, so a compositor that
// silently skips a pixel is caught by the leftover rather than by a zero that
// matches the background.
func seededCanvas(t testing.TB, w, h, seed int) *Canvas {
	t.Helper()
	c, err := NewCanvas(make([]byte, w*4*h), w, h, w*4)
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.Pix {
		c.Pix[i] = byte((i*7 + seed*13) % 251)
	}
	return c
}

var maskBlendPlacements = []struct {
	name               string
	w, h, radius, x, y int
	col                Color
}{
	{"opaque interior", 200, 30, 15, 4, 4, Color{R: 0x3a, G: 0x41, B: 0x49, A: 0xff}},
	{"half alpha", 120, 40, 20, 0, 0, Color{R: 0xff, B: 0x80, A: 0x80}},
	{"negative origin", 60, 60, 30, -7, -3, Color{R: 0x12, G: 0xcd, B: 0x44, A: 0xc0}},
	{"clipped at the far edge", 80, 24, 12, 35, 18, Color{R: 0xaa, G: 0xbb, B: 0xcc, A: 0x01}},
	{"square corners", 50, 50, 0, 10, 10, Color{A: 0xff}},
	{"fully transparent", 40, 40, 8, 2, 2, Color{R: 0xff, G: 0xff, B: 0xff}},
}

func TestBlendMaskMatchesGenericAccessor(t *testing.T) {
	t.Parallel()
	for i, tc := range maskBlendPlacements {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mask := RoundedMask(tc.radius, tc.w, tc.h)
			got, want := seededCanvas(t, 96, 64, i), seededCanvas(t, 96, 64, i)
			blendMask(got, mask, tc.x, tc.y, tc.col)
			referenceBlendMask(want, mask, tc.x, tc.y, tc.col)
			for j := range want.Pix {
				if got.Pix[j] != want.Pix[j] {
					t.Fatalf("byte %d = %d, want %d", j, got.Pix[j], want.Pix[j])
				}
			}
		})
	}
}

// The gradient and image compositors read coverage through the same helper, so
// one placement each is enough to catch an offset mistake in it.
func TestMaskCompositorsAgreeOnCoverage(t *testing.T) {
	t.Parallel()
	mask := RoundedMask(12, 90, 36)
	for _, x := range []int{0, 5, -4} {
		b := mask.Bounds()
		for py := max(0, x); py < min(36, 30); py++ {
			for px := max(0, x); px < min(90, 40); px++ {
				want := uint32(mask.AlphaAt(b.Min.X+px-x, b.Min.Y+py-x).A)
				got := uint32(coverageRow(mask, py, x)[px-x])
				if got != want {
					t.Fatalf("x=%d coverage at (%d,%d) = %d, want %d", x, px, py, got, want)
				}
			}
		}
	}
}
