package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestPaintImageCompositesPremultipliedAlpha(t *testing.T) {
	c := newTestCanvas(t, 4, 4)
	fillRect(c, ui.Rect{W: 4, H: 4}, Color{R: 0, G: 0, B: 0, A: 0xff})

	// One opaque white pixel and one half-transparent white pixel, already
	// premultiplied in the canvas's B, G, R, A order.
	img := &ui.Image{Width: 2, Height: 1, Stride: 8, Pix: []byte{
		0xff, 0xff, 0xff, 0xff,
		0x80, 0x80, 0x80, 0x80,
	}}
	paintImage(c, ui.Rect{X: 0, Y: 0, W: 2, H: 1}, img)

	if got := pixelAt(t, c, 0, 0); got != (Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff}) {
		t.Fatalf("opaque pixel = %+v, want white", got)
	}
	half := pixelAt(t, c, 1, 0)
	if half.A != 0xff {
		t.Fatalf("blended alpha = %#x, want the opaque destination preserved", half.A)
	}
	if half.B < 0x70 || half.B > 0x90 {
		t.Fatalf("blended channel = %#x, want roughly half", half.B)
	}
}

func TestPaintImageScalesToTheNodeBox(t *testing.T) {
	c := newTestCanvas(t, 4, 4)
	img := &ui.Image{Width: 1, Height: 1, Stride: 4, Pix: []byte{0xff, 0xff, 0xff, 0xff}}
	paintImage(c, ui.Rect{X: 0, Y: 0, W: 4, H: 4}, img)
	for _, at := range [][2]int{{0, 0}, {3, 3}, {0, 3}, {3, 0}} {
		if got := pixelAt(t, c, at[0], at[1]); got.A != 0xff {
			t.Fatalf("pixel %v = %+v, want the source stretched across the box", at, got)
		}
	}
}

func TestPaintImageIgnoresMissingAndDegenerateRasters(t *testing.T) {
	c := newTestCanvas(t, 2, 2)
	before := append([]byte(nil), c.Pix...)
	for name, img := range map[string]*ui.Image{
		"nil":        nil,
		"no pixels":  {Width: 2, Height: 2, Stride: 8},
		"zero width": {Width: 0, Height: 2, Stride: 8, Pix: make([]byte, 16)},
		"short":      {Width: 4, Height: 4, Stride: 16, Pix: []byte{1, 2, 3, 4}},
	} {
		t.Run(name, func(t *testing.T) {
			paintImage(c, ui.Rect{W: 2, H: 2}, img)
		})
	}
	paintImage(c, ui.Rect{W: 0, H: 0}, &ui.Image{Width: 1, Height: 1, Stride: 4, Pix: []byte{1, 2, 3, 4}})
	for i := range before {
		if c.Pix[i] != before[i] {
			t.Fatal("a degenerate raster changed the canvas")
		}
	}
}

func TestPaintImageClipsToTheCanvas(t *testing.T) {
	c := newTestCanvas(t, 2, 2)
	img := &ui.Image{Width: 1, Height: 1, Stride: 4, Pix: []byte{0xff, 0xff, 0xff, 0xff}}
	// A box that starts inside and runs off the edge must not write past it.
	paintImage(c, ui.Rect{X: 1, Y: 1, W: 8, H: 8}, img)
	if got := pixelAt(t, c, 1, 1); got.A != 0xff {
		t.Fatalf("in-bounds pixel = %+v, want painted", got)
	}
}

func TestPaintImageClipsCircleShape(t *testing.T) {
	c := newTestCanvas(t, 8, 8)
	img := &ui.Image{Width: 1, Height: 1, Stride: 4, Pix: []byte{0xff, 0xff, 0xff, 0xff}}
	n := &ui.Node{Kind: ui.KindImage, Shape: ui.ShapeCircle, Image: img, Bounds: ui.Rect{W: 8, H: 8}}

	if err := paintNode(c, n, nil, testStyle, testStyle.Size); err != nil {
		t.Fatal(err)
	}
	for _, at := range [][2]int{{0, 0}, {7, 0}, {0, 7}, {7, 7}} {
		if got := pixelAt(t, c, at[0], at[1]); got.A != 0 {
			t.Errorf("corner %v alpha = %d, want transparent", at, got.A)
		}
	}
	if got := pixelAt(t, c, 4, 4); got.A != 0xff {
		t.Fatalf("centre alpha = %d, want opaque", got.A)
	}
}

// twoPixelRamp is black beside white, premultiplied in the canvas's order.
func twoPixelRamp() *ui.Image {
	return &ui.Image{Width: 2, Height: 1, Stride: 8, Pix: []byte{
		0, 0, 0, 0xff,
		0xff, 0xff, 0xff, 0xff,
	}}
}

func TestBlendMaskImageInterpolatesBetweenPixels(t *testing.T) {
	// A quarter-resolution backdrop scaled up 4x with nearest sampling bands
	// visibly across a large flat panel. Two source pixels scaled up must
	// produce intermediate values between them, not a hard step. A zero radius
	// makes the mask full coverage, isolating the sampling from the clipping.
	c := newTestCanvas(t, 16, 1)
	blendMaskImage(c, RoundedMask(0, 16, 1), 0, 0, twoPixelRamp())

	var seen int
	for x := 0; x < 16; x++ {
		if v := c.Pix[x*4]; v != 0x00 && v != 0xff {
			seen++
		}
	}
	if seen == 0 {
		t.Error("no intermediate values; sampling is not bilinear")
	}
}

func TestBlendMaskImageClipsToTheMask(t *testing.T) {
	// A panel's corners are genuinely transparent: the painter clears the
	// buffer and fills a rounded body, so the compositor shows what is behind
	// them. A backdrop blitted into the raw rectangle would fill those corners
	// with blurred pixels and the panel would read as a square.
	c := newTestCanvas(t, 16, 16)
	blendMaskImage(c, RoundedMask(8, 16, 16), 0, 0, solid(4, 4, 0xff, 0xff, 0xff, 0xff))

	if got := pixelAt(t, c, 0, 0); got.A != 0 {
		t.Errorf("corner = %+v, want left transparent by the mask", got)
	}
	if got := pixelAt(t, c, 8, 8); got.A == 0 {
		t.Error("the centre was not painted at all")
	}
}

func TestPaintImageKeepsNearestForIcons(t *testing.T) {
	// paintImage must not change. The icon worker produces the exact size the
	// node asked for, and resampling there would be a second, worse scaler.
	c := newTestCanvas(t, 16, 1)
	paintImage(c, ui.Rect{W: 16, H: 1}, twoPixelRamp())
	for x := 0; x < 16; x++ {
		if v := c.Pix[x*4]; v != 0x00 && v != 0xff {
			t.Fatalf("paintImage interpolated at x=%d (%#x); it must stay nearest", x, v)
		}
	}
}

func TestBlendMaskImageIgnoresDegenerateRasters(t *testing.T) {
	c := newTestCanvas(t, 2, 2)
	before := append([]byte(nil), c.Pix...)
	for name, img := range map[string]*ui.Image{
		"nil":        nil,
		"no pixels":  {Width: 2, Height: 2, Stride: 8},
		"zero width": {Width: 0, Height: 2, Stride: 8, Pix: make([]byte, 16)},
		"short":      {Width: 4, Height: 4, Stride: 16, Pix: []byte{1, 2, 3, 4}},
	} {
		t.Run(name, func(t *testing.T) {
			blendMaskImage(c, RoundedMask(0, 2, 2), 0, 0, img)
		})
	}
	blendMaskImage(c, nil, 0, 0, twoPixelRamp())
	for i := range before {
		if c.Pix[i] != before[i] {
			t.Fatal("a degenerate raster changed the canvas")
		}
	}
}
