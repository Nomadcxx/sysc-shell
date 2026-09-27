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
	// AttachEdge is "top" or "bottom" when that edge meets the bar or the
	// screen. Its corners are square. Empty rounds all four.
	AttachEdge string
	// JointLeft and JointRight are concave wedges outside the body beside
	// the attached edge, joining it to the bar. Zero draws none on that side.
	JointLeft, JointRight int
	// EdgeFillet is the radius of the wedges on the far edge that curve into
	// the screen's side: an attached bar carries them at both ends, and a
	// panel flush against the screen edge carries one on that side. The far
	// corner on a side that carries one is square.
	EdgeFillet          int
	EdgeLeft, EdgeRight bool
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
	for y := 0; y < b.H; y++ {
		upper := y <= b.H-1-y
		rl, rr := rBL, rBR
		if upper {
			rl, rr = rTL, rTR
		}
		add(b.Y+y, b.X+RoundedInset(y, b.H, rl), b.X+b.W-RoundedInset(y, b.H, rr))
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
