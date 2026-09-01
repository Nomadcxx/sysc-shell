package ui

import "testing"

func TestColumnLayoutStacksAndCentersText(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ bool) (int, int) { return len(s) * 7, 16 }
	root := &Node{Kind: KindColumn, Gap: 8, Padding: 12, Children: []*Node{
		{Kind: KindText, Text: "Power"},
		{Kind: KindSeparator},
		{Kind: KindButton, Text: "Lock", Name: "Lock", Role: "button", Focusable: true},
	}}
	if err := LayoutColumn(root, Rect{W: 300, H: 400}, measure); err != nil {
		t.Fatal(err)
	}
	if root.Bounds != (Rect{W: 300, H: 400}) {
		t.Fatalf("root bounds = %+v", root.Bounds)
	}
	fill := 300 - 24
	want := []Rect{
		{X: 12, Y: 12, W: fill, H: 16},
		{X: 12, Y: 36, W: fill, H: 1},
		{X: 12, Y: 45, W: fill, H: 16},
	}
	for i, w := range want {
		if got := root.Children[i].Bounds; got != w {
			t.Errorf("child %d bounds = %+v, want %+v", i, got, w)
		}
	}
}

func samplePanelTree() *Node {
	return &Node{Kind: KindColumn, Gap: 8, Padding: 12, Children: []*Node{
		{Kind: KindText, Text: "Power"},
		{Kind: KindSeparator},
		{Kind: KindButton, Text: "Lock", Name: "Lock", Role: "button", Focusable: true},
		{Kind: KindRow, Children: []*Node{
			{Kind: KindButton, Text: "Sleep", Name: "Sleep", Role: "button", Focusable: true},
			{Kind: KindButton, Text: "Logout", Name: "Logout", Role: "button", Focusable: true},
			{Kind: KindButton, Text: "Shutdown", Name: "Shutdown", Role: "button", Focusable: true},
		}},
	}}
}

func TestImageNodeMeasuresThroughTheColumnPath(t *testing.T) {
	measure := func(string, bool) (int, int) { return 7, 20 }
	h, err := columnChildHeight(&Node{Kind: KindImage, ImageSize: 32}, 200, measure)
	if err != nil {
		t.Fatal(err)
	}
	if h != 32 {
		t.Fatalf("column height = %d, want 32", h)
	}
	// With no declared size the node still occupies a text line, so a card
	// with a missing icon keeps its shape.
	h, err = columnChildHeight(&Node{Kind: KindImage}, 200, measure)
	if err != nil {
		t.Fatal(err)
	}
	if h != 20 {
		t.Fatalf("column height = %d, want the line height 20", h)
	}
}
