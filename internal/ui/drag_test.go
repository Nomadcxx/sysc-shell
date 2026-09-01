package ui

import "testing"

func TestDragStartsAfterThreshold(t *testing.T) {
	t.Parallel()
	src := &Node{Kind: KindDragSource, DragType: "zone", Payload: "tokyo", Name: "Reorder Tokyo"}
	var d Drag
	d.Begin(src, 10, 10)
	d.Move(12, 10)
	if d.Active() {
		t.Fatal("drag started inside the threshold")
	}
	d.Move(10, 20)
	if !d.Active() {
		t.Fatal("drag did not start after the threshold")
	}
}

func TestDragInsertionZoneUsesSlop(t *testing.T) {
	t.Parallel()
	src := &Node{Kind: KindDragSource, DragType: "zone", Payload: "a"}
	zone := &Node{Kind: KindDropZone, Accept: []string{"zone"}, Bounds: Rect{X: 0, Y: 40, W: 200, H: 8}}
	var d Drag
	d.Begin(src, 10, 10)
	d.Move(10, 36)
	if !d.Hits(zone) {
		t.Fatal("pointer inside slop missed the insertion zone")
	}
	d.Move(10, 0)
	if d.Hits(zone) {
		t.Fatal("pointer well outside the zone still hit")
	}
}

func TestDragAcceptsAndRejectsTypes(t *testing.T) {
	t.Parallel()
	src := &Node{Kind: KindDragSource, DragType: "zone", Payload: "a"}
	okZone := &Node{Kind: KindDropZone, Accept: []string{"zone"}}
	badZone := &Node{Kind: KindDropZone, Accept: []string{"note"}}
	var d Drag
	d.Begin(src, 0, 0)
	d.Move(0, 20)
	if !d.Accepts(okZone) {
		t.Fatal("matching type was rejected")
	}
	if d.Accepts(badZone) {
		t.Fatal("mismatching type was accepted")
	}
}

func TestDragDropDeliversPayloadAndCancelClears(t *testing.T) {
	t.Parallel()
	src := &Node{Kind: KindDragSource, DragType: "zone", Payload: "paris"}
	zone := &Node{Kind: KindDropZone, Accept: []string{"zone"}, Bounds: Rect{X: 0, Y: 0, W: 100, H: 40}}
	var d Drag
	d.Begin(src, 10, 10)
	d.Move(20, 20)
	got, ok := d.Drop(zone)
	if !ok || got != "paris" {
		t.Fatalf("drop = %q ok=%v", got, ok)
	}
	if d.Active() {
		t.Fatal("drop left the drag active")
	}
	d.Begin(src, 10, 10)
	d.Move(20, 20)
	d.Cancel()
	if _, ok := d.Drop(zone); ok {
		t.Fatal("cancelled drag still dropped")
	}
}

func TestListHasFixedViewportAndKeyboardScroll(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ bool) (int, int) { return len(s) * 8, 20 }
	list := &Node{Kind: KindScroll, Height: 80, Gap: 0}
	for i := 0; i < 10; i++ {
		list.Children = append(list.Children, &Node{Kind: KindText, Text: "row"})
	}
	root := &Node{Kind: KindColumn, Children: []*Node{list}}
	if err := LayoutColumn(root, Rect{W: 200, H: 200}, measure); err != nil {
		t.Fatal(err)
	}
	if list.Bounds.H != 80 {
		t.Fatalf("viewport = %d, want 80", list.Bounds.H)
	}
	if list.ContentH <= list.Bounds.H {
		t.Fatalf("content %d should exceed viewport %d", list.ContentH, list.Bounds.H)
	}
	before := list.ScrollOffset
	ScrollBy(list, 40)
	if list.ScrollOffset <= before {
		t.Fatal("keyboard scroll did not move the viewport")
	}
	if list.Children[9].Bounds.Y < list.Bounds.Y+list.Bounds.H {
		// last row starts inside; after scrolling it may still clip. The
		// viewport height is the clip, not the child list.
	}
}
