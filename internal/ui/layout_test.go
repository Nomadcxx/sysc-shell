package ui

import "testing"

// fakeMeasure gives every glyph a width of 8 and every line a height of 16.
func fakeMeasure(s string, _ bool) (int, int) { return len(s) * 8, 16 }

// proofTree returns the fixed proof fixture: one row holding two text nodes, a
// fixed-width meter, and a button.
func proofTree() *Node {
	return &Node{
		Kind:    KindRow,
		Padding: 4,
		Gap:     8,
		Children: []*Node{
			{Kind: KindText, Text: "sysc-shell"},
			{Kind: KindText, Text: "1"},
			{Kind: KindMeter, Width: 40, Value: 0.42},
			{Kind: KindButton, Text: "toggle", Padding: 6, Action: "toggle"},
		},
	}
}

func TestLayoutArrangesRow(t *testing.T) {
	t.Parallel()

	root := proofTree()
	bounds := Rect{X: 0, Y: 0, W: 400, H: 48}
	if err := Layout(root, bounds, fakeMeasure); err != nil {
		t.Fatal(err)
	}

	if root.Bounds != bounds {
		t.Fatalf("root bounds = %+v, want %+v", root.Bounds, bounds)
	}

	// Padding opens the row once, each gap separates one pair, and every child
	// is centred in the padded content box.
	want := []Rect{
		{X: 4, Y: 16, W: 80, H: 16},
		{X: 92, Y: 16, W: 8, H: 16},
		{X: 108, Y: 4, W: 40, H: 40},
		{X: 156, Y: 10, W: 60, H: 28},
	}
	for i, w := range want {
		if got := root.Children[i].Bounds; got != w {
			t.Errorf("child %d bounds = %+v, want %+v", i, got, w)
		}
	}
}

func TestLayoutPreservesMeterWidth(t *testing.T) {
	t.Parallel()

	root := proofTree()
	if err := Layout(root, Rect{W: 400, H: 48}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	if got := root.Children[2].Bounds.W; got != 40 {
		t.Fatalf("meter width = %d, want the fixed 40", got)
	}
}

func TestLayoutSizesButtonWithHorizontalPadding(t *testing.T) {
	t.Parallel()

	root := proofTree()
	if err := Layout(root, Rect{W: 400, H: 48}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	// "toggle" measures 48 wide; the button adds its padding on both sides.
	if got := root.Children[3].Bounds.W; got != 60 {
		t.Fatalf("button width = %d, want 60", got)
	}
}

func TestLayoutRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	badMeter := proofTree()
	badMeter.Children[2].Value = 1.5

	negativeMeter := proofTree()
	negativeMeter.Children[2].Value = -0.1

	unsupported := proofTree()
	unsupported.Children[1].Kind = Kind(9)

	tests := []struct {
		name   string
		root   *Node
		bounds Rect
	}{
		{"nil root", nil, Rect{W: 400, H: 48}},
		{"non-row root", &Node{Kind: KindText, Text: "x"}, Rect{W: 400, H: 48}},
		{"negative width", proofTree(), Rect{W: -1, H: 48}},
		{"negative height", proofTree(), Rect{W: 400, H: -1}},
		{"unsupported kind", unsupported, Rect{W: 400, H: 48}},
		{"meter above one", badMeter, Rect{W: 400, H: 48}},
		{"meter below zero", negativeMeter, Rect{W: 400, H: 48}},
		{"too narrow for children", proofTree(), Rect{W: 50, H: 48}},
		{"too short for children", proofTree(), Rect{W: 400, H: 10}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := Layout(tc.root, tc.bounds, fakeMeasure); err == nil {
				t.Fatal("Layout accepted invalid input")
			}
		})
	}
}

func TestHitReturnsButtonAction(t *testing.T) {
	t.Parallel()

	root := proofTree()
	if err := Layout(root, Rect{W: 400, H: 48}, fakeMeasure); err != nil {
		t.Fatal(err)
	}

	button := root.Children[3].Bounds
	action, ok := Hit(root, button.X+button.W/2, button.Y+button.H/2)
	if !ok || action != "toggle" {
		t.Fatalf("Hit inside the button = (%q, %t), want (\"toggle\", true)", action, ok)
	}
}

func TestHitOutsideActionableNodes(t *testing.T) {
	t.Parallel()

	root := proofTree()
	if err := Layout(root, Rect{W: 400, H: 48}, fakeMeasure); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		x, y int
	}{
		{"left of the button", 10, 24},
		{"right of the row", 399, 24},
		{"above the row", 160, -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if action, ok := Hit(root, tc.x, tc.y); ok {
				t.Fatalf("Hit = (%q, true), want no action", action)
			}
		})
	}
}

// A text node with MaxWidth never measures wider than its cap, so unbounded
// user text cannot consume a whole section.
func TestTextIsClampedToItsMaxWidth(t *testing.T) {
	t.Parallel()
	// Ten pixels per rune keeps the arithmetic obvious.
	measure := func(s string, _ bool) (int, int) { return 10 * len([]rune(s)), 10 }

	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindText, Text: "aaaaaaaaaa", MaxWidth: 40},
	}}
	if err := Layout(root, Rect{X: 0, Y: 0, W: 200, H: 20}, measure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if got := root.Children[0].Bounds.W; got != 40 {
		t.Fatalf("clamped width = %d, want the 40 cap", got)
	}
}

// A cap wider than the text must not stretch it.
func TestMaxWidthDoesNotPadShortText(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ bool) (int, int) { return 10 * len([]rune(s)), 10 }

	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindText, Text: "ab", MaxWidth: 200},
	}}
	if err := Layout(root, Rect{X: 0, Y: 0, W: 200, H: 20}, measure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if got := root.Children[0].Bounds.W; got != 20 {
		t.Fatalf("width = %d, want the natural 20", got)
	}
}

// Zero means unbounded, so existing nodes are unaffected.
func TestZeroMaxWidthIsUnbounded(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ bool) (int, int) { return 10 * len([]rune(s)), 10 }

	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindText, Text: "aaaaa"},
	}}
	if err := Layout(root, Rect{X: 0, Y: 0, W: 200, H: 20}, measure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if got := root.Children[0].Bounds.W; got != 50 {
		t.Fatalf("width = %d, want the natural 50", got)
	}
}

// A short string in a floored node still occupies the floor, so a percentage
// does not reflow its section as it crosses from one digit to three.
func TestTextIsFlooredAtItsMinWidth(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ bool) (int, int) { return 10 * len([]rune(s)), 10 }

	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindText, Text: "9%", MinWidth: 40},
	}}
	if err := Layout(root, Rect{X: 0, Y: 0, W: 200, H: 20}, measure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if got := root.Children[0].Bounds.W; got != 40 {
		t.Fatalf("floored width = %d, want the 40 floor", got)
	}
}

// Text wider than the floor keeps its natural width.
func TestMinWidthDoesNotShrinkWideText(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ bool) (int, int) { return 10 * len([]rune(s)), 10 }

	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindText, Text: "100%", MinWidth: 20},
	}}
	if err := Layout(root, Rect{X: 0, Y: 0, W: 200, H: 20}, measure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if got := root.Children[0].Bounds.W; got != 40 {
		t.Fatalf("width = %d, want the natural 40", got)
	}
}

// The floor and the cap compose: the cap still wins over a wider floor.
func TestMaxWidthStillCapsAFlooredNode(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ bool) (int, int) { return 10 * len([]rune(s)), 10 }

	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindText, Text: "aaaaaaaa", MinWidth: 70, MaxWidth: 50},
	}}
	if err := Layout(root, Rect{X: 0, Y: 0, W: 200, H: 20}, measure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if got := root.Children[0].Bounds.W; got != 50 {
		t.Fatalf("width = %d, want the 50 cap to win over the 70 floor", got)
	}
}
