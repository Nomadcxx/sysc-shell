package ui

import "testing"

// CenterX exists because LayoutColumn hands every child the full content width
// and a leaf then takes its natural width at the track's left edge. The SYSC
// rail is the first thing in the tree that has to sit in the middle.
func TestCenterXCentersTheChildInItsTrack(t *testing.T) {
	t.Parallel()
	child := &Node{Kind: KindText, Text: "//////SYSC//////", CenterX: true}
	root := &Node{Kind: KindColumn, Children: []*Node{child}}
	if err := LayoutColumn(root, Rect{X: 0, Y: 0, W: 200, H: 60}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	want := (200 - child.Bounds.W) / 2
	if child.Bounds.X != want {
		t.Fatalf("X = %d, want %d (track 200, child %d)", child.Bounds.X, want, child.Bounds.W)
	}
}

// Without the flag nothing moves, so every existing panel lays out unchanged.
func TestWithoutCenterXTheChildStaysLeft(t *testing.T) {
	t.Parallel()
	child := &Node{Kind: KindText, Text: "SYSC"}
	root := &Node{Kind: KindColumn, Children: []*Node{child}}
	if err := LayoutColumn(root, Rect{X: 12, Y: 0, W: 200, H: 60}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	if child.Bounds.X != 12 {
		t.Fatalf("X = %d, want the track's left edge 12", child.Bounds.X)
	}
}

// Centring is a translation applied after placement, so a subtree moves whole
// rather than being re-laid-out at a new origin.
func TestCenterXCarriesTheSubtree(t *testing.T) {
	t.Parallel()
	leaf := &Node{Kind: KindText, Text: "SYSC"}
	row := &Node{Kind: KindRow, Gap: 4, CenterX: true, Children: []*Node{
		{Kind: KindText, Text: "//////"}, leaf,
	}}
	root := &Node{Kind: KindColumn, Children: []*Node{row}}
	if err := LayoutColumn(root, Rect{X: 0, Y: 0, W: 400, H: 60}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	if row.Bounds.X <= 0 {
		t.Fatalf("row was not centred: X = %d", row.Bounds.X)
	}
	if leaf.Bounds.X < row.Bounds.X {
		t.Fatalf("child at %d is left of its centred parent at %d; the "+
			"subtree did not travel with it", leaf.Bounds.X, row.Bounds.X)
	}
}

// A child at least as wide as the track has nowhere to go. Shifting it by a
// negative amount would push it off the panel's left edge, so it must not move.
func TestCenterXLeavesAnOversizedChildAlone(t *testing.T) {
	t.Parallel()
	child := &Node{Kind: KindText, Text: "//////SYSC//////", CenterX: true}
	root := &Node{Kind: KindColumn, Children: []*Node{child}}
	if err := LayoutColumn(root, Rect{X: 5, Y: 0, W: 8, H: 60}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	if child.Bounds.X < 5 {
		t.Fatalf("X = %d, want no shift left of the track origin 5", child.Bounds.X)
	}
}

// The mark is a wide lockup, so it measures from its explicit box rather than
// the square KindIcon reserves.
func TestWordmarkMeasuresFromItsBox(t *testing.T) {
	t.Parallel()
	n := &Node{Kind: KindWordmark, ImageW: 167, ImageH: 23}
	w, h, err := measureNode(n, 40, fakeMeasure)
	if err != nil {
		t.Fatal(err)
	}
	if w != 167 || h != 23 {
		t.Fatalf("measured %dx%d, want 167x23", w, h)
	}
	if _, _, err := measureNode(&Node{Kind: KindWordmark}, 40, fakeMeasure); err == nil {
		t.Fatal("a wordmark with no box measured without error")
	}
}
