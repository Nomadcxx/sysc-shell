package shell

import (
	"sort"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// panelPlacement starts at the panel's legacy position and moves along the
// bar's main axis to the nearest clear spot. workArea is the body range.
func panelPlacement(anchor, workArea ui.Rect, alreadyOpen []ui.Rect, size ui.Size, vertical bool) ui.Rect {
	if vertical {
		open := make([]ui.Rect, len(alreadyOpen))
		for i, r := range alreadyOpen {
			open[i] = transposeRect(r)
		}
		return transposeRect(panelPlacement(transposeRect(anchor), transposeRect(workArea), open,
			ui.Size{W: size.H, H: size.W}, false))
	}
	w := min(max(size.W, 0), max(workArea.W, 0))
	h := min(max(size.H, 0), max(workArea.H, 0))
	minX, maxX := workArea.X, max(workArea.X, workArea.X+workArea.W-w)
	minY, maxY := workArea.Y, max(workArea.Y, workArea.Y+workArea.H-h)
	base := ui.Rect{
		X: min(max(anchor.X, minX), maxX),
		Y: min(max(anchor.Y, minY), maxY),
		W: w, H: h,
	}
	candidates := []int{base.X}
	for _, open := range alreadyOpen {
		if base.Y >= open.Y+open.H || open.Y >= base.Y+base.H {
			continue
		}
		candidates = append(candidates, open.X-w, open.X+open.W)
	}

	best, bestDistance, found := base, 0, false
	for _, x := range candidates {
		x = min(max(x, minX), maxX)
		candidate := ui.Rect{X: x, Y: base.Y, W: w, H: h}
		clear := true
		for _, open := range alreadyOpen {
			if overlaps(candidate, open) {
				clear = false
				break
			}
		}
		if !clear {
			continue
		}
		distance := x - base.X
		if distance < 0 {
			distance = -distance
		}
		if !found || distance < bestDistance || distance == bestDistance && x < best.X {
			best, bestDistance, found = candidate, distance, true
		}
	}
	// ponytail: O(n²) candidates suit the small fixed panel set; sort and merge
	// blocked intervals if the number of simultaneous surfaces grows materially.
	if found {
		return best
	}
	return base
}

// panelArrangement moves the existing row only when the new panel cannot fit
// beside it. It keeps existing panels as close as possible to their positions
// and preserves each panel's vertical position.
func panelArrangement(anchor, workArea ui.Rect, alreadyOpen []ui.Rect, size ui.Size, vertical bool) (ui.Rect, []ui.Rect) {
	if vertical {
		open := make([]ui.Rect, len(alreadyOpen))
		for i, r := range alreadyOpen {
			open[i] = transposeRect(r)
		}
		placed, shifted := panelArrangement(transposeRect(anchor), transposeRect(workArea), open,
			ui.Size{W: size.H, H: size.W}, false)
		for i, r := range shifted {
			shifted[i] = transposeRect(r)
		}
		return transposeRect(placed), shifted
	}
	placed := panelPlacement(anchor, workArea, alreadyOpen, size, false)
	shifted := append([]ui.Rect(nil), alreadyOpen...)
	needsReflow := false
	for _, open := range alreadyOpen {
		if overlaps(placed, open) {
			needsReflow = true
			break
		}
	}
	if !needsReflow {
		return placed, shifted
	}

	type item struct {
		index int
		rect  ui.Rect
		fresh bool
	}
	items := make([]item, 0, len(alreadyOpen)+1)
	for i, open := range alreadyOpen {
		if open.Y < placed.Y+placed.H && placed.Y < open.Y+open.H {
			items = append(items, item{index: i, rect: open})
		}
	}
	items = append(items, item{index: -1, rect: placed, fresh: true})
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].rect.X != items[j].rect.X {
			return items[i].rect.X < items[j].rect.X
		}
		return items[i].fresh && !items[j].fresh
	})

	width, offset := 0, 0
	oldStarts := make([]int, 0, len(items)-1)
	for _, item := range items {
		if !item.fresh {
			oldStarts = append(oldStarts, item.rect.X-offset)
		}
		width += item.rect.W
		offset += item.rect.W
	}
	if width > workArea.W {
		// ponytail: if the natural panel widths exceed the output, overlap is
		// unavoidable; resize panels only if that becomes a real use case.
		return placed, shifted
	}
	maxStart := workArea.X + workArea.W - width
	sort.Ints(oldStarts)
	preferredStart := oldStarts[len(oldStarts)/2]
	start := min(max(preferredStart, workArea.X), maxStart)
	for _, item := range items {
		item.rect.X = start
		if item.fresh {
			placed = item.rect
		} else {
			shifted[item.index] = item.rect
		}
		start += item.rect.W
	}
	return placed, shifted
}

func overlaps(a, b ui.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func (p Placement) panelRect() ui.Rect { return p.Rect() }

func (p Placement) workArea(anchor ui.Rect) ui.Rect {
	if p.sideAxis() {
		return transposeRect(p.horizontalAxis().workArea(transposeRect(anchor)))
	}
	left := min(p.Padding, anchor.X)
	right := max(p.Output.W-p.Padding, anchor.X+anchor.W)
	return ui.Rect{X: left, Y: anchor.Y, W: max(right-left, 0), H: anchor.H}
}

func marginsFor(rect ui.Rect, p Placement) Margins {
	if p.sideAxis() {
		m := marginsFor(transposeRect(rect), p.horizontalAxis())
		return Margins{Top: m.Left, Bottom: m.Right, Left: m.Top, Right: m.Bottom}
	}
	if p.BarEdge == "bottom" {
		return Margins{Left: rect.X, Bottom: p.Output.H - rect.Y - rect.H}
	}
	return Margins{Left: rect.X, Top: rect.Y}
}
