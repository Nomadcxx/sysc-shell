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

func TestHiddenScrollbarExposesNoPointerTrack(t *testing.T) {
	t.Parallel()
	n := &Node{
		Kind: KindVirtualList, HideScrollbar: true,
		Bounds: Rect{W: 400, H: 200}, ContentH: 800,
	}
	if got := ScrollTrack(n); got != (Rect{}) {
		t.Fatalf("hidden scrollbar track = %+v, want none", got)
	}
	ScrollBy(n, 40)
	if n.ScrollOffset != 40 {
		t.Fatalf("hidden scrollbar disabled scrolling: offset = %d, want 40", n.ScrollOffset)
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

func TestScrollBarPlacement(t *testing.T) {
	t.Parallel()
	base := Node{Kind: KindVirtualList, Bounds: Rect{X: 10, Y: 20, W: 400, H: 200}, ContentH: 800}

	inside := base
	if got, want := ScrollBar(&inside), (Rect{X: 404, Y: 22, W: 4, H: 196}); got != want {
		t.Fatalf("default bar = %+v, want %+v against the inside of the right edge", got, want)
	}
	if got, want := ScrollTrack(&inside), (Rect{X: 400, Y: 22, W: 8, H: 196}); got != want {
		t.Fatalf("default track = %+v, want %+v", got, want)
	}

	gutter := base
	gutter.ScrollbarGap, gutter.ScrollbarEnds = 4, 6
	if got, want := ScrollBar(&gutter), (Rect{X: 414, Y: 26, W: 4, H: 188}); got != want {
		t.Fatalf("gutter bar = %+v, want %+v beyond the right edge", got, want)
	}
	if got := ScrollTrack(&gutter); got.X+got.W != 418 || got.Y != 26 || got.H != 188 {
		t.Fatalf("gutter track = %+v, want it to share the bar's right edge and span", got)
	}
}

// A menu's separators are thinner than its rows, so a virtual list can take
// each item's own extent and still place, cull and reveal by index.
func TestVirtualListItemExtents(t *testing.T) {
	t.Parallel()
	const n = 40
	extent := func(i int) int {
		if i%5 == 2 {
			return 17
		}
		return 48
	}
	top := func(i int) int {
		y := 0
		for j := 0; j < i; j++ {
			y += extent(j)
		}
		return y
	}
	rows := make([]*Node, n)
	for i := range rows {
		rows[i] = &Node{Kind: KindRow}
	}
	v := &Node{
		Kind: KindVirtualList, ItemCount: n, ItemHeight: 48, ItemExtent: extent,
		Item: func(i int) *Node { return rows[i] },
	}
	measure := func(string, TextAttrs) (int, int) { return 8, 16 }
	if err := LayoutColumn(v, Rect{W: 200, H: 300}, measure); err != nil {
		t.Fatal(err)
	}
	if v.ContentH != top(n) {
		t.Fatalf("content = %d, want %d", v.ContentH, top(n))
	}
	for _, i := range []int{1, 2, 3} {
		if want := (Rect{Y: top(i), W: 200, H: extent(i)}); rows[i].Bounds != want {
			t.Fatalf("row %d = %+v, want %+v", i, rows[i].Bounds, want)
		}
	}
	ScrollBy(v, top(23)+10)
	if err := LayoutColumn(v, Rect{W: 200, H: 300}, measure); err != nil {
		t.Fatal(err)
	}
	lo, hi := VisibleRange(v)
	// Rows 23 through 30 are in view; two rows of overscan either side.
	if lo != 21 || hi != 33 {
		t.Fatalf("scrolled range %d..%d, want 21..33", lo, hi)
	}
	if want := (Rect{Y: -10, W: 200, H: extent(23)}); rows[23].Bounds != want {
		t.Fatalf("row 23 = %+v, want %+v", rows[23].Bounds, want)
	}
	if y, h := ItemSpan(v, 22); y != top(22) || h != extent(22) {
		t.Fatalf("span 22 = %d,%d, want %d,%d", y, h, top(22), extent(22))
	}
	if y, h := ItemSpan(&Node{Kind: KindVirtualList, ItemCount: 3, ItemHeight: 40}, 2); y != 80 || h != 40 {
		t.Fatalf("uniform span = %d,%d, want 80,40", y, h)
	}
}
