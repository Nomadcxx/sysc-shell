package ui

import "fmt"

// BarOverflow counts the items each section could not place. The shell decides
// what to show for them; layout only reports the number, the way arrangeTray
// hands back the items it could not fit rather than drawing chrome itself.
type BarOverflow struct {
	Left   int
	Center int
	Right  int
}

// Any reports whether any section dropped an item.
func (o BarOverflow) Any() bool { return o.Left > 0 || o.Center > 0 || o.Right > 0 }

// ArrangeBar places three sections in one content band, writes each node's
// Bounds, and reports what would not fit.
//
// The centre is pinned to the absolute centre of the band, computed without
// reference to the side widths, so it reads as centred on the monitor rather
// than drifting as its neighbours change. Collision priority is a total order:
// the centre keeps its natural width while it fits; the sides give way; a
// section with no room places nothing; and only a centre wider than the whole
// band takes the band, clearing both sides.
//
// A section gives way by dropping whole items, never by granting a fraction of
// one: an item is placed at its natural width or not at all, and a dropped item
// gets the zero Rect. An item granted its full width may still be ellipsized by
// the painter when its glyph run runs long, which is the painter's business and
// not an overflow.
func ArrangeBar(content Rect, left, center, right []*Node, spacing int, measure MeasureText) (BarOverflow, error) {
	if content.W < 0 || content.H < 0 {
		return BarOverflow{}, fmt.Errorf("ui: negative content bounds %dx%d", content.W, content.H)
	}
	// ponytail: keep anchored placement specific to this one wordmark consumer;
	// a second anchored composition needs its own layout contract.
	if mark := wordmarkIndex(center); mark >= 0 {
		return arrangeAnchoredWordmark(content, left, center, right, mark, spacing, measure)
	}
	return arrangeBarSections(content, left, center, right, spacing, measure)
}

func wordmarkIndex(items []*Node) int {
	mark := -1
	for i, n := range items {
		if n != nil && n.Kind == KindWordmark {
			if mark >= 0 {
				return -1
			}
			mark = i
		}
	}
	return mark
}

func arrangeAnchoredWordmark(content Rect, left, center, right []*Node, markIndex, spacing int, measure MeasureText) (BarOverflow, error) {
	var over BarOverflow
	before, mark, after := center[:markIndex], center[markIndex:markIndex+1], center[markIndex+1:]
	wL, err := sectionWidth(left, spacing, content.H, measure)
	if err != nil {
		return over, fmt.Errorf("ui: left section: %w", err)
	}
	wR, err := sectionWidth(right, spacing, content.H, measure)
	if err != nil {
		return over, fmt.Errorf("ui: right section: %w", err)
	}
	wBefore, err := sectionWidth(before, spacing, content.H, measure)
	if err != nil {
		return over, fmt.Errorf("ui: centre before wordmark: %w", err)
	}
	wMark, _, err := measureNode(mark[0], content.H, measure)
	if err != nil {
		return over, fmt.Errorf("ui: wordmark: %w", err)
	}
	wAfter, err := sectionWidth(after, spacing, content.H, measure)
	if err != nil {
		return over, fmt.Errorf("ui: centre after wordmark: %w", err)
	}
	beforeActive, afterActive := wBefore > 0, wAfter > 0
	compositionWidth := wBefore + wMark + wAfter
	if beforeActive {
		compositionWidth += spacing
	}
	if afterActive {
		compositionWidth += spacing
	}
	if wMark > content.W || compositionWidth > content.W {
		return arrangeBarSections(content, left, center, right, spacing, measure)
	}

	markX := content.X + (content.W-wMark)/2
	markRight := markX + wMark
	beforeStart := content.X
	if beforeActive {
		beforeBudget := max(markX-content.X-spacing, 0)
		beforeGrant := min(wBefore, beforeBudget)
		if beforeGrant > 0 {
			beforeStart = markX - spacing - beforeGrant
		}
		n, err := placeSection(before, beforeStart, content, beforeGrant, 0, spacing, measure)
		if err != nil {
			return over, fmt.Errorf("ui: centre before wordmark: %w", err)
		}
		over.Center += n
	} else if len(before) > 0 {
		n, err := placeSection(before, content.X, content, 0, 0, spacing, measure)
		if err != nil {
			return over, fmt.Errorf("ui: centre before wordmark: %w", err)
		}
		over.Center += n
	}
	if n, err := placeSection(mark, markX, content, wMark, 0, spacing, measure); err != nil {
		return over, fmt.Errorf("ui: wordmark: %w", err)
	} else {
		over.Center += n
	}
	afterStart, afterBudget := markRight, 0
	if afterActive {
		afterBudget = max(content.X+content.W-markRight-spacing, 0)
		afterGrant := min(wAfter, afterBudget)
		afterStart = min(markRight+spacing, content.X+content.W)
		n, err := placeSection(after, afterStart, content, afterGrant, 0, spacing, measure)
		if err != nil {
			return over, fmt.Errorf("ui: centre after wordmark: %w", err)
		}
		over.Center += n
		afterBudget = afterGrant
	} else if len(after) > 0 {
		n, err := placeSection(after, markRight, content, 0, 0, spacing, measure)
		if err != nil {
			return over, fmt.Errorf("ui: centre after wordmark: %w", err)
		}
		over.Center += n
	}

	compositionLeft := markX
	if beforeActive {
		compositionLeft = beforeStart
	}
	compositionRight := markRight
	if afterActive {
		compositionRight = afterStart + afterBudget
	}
	leftMax := max(0, compositionLeft-content.X-spacing)
	nL, err := placeSection(left, content.X, content, min(wL, leftMax), 0, spacing, measure)
	if err != nil {
		return over, fmt.Errorf("ui: left section: %w", err)
	}
	over.Left = nL
	rightMax := max(0, content.X+content.W-compositionRight-spacing)
	rightBudget := min(wR, rightMax)
	used, err := fittedWidth(right, content, rightBudget, 0, spacing, measure)
	if err != nil {
		return over, fmt.Errorf("ui: right section: %w", err)
	}
	nR, err := placeSection(right, content.X+content.W-used, content, rightBudget, 0, spacing, measure)
	if err != nil {
		return over, fmt.Errorf("ui: right section: %w", err)
	}
	over.Right = nR
	return over, nil
}

func arrangeBarSections(content Rect, left, center, right []*Node, spacing int, measure MeasureText) (BarOverflow, error) {
	var over BarOverflow
	wL, err := sectionWidth(left, spacing, content.H, measure)
	if err != nil {
		return over, fmt.Errorf("ui: left section: %w", err)
	}
	wC, err := sectionWidth(center, spacing, content.H, measure)
	if err != nil {
		return over, fmt.Errorf("ui: center section: %w", err)
	}
	wR, err := sectionWidth(right, spacing, content.H, measure)
	if err != nil {
		return over, fmt.Errorf("ui: right section: %w", err)
	}

	// Only a centre that alone exceeds the band truncates, and it then takes
	// the whole band; the sides have nowhere left to go.
	//
	// This is the one place a partial grant survives, and deliberately: there
	// is nowhere to move a centre wider than the output, so dropping it whole
	// would leave the bar empty rather than degraded. The item is granted every
	// pixel there is and the painter ellipsizes inside it, which is the
	// documented "granted its whole extent" case and not a silent clip.
	if wC > content.W {
		if err = placeTruncating(center, content.X, content, content.W, spacing, measure); err != nil {
			return over, err
		}
		if over.Left, err = placeSection(left, content.X, content, 0, 0, spacing, measure); err != nil {
			return over, err
		}
		if over.Right, err = placeSection(right, content.X+content.W, content, 0, 0, spacing, measure); err != nil {
			return over, err
		}
		return over, nil
	}

	centerX := content.X + (content.W-wC)/2
	leftMax := max(0, centerX-content.X-spacing)
	rightMax := max(0, content.X+content.W-(centerX+wC)-spacing)

	if over.Center, err = placeSection(center, centerX, content, wC, 0, spacing, measure); err != nil {
		return over, err
	}
	if over.Left, err = placeSection(left, content.X, content, min(wL, leftMax), 0, spacing, measure); err != nil {
		return over, err
	}
	granted := min(wR, rightMax)
	used, err := fittedWidth(right, content, granted, 0, spacing, measure)
	if err != nil {
		return over, err
	}
	if over.Right, err = placeSection(right, content.X+content.W-used, content, granted, 0, spacing, measure); err != nil {
		return over, err
	}
	return over, nil
}

// sectionWidth reports a section's natural width: its items plus the spacing
// between them. An empty section is zero wide and contributes no spacing.
func sectionWidth(items []*Node, spacing, height int, measure MeasureText) (int, error) {
	total := 0
	for i, n := range items {
		if n == nil {
			return 0, fmt.Errorf("nil item %d", i)
		}
		w, _, err := measureNode(n, height, measure)
		if err != nil {
			return 0, fmt.Errorf("item %d: %w", i, err)
		}
		if i > 0 {
			total += spacing
		}
		total += w
	}
	return total, nil
}

// placeSection lays items left to right from x within a budget, centring each
// vertically.
//
// An item is placed whole or not at all, and placeSection returns how many it
// could not place. Granting a fraction of an item is what cut a label
// mid-glyph and made a widget vanish with no diagnostic, which is sysc-313.
// A dropped item gets the zero Rect on both axes: a zero-width box at full
// height is exactly what the silent clipping looked like.
//
// An item granted its whole natural width may still be ellipsized by the
// painter, which owns cluster measurement. That is not this bug: the item is
// present, and the text inside it is what ran long.
//
// reserve is main-axis extent to keep for an overflow indicator. It is charged
// only when the section actually overflows, because a section that fits needs
// no indicator — which is why the fit is computed before anything is placed
// rather than decided item by item.
func placeSection(items []*Node, x int, content Rect, budget, reserve, spacing int, measure MeasureText) (int, error) {
	widths, heights, err := measureSection(items, content.H, measure)
	if err != nil {
		return 0, err
	}
	granted, dropped := sectionGrants(items, widths, budget, reserve, spacing)

	placed := 0
	for i, n := range items {
		if granted[i] < 0 {
			n.Bounds = Rect{}
			continue
		}
		if placed > 0 {
			x += spacing
		}
		placed++
		h := heights[i]
		n.Bounds = Rect{X: x, Y: content.Y + (content.H-h)/2, W: granted[i], H: h}
		// A section places items itself rather than through Layout, so a
		// capsule's contents are arranged here too. Without this a bar paints
		// empty pills.
		if n.Kind == KindCapsule {
			if err := layoutCapsuleChild(n, measure); err != nil {
				return 0, fmt.Errorf("ui: item %d: %w", i, err)
			}
		}
		x += granted[i]
	}
	return dropped, nil
}

// elastic reports whether a node is built to shrink rather than disappear.
// Declaring a width cap is the marker: in this bar that is the focused-window
// title, the media title and the weather line, each of which ellipsizes inside
// whatever box it is given. A fixed item has one right size and is better
// dropped and counted than shown as a sliver.
//
// A capsule is chrome wrapped around one child, so it is as elastic as what it
// holds. Without descending, every capsuled widget reads as fixed and the bar
// drops the very titles this rule exists to keep.
func elastic(n *Node) bool {
	if n == nil {
		return false
	}
	if n.MaxWidth > 0 {
		return true
	}
	if n.Kind != KindCapsule {
		return false
	}
	for _, c := range n.Children {
		if elastic(c) {
			return true
		}
	}
	return false
}

// sectionGrants decides each item's width, returning -1 for a dropped item and
// how many were dropped. The reserve is charged only when something overflows
// without it.
func sectionGrants(items []*Node, widths []int, budget, reserve, spacing int) ([]int, int) {
	granted, dropped := grantsWithin(items, widths, max(0, budget), spacing)
	if dropped > 0 && reserve > 0 {
		granted, dropped = grantsWithin(items, widths, max(0, budget-reserve), spacing)
	}
	return granted, dropped
}

// grantsWithin gives every fixed item its natural width and lets the elastic
// ones share what is left, so a narrow bar keeps its title -- narrowed -- and
// its chips, instead of dropping whichever came last. Only when the fixed items
// alone do not fit does the section fall back to dropping whole items from the
// far end.
func grantsWithin(items []*Node, widths []int, budget, spacing int) ([]int, int) {
	granted := make([]int, len(items))
	if len(items) == 0 {
		return granted, 0
	}
	spacingTotal := spacing * (len(items) - 1)
	fixedTotal, elasticNatural, elasticCount := 0, 0, 0
	for i, n := range items {
		if elastic(n) {
			elasticNatural += widths[i]
			elasticCount++
			continue
		}
		fixedTotal += widths[i]
	}

	if fixedTotal+elasticNatural+spacingTotal <= budget {
		copy(granted, widths)
		return granted, 0
	}

	// The fixed items and the spacing still fit, so the elastic ones absorb the
	// shortfall by sharing the remainder evenly, each capped at its natural
	// width. Nothing is dropped: a narrowed title is present and legible in a
	// way a dropped one is not.
	if elasticCount > 0 && fixedTotal+spacingTotal < budget {
		share := (budget - fixedTotal - spacingTotal) / elasticCount
		if share > 0 {
			for i, n := range items {
				if elastic(n) {
					granted[i] = min(widths[i], share)
					continue
				}
				granted[i] = widths[i]
			}
			return granted, 0
		}
	}

	fits := fitCount(widths, budget, spacing)
	for i := range items {
		if i < fits {
			granted[i] = widths[i]
			continue
		}
		granted[i] = -1
	}
	return granted, len(items) - fits
}

// placeTruncating grants each item min(natural, remaining), the behaviour the
// rest of the bar gave up in sysc-313. It exists for the single documented case
// of a centre wider than the whole band, where there is no room to drop into.
func placeTruncating(items []*Node, x int, content Rect, budget, spacing int, measure MeasureText) error {
	widths, heights, err := measureSection(items, content.H, measure)
	if err != nil {
		return err
	}
	remaining := max(0, budget)
	for i, n := range items {
		if i > 0 {
			if remaining < spacing {
				remaining = 0
			} else {
				x += spacing
				remaining -= spacing
			}
		}
		granted := min(widths[i], remaining)
		h := heights[i]
		n.Bounds = Rect{X: x, Y: content.Y + (content.H-h)/2, W: granted, H: h}
		if n.Kind == KindCapsule {
			if err := layoutCapsuleChild(n, measure); err != nil {
				return fmt.Errorf("ui: item %d: %w", i, err)
			}
		}
		x += granted
		remaining -= granted
	}
	return nil
}

// measureSection measures every item once, so the fit decision and the
// placement that follows it can never disagree about a width.
func measureSection(items []*Node, height int, measure MeasureText) (widths, heights []int, err error) {
	widths = make([]int, len(items))
	heights = make([]int, len(items))
	for i, n := range items {
		if n == nil {
			return nil, nil, fmt.Errorf("ui: nil item %d", i)
		}
		w, h, err := measureNode(n, height, measure)
		if err != nil {
			return nil, nil, fmt.Errorf("ui: item %d: %w", i, err)
		}
		widths[i] = max(0, w)
		heights[i] = min(max(0, h), height)
	}
	return widths, heights, nil
}

// fitCount is how many leading items fit whole in budget, charging spacing
// between the ones that survive. Collapse order is declaration order from the
// far end, so the answer is always a prefix: the last-declared item goes first.
func fitCount(widths []int, budget, spacing int) int {
	used := 0
	for i, w := range widths {
		next := used + w
		if i > 0 {
			next += spacing
		}
		if next > budget {
			return i
		}
		used = next
	}
	return len(widths)
}

// fittedWidth is the extent the surviving prefix of a section will occupy.
// A right-aligned section needs this before it can choose its origin, so that
// dropping an item slides the rest flush to the edge instead of leaving a gap
// where the dropped one would have been.
func fittedWidth(items []*Node, content Rect, budget, reserve, spacing int, measure MeasureText) (int, error) {
	widths, _, err := measureSection(items, content.H, measure)
	if err != nil {
		return 0, err
	}
	granted, _ := sectionGrants(items, widths, budget, reserve, spacing)
	used, placed := 0, 0
	for _, w := range granted {
		if w < 0 {
			continue
		}
		if placed > 0 {
			used += spacing
		}
		placed++
		used += w
	}
	return used, nil
}
