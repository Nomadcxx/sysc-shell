package ui

import "fmt"

// ArrangeBar places three sections in one content band and writes each node's
// Bounds.
//
// The centre is pinned to the absolute centre of the band, computed without
// reference to the side widths, so it reads as centred on the monitor rather
// than drifting as its neighbours change. Collision priority is a total order:
// the centre keeps its natural width while it fits; the sides truncate; a
// section with no room renders zero-width; and only a centre wider than the
// whole band truncates, clearing both sides.
func ArrangeBar(content Rect, left, center, right []*Node, spacing int, measure MeasureText) error {
	if content.W < 0 || content.H < 0 {
		return fmt.Errorf("ui: negative content bounds %dx%d", content.W, content.H)
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

func arrangeAnchoredWordmark(content Rect, left, center, right []*Node, markIndex, spacing int, measure MeasureText) error {
	before, mark, after := center[:markIndex], center[markIndex:markIndex+1], center[markIndex+1:]
	wL, err := sectionWidth(left, spacing, content.H, measure)
	if err != nil {
		return fmt.Errorf("ui: left section: %w", err)
	}
	wR, err := sectionWidth(right, spacing, content.H, measure)
	if err != nil {
		return fmt.Errorf("ui: right section: %w", err)
	}
	wBefore, err := sectionWidth(before, spacing, content.H, measure)
	if err != nil {
		return fmt.Errorf("ui: centre before wordmark: %w", err)
	}
	wMark, _, err := measureNode(mark[0], content.H, measure)
	if err != nil {
		return fmt.Errorf("ui: wordmark: %w", err)
	}
	wAfter, err := sectionWidth(after, spacing, content.H, measure)
	if err != nil {
		return fmt.Errorf("ui: centre after wordmark: %w", err)
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
		if err := placeSection(before, beforeStart, content, beforeGrant, spacing, measure); err != nil {
			return fmt.Errorf("ui: centre before wordmark: %w", err)
		}
	} else if len(before) > 0 {
		if err := placeSection(before, content.X, content, 0, spacing, measure); err != nil {
			return fmt.Errorf("ui: centre before wordmark: %w", err)
		}
	}
	if err := placeSection(mark, markX, content, wMark, spacing, measure); err != nil {
		return fmt.Errorf("ui: wordmark: %w", err)
	}
	afterStart, afterBudget := markRight, 0
	if afterActive {
		afterBudget = max(content.X+content.W-markRight-spacing, 0)
		afterGrant := min(wAfter, afterBudget)
		afterStart = min(markRight+spacing, content.X+content.W)
		if err := placeSection(after, afterStart, content, afterGrant, spacing, measure); err != nil {
			return fmt.Errorf("ui: centre after wordmark: %w", err)
		}
		afterBudget = afterGrant
	} else if len(after) > 0 {
		if err := placeSection(after, markRight, content, 0, spacing, measure); err != nil {
			return fmt.Errorf("ui: centre after wordmark: %w", err)
		}
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
	if err := placeSection(left, content.X, content, min(wL, leftMax), spacing, measure); err != nil {
		return fmt.Errorf("ui: left section: %w", err)
	}
	rightMax := max(0, content.X+content.W-compositionRight-spacing)
	return placeSection(right, content.X+content.W-min(wR, rightMax), content,
		min(wR, rightMax), spacing, measure)
}

func arrangeBarSections(content Rect, left, center, right []*Node, spacing int, measure MeasureText) error {
	wL, err := sectionWidth(left, spacing, content.H, measure)
	if err != nil {
		return fmt.Errorf("ui: left section: %w", err)
	}
	wC, err := sectionWidth(center, spacing, content.H, measure)
	if err != nil {
		return fmt.Errorf("ui: center section: %w", err)
	}
	wR, err := sectionWidth(right, spacing, content.H, measure)
	if err != nil {
		return fmt.Errorf("ui: right section: %w", err)
	}

	// Only a centre that alone exceeds the band truncates, and it then takes
	// the whole band; the sides have nowhere left to go.
	if wC > content.W {
		if err := placeSection(center, content.X, content, content.W, spacing, measure); err != nil {
			return err
		}
		if err := placeSection(left, content.X, content, 0, spacing, measure); err != nil {
			return err
		}
		return placeSection(right, content.X+content.W, content, 0, spacing, measure)
	}

	centerX := content.X + (content.W-wC)/2
	leftMax := max(0, centerX-content.X-spacing)
	rightMax := max(0, content.X+content.W-(centerX+wC)-spacing)

	if err := placeSection(center, centerX, content, wC, spacing, measure); err != nil {
		return err
	}
	if err := placeSection(left, content.X, content, min(wL, leftMax), spacing, measure); err != nil {
		return err
	}
	granted := min(wR, rightMax)
	return placeSection(right, content.X+content.W-granted, content, granted, spacing, measure)
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
// An item granted less than its natural width is truncated by the painter,
// which owns cluster measurement. An item with no room left is placed
// zero-wide rather than negative, so its bounds stay valid for hit testing.
func placeSection(items []*Node, x int, content Rect, budget, spacing int, measure MeasureText) error {
	remaining := max(0, budget)
	for i, n := range items {
		if n == nil {
			return fmt.Errorf("ui: nil item %d", i)
		}
		if i > 0 {
			if remaining < spacing {
				remaining = 0
			} else {
				x += spacing
				remaining -= spacing
			}
		}
		w, h, err := measureNode(n, content.H, measure)
		if err != nil {
			return fmt.Errorf("ui: item %d: %w", i, err)
		}
		granted := min(max(0, w), remaining)
		if h > content.H {
			h = content.H
		}
		if h < 0 {
			h = 0
		}
		n.Bounds = Rect{X: x, Y: content.Y + (content.H-h)/2, W: granted, H: h}
		// A section places items itself rather than through Layout, so a
		// capsule's contents are arranged here too. Without this a bar paints
		// empty pills.
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
