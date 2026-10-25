package ui

import "sort"

func ScrollBy(n *Node, delta int) {
	if n == nil {
		return
	}
	n.ScrollOffset += delta
	clampScroll(n)
}

func clampScroll(n *Node) {
	if n.ScrollOffset < 0 {
		n.ScrollOffset = 0
	}
	maxOff := n.ContentH - n.Bounds.H
	if n.Padding > 0 {
		maxOff = n.ContentH - (n.Bounds.H - 2*n.Padding)
	}
	if maxOff < 0 {
		maxOff = 0
	}
	if n.ScrollOffset > maxOff {
		n.ScrollOffset = maxOff
	}
}

const (
	scrollBarWidth = 4
	scrollHitWidth = 8
)

// ScrollBar is the logical-pixel strip the scrollbar paints in, on the right
// of an overflowing scroll view. Painting and pointer hit-testing both derive
// from it, so the bar the user sees is the bar the user can drag.
func ScrollBar(n *Node) Rect {
	if n == nil || n.HideScrollbar {
		return Rect{}
	}
	inner := n.Bounds.H - 2*n.Padding
	if inner <= 0 || n.ContentH <= inner {
		return Rect{}
	}
	pad := max(n.Padding, 2)
	x := n.Bounds.X + n.Bounds.W - pad - scrollBarWidth
	if n.ScrollbarGap > 0 {
		x = n.Bounds.X + n.Bounds.W + n.ScrollbarGap
	}
	ends := pad
	if n.ScrollbarEnds > 0 {
		ends = n.ScrollbarEnds
	}
	return Rect{X: x, Y: n.Bounds.Y + ends, W: scrollBarWidth, H: n.Bounds.H - 2*ends}
}

// ScrollTrack is the pointer strip for the scrollbar: the painted bar widened
// leftward to a comfortable hit width.
func ScrollTrack(n *Node) Rect {
	bar := ScrollBar(n)
	if bar.W == 0 || bar.H <= 0 {
		return Rect{}
	}
	return Rect{X: bar.X + bar.W - scrollHitWidth, Y: bar.Y, W: scrollHitWidth, H: bar.H}
}

// ScrollSetFromY maps a pointer y on the track to ScrollOffset.
func ScrollSetFromY(n *Node, y int) {
	track := ScrollTrack(n)
	if track.H <= 0 {
		return
	}
	inner := n.Bounds.H - 2*n.Padding
	maxOff := n.ContentH - inner
	if maxOff < 0 {
		maxOff = 0
	}
	thumbH := track.H * inner / n.ContentH
	if thumbH < 16 {
		thumbH = 16
	}
	if thumbH > track.H {
		thumbH = track.H
	}
	span := track.H - thumbH
	if span <= 0 {
		n.ScrollOffset = 0
		clampScroll(n)
		return
	}
	frac := float64(y-track.Y-thumbH/2) / float64(span)
	n.ScrollOffset = int(frac * float64(maxOff))
	clampScroll(n)
}

// VisibleRange returns [lo, hi) of virtual-list indices in view plus 2 overscan.
func VisibleRange(n *Node) (lo, hi int) {
	if n == nil || n.ItemHeight <= 0 {
		return 0, 0
	}
	return visibleRangeUniform(n.ItemHeight, n.ItemCount, n.ScrollOffset, max(n.Bounds.H, 0))
}

// visibleRangeUniform returns [lo, hi) for a fixed ItemHeight list. top is the
// viewport's first visible pixel in content space; view is its height.
func visibleRangeUniform(itemHeight, count, top, view int) (lo, hi int) {
	if itemHeight <= 0 || count <= 0 {
		return 0, 0
	}
	first := top / itemHeight
	vis := 0
	if view > 0 {
		vis = (view + itemHeight - 1) / itemHeight
	}
	if first < 0 {
		// The viewport begins above the list: part of the first screen is
		// empty, so fewer rows are in view.
		vis += first
		first = 0
		if vis < 1 {
			vis = 1
		}
	}
	lo = max(first-2, 0)
	hi = min(first+vis+2, count)
	if lo > hi {
		lo = hi
	}
	return lo, hi
}

// visibleRangeOffsets returns [lo, hi) for a mixed-height list whose item i
// starts at offset(i). top and bottom are in content space. Two items of
// overscan each side cover rows that scroll in without another layout pass.
func visibleRangeOffsets(offset func(int) int, count, top, bottom int) (lo, hi int) {
	if offset == nil || count <= 0 || bottom <= top {
		return 0, 0
	}
	// The first item whose end lies below the viewport. A zero-height item
	// ends where it starts, so it is skipped along with the empty space it
	// occupies.
	lo = sort.Search(count, func(i int) bool { return offset(i+1) > top })
	hi = sort.Search(count, func(i int) bool { return offset(i) >= bottom })
	lo = max(lo-2, 0)
	hi = min(hi+2, count)
	if lo > hi {
		lo = hi
	}
	return lo, hi
}
