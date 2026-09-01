package ui

import "testing"

func TestColumnMeasuresMeterAndGraph(t *testing.T) {
	t.Parallel()
	measure := func(string, bool) (int, int) { return 7, 16 }
	h, err := columnChildHeight(&Node{Kind: KindMeter, Value: 0.5}, 200, measure)
	if err != nil {
		t.Fatalf("meter: %v", err)
	}
	if h != MeterHeight {
		t.Fatalf("meter height = %d, want MeterHeight %d", h, MeterHeight)
	}
	h, err = columnChildHeight(&Node{Kind: KindGraph, Values: []float64{0.1, 0.9}}, 200, measure)
	if err != nil {
		t.Fatalf("graph: %v", err)
	}
	// Width is the graph's width in a row, not a height. Reusing it here makes
	// the monitor popout's 240-wide sparkline 240 tall.
	if h != GraphHeight {
		t.Fatalf("graph height = %d, want GraphHeight %d", h, GraphHeight)
	}
}

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
