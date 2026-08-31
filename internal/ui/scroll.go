package ui

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

// VisibleRange returns [lo, hi) of virtual-list indices in view plus 2 overscan.
func VisibleRange(n *Node) (lo, hi int) {
	if n == nil || n.ItemHeight <= 0 {
		return 0, 0
	}
	view := n.Bounds.H
	if view <= 0 {
		view = 0
	}
	first := 0
	if n.ItemHeight > 0 {
		first = n.ScrollOffset / n.ItemHeight
	}
	vis := 0
	if n.ItemHeight > 0 {
		vis = (view + n.ItemHeight - 1) / n.ItemHeight
	}
	lo = first - 2
	if lo < 0 {
		lo = 0
	}
	hi = first + vis + 2
	if hi > n.ItemCount {
		hi = n.ItemCount
	}
	if lo > hi {
		lo = hi
	}
	return lo, hi
}
