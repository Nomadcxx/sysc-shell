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
