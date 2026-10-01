package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// An editing field shows the end of a long value, not its start: the caret
// must stay on screen as the user types past the right edge.
func TestEditingFieldPaintsScrolledText(t *testing.T) {
	t.Parallel()
	const w, h = 160, 40
	style := capsuleStyle()
	style.Body = ui.Rect{W: w, H: h}
	long := "abcdefghijklmnopqrstuvwxyz0123456789"
	paint := func(editing bool, scroll, cursor int) *Canvas {
		c := newTestCanvas(t, w, h)
		root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
			Kind: ui.KindTextField, Text: long, Cursor: cursor, Padding: 10,
			Bounds: ui.Rect{W: w, H: h}, Editing: editing, ScrollX: scroll,
		}}}
		if err := Paint(c, root, NewTextRenderer(mustTestFace(t)), style); err != nil {
			t.Fatal(err)
		}
		return c
	}
	rest, scrolled := paint(false, 0, len(long)), paint(true, 200, len(long))
	// The caret is the accent column; unscrolled it is off the right edge (not
	// painted in view), scrolled it is inside the text box.
	box := FieldTextRect(&ui.Node{Kind: ui.KindTextField, Padding: 10, Bounds: ui.Rect{W: w, H: h}})
	if countColor(t, scrolled, box, style.accent()) == 0 {
		t.Fatal("scrolled editing field painted no caret inside its box")
	}
	if countColor(t, rest, box, style.accent()) != 0 {
		t.Fatal("unscrolled field painted its end-of-text caret inside the box")
	}
	visibleUnfocused := paint(false, 0, 1)
	if countColor(t, visibleUnfocused, box, style.accent()) != 0 {
		t.Fatal("unfocused field painted a visible caret")
	}
}

func TestUnfocusedMultilineFieldDoesNotPaintCaret(t *testing.T) {
	t.Parallel()
	const w, h = 200, 80
	style := capsuleStyle()
	style.Body = ui.Rect{W: w, H: h}
	paint := func(editing bool) *Canvas {
		c := newTestCanvas(t, w, h)
		n := &ui.Node{Kind: ui.KindTextField, Text: "first\nsecond", Cursor: 2, Padding: 8,
			Bounds: ui.Rect{W: w, H: h}, Multiline: true, Editing: editing}
		if err := Paint(c, &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{n}}, NewTextRenderer(mustTestFace(t)), style); err != nil {
			t.Fatal(err)
		}
		return c
	}
	box := ui.Rect{W: w, H: h}
	if countColor(t, paint(false), box, style.accent()) != 0 {
		t.Fatal("unfocused multiline field painted a caret")
	}
	if countColor(t, paint(true), box, style.accent()) == 0 {
		t.Fatal("focused multiline field painted no caret")
	}
}

func TestTextFieldBoundaryDistinguishesEditingFocus(t *testing.T) {
	t.Parallel()
	const w, h = 160, 40
	style := capsuleStyle()
	style.Body = ui.Rect{W: w, H: h}
	paint := func(editing bool) *Canvas {
		c := newTestCanvas(t, w, h)
		n := &ui.Node{Kind: ui.KindTextField, Text: "search", Cursor: 1, Padding: 8,
			Bounds: ui.Rect{W: w, H: h}, Editing: editing}
		if err := Paint(c, &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{n}}, NewTextRenderer(mustTestFace(t)), style); err != nil {
			t.Fatal(err)
		}
		return c
	}
	box := ui.Rect{W: w, H: h}
	idle, focused := paint(false), paint(true)
	if countColor(t, idle, box, style.OutlineVariant) == 0 {
		t.Fatal("idle text field has no quiet outline")
	}
	if countColor(t, focused, box, style.Outline) == 0 {
		t.Fatal("focused text field has no stronger outline")
	}
}

func countColor(t *testing.T, c *Canvas, r ui.Rect, want Color) int {
	t.Helper()
	n := 0
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			if pixelAt(t, c, x, y) == want {
				n++
			}
		}
	}
	return n
}
