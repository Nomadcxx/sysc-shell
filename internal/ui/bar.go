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

// Axis is the direction a bar runs along. The main axis carries the lane
// order, the spacing and the collision total order; the cross axis is what
// every placed item centres on. Text never rotates, so the axis switch lives
// where a Rect is built from main and cross quantities; everything above that
// leaf is axis-neutral arithmetic.
type Axis int

const (
	// Horizontal runs the bar along X: lanes place left to right.
	Horizontal Axis = iota
	// Vertical runs the bar along Y: lanes transpose fixed, the left lane at
	// the top and the right lane at the lower end, without mirroring.
	Vertical
)

// mainOf splits a content band into main-axis origin and extent.
func (a Axis) mainOf(content Rect) (mainOrigin, mainExtent int) {
	if a == Vertical {
		return content.Y, content.H
	}
	return content.X, content.W
}

// crossOf splits a content band into cross-axis origin and extent.
func (a Axis) crossOf(content Rect) (crossOrigin, crossExtent int) {
	if a == Vertical {
		return content.X, content.W
	}
	return content.Y, content.H
}

// rect builds a placed item's box from main-axis and cross-axis quantities.
// This leaf is the one place the axis has coordinates in it.
func (a Axis) rect(main, mainExtent, crossOrigin, cross, crossExtent int) Rect {
	if a == Vertical {
		return Rect{X: crossOrigin + (crossExtent-cross)/2, Y: main, W: cross, H: mainExtent}
	}
	return Rect{X: main, Y: crossOrigin + (crossExtent-cross)/2, W: mainExtent, H: cross}
}

// ArrangeBar places three sections in one content band, writes each node's
// Bounds, and reports what would not fit.
//
// The centre is pinned to the absolute centre of the band, computed without
// reference to the side widths, so it reads as centred on the monitor rather
// than drifting as its neighbours change. Collision priority is a total order:
// the centre keeps its natural main-axis extent while it fits; the sides give way; a
// section with no room places nothing; and only a centre wider than the whole
// band takes the band, clearing both sides.
//
// A section gives way by dropping whole items. Horizontal width-capped text
// may share a shortfall; a vertical item keeps its intrinsic height. A dropped
// subtree gets zero bounds. Text can still ellipsize across its granted box.
func ArrangeBar(content Rect, axis Axis, left, center, right []*Node, spacing int, measure MeasureText) (BarOverflow, error) {
	if content.W < 0 || content.H < 0 {
		return BarOverflow{}, fmt.Errorf("ui: negative content bounds %dx%d", content.W, content.H)
	}
	var over BarOverflow
	mainOrigin, mainExtent := axis.mainOf(content)
	_, crossExtent := axis.crossOf(content)
	wL, err := sectionExtent(left, crossExtent, axis, spacing, measure)
	if err != nil {
		return over, fmt.Errorf("ui: left section: %w", err)
	}
	wR, err := sectionExtent(right, crossExtent, axis, spacing, measure)
	if err != nil {
		return over, fmt.Errorf("ui: right section: %w", err)
	}
	start, end, err := centreInterval(content, axis, center, spacing, measure, &over)
	if err != nil {
		return over, err
	}
	leftMax := max(0, start-mainOrigin-spacing)
	if over.Left, err = placeSection(left, axis, mainOrigin, content, min(wL, leftMax), 0, spacing, measure); err != nil {
		return over, fmt.Errorf("ui: left section: %w", err)
	}
	rightMax := max(0, mainOrigin+mainExtent-end-spacing)
	granted := min(wR, rightMax)
	used, err := fittedExtent(right, content, axis, granted, 0, spacing, measure)
	if err != nil {
		return over, fmt.Errorf("ui: right section: %w", err)
	}
	if over.Right, err = placeSection(right, axis, mainOrigin+mainExtent-used, content, granted, 0, spacing, measure); err != nil {
		return over, fmt.Errorf("ui: right section: %w", err)
	}
	return over, nil
}

// centreInterval places the centre section and reports the interval [start,
// end) it occupies along the main axis. It is the skeleton's one variant: a
// plain centre is one block centred in the band; a lone wordmark pins the mark
// to the band centre with its flanks, falling back to the plain variant when
// the composition does not fit.
func centreInterval(content Rect, axis Axis, center []*Node, spacing int, measure MeasureText, over *BarOverflow) (int, int, error) {
	// ponytail: only the centre pill/mark uses anchored placement; another
	// anchored composition needs a separate layout contract.
	if mark := wordmarkIndex(center); mark >= 0 {
		return anchoredCentreInterval(content, axis, center, mark, spacing, measure, over)
	}
	return sectionCentreInterval(content, axis, center, spacing, measure, over)
}

// sectionCentreInterval centres one block and reports its interval. The
// truncating escape below is the one place a partial grant survives.
func sectionCentreInterval(content Rect, axis Axis, center []*Node, spacing int, measure MeasureText, over *BarOverflow) (int, int, error) {
	mainOrigin, mainExtent := axis.mainOf(content)
	_, crossExtent := axis.crossOf(content)
	wC, err := sectionExtent(center, crossExtent, axis, spacing, measure)
	if err != nil {
		return 0, 0, fmt.Errorf("ui: center section: %w", err)
	}

	// Only a centre that alone exceeds the band truncates, and it then takes
	// the whole band; the sides have nowhere left to go.
	//
	// This is the one place a partial grant survives, and deliberately: there
	// is nowhere to move a centre wider than the output, so dropping it whole
	// would leave the bar empty rather than degraded. The item is granted every
	// pixel there is and the painter ellipsizes inside it, which is the
	// documented "granted its whole extent" case and not a silent clip.
	if wC > mainExtent {
		if err := placeTruncating(center, axis, mainOrigin, content, mainExtent, spacing, measure); err != nil {
			return 0, 0, err
		}
		return mainOrigin, mainOrigin + mainExtent, nil
	}

	start := mainOrigin + (mainExtent-wC)/2
	if over.Center, err = placeSection(center, axis, start, content, wC, 0, spacing, measure); err != nil {
		return 0, 0, err
	}
	return start, start + wC, nil
}

// anchoredCentreInterval pins the mark to the band centre with its before and
// after flanks, and reports the interval the whole composition spans so the
// side budgets leave the flanks their room. When the mark or the composition
// does not fit it falls back to the plain variant, which is the documented
// degradation for an over-wide anchored centre.
func anchoredCentreInterval(content Rect, axis Axis, center []*Node, markIndex, spacing int, measure MeasureText, over *BarOverflow) (int, int, error) {
	mainOrigin, mainExtent := axis.mainOf(content)
	_, crossExtent := axis.crossOf(content)
	before, mark, after := center[:markIndex], center[markIndex:markIndex+1], center[markIndex+1:]
	wBefore, err := sectionExtent(before, crossExtent, axis, spacing, measure)
	if err != nil {
		return 0, 0, fmt.Errorf("ui: centre before wordmark: %w", err)
	}
	wMark, _, err := measureBarItem(mark[0], crossExtent, axis, measure)
	if err != nil {
		return 0, 0, fmt.Errorf("ui: wordmark: %w", err)
	}
	wAfter, err := sectionExtent(after, crossExtent, axis, spacing, measure)
	if err != nil {
		return 0, 0, fmt.Errorf("ui: centre after wordmark: %w", err)
	}
	beforeActive, afterActive := wBefore > 0, wAfter > 0
	compositionWidth := wBefore + wMark + wAfter
	if beforeActive {
		compositionWidth += spacing
	}
	if afterActive {
		compositionWidth += spacing
	}
	if wMark > mainExtent || compositionWidth > mainExtent {
		return sectionCentreInterval(content, axis, center, spacing, measure, over)
	}

	markX := mainOrigin + (mainExtent-wMark)/2
	markRight := markX + wMark
	beforeStart := mainOrigin
	if beforeActive {
		beforeBudget := max(markX-mainOrigin-spacing, 0)
		beforeGrant := min(wBefore, beforeBudget)
		if beforeGrant > 0 {
			beforeStart = markX - spacing - beforeGrant
		}
		n, err := placeSection(before, axis, beforeStart, content, beforeGrant, 0, spacing, measure)
		if err != nil {
			return 0, 0, fmt.Errorf("ui: centre before wordmark: %w", err)
		}
		over.Center += n
	} else if len(before) > 0 {
		n, err := placeSection(before, axis, mainOrigin, content, 0, 0, spacing, measure)
		if err != nil {
			return 0, 0, fmt.Errorf("ui: centre before wordmark: %w", err)
		}
		over.Center += n
	}
	if n, err := placeSection(mark, axis, markX, content, wMark, 0, spacing, measure); err != nil {
		return 0, 0, fmt.Errorf("ui: wordmark: %w", err)
	} else {
		over.Center += n
	}
	afterStart, afterBudget := markRight, 0
	if afterActive {
		afterBudget = max(mainOrigin+mainExtent-markRight-spacing, 0)
		afterGrant := min(wAfter, afterBudget)
		afterStart = min(markRight+spacing, mainOrigin+mainExtent)
		n, err := placeSection(after, axis, afterStart, content, afterGrant, 0, spacing, measure)
		if err != nil {
			return 0, 0, fmt.Errorf("ui: centre after wordmark: %w", err)
		}
		over.Center += n
		afterBudget = afterGrant
	} else if len(after) > 0 {
		n, err := placeSection(after, axis, markRight, content, 0, 0, spacing, measure)
		if err != nil {
			return 0, 0, fmt.Errorf("ui: centre after wordmark: %w", err)
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
	return compositionLeft, compositionRight, nil
}

func wordmarkIndex(items []*Node) int {
	mark := -1
	for i, n := range items {
		if anchorsWordmark(n) {
			if mark >= 0 {
				return -1
			}
			mark = i
		}
	}
	return mark
}

// anchorsWordmark reports whether a centre item is the mark or its pill.
// The pill's outer box, rather than its inner decorative mark, stays pinned.
func anchorsWordmark(n *Node) bool {
	if n == nil {
		return false
	}
	if n.Kind == KindWordmark {
		return true
	}
	if n.Kind != KindCapsule || len(n.Children) != 1 || n.Children[0] == nil {
		return false
	}
	inner := n.Children[0]
	if inner.Kind != KindRow && inner.Kind != KindColumn {
		return false
	}
	for _, c := range inner.Children {
		if c != nil && c.Kind == KindWordmark {
			return true
		}
	}
	return false
}

// sectionExtent reports a section's natural extent along the main axis: its
// items plus the spacing between them. An empty section is zero long and
// contributes no spacing.
func sectionExtent(items []*Node, crossExtent int, axis Axis, spacing int, measure MeasureText) (int, error) {
	main, _, err := measureSection(items, crossExtent, axis, measure)
	if err != nil {
		return 0, err
	}
	total := 0
	for i, m := range main {
		if i > 0 {
			total += spacing
		}
		total += m
	}
	return total, nil
}

// placeSection lays items main-axis forward from mainOrigin within a budget,
// centring each on the cross axis.
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
func placeSection(items []*Node, axis Axis, mainOrigin int, content Rect, budget, reserve, spacing int, measure MeasureText) (int, error) {
	crossOrigin, crossExtent := axis.crossOf(content)
	main, cross, err := measureSection(items, crossExtent, axis, measure)
	if err != nil {
		return 0, err
	}
	granted, dropped := barGrants(items, main, axis, budget, reserve, spacing)

	placed := 0
	m := mainOrigin
	for i, n := range items {
		n.ClipBounds = false
		if granted[i] < 0 {
			clearLayoutBounds(n)
			continue
		}
		if placed > 0 {
			m += spacing
		}
		placed++
		n.Bounds = axis.rect(m, granted[i], crossOrigin, cross[i], crossExtent)
		// A section places items itself rather than through Layout, so a
		// capsule's contents are arranged here too. Without this a bar paints
		// empty pills.
		if n.Kind == KindCapsule {
			if err := layoutCapsuleChild(n, measure); err != nil {
				return 0, fmt.Errorf("ui: item %d: %w", i, err)
			}
		}
		m += granted[i]
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

func barGrants(items []*Node, main []int, axis Axis, budget, reserve, spacing int) ([]int, int) {
	if axis == Horizontal {
		return sectionGrants(items, main, budget, reserve, spacing)
	}
	// Height is intrinsic for upright content. A width-capped label may
	// ellipsize across the strip, but its line height cannot be compressed.
	fit := fitCount(main, max(0, budget), spacing)
	if fit < len(main) && reserve > 0 {
		fit = fitCount(main, max(0, budget-reserve), spacing)
	}
	granted := make([]int, len(main))
	for i, extent := range main {
		if i < fit {
			granted[i] = extent
		} else {
			granted[i] = -1
		}
	}
	return granted, len(main) - fit
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

// placeTruncating grants each item min(natural, remaining) along the main
// axis, the behaviour the rest of the bar gave up in sysc-313. It exists for
// the single documented case of a centre wider than the whole band, where
// there is no room to drop into.
func placeTruncating(items []*Node, axis Axis, mainOrigin int, content Rect, budget, spacing int, measure MeasureText) error {
	crossOrigin, crossExtent := axis.crossOf(content)
	main, cross, err := measureSection(items, crossExtent, axis, measure)
	if err != nil {
		return err
	}
	remaining := max(0, budget)
	m := mainOrigin
	for i, n := range items {
		if i > 0 {
			if remaining < spacing {
				remaining = 0
			} else {
				m += spacing
				remaining -= spacing
			}
		}
		granted := min(main[i], remaining)
		if axis == Vertical && granted == 0 {
			n.ClipBounds = false
			clearLayoutBounds(n)
			continue
		}
		n.ClipBounds = axis == Vertical && granted < main[i]
		n.Bounds = axis.rect(m, granted, crossOrigin, cross[i], crossExtent)
		if n.Kind == KindCapsule {
			if err := layoutCapsuleChild(n, measure); err != nil {
				return fmt.Errorf("ui: item %d: %w", i, err)
			}
		}
		m += granted
		remaining -= granted
	}
	return nil
}

// measureSection measures every item once, so fitting and placement agree.
func measureSection(items []*Node, crossExtent int, axis Axis, measure MeasureText) (main, cross []int, err error) {
	main = make([]int, len(items))
	cross = make([]int, len(items))
	for i, n := range items {
		if n == nil {
			return nil, nil, fmt.Errorf("ui: nil item %d", i)
		}
		m, c, err := measureBarItem(n, crossExtent, axis, measure)
		if err != nil {
			return nil, nil, fmt.Errorf("ui: item %d: %w", i, err)
		}
		main[i], cross[i] = m, c
	}
	return main, cross, nil
}

func measureBarItem(n *Node, crossExtent int, axis Axis, measure MeasureText) (main, cross int, err error) {
	w, h, err := measureNode(n, crossExtent, measure)
	if err != nil {
		return 0, 0, err
	}
	if axis == Vertical {
		// measureNode's offered height is the strip width here. It can still
		// supply cross width, but ContentHeight owns upright intrinsic height.
		h, err = ContentHeight(n, crossExtent, measure)
		if err != nil {
			return 0, 0, err
		}
		return max(0, h), min(max(0, w), crossExtent), nil
	}
	return max(0, w), min(max(0, h), crossExtent), nil
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

// fittedExtent is the extent the surviving prefix of a section will occupy
// along the main axis. An end-anchored section needs this before it can choose
// its origin, so that dropping an item slides the rest flush to the edge
// instead of leaving a gap where the dropped one would have been.
func fittedExtent(items []*Node, content Rect, axis Axis, budget, reserve, spacing int, measure MeasureText) (int, error) {
	_, crossExtent := axis.crossOf(content)
	main, _, err := measureSection(items, crossExtent, axis, measure)
	if err != nil {
		return 0, err
	}
	granted, _ := barGrants(items, main, axis, budget, reserve, spacing)
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
