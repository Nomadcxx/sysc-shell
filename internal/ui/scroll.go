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

const scrollHitWidth = 8

// ScrollTrack is the logical-pixel strip on the right of an overflowing scroll view.
func ScrollTrack(n *Node) Rect {
	if n == nil {
		return Rect{}
	}
	inner := n.Bounds.H - 2*n.Padding
	if inner <= 0 || n.ContentH <= inner {
		return Rect{}
	}
	pad := n.Padding
	if pad < 2 {
		pad = 2
	}
	w := scrollHitWidth
	return Rect{
		X: n.Bounds.X + n.Bounds.W - pad - w,
		Y: n.Bounds.Y + pad,
		W: w,
		H: n.Bounds.H - 2*pad,
	}
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
