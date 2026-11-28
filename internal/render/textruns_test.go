package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// A text node with runs paints each run in its own tone, over the same glyph
// coverage as the plain text.
func TestPainterColoursRuns(t *testing.T) {
	t.Parallel()
	style := testStyle
	style.Subtle = Color{R: 0xc4, G: 0xc6, B: 0xd0, A: 0xff}
	text := NewTextRenderer(mustTestFace(t))
	n := &ui.Node{Kind: ui.KindText, Text: "████████", Bounds: ui.Rect{X: 4, Y: 4, W: 200, H: 32},
		Runs: []ui.TextRun{{Start: 0, End: len("████"), Tone: ui.ToneAccent}, {Start: len("████"), End: len("████████"), Tone: ui.ToneSubtle}}}
	c := newTestCanvas(t, 220, 40)
	if err := paintNodeContent(c, n, text, style, 16); err != nil {
		t.Fatal(err)
	}
	plain := newTestCanvas(t, 220, 40)
	if err := paintNodeContent(plain, &ui.Node{Kind: ui.KindText, Text: n.Text, Bounds: n.Bounds}, text, style, 16); err != nil {
		t.Fatal(err)
	}
	spec := textSpec(style, n)
	half, _, err := text.Measure("████", spec, false)
	if err != nil {
		t.Fatal(err)
	}
	full, h, err := text.Measure(n.Text, spec, false)
	if err != nil {
		t.Fatal(err)
	}
	y := n.Bounds.Y + h/2
	sample := func(c *Canvas, x int) Color {
		i := y*c.Stride + x*4
		return Color{B: c.Pix[i], G: c.Pix[i+1], R: c.Pix[i+2], A: c.Pix[i+3]}
	}
	near := func(got, want Color) bool {
		d := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }
		return d(got.R, want.R) < 8 && d(got.G, want.G) < 8 && d(got.B, want.B) < 8
	}
	left, right := n.Bounds.X+half/2, n.Bounds.X+half+(full-half)/2
	if got := sample(c, left); !near(got, style.accent()) {
		t.Fatalf("first run pixel = %+v, want accent %+v", got, style.accent())
	}
	if got := sample(c, right); !near(got, style.Subtle) {
		t.Fatalf("second run pixel = %+v, want subtle %+v", got, style.Subtle)
	}
	for i := 3; i < len(c.Pix); i += 4 {
		if (c.Pix[i] == 0) != (plain.Pix[i] == 0) {
			t.Fatalf("runs changed coverage at byte %d", i)
		}
	}
}
