package shell

import "github.com/Nomadcxx/sysc-shell/internal/ui"

// toastCardWidth is the design's card width. Cards clamp to a narrower output.
const (
	toastCardWidth = 380
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
	BarZone          int
	// BarEdge is the screen edge the bar occupies (top, right, bottom, left).
	// Empty keeps the older reading: BarZone is a vertical inset on the anchor side.
	BarEdge string
}

// toastSideInsets is the horizontal thickness a side bar occupies. A top or
// bottom bar has none; an unset edge does too, so older geometry is unchanged.
func toastSideInsets(g toastGeometry) (left, right int) {
	zone := g.BarZone
	if zone < 0 {
		zone = 0
	}
	switch g.BarEdge {
	case "left":
		return zone, 0
	case "right":
		return 0, zone
	default:
		return 0, 0
	}
}

// toastVerticalInset is the thickness reserved on the anchor edge. A right
// bar spends that thickness horizontally, so it is not also a top inset.
// A left bar keeps the reservation: the top-right stack already clears it,
// and that placement stays.
func toastVerticalInset(g toastGeometry) int {
	if g.BarEdge == "right" || g.BarZone < 0 {
		return 0
	}
	return g.BarZone
}

// toastFitWidth is the card width for this output: the design width, or
// narrower once the margins and a side bar are taken out.
func toastFitWidth(g toastGeometry) int {
	left, right := toastSideInsets(g)
	width := toastCardWidth
	if maxW := g.OutputW - 2*toastMargin - left - right; maxW < width {
		width = maxW
	}
	return width
}

// toastLayout places as many cards as the geometry holds, stacking away from
// the configured edge, and returns the visible rectangles plus the indexes
// that did not fit. Geometry, not a fixed count, decides overflow.
func toastLayout(g toastGeometry, heights []int) (rects []ui.Rect, queued []int) {
	width := toastFitWidth(g)
	if width < 1 {
		return nil, allIndexes(heights)
	}
	leftInset, rightInset := toastSideInsets(g)

	var x int
	switch g.Corner {
	case toastTopRight, toastBottomRight:
		x = g.OutputW - toastMargin - rightInset - width
	default:
		x = toastMargin + leftInset
	}

	vertical := toastVerticalInset(g)
	top := g.Corner == toastTopRight || g.Corner == toastTopLeft
	y := vertical + toastMargin
	if !top {
		y = g.OutputH - vertical - toastMargin
	}
	limit := g.OutputH - 2*toastMargin - vertical
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

// toastCardHeight is the tree's intrinsic height. The card is the surface
// ground itself, so nothing needs room around the tree. A missing tree or
// measure falls back to 96 so the card still places.
func toastCardHeight(root *ui.Node, width int, measure ui.MeasureText) int {
	if root == nil || measure == nil {
		return 96
	}
	ht, err := ui.ContentHeight(root, width, measure)
	if err != nil || ht <= 0 {
		return 96
	}
	return ht
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
