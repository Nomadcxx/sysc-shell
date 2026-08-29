package render

import (
	"image"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	canvasW = 240
	canvasH = 48
)

var testStyle = ProofStyle{
	Size:       16,
	Scale120:   ui.ScaleUnit,
	Body:       ui.Rect{W: canvasW, H: canvasH},
	Background: Color{R: 0x10, G: 0x14, B: 0x18, A: 0xff},
	Foreground: Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
	Track:      Color{R: 0x30, G: 0x34, B: 0x38, A: 0xff},
	Accent:     Color{R: 0x00, G: 0x80, B: 0xff, A: 0xff},
	AccentOn:   Color{R: 0xff, G: 0x60, B: 0x00, A: 0xff},
}

func TestPaintFillsOnlyTheRoundedBody(t *testing.T) {
	t.Parallel()

	c := newTestCanvas(t, 100, 44)
	for i := range c.Pix {
		c.Pix[i] = 0xff
	}
	style := testStyle
	style.Body = ui.Rect{X: 4, Y: 4, W: 92, H: 40}
	style.Radius = 12
	r := NewTextRenderer(mustTestFace(t))
	root := &ui.Node{Kind: ui.KindRow}
	if err := Paint(c, root, r, style); err != nil {
		t.Fatal(err)
	}

	transparent := Color{}
	for _, p := range []image.Point{{X: 0, Y: 0}, {X: 50, Y: 2}, {X: 4, Y: 4}, {X: 95, Y: 4}} {
		if got := pixelAt(t, c, p.X, p.Y); got != transparent {
			t.Errorf("gap/corner pixel %v = %+v, want transparent", p, got)
		}
	}
	for _, p := range []image.Point{{X: 16, Y: 4}, {X: 50, Y: 20}, {X: 4, Y: 16}, {X: 95, Y: 16}} {
		if got := pixelAt(t, c, p.X, p.Y); got != style.Background {
			t.Errorf("body pixel %v = %+v, want %+v", p, got, style.Background)
		}
	}
}

func TestPaintClearsPixelsFromThePreviousBody(t *testing.T) {
	t.Parallel()

	c := newTestCanvas(t, 100, 44)
	r := NewTextRenderer(mustTestFace(t))
	root := &ui.Node{Kind: ui.KindRow}
	style := testStyle
	style.Body = ui.Rect{W: 100, H: 44}
	if err := Paint(c, root, r, style); err != nil {
		t.Fatal(err)
	}
	style.Body = ui.Rect{X: 4, Y: 4, W: 92, H: 40}
	style.Radius = 12
	if err := Paint(c, root, r, style); err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, c, 0, 0); got != (Color{}) {
		t.Fatalf("old background pixel = %+v, want transparent", got)
	}
}

func TestPaintScalesRoundedBodyToBufferPixels(t *testing.T) {
	t.Parallel()

	c := newTestCanvas(t, 150, 66)
	r := NewTextRenderer(mustTestFace(t))
	style := testStyle
	style.Scale120 = 180
	style.Body = ui.Rect{X: 4, Y: 4, W: 92, H: 40}
	style.Radius = 12
	if err := Paint(c, &ui.Node{Kind: ui.KindRow}, r, style); err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, c, 75, 30); got != style.Background {
		t.Fatalf("scaled body centre = %+v, want %+v", got, style.Background)
	}
	if got := pixelAt(t, c, 6, 6); got != (Color{}) {
		t.Fatalf("scaled rounded corner = %+v, want transparent", got)
	}
}

func newTestCanvas(t *testing.T, w, h int) *Canvas {
	t.Helper()
	stride := w * 4
	c, err := NewCanvas(make([]byte, stride*h), w, h, stride)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// pixelAt reads one canvas pixel. Opaque colours survive premultiplication
// unchanged, so the test can compare against the style colours directly.
func pixelAt(t *testing.T, c *Canvas, x, y int) Color {
	t.Helper()
	i := y*c.Stride + x*4
	return Color{B: c.Pix[i], G: c.Pix[i+1], R: c.Pix[i+2], A: c.Pix[i+3]}
}

// paintTree lays out and paints the proof fixture, returning the tree.
func paintTree(t *testing.T, c *Canvas, style ProofStyle) *ui.Node {
	t.Helper()

	r := NewTextRenderer(mustTestFace(t))
	measure := func(s string) (int, int) {
		w, h, err := r.Measure(s, style.Size)
		if err != nil {
			t.Fatal(err)
		}
		return w, h
	}

	root := &ui.Node{
		Kind:    ui.KindRow,
		Padding: 8,
		Gap:     8,
		Children: []*ui.Node{
			{Kind: ui.KindText, Text: "sysc-shell"},
			{Kind: ui.KindMeter, Width: 60, Value: 0.5},
			{Kind: ui.KindButton, Text: "Go", Padding: 4, Action: "toggle-meter"},
		},
	}
	if err := ui.Layout(root, ui.Rect{W: canvasW, H: canvasH}, measure); err != nil {
		t.Fatal(err)
	}
	if err := Paint(c, root, r, style); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPaintFillsBackground(t *testing.T) {
	t.Parallel()

	c := newTestCanvas(t, canvasW, canvasH)
	paintTree(t, c, testStyle)

	for _, p := range []image.Point{{X: 0, Y: 0}, {X: canvasW - 1, Y: canvasH - 1}, {X: 0, Y: canvasH - 1}} {
		if got := pixelAt(t, c, p.X, p.Y); got != testStyle.Background {
			t.Errorf("pixel %v = %+v, want the background %+v", p, got, testStyle.Background)
		}
	}
}

func TestPaintMeterTrackAndFill(t *testing.T) {
	t.Parallel()

	c := newTestCanvas(t, canvasW, canvasH)
	root := paintTree(t, c, testStyle)
	meter := root.Children[1].Bounds

	if got := pixelAt(t, c, meter.X+2, meter.Y+2); got != testStyle.Accent {
		t.Errorf("filled meter pixel = %+v, want the accent %+v", got, testStyle.Accent)
	}
	if got := pixelAt(t, c, meter.X+meter.W-2, meter.Y+2); got != testStyle.Track {
		t.Errorf("unfilled meter pixel = %+v, want the track %+v", got, testStyle.Track)
	}
}

func TestPaintButtonTogglesColor(t *testing.T) {
	t.Parallel()

	off := newTestCanvas(t, canvasW, canvasH)
	root := paintTree(t, off, testStyle)
	button := root.Children[2].Bounds
	if got := pixelAt(t, off, button.X+1, button.Y+1); got != testStyle.Accent {
		t.Errorf("untoggled button pixel = %+v, want the accent %+v", got, testStyle.Accent)
	}

	on := newTestCanvas(t, canvasW, canvasH)
	toggled := testStyle
	toggled.Toggled = true
	paintTree(t, on, toggled)
	if got := pixelAt(t, on, button.X+1, button.Y+1); got != testStyle.AccentOn {
		t.Errorf("toggled button pixel = %+v, want %+v", got, testStyle.AccentOn)
	}
}

func TestPaintDrawsTextPixels(t *testing.T) {
	t.Parallel()

	c := newTestCanvas(t, canvasW, canvasH)
	root := paintTree(t, c, testStyle)
	label := root.Children[0].Bounds

	for y := label.Y; y < label.Y+label.H; y++ {
		for x := label.X; x < label.X+label.W; x++ {
			if pixelAt(t, c, x, y) != testStyle.Background {
				return
			}
		}
	}
	t.Fatal("no text pixel differs from the background")
}

func TestPaintScalesToPhysicalPixels(t *testing.T) {
	t.Parallel()

	// scale120 = 180 is a 1.5x fractional scale.
	style := testStyle
	style.Scale120 = 180

	c := newTestCanvas(t, canvasW*3/2, canvasH*3/2)
	r := NewTextRenderer(mustTestFace(t))
	measure := func(s string) (int, int) {
		w, h, err := r.Measure(s, style.Size)
		if err != nil {
			t.Fatal(err)
		}
		return w, h
	}
	root := &ui.Node{
		Kind:     ui.KindRow,
		Padding:  8,
		Children: []*ui.Node{{Kind: ui.KindMeter, Width: 60, Value: 1}},
	}
	if err := ui.Layout(root, ui.Rect{W: canvasW, H: canvasH}, measure); err != nil {
		t.Fatal(err)
	}
	if err := Paint(c, root, r, style); err != nil {
		t.Fatal(err)
	}

	meter := root.Children[0].Bounds
	// The meter's logical right edge maps to 1.5x in buffer pixels.
	physicalRight := (meter.X+meter.W)*180/120 - 1
	if got := pixelAt(t, c, physicalRight, meter.Y*180/120+2); got != testStyle.Accent {
		t.Errorf("scaled meter pixel at x=%d = %+v, want the accent %+v", physicalRight, got, testStyle.Accent)
	}
	// One pixel past the scaled edge must still be background.
	if got := pixelAt(t, c, physicalRight+1, meter.Y*180/120+2); got != testStyle.Background {
		t.Errorf("pixel past the scaled meter = %+v, want the background", got)
	}
}

func TestPaintClipsFillsToCanvas(t *testing.T) {
	t.Parallel()

	c := newTestCanvas(t, canvasW, canvasH)
	fillRect(c, ui.Rect{X: canvasW - 4, Y: canvasH - 4, W: 100, H: 100}, testStyle.Accent)
	fillRect(c, ui.Rect{X: -10, Y: -10, W: 12, H: 12}, testStyle.AccentOn)

	if got := pixelAt(t, c, canvasW-1, canvasH-1); got != testStyle.Accent {
		t.Errorf("bottom-right pixel = %+v, want the accent", got)
	}
	if got := pixelAt(t, c, 1, 1); got != testStyle.AccentOn {
		t.Errorf("top-left pixel = %+v, want the toggled accent", got)
	}
	if got := pixelAt(t, c, 3, 3); got == testStyle.AccentOn {
		t.Error("fill wrote past the clipped rectangle")
	}
}

func TestPaintClipsGlyphsToCanvas(t *testing.T) {
	t.Parallel()

	c := newTestCanvas(t, canvasW, canvasH)
	r := NewTextRenderer(mustTestFace(t))
	mask, err := r.Raster("sysc-shell", 16)
	if err != nil {
		t.Fatal(err)
	}

	// Straddle both edges; neither may panic or write outside the canvas.
	blendMask(c, mask.Alpha, canvasW-4, canvasH-4, testStyle.Foreground)
	blendMask(c, mask.Alpha, -mask.Alpha.Bounds().Dx()+4, -4, testStyle.Foreground)
}

func TestPaintCanvasRejectsInvalidGeometry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		pix                   int
		width, height, stride int
	}{
		{"zero width", 0, 0, 48, 0},
		{"negative height", 4 * 48, 1, -1, 4},
		{"stride below row", 240 * 48 * 4, 240, 48, 240*4 - 4},
		{"buffer too short", 240*48*4 - 1, 240, 48, 240 * 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewCanvas(make([]byte, tc.pix), tc.width, tc.height, tc.stride); err == nil {
				t.Fatal("NewCanvas accepted invalid geometry")
			}
		})
	}
}

func TestPaintRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	c := newTestCanvas(t, canvasW, canvasH)
	r := NewTextRenderer(mustTestFace(t))
	root := &ui.Node{Kind: ui.KindRow}

	noScale := testStyle
	noScale.Scale120 = 0
	noSize := testStyle
	noSize.Size = 0

	tests := []struct {
		name  string
		c     *Canvas
		root  *ui.Node
		text  *TextRenderer
		style ProofStyle
	}{
		{"nil canvas", nil, root, r, testStyle},
		{"nil root", c, nil, r, testStyle},
		{"nil renderer", c, root, nil, testStyle},
		{"zero scale", c, root, r, noScale},
		{"zero size", c, root, r, noSize},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := Paint(tc.c, tc.root, tc.text, tc.style); err == nil {
				t.Fatal("Paint accepted invalid input")
			}
		})
	}
}
