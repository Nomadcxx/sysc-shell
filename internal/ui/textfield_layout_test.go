package ui

import "testing"

func fixedMeasure(s string, _ TextAttrs) (int, int) { return len(s) * 8, 16 }

func fieldRow(pinEnd bool, kids ...*Node) *Node {
	return &Node{Kind: KindRow, Gap: 8, PinEnd: pinEnd, Children: kids}
}

// A field declared 100 wide holds 100 whatever it contains: growth pushed the
// github-notifications "Mark all read" pill across the panel.
func TestTextFieldKeepsDeclaredWidthAsTextGrows(t *testing.T) {
	for _, text := range []string{"", "short", "a query far longer than the field could ever show"} {
		field := &Node{Kind: KindTextField, Text: text, Width: 100, Height: 40, Focusable: true, Name: "q", Role: "textbox"}
		root := fieldRow(false, field, &Node{Kind: KindText, Text: "tail"})
		if err := Layout(root, Rect{W: 400, H: 40}, fixedMeasure); err != nil {
			t.Fatal(err)
		}
		if field.Bounds.W != 100 {
			t.Fatalf("text %q: width %d, want 100", text, field.Bounds.W)
		}
	}
}

// With no declared width a trailing field fills what the row has left.
func TestTrailingTextFieldWithoutWidthFillsTheRow(t *testing.T) {
	field := &Node{Kind: KindTextField, Height: 40, Focusable: true, Name: "q", Role: "textbox"}
	root := fieldRow(false, &Node{Kind: KindText, Text: "Find"}, field)
	if err := Layout(root, Rect{W: 400, H: 40}, fixedMeasure); err != nil {
		t.Fatal(err)
	}
	if want := 400 - 32 - 8; field.Bounds.W != want {
		t.Fatalf("width %d, want %d", field.Bounds.W, want)
	}
}

// A leading field in a pinned row fills up to the pinned trailing control.
func TestLeadingFieldInPinnedRowFillsToThePin(t *testing.T) {
	field := &Node{Kind: KindTextField, Height: 40, Focusable: true, Name: "q", Role: "textbox"}
	pill := &Node{Kind: KindButton, Text: "Go", Action: "go", Focusable: true, Name: "Go", Role: "button"}
	root := fieldRow(true, field, pill)
	if err := Layout(root, Rect{W: 400, H: 40}, fixedMeasure); err != nil {
		t.Fatal(err)
	}
	if field.Bounds.X+field.Bounds.W+8 != pill.Bounds.X || pill.Bounds.X+pill.Bounds.W != 400 {
		t.Fatalf("field ends at %d, pill spans %d..%d; want the pill at the row end", field.Bounds.X+field.Bounds.W, pill.Bounds.X, pill.Bounds.X+pill.Bounds.W)
	}
}

// A middle field with no width takes the minimum, not the row.
func TestMiddleTextFieldWithoutWidthTakesTheMinimum(t *testing.T) {
	field := &Node{Kind: KindTextField, Height: 40, Focusable: true, Name: "q", Role: "textbox"}
	root := fieldRow(false, &Node{Kind: KindText, Text: "a"}, field, &Node{Kind: KindText, Text: "b"})
	if err := Layout(root, Rect{W: 400, H: 40}, fixedMeasure); err != nil {
		t.Fatal(err)
	}
	if field.Bounds.W != MinTextFieldWidth {
		t.Fatalf("width %d, want %d", field.Bounds.W, MinTextFieldWidth)
	}
}
