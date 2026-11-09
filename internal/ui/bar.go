package ui

import (
	"fmt"
	"math"
)

// BarOverflow counts the items each section could not place. The shell decides
// what to show for them; layout only reports the number, the way arrangeTray
// hands back the items it could not fit rather than drawing chrome itself.
type BarOverflow struct {
	Left   int
	Center int
	Right  int
	// LeftStrip and RightStrip describe an end section that dropped items,
	// so the shell can reveal its tail; zero when the section fits.
	LeftStrip, RightStrip Strip
}

// Side names a bar's end section by where it sits on the main axis: the
// start (left or upper) section or the end (right or lower) one.
type Side uint8

const (
	SideStart Side = iota
	SideEnd
)

// Strip is an overflowing end section seen as one run of whole items at
// their natural extents, in declaration order. When a section overflows
// every item it places already has its natural extent and the dropped items
// are exactly its tail, so the resting layout is this run's prefix.
type Strip struct {
	// Total is the whole run; Budget the room the section was granted.
	Total, Budget int
	// Rest is the prefix placed at rest; Tail the longest whole-item suffix
	// that fits the budget.
	Rest, Tail int
}

// MaxShift is how far the run moves toward the main-axis start to show its
// tail flush to the section's outer edge.
func (s Strip) MaxShift(side Side) int {
	if side == SideEnd {
		return s.Total - s.Rest
	}
	return s.Total - s.Tail
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
	if over.Left > 0 {
		if over.LeftStrip, err = sectionStrip(left, axis, content, min(wL, leftMax), spacing, measure); err != nil {
			return over, fmt.Errorf("ui: left section: %w", err)
		}
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
	if over.Right > 0 {
		if over.RightStrip, err = sectionStrip(right, axis, content, granted, spacing, measure); err != nil {
			return over, fmt.Errorf("ui: right section: %w", err)
		}
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
	granted, dropped, err := barGrants(items, main, axis, budget, reserve, spacing, measure)
	if err != nil {
		return 0, err
	}

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
		if axis == Vertical && subtreeExceedsCross(n, n.Bounds, axis) {
			n.ClipBounds = true
		}
		m += granted[i]
	}
	return dropped, nil
}

// subtreeExceedsCross detects upright content that would paint outside its
// granted cross-axis box; the outer item then clips paint to that box.
func subtreeExceedsCross(n *Node, bounds Rect, axis Axis) bool {
	if n == nil {
		return false
	}
	if n.Bounds.W > 0 && n.Bounds.H > 0 {
		start, size := axis.crossOf(n.Bounds)
		clipStart, clipSize := axis.crossOf(bounds)
		if start < clipStart || start+size > clipStart+clipSize {
			return true
		}
	}
	for _, child := range n.Children {
		if subtreeExceedsCross(child, bounds, axis) {
			return true
		}
	}
	return false
}

// elasticLegible is the narrowest box, in logical pixels, a squeezed elastic
// item is shown in: a few characters and an ellipsis, padding included.
const elasticLegible = 64

// elastic reports whether a node is built to shrink rather than disappear.
// Declaring a width cap is the marker: in this bar that is the focused-window
// title, the media title and the weather line, each of which ellipsizes inside
// whatever box it is given. A fixed item has one right size and is better
// dropped and counted than shown as a sliver.
func elastic(n *Node) bool {
	text, _ := elasticText(n)
	return text != nil
}

// elasticText finds the width-capped node a squeezed item narrows, and the row
// holding it when it shares its pill with fixed members.
//
// A capsule is chrome wrapped around one child, so it is as elastic as what it
// holds. Without descending, every capsuled widget reads as fixed and the bar
// drops the very titles this rule exists to keep. A row inside a pill is
// elastic when exactly one member is a capped text: the media widget is art, a
// track title and a play button, and only the title can give up width. A row
// with two capped texts has no single place to take width from, so it stays
// fixed.
func elasticText(n *Node) (text, row *Node) {
	if n == nil {
		return nil, nil
	}
	if n.MaxWidth > 0 {
		return n, nil
	}
	if n.Kind != KindCapsule {
		return nil, nil
	}
	for _, c := range n.Children {
		if text, row := elasticText(c); text != nil {
			return text, row
		}
		if c != nil && c.Kind == KindRow {
			if text := rowCappedText(c); text != nil {
				return text, c
			}
		}
	}
	return nil, nil
}

// rowCappedText returns a row's one width-capped text member, or nil when it
// has none or several.
func rowCappedText(row *Node) *Node {
	var found *Node
	for _, c := range row.Children {
		if c == nil || c.MaxWidth <= 0 {
			continue
		}
		if c.Kind != KindText || found != nil {
			return nil
		}
		found = c
	}
	return found
}

// elasticFloors is the narrowest width each elastic item stays legible at: a
// few characters of its text, plus whatever shares a row with that text and
// cannot shrink, such as the media widget's art and play button. Fixed items
// get zero. An item narrower than its floor keeps its natural width as the
// floor: it can be shown whole or left out, not narrowed.
func elasticFloors(items []*Node, widths []int, measure MeasureText) ([]int, error) {
	floors := make([]int, len(items))
	for i, n := range items {
		text, row := elasticText(n)
		if text == nil {
			continue
		}
		floor := elasticLegible
		if row != nil {
			rowW, _, err := measureNode(row, 0, measure)
			if err != nil {
				return nil, fmt.Errorf("ui: item %d: %w", i, err)
			}
			textW, _, err := measureNode(text, 0, measure)
			if err != nil {
				return nil, fmt.Errorf("ui: item %d: %w", i, err)
			}
			floor += max(rowW-textW, 0)
		}
		floors[i] = min(widths[i], max(floor, n.MinWidth))
	}
	return floors, nil
}

// sectionGrants decides each item's width, returning -1 for a dropped item and
// how many were dropped. The reserve is charged only when something overflows
// without it.
func sectionGrants(items []*Node, widths, floors []int, budget, reserve, spacing int) ([]int, int) {
	granted, dropped := grantsWithin(items, widths, floors, max(0, budget), spacing)
	if dropped > 0 && reserve > 0 {
		granted, dropped = grantsWithin(items, widths, floors, max(0, budget-reserve), spacing)
	}
	return granted, dropped
}

func barGrants(items []*Node, main []int, axis Axis, budget, reserve, spacing int, measure MeasureText) ([]int, int, error) {
	if axis == Horizontal {
		floors, err := elasticFloors(items, main, measure)
		if err != nil {
			return nil, 0, err
		}
		granted, dropped := sectionGrants(items, main, floors, budget, reserve, spacing)
		return granted, dropped, nil
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
	return granted, len(main) - fit, nil
}

// grantsWithin gives every fixed item its natural width and lets the elastic
// ones share what is left, so a narrow bar keeps its title -- narrowed -- and
// its chips, instead of dropping whichever came last. Only when the fixed items
// alone do not fit does the section fall back to dropping whole items from the
// far end.
//
// Each elastic item is first given its floor, the narrowest width it stays
// legible at, and the rest is shared evenly, each capped at its natural width.
// A narrowed title is present and legible in a way a dropped one is not; below
// its floor it would read as an empty pill. So when the floors do not all fit,
// elastic items are left out one at a time, lowest floor first: a bare title
// goes before a pill that also carries controls, such as the media widget's
// play button. A left-out elastic item is not counted as overflow: overflow
// marks a cut at the section's tail (overflowFades), and nothing after the
// title was cut.
func grantsWithin(items []*Node, widths, floors []int, budget, spacing int) ([]int, int) {
	granted := make([]int, len(items))
	if len(items) == 0 {
		return granted, 0
	}
	fixedTotal, fixedCount := 0, 0
	var shown []int
	for i, n := range items {
		if elastic(n) {
			shown = append(shown, i)
			continue
		}
		fixedTotal += widths[i]
		fixedCount++
	}

	for {
		count := fixedCount + len(shown)
		room := budget - fixedTotal - spacing*max(count-1, 0)
		floorTotal := 0
		for _, i := range shown {
			floorTotal += floors[i]
		}
		if room >= floorTotal {
			for i, n := range items {
				if elastic(n) {
					granted[i] = -1
				} else {
					granted[i] = widths[i]
				}
			}
			shareAboveFloors(granted, shown, widths, floors, room-floorTotal)
			return granted, 0
		}
		if len(shown) == 0 {
			break
		}
		lowest := 0
		for k, i := range shown {
			if floors[i] <= floors[shown[lowest]] {
				lowest = k
			}
		}
		shown = append(shown[:lowest], shown[lowest+1:]...)
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

// shareAboveFloors grants each shown elastic item its floor and then shares
// spare evenly among them, each capped at its natural width, handing what a
// capped item cannot use to the others.
func shareAboveFloors(granted, shown, widths, floors []int, spare int) {
	open := make([]int, 0, len(shown))
	for _, i := range shown {
		granted[i] = floors[i]
		if granted[i] < widths[i] {
			open = append(open, i)
		}
	}
	for len(open) > 0 && spare > 0 {
		share := spare / len(open)
		if share == 0 {
			return
		}
		next := open[:0]
		for _, i := range open {
			add := min(share, widths[i]-granted[i])
			granted[i] += add
			spare -= add
			if granted[i] < widths[i] {
				next = append(next, i)
			}
		}
		open = next
	}
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
		if axis == Vertical && subtreeExceedsCross(n, n.Bounds, axis) {
			n.ClipBounds = true
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
	granted, _, err := barGrants(items, main, axis, budget, reserve, spacing, measure)
	if err != nil {
		return 0, err
	}
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

// sectionStrip measures a section that dropped items as one run. It is zero
// when not even one whole item fits the budget.
func sectionStrip(items []*Node, axis Axis, content Rect, budget, spacing int, measure MeasureText) (Strip, error) {
	_, crossExtent := axis.crossOf(content)
	main, _, err := measureSection(items, crossExtent, axis, measure)
	if err != nil {
		return Strip{}, err
	}
	s := Strip{Budget: budget}
	for i, m := range main {
		if i > 0 {
			s.Total += spacing
		}
		s.Total += m
	}
	for i, m := range main[:fitCount(main, budget, spacing)] {
		if i > 0 {
			s.Rest += spacing
		}
		s.Rest += m
	}
	run := 0
	for i := len(main) - 1; i >= 0; i-- {
		next := run + main[i]
		if run > 0 {
			next += spacing
		}
		if next > budget {
			break
		}
		run = next
	}
	if run == 0 {
		// No whole item fits at any shift, so there is no tail to reveal.
		return Strip{}, nil
	}
	s.Tail = run
	return s, nil
}

// PlaceStrip places an overflowing end section shifted toward the main-axis
// start by shift (0 is the resting layout, MaxShift the revealed tail) and
// returns the window the section shows. The window opens to the whole budget
// while the run leans and closes on the whole-item tail as it arrives, so both
// resting states show whole items. An item wholly outside the window gets
// zero bounds; one partly inside keeps its bounds for the painter to clip.
func PlaceStrip(items []*Node, axis Axis, content Rect, side Side, s Strip, shift float64, spacing int, measure MeasureText) (Rect, error) {
	mainOrigin, mainExtent := axis.mainOf(content)
	end := mainOrigin + mainExtent
	e := 0.0
	if full := s.MaxShift(side); full > 0 {
		p := min(max(shift/float64(full), 0), 1)
		e = p * p
	}
	lerp := func(a, b int) int { return int(math.Round(float64(a) + float64(b-a)*e)) }
	offset := int(math.Round(shift))
	origin, lo, hi := mainOrigin-offset, mainOrigin, mainOrigin+lerp(s.Rest, s.Tail)
	if side == SideEnd {
		origin, lo, hi = end-s.Rest-offset, end-lerp(s.Budget, s.Tail), end
	}
	if _, err := placeSection(items, axis, origin, content, s.Total, 0, spacing, measure); err != nil {
		return Rect{}, err
	}
	for _, n := range items {
		if p, extent := axis.mainOf(n.Bounds); extent > 0 && (p+extent <= lo || p >= hi) {
			clearLayoutBounds(n)
		}
	}
	crossOrigin, crossExtent := axis.crossOf(content)
	return axis.rect(lo, hi-lo, crossOrigin, crossExtent, crossExtent), nil
}
