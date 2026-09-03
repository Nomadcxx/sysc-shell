package render

import (
	"image"
	"math"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/go-text/typesetting/language"
)

const (
	canvasW = 240
	canvasH = 48
)

var testStyle = Style{
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
	OnError:    Color{R: 0xff, G: 0xee, B: 0xee, A: 0xff},
	// Capsule is the high container the bar's pills and the panels' cards
	// share; ContainerHighest is the level a control resting on one of those
	// needs in order to separate.
	Capsule:          Color{R: 0x3a, G: 0x41, B: 0x49, A: 0xff},
	ContainerHighest: Color{R: 0x46, G: 0x4e, B: 0x58, A: 0xff},
	Container:        Color{R: 0x1f, G: 0x7a, B: 0xb5, A: 0xff},
	OnAccent:         Color{R: 0x0b, G: 0x10, B: 0x16, A: 0xff},
	OnContainer:      Color{R: 0x0b, G: 0x10, B: 0x16, A: 0xff},
	Outline:          Color{R: 0x73, G: 0x7d, B: 0x89, A: 0xff},
	OutlineVariant:   Color{R: 0x59, G: 0x61, B: 0x6b, A: 0xff},
}

func TestPaintKeepsColorEmojiUntinted(t *testing.T) {
	t.Parallel()
	m := newSystemMap(t)
	const glyph = '👋'
	m.inner.SetScript(language.LookupScript(glyph))
	resolved := m.inner.ResolveFace(glyph)
	if resolved == nil {
		t.Skip("system font set has no emoji fallback")
	}
	gid, ok := resolved.NominalGlyph(glyph)
	if !ok {
		t.Skip("resolved emoji face has no nominal glyph")
	}
	if _, ok := resolved.GlyphDataBitmap(gid); !ok {
		t.Skip("resolved emoji face has no bitmap glyph")
	}

	c := newTestCanvas(t, 64, 48)
	ink := Color{R: 0xff, G: 0x00, B: 0xff, A: 0xff}
	err := paintTextColor(c, string(glyph), ui.Rect{X: 4, Y: 4, W: 56, H: 40},
		NewTextRendererWithFontMap(m), testStyle, 32, false, ink, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	// Magenta notdef coverage has G≈0. A real 👋 bitmap has skin/yellow (G>0).
	for i := 0; i+3 < len(c.Pix); i += 4 {
		if c.Pix[i+1] > 40 && c.Pix[i+3] > 40 {
			return
		}
	}
	t.Fatal("emoji painted as tinted notdef; no un-tinted colour pixel")
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
func paintTree(t *testing.T, c *Canvas, style Style) *ui.Node {
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
	// A button is a stadium, so its corners are transparent; the fill is read
	// at the centre.
	cx, cy := button.X+button.W/2, button.Y+button.H/2
	if got := pixelAt(t, off, cx, cy); got != testStyle.Accent {
		t.Errorf("untoggled button pixel = %+v, want the accent %+v", got, testStyle.Accent)
	}

	on := newTestCanvas(t, canvasW, canvasH)
	toggled := testStyle
	toggled.Toggled = true
	paintTree(t, on, toggled)
	if got := pixelAt(t, on, cx, cy); got != testStyle.AccentOn {
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
		style Style
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
func capsuleStyle() Style {
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
		want func(Style) Color
	}{
		{"accent", ui.FillAccent, func(s Style) Color { return s.OnAccent }},
		{"container", ui.FillContainer, func(s Style) Color { return s.OnContainer }},
		{"soft", ui.FillSoft, func(s Style) Color { return s.Foreground }},
		{"default", ui.FillNone, func(s Style) Color { return s.Foreground }},
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

func TestPaintStrokesTheRimWhenSet(t *testing.T) {
	t.Parallel()
	c := newTestCanvas(t, 100, 80)
	style := testStyle
	style.Body = ui.Rect{X: 8, Y: 8, W: 84, H: 64}
	style.Radius = 0
	// Rim, not Outline: every surface carries the outline token for its
	// controls, so gating the panel edge on Outline would rim the bar too.
	style.Rim = Color{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff}
	if err := Paint(c, &ui.Node{Kind: ui.KindRow}, NewTextRenderer(mustTestFace(t)), style); err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, c, 8, 40); got != style.Rim {
		t.Fatalf("left rim = %+v, want the rim %+v", got, style.Rim)
	}
	if got := pixelAt(t, c, 50, 40); got != style.Background {
		t.Fatalf("interior = %+v, want body %+v", got, style.Background)
	}
}

func TestPaintTextFieldIsAStadiumOnCapsule(t *testing.T) {
	t.Parallel()
	const w, h = 120, 48
	c := newTestCanvas(t, w, h)
	style := capsuleStyle()
	style.Body = ui.Rect{W: w, H: h}
	style.Track = Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
		Kind: ui.KindTextField, Padding: 8, Bounds: ui.Rect{X: 4, Y: 8, W: 112, H: 32},
	}}}
	if err := Paint(c, root, NewTextRenderer(mustTestFace(t)), style); err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, c, 60, 24); got != style.Capsule {
		t.Fatalf("well centre = %+v, want Capsule %+v", got, style.Capsule)
	}
	if got := pixelAt(t, c, 4, 8); got != style.Background {
		t.Fatalf("stadium corner = %+v, want Background %+v", got, style.Background)
	}
}

func TestPaintSearchFieldDrawsALeadingMark(t *testing.T) {
	t.Parallel()
	const w, h = 120, 48
	c := newTestCanvas(t, w, h)
	style := capsuleStyle()
	style.Body = ui.Rect{W: w, H: h}
	style.Outline = Color{R: 0xaa, G: 0x99, B: 0xcc, A: 0xff}
	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
		Kind: ui.KindTextField, Name: "Search", Padding: 8, Bounds: ui.Rect{X: 4, Y: 8, W: 112, H: 32},
	}}}
	if err := Paint(c, root, NewTextRenderer(mustTestFace(t)), style); err != nil {
		t.Fatal(err)
	}
	found := false
	for y := 12; y < 36; y++ {
		for x := 6; x < 24; x++ {
			if pixelAt(t, c, x, y) == style.Foreground {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("search field painted no leading mark")
	}
	if got := pixelAt(t, c, 60, 24); got != style.Capsule {
		t.Fatalf("search well = %+v, want Capsule %+v", got, style.Capsule)
	}
}

func TestPaintSoftCapsuleWashesAccent(t *testing.T) {
	t.Parallel()
	c := newTestCanvas(t, canvasW, canvasH)
	style := capsuleStyle()
	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{
		{Kind: ui.KindCapsule, Fill: ui.FillSoft, Bounds: ui.Rect{X: 10, Y: 8, W: 60, H: 32}},
	}}
	if err := Paint(c, root, NewTextRenderer(mustTestFace(t)), style); err != nil {
		t.Fatal(err)
	}
	want := wash(style.Accent, style.Capsule)
	if got := pixelAt(t, c, 40, 24); got != want {
		t.Fatalf("soft fill = %+v, want wash %+v", got, want)
	}
	if got := pixelAt(t, c, 40, 24); got == style.Accent {
		t.Fatal("soft fill used solid Accent")
	}
}

func TestPaintSearchMarkHandleGoesDiagonal(t *testing.T) {
	t.Parallel()
	const w, h = 120, 48
	c := newTestCanvas(t, w, h)
	style := capsuleStyle()
	style.Body = ui.Rect{W: w, H: h}
	field := ui.Rect{X: 4, Y: 8, W: 112, H: 32}
	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
		Kind: ui.KindTextField, Name: "Search", Padding: 8, Bounds: field,
	}}}
	if err := Paint(c, root, NewTextRenderer(mustTestFace(t)), style); err != nil {
		t.Fatal(err)
	}
	box := field
	slot := 22
	cx, cy := box.X+slot/2, box.Y+box.H/2-3
	r := min(box.H/5, slot/3)
	if r < 3 {
		r = 3
	}
	if got := pixelAt(t, c, cx+r+3, cy+r+3); got != style.Foreground {
		t.Fatalf("diagonal handle = %+v, want Foreground %+v", got, style.Foreground)
	}
	if got := pixelAt(t, c, cx+r+4, cy); got == style.Foreground {
		t.Fatal("handle painted a horizontal dash")
	}
}

// --- Catalogue chrome -------------------------------------------------------

// paintChromeNode lays a single node out at a fixed box and paints it against a
// filled background, so a test can read the resolved fill straight off the
// canvas. The background stands in for the panel the control rests on.
func paintChromeNode(t *testing.T, n *ui.Node, style Style) *Canvas {
	t.Helper()
	const w, h = 200, 60
	c := newTestCanvas(t, w, h)
	fillRect(c, ui.Rect{W: w, H: h}, style.Background)

	r := NewTextRenderer(mustTestFace(t))
	measure := func(s string, tabular bool) (int, int) {
		tw, th, err := r.Measure(s, style.Size, tabular)
		if err != nil {
			t.Fatal(err)
		}
		return tw, th
	}
	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{n}}
	if err := ui.Layout(root, ui.Rect{W: w, H: h}, measure); err != nil {
		t.Fatal(err)
	}
	if err := paintNode(c, n, r, style, style.Size); err != nil {
		t.Fatal(err)
	}
	return c
}

func centreOf(n *ui.Node) (int, int) {
	return n.Bounds.X + n.Bounds.W/2, n.Bounds.Y + n.Bounds.H/2
}

// fillPointOf is a point inside the node's chrome but clear of its centred
// label, so a test reads the resolved fill rather than an antialiased glyph.
// At the vertical midpoint a stadium's corner inset is zero, so this sits just
// inside the left edge and past any boundary stroke.
func fillPointOf(n *ui.Node) (int, int) {
	return n.Bounds.X + 4, n.Bounds.Y + n.Bounds.H/2
}

// overlay is the colour the painter must produce when it composites src over
// dst at the given alpha, following the canvas's own blend.
func overlay(dst, src Color, alpha float64) Color {
	a := uint32(math.Round(float64(src.A) * alpha))
	if a == 255 {
		return src
	}
	inv := 255 - a
	ch := func(s, d uint8) uint8 { return byte(uint32(s)*a/255 + uint32(d)*inv/255) }
	return Color{R: ch(src.R, dst.R), G: ch(src.G, dst.G), B: ch(src.B, dst.B), A: ch(src.A, dst.A)}
}

func TestIdleButtonIsAStadiumOnTheHighestContainer(t *testing.T) {
	t.Parallel()
	n := &ui.Node{Kind: ui.KindButton, Text: "Lock", Height: 40, Width: 120,
		Padding: 12, Action: "session:lock"}
	c := paintChromeNode(t, n, testStyle)

	fx, fy := fillPointOf(n)
	if got := pixelAt(t, c, fx, fy); got != testStyle.ContainerHighest {
		t.Errorf("idle fill = %+v, want the highest container %+v", got, testStyle.ContainerHighest)
	}
	if n.Bounds.H != 40 {
		t.Errorf("button height = %d, want 40", n.Bounds.H)
	}
	// A stadium rounds to half its short side, so the box corner keeps whatever
	// the panel painted rather than a square of control fill.
	if got := pixelAt(t, c, n.Bounds.X, n.Bounds.Y); got != testStyle.Background {
		t.Errorf("corner = %+v, want the untouched panel %+v", got, testStyle.Background)
	}
}

func TestOutlinedButtonKeepsParentFillAndDrawsABoundary(t *testing.T) {
	t.Parallel()
	n := &ui.Node{Kind: ui.KindButton, Text: "Reboot", Height: 40, Width: 120,
		Padding: 12, Fill: ui.FillOutline, Action: "session:reboot"}
	c := paintChromeNode(t, n, testStyle)

	fx, fy := fillPointOf(n)
	if got := pixelAt(t, c, fx, fy); got != testStyle.Background {
		t.Errorf("outlined fill = %+v, want the parent fill %+v", got, testStyle.Background)
	}
	if got := pixelAt(t, c, fx, fy); got == testStyle.Accent {
		t.Error("outlined control filled with Primary; idle chrome must not read as selected")
	}
	_, cy := centreOf(n)
	// The boundary sits on the left edge at the vertical midpoint, where the
	// stadium's inset is zero.
	if got := pixelAt(t, c, n.Bounds.X, cy); got != testStyle.Outline {
		t.Errorf("boundary = %+v, want Outline %+v", got, testStyle.Outline)
	}
}

func TestSelectedChromeUsesPrimaryAndItsPairedForeground(t *testing.T) {
	t.Parallel()
	n := &ui.Node{Kind: ui.KindButton, Text: "Balanced", Height: 40, Width: 140,
		Padding: 12, Action: "profile:balanced", State: ui.StateSelected}
	c := paintChromeNode(t, n, testStyle)

	fx, fy := fillPointOf(n)
	if got := pixelAt(t, c, fx, fy); got != testStyle.Accent {
		t.Errorf("selected fill = %+v, want Primary %+v", got, testStyle.Accent)
	}
	// The label must be the token paired with the fill, not the panel's own
	// foreground, or a selected segment loses contrast.
	if litPixels(c, testStyle.OnPrimary) == 0 {
		t.Error("selected label is not painted in OnPrimary")
	}
}

func TestDestructiveOutlinedUsesTheErrorPair(t *testing.T) {
	t.Parallel()
	rest := &ui.Node{Kind: ui.KindButton, Text: "Power off", Height: 40, Width: 140,
		Padding: 12, Fill: ui.FillOutline, Tone: ui.ToneError, Action: "session:poweroff"}
	c := paintChromeNode(t, rest, testStyle)
	fx, fy := fillPointOf(rest)
	_, cy := centreOf(rest)

	// Destructive controls are error-toned outlines at rest, not solid red.
	if got := pixelAt(t, c, fx, fy); got != testStyle.Background {
		t.Errorf("resting fill = %+v, want the parent fill, not a red block", got)
	}
	if got := pixelAt(t, c, rest.Bounds.X, cy); got != testStyle.Error {
		t.Errorf("boundary = %+v, want Error %+v", got, testStyle.Error)
	}

	hovered := *rest
	hovered.State = ui.StateHovered
	hc := paintChromeNode(t, &hovered, testStyle)
	hx, hy := fillPointOf(&hovered)
	want := overlay(testStyle.Background, testStyle.Error, hoverLayerAlpha)
	if got := pixelAt(t, hc, hx, hy); got != want {
		t.Errorf("hovered destructive = %+v, want an Error state layer %+v", got, want)
	}

	filled := &ui.Node{Kind: ui.KindButton, Text: "Power off", Height: 40, Width: 140,
		Padding: 12, Fill: ui.FillError, Action: "session:poweroff"}
	fc := paintChromeNode(t, filled, testStyle)
	if litPixels(fc, testStyle.OnError) == 0 {
		t.Error("an Error-filled control does not label itself in OnError")
	}
}

func TestStateLayersCompositeThePairedForeground(t *testing.T) {
	t.Parallel()
	base := ui.Node{Kind: ui.KindButton, Text: "Lock", Height: 40, Width: 120,
		Padding: 12, Action: "session:lock"}
	fill, fg := testStyle.ContainerHighest, testStyle.Foreground

	for _, tc := range []struct {
		name  string
		state ui.Interaction
		want  Color
	}{
		{"idle", 0, fill},
		{"hover", ui.StateHovered, overlay(fill, fg, hoverLayerAlpha)},
		{"pressed", ui.StatePressed, overlay(fill, fg, pressedLayerAlpha)},
		// Pressed outranks hover: a pointer is always inside the node it is
		// pressing, so the two arrive together.
		{"pressed while hovered", ui.StatePressed | ui.StateHovered, overlay(fill, fg, pressedLayerAlpha)},
		{"disabled", ui.StateDisabled, fill},
	} {
		n := base
		n.State = tc.state
		c := paintChromeNode(t, &n, testStyle)
		fx, fy := fillPointOf(&n)
		if got := pixelAt(t, c, fx, fy); got != tc.want {
			t.Errorf("%s fill = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestDisabledLowersForegroundEmphasis(t *testing.T) {
	t.Parallel()
	base := ui.Node{Kind: ui.KindButton, Text: "Lock", Height: 40, Width: 120,
		Padding: 12, Action: "session:lock"}
	idle := base
	c := paintChromeNode(t, &idle, testStyle)
	full := litPixels(c, testStyle.Foreground)
	if full == 0 {
		t.Fatal("idle label is not painted in the foreground")
	}

	off := base
	off.State = ui.StateDisabled
	dc := paintChromeNode(t, &off, testStyle)
	// Lower emphasis, not absent: the label still has to be readable.
	if got := litPixels(dc, testStyle.Foreground); got >= full {
		t.Errorf("disabled label paints %d full-strength pixels, want fewer than %d", got, full)
	}
	if litPixels(dc, testStyle.ContainerHighest) == 0 {
		t.Error("disabled control lost its fill entirely")
	}
}

func TestCapsuleAndCardShareTheHighContainer(t *testing.T) {
	t.Parallel()
	// One continuous surface carries the bar and the panels, so a pill and a
	// card are the same fill; only the radius differs. A capsule is sized by
	// its child, so each of these carries one label and the assertions sample
	// the far side of the pill, clear of the glyphs.
	style := testStyle
	style.Radius = 12
	label := func(s string) []*ui.Node { return []*ui.Node{{Kind: ui.KindText, Text: s}} }
	rightOf := func(n *ui.Node) (int, int) {
		return n.Bounds.X + n.Bounds.W - 8, n.Bounds.Y + n.Bounds.H/2
	}

	pill := &ui.Node{Kind: ui.KindCapsule, Width: 100, Children: label("CPU")}
	pc := paintChromeNode(t, pill, style)
	px, py := rightOf(pill)
	if got := pixelAt(t, pc, px, py); got != style.Capsule {
		t.Errorf("capsule fill = %+v, want the high container %+v", got, style.Capsule)
	}

	card := &ui.Node{Kind: ui.KindCapsule, Width: 160, Radius: 12,
		Fill: ui.FillContainerHigh, Children: label("Battery")}
	cc := paintChromeNode(t, card, style)
	cx, cy := rightOf(card)
	if got := pixelAt(t, cc, cx, cy); got != style.Capsule {
		t.Errorf("card fill = %+v, want the same high container %+v", got, style.Capsule)
	}

	// A card keeps the theme's rounded-rectangle radius instead of clamping to
	// a stadium. Four rows down from the top the card's corner inset is 3 px
	// while a stadium of the same height has cut 14 px away, so this point
	// separates the two shapes rather than merely proving something painted.
	corner := func(n *ui.Node) (int, int) { return n.Bounds.X + 5, n.Bounds.Y + 4 }
	ccx, ccy := corner(card)
	if got := pixelAt(t, cc, ccx, ccy); got != style.Capsule {
		t.Errorf("card corner = %+v, want the card radius, not a stadium", got)
	}
	// A button of the same box is the contrast: it clamps to a stadium, whose
	// corner has cut this point away.
	button := &ui.Node{Kind: ui.KindButton, Width: 160, Height: 60, Padding: 12,
		Text: "Battery", Action: "battery"}
	bc := paintChromeNode(t, button, style)
	bcx, bcy := corner(button)
	if got := pixelAt(t, bc, bcx, bcy); got == style.ContainerHighest {
		t.Error("button kept a card corner; a button clamps to a stadium")
	}
}

func TestButtonPaintsChildrenOverItsStateLayer(t *testing.T) {
	t.Parallel()
	label := &ui.Node{Kind: ui.KindText, Text: "Lock"}
	n := &ui.Node{Kind: ui.KindButton, Height: 40, Width: 140, Padding: 12, Gap: 8,
		Action: "session:lock", State: ui.StateHovered,
		Children: []*ui.Node{label},
	}
	c := paintChromeNode(t, n, testStyle)

	washed := overlay(testStyle.ContainerHighest, testStyle.Foreground, hoverLayerAlpha)
	wx, wy := fillPointOf(n)
	if got := pixelAt(t, c, wx, wy); got != washed {
		t.Errorf("hovered fill = %+v, want the state layer %+v", got, washed)
	}
	// Children land on top of the layer at full strength; a label must not be
	// dimmed by its own control's hover.
	if litPixels(c, testStyle.Foreground) == 0 {
		t.Error("child text was painted under the state layer, or not at all")
	}
}

func TestStrokeRoundedLeavesTheInteriorAlone(t *testing.T) {
	t.Parallel()
	// The focus ring is independent of any fill or state layer: it outlines a
	// node without repainting what is inside it.
	c := newTestCanvas(t, 60, 40)
	box := ui.Rect{X: 5, Y: 5, W: 50, H: 30}
	fillRoundedRect(c, box, 12, testStyle.Capsule)
	c.StrokeRounded(box, 12, 2, testStyle.Accent)

	if got := pixelAt(t, c, box.X+box.W/2, box.Y+box.H/2); got != testStyle.Capsule {
		t.Errorf("interior = %+v, want the untouched fill %+v", got, testStyle.Capsule)
	}
	if got := pixelAt(t, c, box.X, box.Y+box.H/2); got != testStyle.Accent {
		t.Errorf("ring = %+v, want the accent %+v", got, testStyle.Accent)
	}
	// Two pixels wide, so the third pixel in is fill again.
	if got := pixelAt(t, c, box.X+2, box.Y+box.H/2); got != testStyle.Capsule {
		t.Errorf("ring is thicker than 2 px: %+v at inset 2", got)
	}
}

// TestPaintSegmentedPaintsItsSegments guards the gap that shipped the segmented
// row with layout but no paint: KindSegmented fell through paintNode's switch to
// the default arm, so opening the power panel returned "unsupported kind 17" and
// took the shell down with it. The container owns allocation, not chrome, so
// each segment paints its own fill: Primary when selected, quiet container
// otherwise.
func TestPaintSegmentedPaintsItsSegments(t *testing.T) {
	t.Parallel()

	selected := &ui.Node{
		Kind: ui.KindButton, Height: 40, Padding: 4,
		State:    ui.StateSelected,
		Children: []*ui.Node{{Kind: ui.KindText, Text: "Auto"}},
	}
	idle := &ui.Node{
		Kind: ui.KindButton, Height: 40, Padding: 4,
		Children: []*ui.Node{{Kind: ui.KindText, Text: "Eco"}},
	}
	row := &ui.Node{
		Kind: ui.KindSegmented, Key: "power-profiles", Gap: 2, Height: 40,
		Children: []*ui.Node{selected, idle},
	}

	c := paintChromeNode(t, row, testStyle)

	x, y := fillPointOf(selected)
	if got := pixelAt(t, c, x, y); got != testStyle.Accent {
		t.Errorf("selected segment fill = %+v, want the accent %+v", got, testStyle.Accent)
	}
	x, y = fillPointOf(idle)
	if got := pixelAt(t, c, x, y); got != testStyle.containerHighest() {
		t.Errorf("idle segment fill = %+v, want the highest container %+v", got, testStyle.containerHighest())
	}
}
