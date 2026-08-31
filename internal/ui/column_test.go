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
