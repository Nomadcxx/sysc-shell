package ui

import "testing"

func stackOf(children ...*Node) *Node {
	return &Node{Kind: KindStack, Children: children}
}

func TestStackGivesEveryChildTheSameBox(t *testing.T) {
	t.Parallel()
	// The point of the kind: children occupy one another's space rather than
	// successive space.
	a := &Node{Kind: KindText, Text: "a"}
	b := &Node{Kind: KindText, Text: "b"}
	n := stackOf(a, b)
	n.Padding = 4
	n.Bounds = Rect{X: 10, Y: 20, W: 100, H: 50}

	if err := layoutStackChildren(n, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	want := Rect{X: 14, Y: 24, W: 92, H: 42}
	if a.Bounds != want {
		t.Errorf("first child = %+v, want %+v", a.Bounds, want)
	}
	if b.Bounds != want {
		t.Errorf("second child = %+v, want %+v", b.Bounds, want)
	}
}

func TestStackMeasuresAsTheMaximumNotTheSum(t *testing.T) {
	t.Parallel()
	// A column sums its children. A stack must not, or every stacked card
	// reserves the height of all its layers added together.
	short := &Node{Kind: KindText, Text: "x"}
	tall := &Node{Kind: KindImage, ImageSize: 40}
	n := stackOf(short, tall)

	h, err := columnChildHeight(n, 100, fakeMeasure)
	if err != nil {
		t.Fatal(err)
	}
	if h < 40 {
		t.Errorf("height = %d, want at least the tallest child's 40", h)
	}
	if h >= 40+10 {
		t.Errorf("height = %d; that looks like a sum, not a maximum", h)
	}
}

func TestStackHonoursExplicitHeight(t *testing.T) {
	t.Parallel()
	n := stackOf(&Node{Kind: KindText, Text: "x"})
	n.Height = 40

	if h, err := columnChildHeight(n, 100, fakeMeasure); err != nil {
		t.Fatal(err)
	} else if h != 40 {
		t.Fatalf("height = %d, want the explicit stack height 40", h)
	}
}

func TestStackWithNoChildrenIsHarmless(t *testing.T) {
	t.Parallel()
	// The other containers degrade rather than error on an empty child list.
	n := stackOf()
	if err := layoutStackChildren(n, fakeMeasure); err != nil {
		t.Errorf("empty stack errored: %v", err)
	}
	if _, err := columnChildHeight(n, 100, fakeMeasure); err != nil {
		t.Errorf("empty stack failed to measure: %v", err)
	}
}

func TestStackRejectsANilChild(t *testing.T) {
	t.Parallel()
	// Every other container names the index. A nil child must not panic.
	n := stackOf(nil)
	if err := layoutStackChildren(n, fakeMeasure); err == nil {
		t.Error("a nil child was accepted")
	}
}

func TestStackHitReturnsTheTopmostChild(t *testing.T) {
	t.Parallel()
	// Hit walks children in reverse, so the last child -- the one painted on
	// top -- must win.
	under := &Node{Kind: KindButton, Text: "under", Action: "under", Bounds: Rect{W: 50, H: 50}}
	over := &Node{Kind: KindButton, Text: "over", Action: "over", Bounds: Rect{W: 50, H: 50}}
	root := &Node{Kind: KindRow, Bounds: Rect{W: 50, H: 50}, Children: []*Node{
		{Kind: KindStack, Bounds: Rect{W: 50, H: 50}, Children: []*Node{under, over}},
	}}

	action, ok := Hit(root, 25, 25)
	if !ok {
		t.Fatal("no hit inside the stack")
	}
	if action != "over" {
		t.Errorf("hit = %q, want the topmost child %q", action, "over")
	}
}
