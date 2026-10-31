package ui

import (
	"slices"
	"sort"
)

// SurfaceShape is a painted silhouette: a body, its rounded corners, and the
// concave wedges that join it to the bar or the screen. Blur regions are cut
// from it, so the compositor blurs exactly what the surface paints.
type SurfaceShape struct {
	Body   Rect
	Radius int
	// AttachEdge is the edge that meets the bar or the
	// screen. Its corners are square. Empty rounds all four.
	AttachEdge string
	// JointLeft and JointRight are the leading and trailing concave wedges
	// along the attached edge (top/bottom on a side surface). Zero draws none.
	JointLeft, JointRight int
	// EdgeFillet is the radius of the wedges on the far edge that curve into
	// the screen's side: an attached bar carries them at both ends, and a
	// panel flush against the screen edge carries one on that side. Left and
	// Right mean leading/trailing along the attached edge. The far
	// corner on a side that carries one is square.
	EdgeFillet          int
	EdgeLeft, EdgeRight bool
	// Scale is the scale the surface paints at; zero or unit keeps logical
	// corner rows. At a fractional scale one logical row spans more than one
	// physical row, and a corner row rounded in logical pixels reaches past
	// the arc the painter fills. A scaled shape measures each corner row where
	// the painter fills it and rounds inward, so a strip never blurs a pixel
	// the surface leaves bare.
	Scale Scale120
}

// BlurStrips returns the shape as rectangles in the body's coordinate space:
// one span per pixel row, merged where consecutive rows match. A row takes a
// fillet pixel when it is at least half covered, so the region stays inside
// the painted, antialiased edge.
func BlurStrips(s SurfaceShape) []Rect {
	b := s.Body
	if b.W <= 0 || b.H <= 0 {
		return nil
	}
	if s.AttachEdge == "left" || s.AttachEdge == "right" {
		// Transpose geometry only; the same coverage math owns every edge.
		s.Body = Rect{X: b.Y, Y: b.X, W: b.H, H: b.W}
		if s.AttachEdge == "left" {
			s.AttachEdge = "top"
		} else {
			s.AttachEdge = "bottom"
		}
		strips := BlurStrips(s)
		for i := range strips {
			r := strips[i]
			strips[i] = Rect{X: r.Y, Y: r.X, W: r.H, H: r.W}
		}
		return strips
	}
	radius := min(max(s.Radius, 0), min(b.W, b.H)/2)
	top, bottom := s.AttachEdge == "top", s.AttachEdge == "bottom"
	// The four corner radii. The attached edge is square; so is a far corner
	// that turns into an edge fillet.
	rTL, rTR, rBL, rBR := radius, radius, radius, radius
	switch {
	case top:
		rTL, rTR = 0, 0
		if s.EdgeFillet > 0 && s.EdgeLeft {
			rBL = 0
		}
		if s.EdgeFillet > 0 && s.EdgeRight {
			rBR = 0
		}
	case bottom:
		rBL, rBR = 0, 0
		if s.EdgeFillet > 0 && s.EdgeLeft {
			rTL = 0
		}
		if s.EdgeFillet > 0 && s.EdgeRight {
			rTR = 0
		}
	}

	rows := map[int][][2]int{}
	add := func(y, x0, x1 int) {
		if x1 > x0 {
			rows[y] = append(rows[y], [2]int{x0, x1})
		}
	}
	edges := func(y, rl, rr int) (int, int) {
		return b.X + RoundedInset(y, b.H, rl), b.X + b.W - RoundedInset(y, b.H, rr)
	}
	if s.Scale.Valid() && s.Scale != ScaleUnit {
		edges = scaledRowEdges(b, s.Scale)
	}
	for y := 0; y < b.H; y++ {
		upper := y <= b.H-1-y
		rl, rr := rBL, rBR
		if upper {
			rl, rr = rTL, rTR
		}
		x0, x1 := edges(y, rl, rr)
		add(b.Y+y, x0, x1)
	}
	// Rows beside the attached edge, counted away from it into the body's
	// height, and rows beyond the far edge, counted away from the body.
	nearRow := func(i int) int {
		if bottom {
			return b.Y + b.H - 1 - i
		}
		return b.Y + i
	}
	farRow := func(i int) int {
		if bottom {
			return b.Y - 1 - i
		}
		return b.Y + b.H + i
	}
	if top || bottom {
		for i := 0; i < s.JointLeft && i < b.H; i++ {
			span := FilletSpan(i, s.JointLeft)
			add(nearRow(i), b.X-span, b.X)
		}
		for i := 0; i < s.JointRight && i < b.H; i++ {
			span := FilletSpan(i, s.JointRight)
			add(nearRow(i), b.X+b.W, b.X+b.W+span)
		}
		for i := 0; i < s.EdgeFillet; i++ {
			span := FilletSpan(i, s.EdgeFillet)
			if s.EdgeLeft {
				add(farRow(i), b.X, b.X+span)
			}
			if s.EdgeRight {
				add(farRow(i), b.X+b.W-span, b.X+b.W)
			}
		}
	}
	return mergeRows(rows)
}

// mergeRows unions each row's spans and joins consecutive rows whose spans
// match into one taller rectangle.
func mergeRows(rows map[int][][2]int) []Rect {
	ys := make([]int, 0, len(rows))
	for y, spans := range rows {
		rows[y] = unionSpans(spans)
		ys = append(ys, y)
	}
	sort.Ints(ys)
	var out []Rect
	open := map[[2]int]int{} // span -> index into out of the rect still growing
	prevY := 0
	for n, y := range ys {
		next := map[[2]int]int{}
		for _, sp := range rows[y] {
			if i, ok := open[sp]; ok && n > 0 && y == prevY+1 {
				out[i].H++
				next[sp] = i
				continue
			}
			out = append(out, Rect{X: sp[0], Y: y, W: sp[1] - sp[0], H: 1})
			next[sp] = len(out) - 1
		}
		open, prevY = next, y
	}
	return out
}

func unionSpans(spans [][2]int) [][2]int {
	slices.SortFunc(spans, func(a, b [2]int) int { return a[0] - b[0] })
	var out [][2]int
	for _, sp := range spans {
		if n := len(out); n > 0 && sp[0] <= out[n-1][1] {
			out[n-1][1] = max(out[n-1][1], sp[1])
			continue
		}
		out = append(out, sp)
	}
	return out
}

// scaledRowEdges returns the logical span of each body row whose physical
// pixels the painter fills. It insets every corner row by the deepest
// physical corner inset among the physical rows it covers, using the same
// RoundedInset and radius clamp the canvas paints with, then keeps the
// innermost logical edges whose physical edges sit inside that fill.
func scaledRowEdges(b Rect, scale Scale120) func(y, rl, rr int) (int, int) {
	p := scale.PhysicalRect(b)
	half := min(p.W, p.H) / 2
	// The painter scales the radius before clamping it to half the physical
	// box. A radius clamped here in logical pixels therefore paints as the
	// physical half, and a larger radius only ever narrows a strip.
	physicalRadius := func(r int) int {
		if r <= 0 {
			return 0
		}
		if r >= min(b.W, b.H)/2 {
			return half
		}
		return min(scale.Physical(r), half)
	}
	return func(y, rl, rr int) (int, int) {
		x0, x1 := b.X, b.X+b.W
		prl, prr := physicalRadius(rl), physicalRadius(rr)
		if prl == 0 && prr == 0 {
			return x0, x1
		}
		top, bottom := scale.Physical(b.Y+y)-p.Y, scale.Physical(b.Y+y+1)-p.Y
		left, right := 0, 0
		for py := max(top, 0); py < min(bottom, p.H); py++ {
			left = max(left, RoundedInset(py, p.H, prl))
			right = max(right, RoundedInset(py, p.H, prr))
		}
		for x0 < x1 && scale.Physical(x0) < p.X+left {
			x0++
		}
		for x1 > x0 && scale.Physical(x1) > p.X+p.W-right {
			x1--
		}
		return x0, x1
	}
}
