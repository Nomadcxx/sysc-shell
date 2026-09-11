package render

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestLerpColorEndpointsAndClamping(t *testing.T) {
	t.Parallel()
	from := Color{R: 0x1d, G: 0x20, B: 0x25, A: 0xff}
	to := Color{R: 0x3a, G: 0x41, B: 0x49, A: 0xff}
	for _, tc := range []struct {
		name     string
		progress float64
		want     Color
	}{
		{"before start", -1, from},
		{"start", 0, from},
		{"end", 1, to},
		{"past end", 5, to},
		{"NaN", math.NaN(), from},
	} {
		if got := LerpColor(from, to, tc.progress); got != tc.want {
			t.Errorf("LerpColor(%s) = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestLerpColorMidpointRoundsPerChannel(t *testing.T) {
	t.Parallel()
	got := LerpColor(Color{R: 0, G: 0, B: 0, A: 0}, Color{R: 255, G: 10, B: 1, A: 200}, 0.5)
	want := Color{R: 128, G: 5, B: 1, A: 100}
	if got != want {
		t.Fatalf("midpoint = %+v, want %+v", got, want)
	}
}

func TestLerpColorCarriesAlpha(t *testing.T) {
	t.Parallel()
	// A panel appearing while the palette changes fades opacity and hue in the
	// same transition, so alpha must not be pinned to either endpoint.
	from := Color{R: 0x28, G: 0x2c, B: 0x33, A: 0x00}
	to := Color{R: 0x28, G: 0x2c, B: 0x33, A: 0xff}
	mid := LerpColor(from, to, 0.5)
	if mid.A != 128 {
		t.Errorf("alpha at midpoint = %d, want 128", mid.A)
	}
	if mid.R != 0x28 || mid.G != 0x2c || mid.B != 0x33 {
		t.Errorf("colour drifted while only alpha changed: %+v", mid)
	}
}

func TestLerpColorDescendsWithoutWrapping(t *testing.T) {
	t.Parallel()
	// uint8 subtraction that underflows would wrap to 255 and flash white.
	got := LerpColor(Color{R: 255, G: 255, B: 255, A: 255}, Color{}, 0.5)
	want := Color{R: 128, G: 128, B: 128, A: 128}
	if got != want {
		t.Fatalf("descending midpoint = %+v, want %+v", got, want)
	}
	for i := 0; i <= 20; i++ {
		p := float64(i) / 20
		c := LerpColor(Color{R: 255, A: 255}, Color{R: 0, A: 255}, p)
		if i > 0 {
			prev := LerpColor(Color{R: 255, A: 255}, Color{R: 0, A: 255}, float64(i-1)/20)
			if c.R > prev.R {
				t.Fatalf("channel rose at %v: %d after %d", p, c.R, prev.R)
			}
		}
	}
}

func TestFilletCoverageSweepsSmoothlyFromBarToPanel(t *testing.T) {
	for _, radius := range []int{12, 14, 15, 18} {
		t.Run(fmt.Sprintf("radius_%d", radius), func(t *testing.T) {
			c := newTestCanvas(t, radius*2+4, radius+1)
			body := ui.Rect{X: radius, W: 4, H: radius + 1}
			fillAttachFillets(c, body, radius, "top", Color{R: 255, A: 255})

			partial := false
			previous := radius*255 + 1
			for y := 0; y < radius; y++ {
				total := 0
				for x := 0; x < radius; x++ {
					a := int(c.Pix[y*c.Stride+x*4+3])
					total += a
					partial = partial || (a > 0 && a < 255)
				}
				if total > previous {
					t.Fatalf("coverage grew at row %d: %d after %d", y, total, previous)
				}
				previous = total
			}
			if !partial {
				t.Fatal("fillet has no partial-alpha boundary pixel")
			}
			if got := c.Pix[(radius-1)*4+3]; got == 0 {
				t.Fatal("fillet does not reach the outside of the attached bar edge")
			}
			if got := c.Pix[(radius-1)*4+3]; got != 255 {
				t.Fatalf("inner attached-edge alpha = %d, want 255", got)
			}
			for x := 0; x < radius; x++ {
				if got := c.Pix[radius*c.Stride+x*4+3]; got != 0 {
					t.Fatalf("alpha beyond curve at (%d,%d) = %d", x, radius, got)
				}
			}
		})
	}
}

func TestFilletCoverageIsSymmetricAcrossAttachedEdges(t *testing.T) {
	const radius = 15
	top := newTestCanvas(t, radius*2+4, radius+1)
	bottom := newTestCanvas(t, radius*2+4, radius+1)
	body := ui.Rect{X: radius, W: 4, H: radius + 1}
	fillAttachFillets(top, body, radius, "top", Color{A: 255})
	fillAttachFillets(bottom, body, radius, "bottom", Color{A: 255})
	for y := 0; y < top.Height; y++ {
		for x := 0; x < top.Width; x++ {
			got := top.Pix[y*top.Stride+x*4+3]
			want := bottom.Pix[(bottom.Height-1-y)*bottom.Stride+x*4+3]
			if got != want {
				t.Fatalf("alpha at (%d,%d) = %d, mirrored bottom = %d", x, y, got, want)
			}
		}
	}
}

func TestFilletCoverageClipsToCanvas(t *testing.T) {
	c := newTestCanvas(t, 4, 4)
	fillAttachFillets(c, ui.Rect{X: 0, Y: -2, W: 4, H: 8}, 8, "top", Color{A: 255})
}

func TestSurfaceTransformScalesPremultipliedChannels(t *testing.T) {
	c := newTestCanvas(t, 1, 1)
	copy(c.Pix, []byte{80, 60, 40, 100})
	c.ApplySurfaceTransform(0.5, 0)
	if got, want := c.Pix[:4], []byte{40, 30, 20, 50}; !bytes.Equal(got, want) {
		t.Fatalf("transformed pixel = %v, want %v", got, want)
	}
}

func TestSurfaceTransformTranslatesInPlaceAndClearsExposedRows(t *testing.T) {
	for _, tc := range []struct {
		name string
		dy   int
		want []byte
	}{
		{name: "down", dy: 1, want: []byte{0, 10, 20, 30}},
		{name: "up", dy: -1, want: []byte{20, 30, 40, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestCanvas(t, 1, 4)
			for y, alpha := range []byte{10, 20, 30, 40} {
				c.Pix[y*c.Stride+3] = alpha
			}
			backing := &c.Pix[0]
			c.ApplySurfaceTransform(1, tc.dy)
			if &c.Pix[0] != backing {
				t.Fatal("surface transform replaced the frame buffer")
			}
			for y, want := range tc.want {
				if got := c.Pix[y*c.Stride+3]; got != want {
					t.Fatalf("row %d alpha = %d, want %d", y, got, want)
				}
			}
		})
	}
}
