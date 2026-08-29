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
		x += granted
		remaining -= granted
	}
	return nil
}
