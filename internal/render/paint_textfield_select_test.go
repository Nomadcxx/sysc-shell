package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestEditingFieldFillsItsSelectionBehindTheText(t *testing.T) {
	t.Parallel()
	const w, h = 200, 40
	style := capsuleStyle()
	style.Body = ui.Rect{W: w, H: h}
	paint := func(sel bool) *Canvas {
		c := newTestCanvas(t, w, h)
		n := &ui.Node{Kind: ui.KindTextField, Text: "hello world", Cursor: 11, Padding: 10,
			Bounds: ui.Rect{W: w, H: h}, Editing: true}
		if sel {
			n.SelStart, n.SelEnd = 0, 11
		}
		if err := Paint(c, &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{n}}, NewTextRenderer(mustTestFace(t)), style); err != nil {
			t.Fatal(err)
		}
		return c
	}
	box := FieldTextRect(&ui.Node{Kind: ui.KindTextField, Padding: 10, Bounds: ui.Rect{W: w, H: h}})
	if countColor(t, paint(false), box, style.Capsule) <= countColor(t, paint(true), box, style.Capsule) {
		t.Fatal("selection did not cover the well behind the text")
	}
}

func TestFieldOffsetAtSnapsToTheNearestBoundary(t *testing.T) {
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len(s) * 10, 16 }
	n := &ui.Node{Kind: ui.KindTextField, Text: "abcdef", Padding: 10, Bounds: ui.Rect{W: 200, H: 40}}
	box := FieldTextRect(n)
	cases := []struct{ x, want int }{
		{box.X - 5, 0}, {box.X + 14, 1}, {box.X + 16, 2}, {box.X + 500, 6},
	}
	for _, tc := range cases {
		if got := FieldOffsetAt(n, tc.x, measure); got != tc.want {
			t.Errorf("x=%d: %d, want %d", tc.x, got, tc.want)
		}
	}
	n.Editing, n.ScrollX = true, 20
	if got := FieldOffsetAt(n, box.X+1, measure); got != 2 {
		t.Errorf("scrolled by 20, x at the box start = %d, want 2", got)
	}
}

// A multiline selection fills each line it covers, not only the first.
func TestMultilineSelectionFillsEveryLineItCovers(t *testing.T) {
	t.Parallel()
	const w, h = 200, 120
	style := capsuleStyle()
	style.Body = ui.Rect{W: w, H: h}
	paint := func(sel bool) *Canvas {
		c := newTestCanvas(t, w, h)
		n := &ui.Node{Kind: ui.KindTextField, Text: "first\nsecond", Cursor: 12, Padding: 10,
			Bounds: ui.Rect{W: w, H: h}, Multiline: true, Editing: true}
		if sel {
			n.SelStart, n.SelEnd = 2, 9
		}
		if err := Paint(c, &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{n}}, NewTextRenderer(mustTestFace(t)), style); err != nil {
			t.Fatal(err)
		}
		return c
	}
	box := FieldTextRect(&ui.Node{Kind: ui.KindTextField, Padding: 10, Bounds: ui.Rect{W: w, H: h}, Multiline: true})
	_, lineH, err := NewTextRenderer(mustTestFace(t)).Measure(" ", textSpec(style, &ui.Node{Kind: ui.KindTextField}), false)
	if err != nil || lineH <= 0 {
		t.Fatalf("line height %d: %v", lineH, err)
	}
	for i, line := range []ui.Rect{{X: box.X, Y: box.Y, W: box.W, H: lineH}, {X: box.X, Y: box.Y + lineH, W: box.W, H: lineH}} {
		if countColor(t, paint(false), line, style.Capsule) <= countColor(t, paint(true), line, style.Capsule) {
			t.Fatalf("line %d: selection did not cover the well", i)
		}
	}
}
