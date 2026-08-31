package ui

import "testing"

// fixed measures each rune as ten logical pixels wide and twenty tall.
func fixed(s string, _ bool) (int, int) { return len([]rune(s)) * 10, 20 }

func text(s string) *Node { return &Node{Kind: KindText, Text: s} }

func TestCenterIsAbsolutelyCentredRegardlessOfSideWidths(t *testing.T) {
	t.Parallel()
	content := Rect{X: 0, Y: 0, W: 1000, H: 40}
	center := text("mid") // 30 wide

	if err := ArrangeBar(content, []*Node{text("a")}, []*Node{center}, []*Node{text("z")}, 6, fixed); err != nil {
		t.Fatalf("ArrangeBar: %v", err)
	}
	withNarrowLeft := center.Bounds.X

	if err := ArrangeBar(content, []*Node{text("aaaaaaaaaaaaaaa")}, []*Node{center}, []*Node{text("z")}, 6, fixed); err != nil {
		t.Fatalf("ArrangeBar: %v", err)
	}
	if center.Bounds.X != withNarrowLeft {
		t.Fatalf("centre moved from %d to %d when the left width changed",
			withNarrowLeft, center.Bounds.X)
	}
	if want := (1000 - 30) / 2; center.Bounds.X != want {
		t.Fatalf("centre X = %d, want %d", center.Bounds.X, want)
	}
}

func TestSidesTruncateBeforeTheCentre(t *testing.T) {
	t.Parallel()
	content := Rect{X: 0, Y: 0, W: 120, H: 40}
	left := text("aaaaaaaaaa")  // 100 natural
	center := text("mid")       // 30 natural
	right := text("zzzzzzzzzz") // 100 natural

	if err := ArrangeBar(content, []*Node{left}, []*Node{center}, []*Node{right}, 6, fixed); err != nil {
		t.Fatalf("ArrangeBar: %v", err)
	}
	if center.Bounds.W != 30 {
		t.Fatalf("centre width = %d, want its natural 30", center.Bounds.W)
	}
	if left.Bounds.W >= 100 {
		t.Fatalf("left width = %d, want it truncated below 100", left.Bounds.W)
	}
	if left.Bounds.X+left.Bounds.W > center.Bounds.X {
		t.Fatal("left overlaps the centre")
	}
	if right.Bounds.X < center.Bounds.X+center.Bounds.W {
		t.Fatal("right overlaps the centre")
	}
}

func TestCentreWiderThanContentTruncatesAndClearsTheSides(t *testing.T) {
	t.Parallel()
	content := Rect{X: 0, Y: 0, W: 50, H: 40}
	left, center, right := text("aaa"), text("mmmmmmmmmm"), text("zzz")

	if err := ArrangeBar(content, []*Node{left}, []*Node{center}, []*Node{right}, 6, fixed); err != nil {
		t.Fatalf("ArrangeBar: %v", err)
	}
	if center.Bounds.X != 0 || center.Bounds.W != 50 {
		t.Fatalf("centre = %+v, want the full content band", center.Bounds)
	}
	if left.Bounds.W != 0 || right.Bounds.W != 0 {
		t.Fatalf("sides = %d/%d, want zero width", left.Bounds.W, right.Bounds.W)
	}
}

func TestEmptySectionsContributeNothing(t *testing.T) {
	t.Parallel()
	content := Rect{X: 0, Y: 0, W: 300, H: 40}
	center := text("mid")
	if err := ArrangeBar(content, nil, []*Node{center}, nil, 6, fixed); err != nil {
		t.Fatalf("ArrangeBar: %v", err)
	}
	if want := (300 - 30) / 2; center.Bounds.X != want {
		t.Fatalf("centre X = %d, want %d", center.Bounds.X, want)
	}
}

func TestNegativeAvailableWidthYieldsZeroNotNegativeBounds(t *testing.T) {
	t.Parallel()
	content := Rect{X: 0, Y: 0, W: 40, H: 40}
	left, center := text("aaaa"), text("mmm")
	if err := ArrangeBar(content, []*Node{left}, []*Node{center}, nil, 6, fixed); err != nil {
		t.Fatalf("ArrangeBar: %v", err)
	}
	if left.Bounds.W < 0 || left.Bounds.H < 0 {
		t.Fatalf("left bounds = %+v, want no negative dimension", left.Bounds)
	}
}

func TestItemsWithinASectionAreSpaced(t *testing.T) {
	t.Parallel()
	content := Rect{X: 0, Y: 0, W: 1000, H: 40}
	one, two := text("ab"), text("cd") // 20 each
	if err := ArrangeBar(content, []*Node{one, two}, nil, nil, 6, fixed); err != nil {
		t.Fatalf("ArrangeBar: %v", err)
	}
	if two.Bounds.X != one.Bounds.X+one.Bounds.W+6 {
		t.Fatalf("second item X = %d, want %d", two.Bounds.X, one.Bounds.X+one.Bounds.W+6)
	}
}

func TestRightSectionEndsAtTheContentEdge(t *testing.T) {
	t.Parallel()
	content := Rect{X: 10, Y: 0, W: 500, H: 40}
	right := text("zz") // 20 wide
	if err := ArrangeBar(content, nil, nil, []*Node{right}, 6, fixed); err != nil {
		t.Fatalf("ArrangeBar: %v", err)
	}
	if got := right.Bounds.X + right.Bounds.W; got != content.X+content.W {
		t.Fatalf("right section ends at %d, want the content edge %d", got, content.X+content.W)
	}
}

func TestArrangeBarRejectsNegativeContent(t *testing.T) {
	t.Parallel()
	if err := ArrangeBar(Rect{W: -1, H: 40}, nil, nil, nil, 6, fixed); err == nil {
		t.Fatal("ArrangeBar accepted negative content bounds")
	}
}

func TestHitDescendsIntoNestedSections(t *testing.T) {
	t.Parallel()
	leaf := &Node{Kind: KindButton, Text: "Toggle", Action: "toggle",
		Bounds: Rect{X: 10, Y: 10, W: 20, H: 20}}
	group := &Node{Kind: KindRow, Bounds: Rect{X: 0, Y: 0, W: 100, H: 40},
		Children: []*Node{leaf}}

	action, ok := Hit(group, 15, 15)
	if !ok || action != "toggle" {
		t.Fatalf("Hit = %q/%v, want toggle/true", action, ok)
	}
	if _, ok := Hit(group, 5, 5); ok {
		t.Fatal("a point outside every actionable child reported a hit")
	}
}

// A bar section places items directly rather than through Layout, so it must
// arrange a capsule's contents too. Without this the bar paints empty pills.
func TestArrangeBarLaysOutCapsuleContents(t *testing.T) {
	t.Parallel()
	label := &Node{Kind: KindText, Text: "11:37"}
	clock := &Node{Kind: KindCapsule, Padding: 8, Children: []*Node{label}}

	pill := &Node{Kind: KindCapsule, Fill: FillAccent,
		Children: []*Node{{Kind: KindText, Text: "1"}}}
	row := &Node{Kind: KindRow, Gap: 8, Children: []*Node{pill}}
	workspace := &Node{Kind: KindCapsule, Padding: 8, Children: []*Node{row}}

	content := Rect{X: 6, Y: 6, W: 600, H: 36}
	if err := ArrangeBar(content, []*Node{workspace}, []*Node{clock}, nil, 4, fakeMeasure); err != nil {
		t.Fatal(err)
	}

	if clock.Bounds.W == 0 {
		t.Fatal("the clock capsule was not placed")
	}
	if label.Bounds.W == 0 || label.Bounds.H == 0 {
		t.Fatalf("capsule label bounds = %+v; contents never laid out", label.Bounds)
	}
	if label.Bounds.X < clock.Bounds.X || label.Bounds.X >= clock.Bounds.X+clock.Bounds.W {
		t.Fatalf("label at %+v sits outside its capsule %+v", label.Bounds, clock.Bounds)
	}
	if pill.Bounds.W == 0 {
		t.Fatalf("workspace pill bounds = %+v; the nested row never arranged", pill.Bounds)
	}
	if pill.Children[0].Bounds.W == 0 {
		t.Fatal("the pill numeral was never laid out")
	}
}

// A capsule may lay its row out in a box taller than its own inner band, so
// that text measured at the physical size is not cropped. Members must still
// centre on the capsule, not on that taller box.
func TestCapsuleCentresAMemberTallerThanItsBand(t *testing.T) {
	t.Parallel()
	// fakeMeasure reports height 16; an 8-padded capsule in a 28-high band
	// leaves an inner band of 12, which is shorter than the member.
	member := &Node{Kind: KindText, Text: "12%"}
	row := &Node{Kind: KindRow, Children: []*Node{member}}
	group := &Node{Kind: KindCapsule, Padding: 8, Children: []*Node{row}}

	content := Rect{X: 0, Y: 0, W: 400, H: 28}
	if err := ArrangeBar(content, []*Node{group}, nil, nil, 4, fakeMeasure); err != nil {
		t.Fatal(err)
	}

	capCentre := group.Bounds.Y + group.Bounds.H/2
	memCentre := member.Bounds.Y + member.Bounds.H/2
	if diff := capCentre - memCentre; diff > 1 || diff < -1 {
		t.Fatalf("member centre %d vs capsule centre %d; member bounds %+v, capsule %+v",
			memCentre, capCentre, member.Bounds, group.Bounds)
	}
}
