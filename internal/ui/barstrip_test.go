package ui

import "testing"

// stripBar is a 400-wide bar with a 20-wide centre at 190..210, spacing 10,
// and five items on one side: four 40 wide and one 20 wide. Their natural run
// is 220 against a 180 budget, so the side places three at rest (140) and its
// longest whole tail that fits is the last four (170).
func stripBar(axis Axis) (content Rect, center []*Node) {
	content = Rect{W: 400, H: 20}
	if axis == Vertical {
		content = Rect{W: 20, H: 400}
	}
	return content, []*Node{text("cc")}
}

func stripItems() []*Node {
	return []*Node{text("aaaa"), text("bbbb"), text("cccc"), text("dddd"), text("ee")}
}

func TestArrangeBarReportsStrips(t *testing.T) {
	content, center := stripBar(Horizontal)
	t.Run("end overflows", func(t *testing.T) {
		over, err := ArrangeBar(content, Horizontal, nil, center, stripItems(), 10, fixed)
		if err != nil {
			t.Fatal(err)
		}
		if want := (Strip{Total: 220, Budget: 180, Rest: 140, Tail: 170}); over.RightStrip != want {
			t.Fatalf("right strip = %+v, want %+v", over.RightStrip, want)
		}
		if over.LeftStrip != (Strip{}) {
			t.Fatalf("empty left reported a strip %+v", over.LeftStrip)
		}
		if got := over.RightStrip.MaxShift(SideEnd); got != 80 {
			t.Fatalf("end max shift = %d, want 80", got)
		}
	})
	t.Run("start overflows", func(t *testing.T) {
		over, err := ArrangeBar(content, Horizontal, stripItems(), center, nil, 10, fixed)
		if err != nil {
			t.Fatal(err)
		}
		if want := (Strip{Total: 220, Budget: 180, Rest: 140, Tail: 170}); over.LeftStrip != want {
			t.Fatalf("left strip = %+v, want %+v", over.LeftStrip, want)
		}
		if got := over.LeftStrip.MaxShift(SideStart); got != 50 {
			t.Fatalf("start max shift = %d, want 50", got)
		}
	})
	t.Run("fitting section reports none", func(t *testing.T) {
		over, err := ArrangeBar(content, Horizontal, nil, center, []*Node{text("aaaa"), text("bb")}, 10, fixed)
		if err != nil {
			t.Fatal(err)
		}
		if over.RightStrip != (Strip{}) {
			t.Fatalf("fitting right reported a strip %+v", over.RightStrip)
		}
	})
	t.Run("no whole item fits the budget", func(t *testing.T) {
		// A 190-wide item cannot show in a 180 budget at any shift, so there
		// is nothing to reveal.
		over, err := ArrangeBar(content, Horizontal, nil, center, []*Node{text("aaaaaaaaaaaaaaaaaaa")}, 10, fixed)
		if err != nil {
			t.Fatal(err)
		}
		if over.Right != 1 || over.RightStrip != (Strip{}) {
			t.Fatalf("overflow = %+v, want one drop and no strip", over)
		}
	})
	t.Run("vertical", func(t *testing.T) {
		content, center := stripBar(Vertical)
		items := make([]*Node, 7)
		for i := range items {
			items[i] = text("x")
		}
		over, err := ArrangeBar(content, Vertical, nil, center, items, 10, fixed)
		if err != nil {
			t.Fatal(err)
		}
		if want := (Strip{Total: 200, Budget: 180, Rest: 170, Tail: 170}); over.RightStrip != want {
			t.Fatalf("vertical right strip = %+v, want %+v", over.RightStrip, want)
		}
	})
}

func boundsOf(items []*Node) []Rect {
	out := make([]Rect, len(items))
	for i, n := range items {
		out[i] = n.Bounds
	}
	return out
}

func TestPlaceStripShiftZeroIsRest(t *testing.T) {
	for _, axis := range []Axis{Horizontal, Vertical} {
		for _, side := range []Side{SideStart, SideEnd} {
			content, center := stripBar(axis)
			items := stripItems()
			if axis == Vertical {
				items = items[:0]
				for range 7 {
					items = append(items, text("x"))
				}
			}
			left, right := items, []*Node(nil)
			if side == SideEnd {
				left, right = nil, items
			}
			over, err := ArrangeBar(content, axis, left, center, right, 10, fixed)
			if err != nil {
				t.Fatal(err)
			}
			rest := boundsOf(items)
			strip := over.LeftStrip
			if side == SideEnd {
				strip = over.RightStrip
			}
			if _, err := PlaceStrip(items, axis, content, side, strip, 0, 10, fixed); err != nil {
				t.Fatal(err)
			}
			for i, got := range boundsOf(items) {
				if got != rest[i] {
					t.Fatalf("axis %d side %d item %d at shift 0 = %+v, want rest %+v", axis, side, i, got, rest[i])
				}
			}
		}
	}
}

func TestPlaceStripRevealedWholeItems(t *testing.T) {
	content, center := stripBar(Horizontal)
	t.Run("end", func(t *testing.T) {
		items := stripItems()
		over, err := ArrangeBar(content, Horizontal, nil, center, items, 10, fixed)
		if err != nil {
			t.Fatal(err)
		}
		window, err := PlaceStrip(items, Horizontal, content, SideEnd, over.RightStrip, 80, 10, fixed)
		if err != nil {
			t.Fatal(err)
		}
		if want := (Rect{X: 230, W: 170, H: 20}); window != want {
			t.Fatalf("window = %+v, want %+v", window, want)
		}
		if items[0].Bounds != (Rect{}) {
			t.Fatalf("head item kept bounds %+v outside the window", items[0].Bounds)
		}
		wantX := []int{230, 280, 330, 380}
		for i, x := range wantX {
			if b := items[i+1].Bounds; b.X != x || b.W == 0 {
				t.Fatalf("tail item %d = %+v, want x %d", i+1, b, x)
			}
		}
	})
	t.Run("start", func(t *testing.T) {
		items := stripItems()
		over, err := ArrangeBar(content, Horizontal, items, center, nil, 10, fixed)
		if err != nil {
			t.Fatal(err)
		}
		window, err := PlaceStrip(items, Horizontal, content, SideStart, over.LeftStrip, 50, 10, fixed)
		if err != nil {
			t.Fatal(err)
		}
		if want := (Rect{X: 0, W: 170, H: 20}); window != want {
			t.Fatalf("window = %+v, want %+v", window, want)
		}
		if items[0].Bounds != (Rect{}) {
			t.Fatalf("head item kept bounds %+v off the start edge", items[0].Bounds)
		}
		if b := items[1].Bounds; b.X != 0 {
			t.Fatalf("first tail item = %+v, want flush to the start", b)
		}
		if b := items[4].Bounds; b.X != 150 || b.W != 20 {
			t.Fatalf("last item = %+v, want 150..170", b)
		}
	})
}

func TestPlaceStripLeanKeepsWindow(t *testing.T) {
	content, center := stripBar(Horizontal)
	items := stripItems()
	over, err := ArrangeBar(content, Horizontal, nil, center, items, 10, fixed)
	if err != nil {
		t.Fatal(err)
	}
	window, err := PlaceStrip(items, Horizontal, content, SideEnd, over.RightStrip, 12, 10, fixed)
	if err != nil {
		t.Fatal(err)
	}
	// e = (12/80)^2, so the window is still about the whole budget.
	if window.X != 220 || window.X+window.W != 400 {
		t.Fatalf("lean window = %+v, want 220..400", window)
	}
	if b := items[0].Bounds; b.X != 248 || b.W != 40 {
		t.Fatalf("head leaned to %+v, want x 248", b)
	}
	if b := items[3].Bounds; b.X != 398 || b.W != 40 {
		t.Fatalf("item straddling the edge = %+v, want its bounds kept at 398", b)
	}
	if items[4].Bounds != (Rect{}) {
		t.Fatalf("item wholly past the edge kept %+v", items[4].Bounds)
	}
}
