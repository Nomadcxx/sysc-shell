package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// copyNodeVisible prunes render copies to what a scroll viewport can show
// (GH #454). Positions carry meaning elsewhere -- applyEditorView maps live
// nodes to copies by index -- so a dropped child must leave its slot filled by
// a placeholder, and only free-flowing content may drop at all.

func visibleRow(y int, text string, kids ...*ui.Node) *ui.Node {
	return &ui.Node{
		Kind:     ui.KindColumn,
		Text:     text,
		Bounds:   ui.Rect{X: 0, Y: y, W: 200, H: 40},
		Children: kids,
	}
}

func TestCopyNodeVisibleReplacesDroppedRowsInSlot(t *testing.T) {
	live := &ui.Node{
		Kind:   ui.KindScroll,
		Bounds: ui.Rect{X: 0, Y: 0, W: 200, H: 100},
		Children: []*ui.Node{
			visibleRow(-80, "above", &ui.Node{Kind: ui.KindText, Bounds: ui.Rect{X: 8, Y: -70, W: 40, H: 12}}),
			visibleRow(10, "visible", &ui.Node{Kind: ui.KindText, Bounds: ui.Rect{X: 8, Y: 20, W: 40, H: 12}}),
			visibleRow(200, "below", &ui.Node{Kind: ui.KindText, Bounds: ui.Rect{X: 8, Y: 210, W: 40, H: 12}}),
		},
	}
	live.Children[1].Values = []float64{1, 2, 3}
	live.Children[1].Frames = []string{"a"}

	cp := copyNodeVisible(live, ui.Rect{}, false)
	if cp == live {
		t.Fatal("root not copied")
	}
	if len(cp.Children) != 3 {
		t.Fatalf("children: got %d slots, want 3", len(cp.Children))
	}
	above, vis, below := cp.Children[0], cp.Children[1], cp.Children[2]
	if above.Kind != ui.KindText || above.Bounds != (ui.Rect{X: 0, Y: -80, W: 200, H: 40}) {
		t.Fatalf("dropped slot: got %v %v, want a placeholder with the row's bounds", above.Kind, above.Bounds)
	}
	if len(above.Children) != 0 || len(below.Children) != 0 {
		t.Fatal("placeholder carries children")
	}
	if below.Kind != ui.KindText || below.Bounds != (ui.Rect{X: 0, Y: 200, W: 200, H: 40}) {
		t.Fatalf("far slot: got %v %v", below.Kind, below.Bounds)
	}
	if vis == live.Children[1] {
		t.Fatal("visible row aliased, not copied")
	}
	if vis.Text != "visible" || len(vis.Children) != 1 || vis.Children[0] == live.Children[1].Children[0] {
		t.Fatal("visible row not deep-copied")
	}
	if len(vis.Values) != 3 || &vis.Values[0] == &live.Children[1].Values[0] {
		t.Fatal("Values not cloned")
	}
	if len(vis.Frames) != 1 || &vis.Frames[0] == &live.Children[1].Frames[0] {
		t.Fatal("Frames not cloned")
	}
}

func TestCopyNodeVisibleKeepsOverflowSubtrees(t *testing.T) {
	// A menu's open option list floats over later rows and a scroll's thumb
	// paints beyond its own box, so subtrees containing them survive the
	// prune no matter where their box lands.
	menuRow := visibleRow(-80, "menu", &ui.Node{Kind: ui.KindMenu, Bounds: ui.Rect{X: 8, Y: -70, W: 40, H: 12}})
	scrollRow := visibleRow(200, "thumb", &ui.Node{Kind: ui.KindScroll, Bounds: ui.Rect{X: 8, Y: 210, W: 40, H: 12}})
	plainRow := visibleRow(-160, "plain")
	live := &ui.Node{
		Kind:     ui.KindScroll,
		Bounds:   ui.Rect{X: 0, Y: 0, W: 200, H: 100},
		Children: []*ui.Node{menuRow, scrollRow, plainRow},
	}
	cp := copyNodeVisible(live, ui.Rect{}, false)
	if len(cp.Children) != 3 {
		t.Fatalf("children: got %d slots, want 3", len(cp.Children))
	}
	if cp.Children[0] == menuRow || cp.Children[0].Kind != ui.KindColumn || len(cp.Children[0].Children) != 1 {
		t.Fatal("menu subtree outside the view was pruned or aliased")
	}
	if cp.Children[1] == scrollRow || cp.Children[1].Kind != ui.KindColumn || len(cp.Children[1].Children) != 1 {
		t.Fatal("scroll subtree outside the view was pruned or aliased")
	}
	if cp.Children[2].Kind != ui.KindText || len(cp.Children[2].Children) != 0 {
		t.Fatal("plain row outside the view should be a placeholder")
	}
}

func TestCopyNodeVisibleZeroSizeScrollKeepsChildren(t *testing.T) {
	live := &ui.Node{
		Kind:   ui.KindScroll,
		Bounds: ui.Rect{},
		Children: []*ui.Node{
			visibleRow(-80, "above"),
			visibleRow(200, "below"),
		},
	}
	cp := copyNodeVisible(live, ui.Rect{}, false)
	for i, child := range cp.Children {
		if child == live.Children[i] || child.Kind != ui.KindColumn || child.Text == "" {
			t.Fatalf("child %d: zero-size scroll must narrow nothing, got %v %q", i, child.Kind, child.Text)
		}
	}
}

func TestCopyNodeVisibleWithoutScrollCopiesEverything(t *testing.T) {
	live := &ui.Node{
		Kind:   ui.KindColumn,
		Bounds: ui.Rect{X: 0, Y: 0, W: 200, H: 100},
		Children: []*ui.Node{
			visibleRow(-80, "above"),
			visibleRow(200, "below"),
		},
	}
	cp := copyNodeVisible(live, ui.Rect{}, false)
	for i, child := range cp.Children {
		if child == live.Children[i] || child.Kind != ui.KindColumn {
			t.Fatalf("child %d: no scroll means no pruning, got %v", i, child.Kind)
		}
	}
}

func TestCopyNodeVisibleNestedScrollIntersectsViews(t *testing.T) {
	inner := &ui.Node{
		Kind:   ui.KindScroll,
		Bounds: ui.Rect{X: 0, Y: 50, W: 200, H: 50},
		Children: []*ui.Node{
			visibleRow(10, "outside inner"),
			visibleRow(60, "inside inner"),
		},
	}
	live := &ui.Node{
		Kind:     ui.KindScroll,
		Bounds:   ui.Rect{X: 0, Y: 0, W: 200, H: 200},
		Children: []*ui.Node{inner},
	}
	cp := copyNodeVisible(live, ui.Rect{}, false)
	got := cp.Children[0]
	if got == inner || len(got.Children) != 2 {
		t.Fatalf("inner scroll: got %d children", len(got.Children))
	}
	if got.Children[0].Kind != ui.KindText {
		t.Fatal("row visible to the outer view but not the inner one must be pruned")
	}
	if got.Children[1].Kind != ui.KindColumn || got.Children[1].Text != "inside inner" {
		t.Fatal("row inside the inner view must be kept")
	}
}
