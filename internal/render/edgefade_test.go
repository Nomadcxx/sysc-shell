package render

import (
	"strconv"
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
	fillEdgeFade(c, ui.Rect{X: 0, Y: 0, W: w, H: h}, surface, ui.Horizontal, false)

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
	fillEdgeFade(c, ui.Rect{X: 0, Y: 0, W: 0, H: h}, Color{A: 0xff}, ui.Horizontal, false)
	fillEdgeFade(c, ui.Rect{X: 0, Y: 0, W: w, H: h}, Color{}, ui.Horizontal, false)
	for i, b := range pix {
		if b != 0 {
			t.Fatalf("pixel %d = %d, want the canvas untouched", i, b)
		}
	}
}

func TestSideEdgeFadeRampsDownAtFractionalScales(t *testing.T) {
	for _, scale := range []ui.Scale120{120, 150} {
		t.Run(strconv.Itoa(int(scale)), func(t *testing.T) {
			logical := ui.Rect{W: 12, H: 20}
			box := scale.PhysicalRect(logical)
			pix := make([]byte, box.W*box.H*4)
			for i := range pix {
				pix[i] = 0xff
			}
			c, err := NewCanvas(pix, box.W, box.H, box.W*4)
			if err != nil {
				t.Fatal(err)
			}
			surface := Color{R: 0x1d, G: 0x20, B: 0x25, A: 0xff}
			node := &ui.Node{Kind: ui.KindEdgeFade, FadeAxis: ui.Vertical, Bounds: logical}
			if err := paintNode(c, node, nil, Style{Scale120: scale, Background: surface}, 0); err != nil {
				t.Fatal(err)
			}
			blue := func(y int) int { return int(pix[y*box.W*4+(box.W/2)*4]) }
			first, middle, last := blue(0), blue(box.H/2), blue(box.H-1)
			if first <= middle || middle <= last || last != int(surface.B) {
				t.Fatalf("scale %d vertical fade blue = %d/%d/%d, want a downward ramp ending at %d", scale, first, middle, last, surface.B)
			}
		})
	}
}

// A leading fade is opaque where the hidden run continues: at its start.
// A tinted fade paints the accent at its strength instead of the surface.
func TestEdgeFadeLeadingAndTint(t *testing.T) {
	const w, h = 40, 10
	paint := func(n *ui.Node, style Style) []byte {
		pix := make([]byte, w*h*4)
		for i := range pix {
			pix[i] = 0xff
		}
		c, err := NewCanvas(pix, w, h, w*4)
		if err != nil {
			t.Fatal(err)
		}
		if err := paintNode(c, n, nil, style, 0); err != nil {
			t.Fatal(err)
		}
		return pix
	}
	surface := Color{R: 0x1d, G: 0x20, B: 0x25, A: 0xff}
	accent := Color{R: 0x20, G: 0x60, B: 0xe0, A: 0xff}
	style := Style{Scale120: 120, Background: surface, Accent: accent}
	blue := func(pix []byte, x int) int { return int(pix[(h/2)*w*4+x*4]) }

	lead := paint(&ui.Node{Kind: ui.KindEdgeFade, FadeLeading: true, Bounds: ui.Rect{W: w, H: h}}, style)
	if first, last := blue(lead, 0), blue(lead, w-1); first != int(surface.B) || last < 0xf0 {
		t.Fatalf("leading fade blue first=%d last=%d, want the surface at the start and the widget at the end", first, last)
	}

	green := func(pix []byte, x int) int { return int(pix[(h/2)*w*4+x*4+1]) }
	full := paint(&ui.Node{Kind: ui.KindEdgeFade, FadeTint: 1, Bounds: ui.Rect{W: w, H: h}}, style)
	if got := green(full, w-1); got != int(accent.G) {
		t.Fatalf("full tint edge green = %d, want the accent (%d)", got, accent.G)
	}
	half := paint(&ui.Node{Kind: ui.KindEdgeFade, FadeTint: 0.5, Bounds: ui.Rect{W: w, H: h}}, style)
	if got := green(half, w-1); got <= int(accent.G)+0x30 || got >= 0xf0 {
		t.Fatalf("half tint edge green = %d, want between the accent and the widget", got)
	}
}
