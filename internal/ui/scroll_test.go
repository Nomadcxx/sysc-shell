package ui

import "testing"

func TestScrollClampsOffset(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ TextAttrs) (int, int) {
		if s == "tall" {
			return 400, 2000
		}
		return len(s) * 7, 16
	}
	s := &Node{Kind: KindScroll, Children: []*Node{{Kind: KindText, Text: "tall"}}}
	if err := LayoutColumn(s, Rect{W: 400, H: 600}, measure); err != nil {
		t.Fatal(err)
	}
	ScrollBy(s, 5000)
	if s.ScrollOffset != 1400 {
		t.Fatalf("clamp high = %d, want 1400", s.ScrollOffset)
	}
	ScrollBy(s, -5000)
	if s.ScrollOffset != 0 {
		t.Fatalf("clamp low = %d, want 0", s.ScrollOffset)
	}
}

func TestVirtualListVisibleRange(t *testing.T) {
	t.Parallel()
	measure := func(string, TextAttrs) (int, int) { return 400, 16 }
	v := &Node{Kind: KindVirtualList, ItemCount: 500, ItemHeight: 40}
	if err := LayoutColumn(v, Rect{W: 400, H: 600}, measure); err != nil {
		t.Fatal(err)
	}
	lo, hi := VisibleRange(v)
	if lo != 0 || hi > 18 {
		t.Fatalf("range %d..%d", lo, hi)
	}
	ScrollBy(v, 4000)
	lo, hi = VisibleRange(v)
	if lo < 98 || hi < 115 || lo > 100 {
		t.Fatalf("scrolled range %d..%d", lo, hi)
	}
}

func TestVirtualListCentresAnExplicitlyShorterItemInItsPitch(t *testing.T) {
	t.Parallel()
	rows := []*Node{
		{Kind: KindRow, Height: 26},
		{Kind: KindRow, Height: 26},
	}
	v := &Node{
		Kind: KindVirtualList, ItemCount: len(rows), ItemHeight: 32,
		Item: func(i int) *Node { return rows[i] },
	}
	measure := func(string, TextAttrs) (int, int) { return 8, 16 }
	if err := LayoutColumn(v, Rect{W: 200, H: 64}, measure); err != nil {
		t.Fatal(err)
	}
	if rows[0].Bounds != (Rect{Y: 3, W: 200, H: 26}) {
		t.Fatalf("first row bounds = %+v, want a centred 26px row", rows[0].Bounds)
	}
	if rows[1].Bounds != (Rect{Y: 35, W: 200, H: 26}) {
		t.Fatalf("second row bounds = %+v, want the next 32px pitch", rows[1].Bounds)
	}
}
