package ui

import "testing"

func TestColumnMeasuresMeterAndGraph(t *testing.T) {
	t.Parallel()
	measure := func(string, TextAttrs) (int, int) { return 7, 16 }
	h, err := columnChildHeight(&Node{Kind: KindMeter, Value: 0.5}, 200, measure)
	if err != nil {
		t.Fatalf("meter: %v", err)
	}
	if h != MeterHeight {
		t.Fatalf("meter height = %d, want MeterHeight %d", h, MeterHeight)
	}
	h, err = columnChildHeight(&Node{Kind: KindMeter, Value: 0.5, Height: 3}, 200, measure)
	if err != nil {
		t.Fatalf("short meter: %v", err)
	}
	if h != 3 {
		t.Fatalf("meter Height 3 = %d, want 3", h)
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
	measure := func(s string, _ TextAttrs) (int, int) { return len(s) * 7, 16 }
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

func TestColumnGraphKeepsItsOwnHeight(t *testing.T) {
	h, err := columnChildHeight(&Node{Kind: KindGraph, Width: 240, Height: 28}, 400, fakeMeasure)
	if err != nil || h != 28 {
		t.Fatalf("graph height = %d, %v; want 28", h, err)
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

func TestSegmentedControlSharesTheSessionWidth(t *testing.T) {
	t.Parallel()
	segments := &Node{Kind: KindSegmented, Height: 40, Gap: 4, Children: []*Node{
		{Kind: KindButton, Text: "Performance", Padding: 8},
		{Kind: KindButton, Text: "Balanced", Padding: 8, State: StateSelected},
		{Kind: KindButton, Text: "Power saver", Padding: 8},
	}}
	root := &Node{Kind: KindColumn, Padding: 12, Children: []*Node{segments}}
	if err := LayoutColumn(root, Rect{W: 420, H: 80}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	if got := segments.Bounds; got.W != 396 || got.H != 40 {
		t.Fatalf("segmented bounds = %+v, want width 396 height 40", got)
	}
	for i, segment := range segments.Children {
		if segment.Bounds.W < 129 || segment.Bounds.W > 130 {
			t.Errorf("segment %d width = %d, want equal 129/130 allocation", i, segment.Bounds.W)
		}
		textW, _ := fakeMeasure(segment.Text, TextAttrs{})
		if textW+2*segment.Padding > segment.Bounds.W {
			t.Errorf("segment %d label %q clips in %+v", i, segment.Text, segment.Bounds)
		}
	}
}

func TestSegmentedControlRejectsMultipleSelections(t *testing.T) {
	t.Parallel()
	segments := &Node{Kind: KindSegmented, Height: 40, Children: []*Node{
		{Kind: KindButton, Text: "One", State: StateSelected},
		{Kind: KindButton, Text: "Two", State: StateSelected},
	}}
	root := &Node{Kind: KindColumn, Children: []*Node{segments}}
	if err := LayoutColumn(root, Rect{W: 200, H: 40}, fakeMeasure); err == nil {
		t.Fatal("LayoutColumn accepted two selected segments")
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
	measure := func(string, TextAttrs) (int, int) { return 7, 20 }
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

// A capsule inside a row inside a column must measure to its child plus
// padding. The row case used to offer measureNode a sentinel band, and every
// kind that fills the band offered reported that sentinel as its height.
func TestColumnChildHeightCapsuleInRow(t *testing.T) {
	row := &Node{Kind: KindRow, Gap: 6, Children: []*Node{
		{Kind: KindCapsule, Padding: 10, Children: []*Node{
			{Kind: KindText, Text: "cpu"},
		}},
		{Kind: KindCapsule, Padding: 10, Children: []*Node{
			{Kind: KindText, Text: "memory"},
		}},
	}}
	h, err := columnChildHeight(row, 400, fakeMeasure)
	if err != nil {
		t.Fatalf("columnChildHeight: %v", err)
	}
	_, text := fakeMeasure("cpu", TextAttrs{})
	want := text + 2*10
	if h != want {
		t.Fatalf("capsule row height = %d, want %d", h, want)
	}
}

// The same sentinel poisoned every other band-filling kind nested in a row.
func TestColumnChildHeightBandKindsInRow(t *testing.T) {
	for _, tc := range []struct {
		name string
		node *Node
		want int
	}{
		{"meter", &Node{Kind: KindMeter, Value: 0.5}, MeterHeight},
		{"graph", &Node{Kind: KindGraph, Width: 240}, GraphHeight},
		{"graph with a height", &Node{Kind: KindGraph, Width: 240, Height: 28}, 28},
		{"separator", &Node{Kind: KindSeparator}, 1},
		{"column", &Node{Kind: KindColumn, Children: []*Node{{Kind: KindMeter, Value: 0.5}}}, MeterHeight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := &Node{Kind: KindRow, Children: []*Node{tc.node}}
			h, err := columnChildHeight(row, 400, fakeMeasure)
			if err != nil {
				t.Fatalf("columnChildHeight: %v", err)
			}
			if h != tc.want {
				t.Fatalf("%s in row = %d, want %d", tc.name, h, tc.want)
			}
		})
	}
}

// A key/value row in a column of known width parks the value on the trailing
// edge. Packed left, "Arch Linux" sits against "OS" and the card looks empty
// on the right.
func TestColumnRowPinsATextValueToTheTrailingEdge(t *testing.T) {
	t.Parallel()
	root := &Node{Kind: KindColumn, Children: []*Node{
		{Kind: KindRow, Gap: 6, Children: []*Node{
			{Kind: KindText, Text: "OS"},
			{Kind: KindText, Text: "Arch Linux"},
		}},
	}}
	if err := LayoutColumn(root, Rect{W: 284, H: 40}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	value := root.Children[0].Children[1]
	if right := value.Bounds.X + value.Bounds.W; right != 284 {
		t.Fatalf("value right edge = %d, want 284", right)
	}
}

// A label/value row pins its second member to the right edge. When that member
// owns children -- a close button's icon, a Restore button's label -- the whole
// subtree has to travel with it. Pinning only the parent rect left the glyph
// behind at the flow position and painted an empty pill at the edge.
func TestPinRowEndCarriesTheSubtree(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ TextAttrs) (int, int) { return len(s) * 7, 16 }
	row := &Node{Kind: KindRow, Gap: 10, Height: 40, Children: []*Node{
		{Kind: KindText, Text: "Wallpaper"},
		{
			Kind: KindButton, Action: "close", Name: "Close", Padding: 8, Height: 40,
			Children: []*Node{{Kind: KindIcon, Icon: "close", IconSize: 18}},
		},
	}}
	box := Rect{X: 0, Y: 0, W: 600, H: 40}
	if err := placeColumnChild(row, box, measure); err != nil {
		t.Fatalf("place: %v", err)
	}

	button := row.Children[1]
	if got, want := button.Bounds.X+button.Bounds.W, box.X+box.W; got != want {
		t.Fatalf("button right edge = %d, want %d", got, want)
	}
	icon := button.Children[0]
	if icon.Bounds.X < button.Bounds.X || icon.Bounds.X+icon.Bounds.W > button.Bounds.X+button.Bounds.W {
		t.Fatalf("icon at %d..%d is outside its button %d..%d",
			icon.Bounds.X, icon.Bounds.X+icon.Bounds.W,
			button.Bounds.X, button.Bounds.X+button.Bounds.W)
	}
}

func TestPinEndPinsPastANonTextFirstChild(t *testing.T) {
	trailing := &Node{Kind: KindButton, Width: 20, Height: 20}
	row := &Node{Kind: KindRow, PinEnd: true, Height: 40, Children: []*Node{
		{Kind: KindColumn, Width: 80, Children: []*Node{{Kind: KindText, Text: "app"}}},
		trailing,
	}}
	if err := placeColumnChild(row, Rect{W: 200, H: 40}, fakeMeasure); err != nil {
		t.Fatal(err)
	}

	if got := trailing.Bounds.X + trailing.Bounds.W; got != 200 {
		t.Fatalf("right edge = %d, want 200", got)
	}
}

func TestRowWithoutPinEndIsUnchanged(t *testing.T) {
	trailing := &Node{Kind: KindButton, Width: 20, Height: 20}
	row := &Node{Kind: KindRow, Height: 40, Children: []*Node{
		{Kind: KindColumn, Width: 80, Children: []*Node{{Kind: KindText, Text: "app"}}},
		trailing,
	}}
	if err := placeColumnChild(row, Rect{W: 200, H: 40}, fakeMeasure); err != nil {
		t.Fatal(err)
	}

	if got := trailing.Bounds.X + trailing.Bounds.W; got == 200 {
		t.Fatal("row pinned without opting in")
	}
}

func TestNestedRowPinsControlWithoutOverlappingContent(t *testing.T) {
	for i, kind := range []Kind{KindRow, KindColumn} {
		t.Run([]string{"row", "column"}[i], func(t *testing.T) {
			label := &Node{Kind: KindText, Text: "a label wider than the available space"}
			content := &Node{Kind: kind, Children: []*Node{label}}
			button := &Node{Kind: KindButton, Padding: 6, Action: "close", Children: []*Node{{Kind: KindIcon, Icon: "close"}}}
			row := &Node{Kind: KindRow, PinEnd: true, Gap: 6, Children: []*Node{content, button}}
			root := &Node{Kind: KindColumn, Padding: 4, Children: []*Node{row}}
			if err := LayoutColumn(root, Rect{X: 10, Y: 20, W: 200, H: 48}, fakeMeasure); err != nil {
				t.Fatal(err)
			}
			if label.Bounds.W <= 0 || label.Bounds.X+label.Bounds.W+row.Gap > button.Bounds.X {
				t.Errorf("label %+v overlaps or has no bounds beside %+v", label.Bounds, button.Bounds)
			}
			if button.Bounds.X+button.Bounds.W != 206 {
				t.Errorf("button %+v is not pinned to inner right edge 206", button.Bounds)
			}
			b := button.Children[0].Bounds
			if got, _ := Hit(root, b.X+b.W/2, b.Y+b.H/2); got != "close" {
				t.Errorf("icon hit = %q, want close", got)
			}
		})
	}
}
