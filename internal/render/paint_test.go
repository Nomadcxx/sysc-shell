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
	OnPrimary:  Color{R: 0x11, G: 0x22, B: 0x33, A: 0xff},
	Error:      Color{R: 0xcc, G: 0x22, B: 0x22, A: 0xff},
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

// An attached panel squares the bar edge so its rounded top does not punch a
// wallpaper seam between the bar and the body.
func TestPaintSquaresTheAttachedEdge(t *testing.T) {
	t.Parallel()
	c := newTestCanvas(t, 100, 44)
	style := testStyle
	style.Body = ui.Rect{X: 4, Y: 4, W: 92, H: 40}
	style.Radius = 12
	style.AttachEdge = "top"
	if err := Paint(c, &ui.Node{Kind: ui.KindRow}, NewTextRenderer(mustTestFace(t)), style); err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, c, 4, 4); got != style.Background {
		t.Fatalf("attached top-left = %+v, want opaque body %+v", got, style.Background)
	}
	if got := pixelAt(t, c, 4, 43); got == style.Background {
		t.Fatal("bottom-left should stay rounded, not square")
	}
}

func TestPaintKeepsChildrenInsideTheRoundedBody(t *testing.T) {
	t.Parallel()

	c := newTestCanvas(t, 100, 44)
	style := testStyle
	style.Body = ui.Rect{X: 4, Y: 4, W: 92, H: 40}
	style.Radius = 12
	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
		Kind: ui.KindMeter, Bounds: style.Body, Value: 1,
	}}}
	if err := Paint(c, root, NewTextRenderer(mustTestFace(t)), style); err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, c, 4, 4); got != (Color{}) {
		t.Fatalf("child repainted rounded corner %+v, want transparent", got)
	}
	if got := pixelAt(t, c, 16, 4); got != style.Accent {
		t.Fatalf("child inside rounded body = %+v, want accent %+v", got, style.Accent)
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

// litPixels counts pixels that took the foreground text colour.
func litPixels(c *Canvas, fg Color) int {
	n := 0
	for i := 0; i+3 < len(c.Pix); i += 4 {
		if c.Pix[i] == fg.B && c.Pix[i+1] == fg.G && c.Pix[i+2] == fg.R && c.Pix[i+3] == fg.A {
			n++
		}
	}
	return n
}

func paintSingleText(t *testing.T, bold, italic, underline bool) *Canvas {
	t.Helper()
	c := newTestCanvas(t, 240, 48)
	style := testStyle
	style.Body = ui.Rect{W: 240, H: 48}
	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
		Kind: ui.KindText, Text: "level", Bounds: ui.Rect{X: 4, Y: 4, W: 220, H: 40},
		Bold: bold, Italic: italic, Underline: underline,
	}}}
	if err := Paint(c, root, NewTextRenderer(mustTestFace(t)), style); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPaintTextStylesChangeThePaintedInk(t *testing.T) {
	t.Parallel()
	fg := testStyle.Foreground

	plain := litPixels(paintSingleText(t, false, false, false), fg)
	bold := litPixels(paintSingleText(t, true, false, false), fg)
	if bold <= plain {
		t.Fatalf("bold ink %d <= plain %d; style was not painted", bold, plain)
	}

	under := litPixels(paintSingleText(t, false, false, true), fg)
	if under <= plain {
		t.Fatalf("underline ink %d <= plain %d; rule was not painted", under, plain)
	}

	// Synthetic italic shears the mask rightward toward the baseline, so ink
	// moves relative to the plain run: the two canvases must differ.
	it := paintSingleText(t, false, true, false)
	same := paintSingleText(t, false, false, false)
	if string(it.Pix) == string(same.Pix) {
		t.Fatal("italic painted identically to plain; shear was not applied")
	}
}

// paintTree lays out and paints the proof fixture, returning the tree.
func paintTree(t *testing.T, c *Canvas, style ProofStyle) *ui.Node {
	t.Helper()

	r := NewTextRenderer(mustTestFace(t))
	measure := func(s string, tabular bool) (int, int) {
		w, h, err := r.Measure(s, style.Size, tabular)
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
			{Kind: ui.KindButton, Text: "Go", Padding: 4, Fill: ui.FillAccent, Action: "toggle-meter"},
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

// A button paints its label in OnPrimary: the fill is the Primary token, so
// the text token paired with it is the only legible choice (launcher D11).
func TestPaintButtonTextUsesOnPrimary(t *testing.T) {
	t.Parallel()

	c := newTestCanvas(t, canvasW, canvasH)
	root := paintTree(t, c, testStyle)
	button := root.Children[2].Bounds
	label := ui.Rect{X: button.X + 4, Y: button.Y + 4, W: button.W - 8, H: button.H - 8}
	for y := label.Y; y < label.Y+label.H; y++ {
		for x := label.X; x < label.X+label.W; x++ {
			if pixelAt(t, c, x, y) == testStyle.OnPrimary {
				return
			}
		}
	}
	t.Fatal("button label painted no OnPrimary pixel")
}

func TestPaintDefaultButtonDoesNotFillTheLabel(t *testing.T) {
	t.Parallel()

	c := newTestCanvas(t, canvasW, canvasH)
	style := testStyle
	style.Body = ui.Rect{W: canvasW, H: canvasH}
	btn := &ui.Node{Kind: ui.KindButton, Text: "", Bounds: ui.Rect{X: 20, Y: 10, W: 80, H: 24}}
	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{btn}}
	if err := Paint(c, root, NewTextRenderer(mustTestFace(t)), style); err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, c, btn.Bounds.X+1, btn.Bounds.Y+1); got != style.Background {
		t.Fatalf("unfilled button corner = %+v, want the body %+v", got, style.Background)
	}
}

func TestPaintErrorFillButtonUsesTheErrorToken(t *testing.T) {
	t.Parallel()

	c := newTestCanvas(t, canvasW, canvasH)
	style := testStyle
	style.Body = ui.Rect{W: canvasW, H: canvasH}
	btn := &ui.Node{
		Kind: ui.KindButton, Text: "Record", Fill: ui.FillError, Padding: 4,
		Bounds: ui.Rect{X: 20, Y: 10, W: 80, H: 24},
	}
	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{btn}}
	if err := Paint(c, root, NewTextRenderer(mustTestFace(t)), style); err != nil {
		t.Fatal(err)
	}
	cx, cy := btn.Bounds.X+btn.Bounds.W/2, btn.Bounds.Y+btn.Bounds.H/2
	if got := pixelAt(t, c, cx, cy); got != style.Error && got != style.OnPrimary {
		t.Fatalf("error chip center = %+v, want error %+v or OnPrimary", got, style.Error)
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
	measure := func(s string, tabular bool) (int, int) {
		w, h, err := r.Measure(s, style.Size, tabular)
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
	mask, err := r.Raster("sysc-shell", 16, false)
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

// A graph paints a column per sample, taller for larger values, and leaves the
// unfilled part of the box alone.
func TestGraphPaintsTallerColumnsForLargerValues(t *testing.T) {
	t.Parallel()

	const w, h = 40, 20
	c := newTestCanvas(t, w, h)
	style := testStyle
	style.Body = ui.Rect{W: w, H: h}

	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
		Kind:   ui.KindGraph,
		Width:  4,
		Values: []float64{0, 1},
		Bounds: ui.Rect{X: 0, Y: 0, W: 4, H: h},
	}}}

	if err := Paint(c, root, NewTextRenderer(mustTestFace(t)), style); err != nil {
		t.Fatalf("Paint: %v", err)
	}

	// The zero sample paints nothing; the full one fills its column's height.
	if got := accentPixels(c, style.Accent, ui.Rect{X: 0, Y: 0, W: 2, H: h}); got != 0 {
		t.Errorf("the zero-valued column painted %d pixels, want none", got)
	}
	if got := accentPixels(c, style.Accent, ui.Rect{X: 2, Y: 0, W: 2, H: h}); got != 2*h {
		t.Errorf("the full-height column painted %d pixels, want %d", got, 2*h)
	}
}

// accentPixels counts the pixels in the box painted with the accent colour.
func accentPixels(c *Canvas, want Color, box ui.Rect) int {
	count := 0
	for y := box.Y; y < box.Y+box.H; y++ {
		for x := box.X; x < box.X+box.W; x++ {
			i := y*c.Stride + x*4
			if (Color{B: c.Pix[i], G: c.Pix[i+1], R: c.Pix[i+2], A: c.Pix[i+3]}) == want {
				count++
			}
		}
	}
	return count
}

// A meter with no reading paints nothing at all. An empty track would be
// pixel-identical to a genuine zero, so a failed collector would render as an
// idle machine.
func TestAnAbsentMeterPaintsNothing(t *testing.T) {
	t.Parallel()

	const w, h = 40, 20
	c := newTestCanvas(t, w, h)
	style := testStyle
	style.Body = ui.Rect{W: w, H: h}

	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
		Kind:   ui.KindMeter,
		Width:  w,
		Value:  0,
		Absent: true,
		Bounds: ui.Rect{X: 0, Y: 0, W: w, H: h},
	}}}
	if err := Paint(c, root, NewTextRenderer(mustTestFace(t)), style); err != nil {
		t.Fatalf("Paint: %v", err)
	}

	box := ui.Rect{X: 0, Y: 0, W: w, H: h}
	if got := accentPixels(c, style.Track, box); got != 0 {
		t.Errorf("an absent meter painted %d track pixels, want none", got)
	}
	if got := accentPixels(c, style.Accent, box); got != 0 {
		t.Errorf("an absent meter painted %d fill pixels, want none", got)
	}
	// A genuine zero still paints its track, which is what makes the two
	// distinguishable.
	root.Children[0].Absent = false
	if err := Paint(c, root, NewTextRenderer(mustTestFace(t)), style); err != nil {
		t.Fatalf("Paint: %v", err)
	}
	if got := accentPixels(c, style.Track, box); got == 0 {
		t.Error("a zero meter painted no track, so absent and zero look alike")
	}
}

// An absent graph paints nothing even when values are still attached.
func TestAnAbsentGraphPaintsNothing(t *testing.T) {
	t.Parallel()

	const w, h = 40, 20
	c := newTestCanvas(t, w, h)
	style := testStyle
	style.Body = ui.Rect{W: w, H: h}

	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
		Kind:   ui.KindGraph,
		Width:  4,
		Values: []float64{1, 1},
		Absent: true,
		Bounds: ui.Rect{X: 0, Y: 0, W: 4, H: h},
	}}}
	if err := Paint(c, root, NewTextRenderer(mustTestFace(t)), style); err != nil {
		t.Fatalf("Paint: %v", err)
	}
	if got := accentPixels(c, style.Accent, ui.Rect{X: 0, Y: 0, W: 4, H: h}); got != 0 {
		t.Fatalf("an absent graph painted %d pixels, want none", got)
	}
}

// Text reporting a failure paints in the error colour, not the foreground, so
// a failed reading is distinguishable at a glance.
func TestErrorToneTextPaintsInTheErrorColour(t *testing.T) {
	t.Parallel()

	canvas := newTestCanvas(t, 80, 20)
	style := testStyle
	style.Body = ui.Rect{W: 80, H: 20}
	r := NewTextRenderer(mustTestFace(t))
	style.Foreground = Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	style.Error = Color{R: 0xff, G: 0x40, B: 0x40, A: 0xff}

	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
		Kind:   ui.KindText,
		Text:   "-",
		Tone:   ui.ToneError,
		Bounds: ui.Rect{X: 0, Y: 0, W: 80, H: 20},
	}}}
	if err := Paint(canvas, root, r, style); err != nil {
		t.Fatalf("Paint: %v", err)
	}

	// The error colour has a low green channel; the foreground is white. Any
	// painted pixel must therefore be redder than it is green.
	var sawErrorPixel bool
	for i := 0; i+3 < len(canvas.Pix); i += 4 {
		red, green := canvas.Pix[i+2], canvas.Pix[i+1]
		if red > 0 && red > green {
			sawErrorPixel = true
			break
		}
	}
	if !sawErrorPixel {
		t.Fatal("error-tone text painted no pixel in the error colour")
	}
}

// capsuleStyle adds the three capsule fills to the shared test style, each
// distinct so a sampled pixel names exactly one of them.
func capsuleStyle() ProofStyle {
	s := testStyle
	s.Capsule = Color{R: 0x18, G: 0x1a, B: 0x1d, A: 0xff}
	s.Container = Color{R: 0x11, G: 0x83, B: 0xa2, A: 0xff}
	s.OnAccent = Color{R: 0x21, G: 0x23, B: 0x37, A: 0xff}
	s.OnContainer = Color{R: 0x0a, G: 0x0b, B: 0x11, A: 0xff}
	return s
}

func TestPaintCapsuleFill(t *testing.T) {
	t.Parallel()
	c := newTestCanvas(t, canvasW, canvasH)
	style := capsuleStyle()
	r := NewTextRenderer(mustTestFace(t))

	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{
		{Kind: ui.KindCapsule, Padding: 8, Bounds: ui.Rect{X: 10, Y: 8, W: 60, H: 32}},
		{Kind: ui.KindCapsule, Fill: ui.FillAccent, Bounds: ui.Rect{X: 90, Y: 12, W: 20, H: 20}},
		{Kind: ui.KindCapsule, Fill: ui.FillContainer, Bounds: ui.Rect{X: 130, Y: 12, W: 20, H: 20}},
	}}
	if err := Paint(c, root, r, style); err != nil {
		t.Fatal(err)
	}

	if got := pixelAt(t, c, 40, 24); got != style.Capsule {
		t.Errorf("default capsule centre = %+v, want Capsule %+v", got, style.Capsule)
	}
	if got := pixelAt(t, c, 100, 22); got != style.Accent {
		t.Errorf("accent dot centre = %+v, want Accent %+v", got, style.Accent)
	}
	if got := pixelAt(t, c, 140, 22); got != style.Container {
		t.Errorf("container dot centre = %+v, want Container %+v", got, style.Container)
	}
	// Between the two dots is bar body, not pill.
	if got := pixelAt(t, c, 120, 22); got != style.Background {
		t.Errorf("gap between dots = %+v, want Background %+v", got, style.Background)
	}
}

// A pill's numeral must be legible on its own fill, so the capsule supplies the
// matching foreground to its subtree rather than each caller tagging a tone.
func TestCapsuleGivesItsChildTheMatchingForeground(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		fill ui.Fill
		want func(ProofStyle) Color
	}{
		{"accent", ui.FillAccent, func(s ProofStyle) Color { return s.OnAccent }},
		{"container", ui.FillContainer, func(s ProofStyle) Color { return s.OnContainer }},
		{"default", ui.FillNone, func(s ProofStyle) Color { return s.Foreground }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			style := capsuleStyle()
			if got := capsuleForeground(style, tc.fill); got != tc.want(style) {
				t.Fatalf("foreground for %v = %+v, want %+v", tc.fill, got, tc.want(style))
			}
		})
	}
}

func TestPaintScrollDrawsAThumbWhenContentOverflows(t *testing.T) {
	t.Parallel()
	const w, h = 120, 80
	c := newTestCanvas(t, w, h)
	style := testStyle
	style.Body = ui.Rect{W: w, H: h}
	style.Radius = 0
	measure := func(s string, _ bool) (int, int) {
		if s == "tall" {
			return 80, 400
		}
		return 8, 16
	}
	root := &ui.Node{Kind: ui.KindScroll, Padding: 8, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "tall"},
	}}
	if err := ui.LayoutColumn(root, ui.Rect{W: w, H: h}, measure); err != nil {
		t.Fatal(err)
	}
	if root.ContentH <= h-16 {
		t.Fatalf("fixture ContentH = %d, want overflow", root.ContentH)
	}
	if err := Paint(c, root, NewTextRenderer(mustTestFace(t)), style); err != nil {
		t.Fatal(err)
	}
	found := false
	for y := 8; y < h-8; y++ {
		if pixelAt(t, c, w-10, y) == style.Foreground {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("overflowing scroll painted no foreground thumb on the right")
	}
}
