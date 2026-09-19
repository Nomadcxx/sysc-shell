package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The fade has to be a ramp, not a wash: a constant veil over the last widget
// would read as a dimmed item rather than a row continuing past the cut.
func TestEdgeFadeRampsTowardTheTrailingEdge(t *testing.T) {
	const w, h = 40, 10
	pix := make([]byte, w*h*4)
	// A white field stands in for the widget the fade covers.
	for i := range pix {
		pix[i] = 0xff
	}
	c, err := NewCanvas(pix, w, h, w*4)
	if err != nil {
		t.Fatal(err)
	}
	surface := Color{R: 0x1d, G: 0x20, B: 0x25, A: 0xff}
	fillEdgeFade(c, ui.Rect{X: 0, Y: 0, W: w, H: h}, surface)

	// The canvas is xrgb8888: byte 0 of a pixel is blue, not red.
	blue := func(x int) int { return int(pix[(h/2)*w*4+x*4]) }
	first, middle, last := blue(0), blue(w/2), blue(w-1)
	if first <= middle || middle <= last {
		t.Fatalf("not a ramp: left=%d middle=%d right=%d", first, middle, last)
	}
	if last != int(surface.B) {
		t.Fatalf("trailing edge = %d, want the surface (%d)", last, surface.B)
	}
	if first < 0xf0 {
		t.Fatalf("leading edge = %d, want the widget left nearly untouched", first)
	}
}

// A fade of zero width, or over a fully transparent surface, paints nothing
// rather than dividing by zero or veiling the bar.
func TestEdgeFadeIgnoresAnEmptyBox(t *testing.T) {
	const w, h = 8, 4
	pix := make([]byte, w*h*4)
	c, err := NewCanvas(pix, w, h, w*4)
	if err != nil {
		t.Fatal(err)
	}
	fillEdgeFade(c, ui.Rect{X: 0, Y: 0, W: 0, H: h}, Color{A: 0xff})
	fillEdgeFade(c, ui.Rect{X: 0, Y: 0, W: w, H: h}, Color{})
	for i, b := range pix {
		if b != 0 {
			t.Fatalf("pixel %d = %d, want the canvas untouched", i, b)
		}
	}
}
