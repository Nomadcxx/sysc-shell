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
	paint := func(editing bool, scroll int) *Canvas {
		c := newTestCanvas(t, w, h)
		root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
			Kind: ui.KindTextField, Text: long, Cursor: len(long), Padding: 10,
			Bounds: ui.Rect{W: w, H: h}, Editing: editing, ScrollX: scroll,
		}}}
		if err := Paint(c, root, NewTextRenderer(mustTestFace(t)), style); err != nil {
			t.Fatal(err)
		}
		return c
	}
	rest, scrolled := paint(false, 0), paint(true, 200)
	// The caret is the accent column; unscrolled it is off the right edge (not
	// painted in view), scrolled it is inside the text box.
	box := FieldTextRect(&ui.Node{Kind: ui.KindTextField, Padding: 10, Bounds: ui.Rect{W: w, H: h}})
	if countColor(t, scrolled, box, style.accent()) == 0 {
		t.Fatal("scrolled editing field painted no caret inside its box")
	}
	if countColor(t, rest, box, style.accent()) != 0 {
		t.Fatal("unscrolled field painted its end-of-text caret inside the box")
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
