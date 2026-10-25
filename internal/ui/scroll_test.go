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

func TestVisibleRangeOffsets(t *testing.T) {
	t.Parallel()
	// Heights 20, 40, 30 with an 8px gap: starts 0, 28, 76, extent 106.
	offsets := []int{0, 20, 28, 68, 76, 106}
	offset := func(i int) int { return offsets[i] }
	cases := []struct {
		name        string
		top, bottom int
		wantLo      int
		wantHi      int
	}{
		{name: "whole extent overscans both ends", top: 0, bottom: 106, wantLo: 0, wantHi: 3},
		{name: "first item in view", top: 0, bottom: 20, wantLo: 0, wantHi: 3},
		{name: "gap between items selects neither", top: 22, bottom: 26, wantLo: 0, wantHi: 3},
		{name: "middle item", top: 30, bottom: 50, wantLo: 0, wantHi: 3},
		{name: "last item", top: 80, bottom: 106, wantLo: 1, wantHi: 3},
		{name: "past the extent", top: 106, bottom: 120, wantLo: 1, wantHi: 3},
		{name: "empty viewport", top: 40, bottom: 40, wantLo: 0, wantHi: 0},
		{name: "inverted viewport", top: 40, bottom: 10, wantLo: 0, wantHi: 0},
	}
	for _, tc := range cases {
		lo, hi := visibleRangeOffsets(offset, 3, tc.top, tc.bottom)
		if lo != tc.wantLo || hi != tc.wantHi {
			t.Errorf("%s: range %d..%d, want %d..%d", tc.name, lo, hi, tc.wantLo, tc.wantHi)
		}
	}
	if lo, hi := visibleRangeOffsets(nil, 3, 0, 10); lo != 0 || hi != 0 {
		t.Fatalf("nil offset range %d..%d, want 0..0", lo, hi)
	}
	if lo, hi := visibleRangeOffsets(offset, 0, 0, 10); lo != 0 || hi != 0 {
		t.Fatalf("empty list range %d..%d, want 0..0", lo, hi)
	}
}

func TestVirtualListItemOffsetLaysOutMixedHeights(t *testing.T) {
	t.Parallel()
	items := []*Node{
		{Kind: KindColumn, Height: 20},
		{Kind: KindColumn, Height: 40},
		{Kind: KindColumn, Height: 30},
	}
	list := &Node{
		Kind: KindVirtualList, ItemCount: len(items),
		// 8px between items, as a KindColumn with Gap 8 would report.
		ItemOffset: func(i int) int {
			starts := []int{0, 28, 76, 106}
			return starts[i]
		},
		Item: func(i int) *Node { return items[i] },
	}
	measure := func(string, TextAttrs) (int, int) { return 8, 16 }
	if err := LayoutColumn(list, Rect{W: 200, H: 60}, measure); err != nil {
		t.Fatal(err)
	}
	if list.ContentH != 106 {
		t.Fatalf("content height = %d, want 106", list.ContentH)
	}
	if len(list.Children) != 3 {
		t.Fatalf("laid out %d items, want all three overscanned", len(list.Children))
	}
	want := []Rect{
		{X: 0, Y: 0, W: 200, H: 20},
		{X: 0, Y: 28, W: 200, H: 40},
		{X: 0, Y: 76, W: 200, H: 30},
	}
	for i, item := range items {
		if item.Bounds != want[i] {
			t.Fatalf("item %d bounds = %+v, want %+v", i, item.Bounds, want[i])
		}
	}
	ScrollBy(list, 30)
	if err := LayoutColumn(list, Rect{W: 200, H: 60}, measure); err != nil {
		t.Fatal(err)
	}
	starts := []int{0, 28, 76, 106}
	for i, item := range items {
		wantY := starts[i] - 30
		if item.Bounds.Y != wantY {
			t.Fatalf("item %d after scroll y = %d, want %d", i, item.Bounds.Y, wantY)
		}
	}
}

func TestVirtualListInsideScrollUsesTheOuterClip(t *testing.T) {
	t.Parallel()
	items := []*Node{
		{Kind: KindColumn, Height: 20},
		{Kind: KindColumn, Height: 20},
	}
	list := &Node{
		Kind: KindVirtualList, ItemCount: len(items),
		ItemOffset: func(i int) int { return i * 20 },
		Item:       func(i int) *Node { return items[i] },
	}
	above := &Node{Kind: KindColumn, Height: 100}
	outer := &Node{Kind: KindScroll, Children: []*Node{above, list}}
	measure := func(string, TextAttrs) (int, int) { return 8, 16 }
	if err := LayoutColumn(outer, Rect{W: 200, H: 40}, measure); err != nil {
		t.Fatal(err)
	}
	if list.ContentH != 40 {
		t.Fatalf("nested list content height = %d, want 40", list.ContentH)
	}
	if len(list.Children) != 0 {
		t.Fatalf("a list below the viewport materialised %d items", len(list.Children))
	}
	// Scrolling the outer column brings the list into view.
	ScrollBy(outer, 90)
	if err := LayoutColumn(outer, Rect{W: 200, H: 40}, measure); err != nil {
		t.Fatal(err)
	}
	if len(list.Children) == 0 {
		t.Fatal("the list stayed unpopulated after the outer view reached it")
	}
}
