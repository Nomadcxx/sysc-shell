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

func TestButtonLaysOutIconAndTextContent(t *testing.T) {
	t.Parallel()
	button := &Node{
		Kind: KindButton, Height: 40, Padding: 12, Gap: 8,
		Children: []*Node{
			{Kind: KindIcon, Icon: "lock"},
			{Kind: KindText, Text: "Lock"},
		},
	}
	root := &Node{Kind: KindRow, Children: []*Node{button}}
	if err := Layout(root, Rect{W: 200, H: 40}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	if got, want := button.Bounds, (Rect{W: 84, H: 40}); got != want {
		t.Fatalf("button bounds = %+v, want %+v", got, want)
	}
	if got, want := button.Children[0].Bounds, (Rect{X: 12, Y: 10, W: 20, H: 20}); got != want {
		t.Errorf("icon bounds = %+v, want %+v", got, want)
	}
	if got, want := button.Children[1].Bounds, (Rect{X: 40, Y: 12, W: 32, H: 16}); got != want {
		t.Errorf("label bounds = %+v, want %+v", got, want)
	}
}

func TestCompactIconButtonIsSquare(t *testing.T) {
	t.Parallel()
	button := &Node{
		Kind: KindButton, Width: 32, Height: 32, Padding: 6,
		Children: []*Node{{Kind: KindIcon, Icon: "chevron_left"}},
	}
	root := &Node{Kind: KindRow, Children: []*Node{button}}
	if err := Layout(root, Rect{W: 32, H: 32}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	if button.Bounds.W != 32 || button.Bounds.H != 32 {
		t.Fatalf("compact button = %+v, want 32x32", button.Bounds)
	}
}

func TestNodeStableKeyPrefersExplicitKeyThenAction(t *testing.T) {
	t.Parallel()
	if got := (&Node{Key: "profile", Action: "activate"}).StableKey(); got != "profile" {
		t.Fatalf("explicit stable key = %q, want profile", got)
	}
	if got := (&Node{Action: "activate"}).StableKey(); got != "activate" {
		t.Fatalf("action stable key = %q, want activate", got)
	}
	if got := (*Node)(nil).StableKey(); got != "" {
		t.Fatalf("nil stable key = %q, want empty", got)
	}
}

func TestInteractionMaskKeepsIndependentStates(t *testing.T) {
	t.Parallel()
	state := StateHovered | StateSelected
	if !state.Has(StateHovered) || !state.Has(StateSelected) || state.Has(StatePressed) {
		t.Fatalf("state mask = %08b", state)
	}
}

func TestLayoutRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	badMeter := proofTree()
	badMeter.Children[2].Value = 1.5

	negativeMeter := proofTree()
	negativeMeter.Children[2].Value = -0.1

	unsupported := proofTree()
	unsupported.Children[1].Kind = Kind(99)

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

// A short string in a floored node occupies the floor sample's measured width,
// so a percentage does not reflow its section as it crosses from one digit to
// three.
func TestTextIsFlooredAtItsMinWidth(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ bool) (int, int) { return 10 * len([]rune(s)), 10 }

	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindText, Text: "9%", MinWidthText: "1000"},
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
		{Kind: KindText, Text: "100%", MinWidthText: "9"},
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
		{Kind: KindText, Text: "aaaaaaaa", MinWidthText: "aaaaaaa", MaxWidth: 50},
	}}
	if err := Layout(root, Rect{X: 0, Y: 0, W: 200, H: 20}, measure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if got := root.Children[0].Bounds.W; got != 50 {
		t.Fatalf("width = %d, want the 50 cap to win over the 70 floor", got)
	}
}

// A graph occupies its configured width and the full content height, so it
// reserves space the way a meter does rather than measuring its data.
func TestAGraphMeasuresItsConfiguredWidth(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ bool) (int, int) { return 10 * len([]rune(s)), 10 }

	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindGraph, Width: 60, Values: []float64{0.1, 0.9}},
	}}
	if err := Layout(root, Rect{X: 0, Y: 0, W: 200, H: 20}, measure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if got := root.Children[0].Bounds.W; got != 60 {
		t.Fatalf("graph width = %d, want the configured 60", got)
	}
}

// A graph with no samples still reserves its width, so a bar does not reflow
// when the first sample arrives.
func TestAnEmptyGraphStillReservesItsWidth(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ bool) (int, int) { return 10 * len([]rune(s)), 10 }

	root := &Node{Kind: KindRow, Children: []*Node{{Kind: KindGraph, Width: 60}}}
	if err := Layout(root, Rect{X: 0, Y: 0, W: 200, H: 20}, measure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if got := root.Children[0].Bounds.W; got != 60 {
		t.Fatalf("empty graph width = %d, want 60", got)
	}
}

func TestImageNodeReservesItsBoxInARow(t *testing.T) {
	measure := func(string, bool) (int, int) { return 7, 20 }
	// The node reserves its box whether or not a raster has resolved, so a
	// late decode cannot reflow the row around it.
	for name, node := range map[string]*Node{
		"resolved":   {Kind: KindImage, ImageSize: 24, Image: &Image{Width: 8, Height: 8, Stride: 32, Pix: make([]byte, 256)}},
		"unresolved": {Kind: KindImage, ImageSize: 24},
	} {
		t.Run(name, func(t *testing.T) {
			w, h, err := measureNode(node, 32, measure)
			if err != nil {
				t.Fatal(err)
			}
			if w != 24 || h != 24 {
				t.Fatalf("measured %dx%d, want 24x24", w, h)
			}
		})
	}
}

func TestImageNodeWithoutASizeFillsTheContentHeight(t *testing.T) {
	measure := func(string, bool) (int, int) { return 7, 20 }
	w, h, err := measureNode(&Node{Kind: KindImage}, 18, measure)
	if err != nil {
		t.Fatal(err)
	}
	if w != 18 || h != 18 {
		t.Fatalf("measured %dx%d, want 18x18", w, h)
	}
}

func TestLayoutArrangesTabsInARow(t *testing.T) {
	t.Parallel()
	root := &Node{Kind: KindRow, Gap: 8, Children: []*Node{
		{Kind: KindTab, Text: "CPU"},
		{Kind: KindTab, Text: "Memory"},
	}}
	if err := Layout(root, Rect{W: 400, H: 48}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	if got := root.Children[0].Bounds.W; got != 24 {
		t.Fatalf("CPU tab width = %d, want 24", got)
	}
	if got := root.Children[1].Bounds.W; got != 48 {
		t.Fatalf("Memory tab width = %d, want 48", got)
	}
}

func TestLayoutArrangesCapsuleAroundText(t *testing.T) {
	t.Parallel()
	root := &Node{Kind: KindRow, Padding: 4, Children: []*Node{{
		Kind: KindCapsule, Padding: 8,
		Children: []*Node{{Kind: KindText, Text: "11:37"}},
	}}}
	if err := Layout(root, Rect{W: 400, H: 40}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	cap := root.Children[0]
	// "11:37" is 5 glyphs * 8 = 40 wide, 16 tall; plus 16 padding.
	if cap.Bounds.W != 56 {
		t.Fatalf("capsule width = %d, want 56", cap.Bounds.W)
	}
	if cap.Bounds.H != 32 { // content band = 40 - 2*row padding
		t.Fatalf("capsule height = %d, want the content band", cap.Bounds.H)
	}
	child := cap.Children[0]
	if child.Bounds.W != 40 || child.Bounds.H != 16 {
		t.Fatalf("child bounds = %+v", child.Bounds)
	}
}

func TestCapsuleWithZeroWidthChildMeasuresZero(t *testing.T) {
	t.Parallel()
	root := &Node{Kind: KindRow, Children: []*Node{{
		Kind: KindCapsule, Padding: 8,
		Children: []*Node{{Kind: KindText, Text: ""}},
	}}}
	if err := Layout(root, Rect{W: 400, H: 40}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	if root.Children[0].Bounds.W != 0 {
		t.Fatal("empty title must not leave an empty pill")
	}
}

func TestEmptyCapsuleWithWidthIsASquareDot(t *testing.T) {
	t.Parallel()
	root := &Node{Kind: KindRow, Padding: 0, Children: []*Node{{
		Kind: KindCapsule, Width: 8,
	}}}
	if err := Layout(root, Rect{W: 400, H: 40}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	got := root.Children[0].Bounds
	if got.W != 8 || got.H != 8 {
		t.Fatalf("dot bounds = %+v, want 8x8", got)
	}
}

// A capsule with children ignores its Width and measures to its content, which
// is what a bar pill wants. A grid cell needs the opposite: an explicit width
// so two cards share a row evenly.
func TestMeasureNodeCapsuleHonoursExplicitWidth(t *testing.T) {
	card := &Node{Kind: KindCapsule, Width: 304, Padding: 10, Children: []*Node{
		{Kind: KindColumn, Children: []*Node{{Kind: KindText, Text: "cpu"}}},
	}}
	w, _, err := measureNode(card, 200, fakeMeasure)
	if err != nil {
		t.Fatalf("measureNode: %v", err)
	}
	if w != 304 {
		t.Fatalf("explicit capsule width = %d, want 304", w)
	}
}

// Without a Width a capsule still measures to its child plus padding.
func TestMeasureNodeCapsuleWithoutWidthMeasuresChild(t *testing.T) {
	pill := &Node{Kind: KindCapsule, Padding: 8, Children: []*Node{
		{Kind: KindText, Text: "abc"},
	}}
	w, _, err := measureNode(pill, 200, fakeMeasure)
	if err != nil {
		t.Fatalf("measureNode: %v", err)
	}
	if want := 3*8 + 2*8; w != want {
		t.Fatalf("measured capsule width = %d, want %d", w, want)
	}
}

// A column grants a nested row a known width. The value in a key/value pair
// can be wider than that box; clipping it keeps the surface up instead of
// tearing it down with "does not fit".
func TestLayoutClampsOverflowingChildToRemainingWidth(t *testing.T) {
	t.Parallel()
	root := &Node{
		Kind: KindRow, Gap: 6,
		Children: []*Node{
			{Kind: KindText, Text: "CPU"},
			{Kind: KindText, Text: "AMD Ryzen 7 8845HS w/ Radeon 780M Graphics"},
		},
	}
	box := Rect{W: 284, H: 20}
	if err := Layout(root, box, fakeMeasure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	value := root.Children[1]
	if right := value.Bounds.X + value.Bounds.W; right > box.W {
		t.Fatalf("value overflows: right edge %d > %d", right, box.W)
	}
	natural, _ := fakeMeasure(root.Children[1].Text, false)
	if value.Bounds.W >= natural {
		t.Fatalf("value width %d was not clamped below natural %d", value.Bounds.W, natural)
	}
}

// TestMeasureSegmentedFitsItsOwnMeasurement pins the agreement between the two
// halves of the segmented control: layoutSegmented gives every segment the same
// width, so measureSegmented has to ask for the widest segment repeated. It
// summed the natural widths instead, which under-measures as soon as the labels
// differ -- the row then failed to lay out inside the very box it requested.
func TestMeasureSegmentedFitsItsOwnMeasurement(t *testing.T) {
	t.Parallel()

	measure := func(s string, tabular bool) (int, int) { return 10 * len(s), 20 }
	segment := func(label string) *Node {
		return &Node{
			Kind: KindButton, Height: 40, Padding: 4,
			Children: []*Node{{Kind: KindText, Text: label}},
		}
	}
	// Deliberately uneven: "Performance" is far wider than "Eco".
	row := &Node{
		Kind: KindSegmented, Gap: 2, Height: 40,
		Children: []*Node{segment("Eco"), segment("Performance"), segment("Auto")},
	}

	w, h, err := measureSegmented(row, measure)
	if err != nil {
		t.Fatalf("measureSegmented: %v", err)
	}

	// The widest segment is "Performance": 110 + 2*4 padding = 118.
	if want := 118*3 + 2*2; w != want {
		t.Errorf("measured width = %d, want the widest segment repeated (%d)", w, want)
	}

	// Laying the row out at exactly its measured size must succeed.
	root := &Node{Kind: KindRow, Children: []*Node{row}}
	if err := Layout(root, Rect{W: w, H: h}, measure); err != nil {
		t.Fatalf("laying out at the measured %dx%d failed: %v", w, h, err)
	}
	for i, seg := range row.Children {
		if seg.Bounds.W < 118 {
			t.Errorf("segment %d got width %d, too narrow for the widest label", i, seg.Bounds.W)
		}
	}
}

func TestImageNodeMeasuresALandscapeBox(t *testing.T) {
	measure := func(string, bool) (int, int) { return 7, 20 }
	// A wallpaper thumbnail is the first non-square raster in the tree. Every
	// other image node is a square icon, so the square form has to keep
	// working unchanged beside it.
	cases := map[string]struct {
		node   *Node
		w, h   int
		reason string
	}{
		"landscape": {
			node: &Node{Kind: KindImage, ImageW: 210, ImageH: 96},
			w:    210, h: 96,
			reason: "an explicit width and height are the box",
		},
		"square": {
			node: &Node{Kind: KindImage, ImageSize: 40},
			w:    40, h: 40,
			reason: "the landed icon form is untouched",
		},
		"width only": {
			node: &Node{Kind: KindImage, ImageW: 210, ImageSize: 40},
			w:    40, h: 40,
			reason: "half a landscape box is not a box; fall back to the square edge",
		},
		"height only": {
			node: &Node{Kind: KindImage, ImageH: 96, ImageSize: 40},
			w:    40, h: 40,
			reason: "half a landscape box is not a box; fall back to the square edge",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w, h, err := measureNode(c.node, 32, measure)
			if err != nil {
				t.Fatal(err)
			}
			if w != c.w || h != c.h {
				t.Fatalf("measured %dx%d, want %dx%d: %s", w, h, c.w, c.h, c.reason)
			}
		})
	}
}

func TestImageNodeLandscapeRowHeightInAColumn(t *testing.T) {
	measure := func(string, bool) (int, int) { return 7, 20 }
	h, err := columnChildHeight(&Node{Kind: KindImage, ImageW: 210, ImageH: 96}, 400, measure)
	if err != nil {
		t.Fatal(err)
	}
	if h != 96 {
		t.Fatalf("column height = %d, want the raster height 96", h)
	}
}
