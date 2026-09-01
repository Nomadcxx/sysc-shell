package shell

import "github.com/Nomadcxx/sysc-shell/internal/ui"

// toastCardWidth is the design's card width. Cards clamp to a narrower output.
const (
	toastCardWidth = 360
	toastMargin    = 12
	toastCardGap   = 8
)

type toastCorner uint8

const (
	toastTopRight toastCorner = iota
	toastTopLeft
	toastBottomRight
	toastBottomLeft
)

type toastGeometry struct {
	OutputW, OutputH int
	Corner           toastCorner
}

// toastLayout places as many cards as the geometry holds, stacking away from
// the configured edge, and returns the visible rectangles plus the indexes
// that did not fit. Geometry, not a fixed count, decides overflow.
func toastLayout(g toastGeometry, heights []int) (rects []ui.Rect, queued []int) {
	width := toastCardWidth
	if max := g.OutputW - 2*toastMargin; max < width {
		width = max
	}
	if width < 1 {
		return nil, allIndexes(heights)
	}

	var x int
	switch g.Corner {
	case toastTopRight, toastBottomRight:
		x = g.OutputW - toastMargin - width
	default:
		x = toastMargin
	}

	top := g.Corner == toastTopRight || g.Corner == toastTopLeft
	y := toastMargin
	if !top {
		y = g.OutputH - toastMargin
	}
	limit := g.OutputH - 2*toastMargin
	used := 0

	for i, h := range heights {
		if h <= 0 {
			continue
		}
		need := h
		if used > 0 {
			need += toastCardGap
		}
		if used+need > limit {
			queued = append(queued, i)
			continue
		}
		var rect ui.Rect
		if top {
			rect = ui.Rect{X: x, Y: y, W: width, H: h}
			y += h + toastCardGap
		} else {
			rect = ui.Rect{X: x, Y: y - h, W: width, H: h}
			y -= h + toastCardGap
		}
		rects = append(rects, rect)
		used += need
	}
	return rects, queued
}

func allIndexes(heights []int) []int {
	out := make([]int, len(heights))
	for i := range heights {
		out[i] = i
	}
	return out
}

// toastInputRegion is the union of visible card rectangles. No cards means a
// non-nil empty slice: the surface accepts no pointer input, which is not the
// same as an unset region covering the whole surface.
func toastInputRegion(rects []ui.Rect) []ui.Rect {
	out := make([]ui.Rect, len(rects))
	copy(out, rects)
	return out
}
