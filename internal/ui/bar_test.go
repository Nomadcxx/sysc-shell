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
