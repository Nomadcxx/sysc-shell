package render

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestMaskedFieldHitTestingDoesNotMeasureEveryPrefix(t *testing.T) {
	n := &ui.Node{Kind: ui.KindTextField, Text: strings.Repeat("a", 4096), Masked: true,
		Padding: 10, Bounds: ui.Rect{W: 200, H: 40}, Editing: true, ScrollX: 20000}
	calls := 0
	measure := func(s string, _ ui.TextAttrs) (int, int) {
		calls++
		return utf8.RuneCountInString(s) * 10, 16
	}
	if got := FieldOffsetAt(n, FieldTextRect(n).X+16, measure); got != 2002 {
		t.Fatalf("scrolled caret = %d, want 2002", got)
	}
	if calls > 32 {
		t.Fatalf("hit testing measured %d prefixes; want logarithmic search", calls)
	}
}

func TestMaskedFieldHitTestingKeepsGraphemesAndNearestTies(t *testing.T) {
	n := &ui.Node{Kind: ui.KindTextField, Text: "ae\u0301👩‍💻z", Masked: true,
		Padding: 10, Bounds: ui.Rect{W: 200, H: 40}}
	measure := func(s string, _ ui.TextAttrs) (int, int) {
		return utf8.RuneCountInString(s) * 10, 16
	}
	// Display positions are 0, 10, 30, 60, 70; a masked grapheme may
	// occupy multiple bullets. An equidistant click keeps the earlier edge.
	for _, tc := range []struct{ x, want int }{{-5, 0}, {5, 0}, {6, 1}, {20, 1}, {21, 4}, {45, 4}, {46, 15}, {80, 16}} {
		if got := FieldOffsetAt(n, FieldTextRect(n).X+tc.x, measure); got != tc.want {
			t.Errorf("x=%d: offset=%d, want %d", tc.x, got, tc.want)
		}
	}
}

func TestMaskedFieldHitTestingKeepsFirstRoundedEdge(t *testing.T) {
	n := &ui.Node{Kind: ui.KindTextField, Text: strings.Repeat("a", 20), Masked: true,
		Padding: 10, Bounds: ui.Rect{W: 200, H: 40}}
	measure := func(s string, _ ui.TextAttrs) (int, int) {
		return (utf8.RuneCountInString(s) + 2) / 3 * 2, 16
	}
	// Several edges share a rounded advance. The exhaustive search chose
	// the earliest one even when the closest width lies before the click.
	if got := FieldOffsetAt(n, FieldTextRect(n).X+5, measure); got != 4 {
		t.Fatalf("rounded tie picked byte %d, want the first edge at width 4 (byte 4)", got)
	}
}

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
