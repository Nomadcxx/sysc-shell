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

func TestColumnLayoutAcceptsGraph(t *testing.T) {
	t.Parallel()
	root := &Node{Kind: KindColumn, Padding: 12, Children: []*Node{
		{Kind: KindGraph, Width: 240, Values: []float64{0.2, 0.8}},
	}}
	if err := LayoutColumn(root, Rect{W: 300, H: 400}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	// Width is the graph's width in a row, not a height. The monitor popout
	// builds a 240-wide sparkline; it must not become 240 tall in a column.
	if got := root.Children[0].Bounds.H; got != GraphHeight {
		t.Fatalf("graph height = %d, want GraphHeight %d", got, GraphHeight)
	}
	if got := root.Children[0].Bounds.W; got != 300-2*12 {
		t.Fatalf("graph width = %d, want the padded column width", got)
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

func TestColumnStacksAMeterAtItsOwnHeight(t *testing.T) {
	t.Parallel()

	// A meter in a row fills the content height; in a column there is no
	// height to fill, so it asks for the fixed one.
	root := &Node{Kind: KindColumn, Padding: 4, Gap: 2, Children: []*Node{
		{Kind: KindText, Text: "05:00"},
		{Kind: KindMeter, Value: 0.5},
		{Kind: KindText, Text: "remaining"},
	}}
	if err := LayoutColumn(root, Rect{W: 200, H: 120}, fakeMeasure); err != nil {
		t.Fatalf("LayoutColumn: %v", err)
	}
	meter := root.Children[1]
	if meter.Bounds.H != MeterHeight {
		t.Errorf("meter height = %d, want %d", meter.Bounds.H, MeterHeight)
	}
	if meter.Bounds.Y != root.Children[0].Bounds.Y+16+2 {
		t.Errorf("meter y = %d, want it stacked below the text", meter.Bounds.Y)
	}
	if below := root.Children[2].Bounds.Y; below != meter.Bounds.Y+MeterHeight+2 {
		t.Errorf("text below the meter is at %d, want it after the meter", below)
	}
}

func TestColumnRejectsAMeterOutsideItsRange(t *testing.T) {
	t.Parallel()

	root := &Node{Kind: KindColumn, Children: []*Node{{Kind: KindMeter, Value: 1.5}}}
	if err := LayoutColumn(root, Rect{W: 200, H: 120}, fakeMeasure); err == nil {
		t.Fatal("LayoutColumn accepted a meter value outside zero through one")
	}
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

// A capsule in a column is a card: a rounded fill around a stack of content.
// Measuring it already accounted for its child, but placing it did not recurse,
// so every card painted as an empty pill.
func TestColumnPlacesACapsuleChild(t *testing.T) {
	t.Parallel()
	inner := &Node{Kind: KindColumn, Gap: 4, Children: []*Node{
		{Kind: KindText, Text: "CPU"},
		{Kind: KindGraph, Values: []float64{0.1, 0.9}},
	}}
	card := &Node{Kind: KindCapsule, Padding: 8, Children: []*Node{inner}}
	root := &Node{Kind: KindColumn, Padding: 6, Children: []*Node{card}}

	if err := LayoutColumn(root, Rect{W: 300, H: 200}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	if card.Bounds.W == 0 || card.Bounds.H == 0 {
		t.Fatalf("card was not placed: %+v", card.Bounds)
	}
	if inner.Bounds.W == 0 || inner.Bounds.H == 0 {
		t.Fatalf("card content was not placed: %+v", inner.Bounds)
	}
	if inner.Bounds.X != card.Bounds.X+card.Padding || inner.Bounds.Y != card.Bounds.Y+card.Padding {
		t.Fatalf("content origin %+v is not inset by the card padding from %+v", inner.Bounds, card.Bounds)
	}
	if want := card.Bounds.W - 2*card.Padding; inner.Bounds.W != want {
		t.Fatalf("content width = %d, want %d", inner.Bounds.W, want)
	}
	for i, leaf := range inner.Children {
		if leaf.Bounds.H == 0 {
			t.Fatalf("card leaf %d was not placed: %+v", i, leaf.Bounds)
		}
	}
}
