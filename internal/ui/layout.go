package ui

import "fmt"

// Layout arranges a row root and its leaf children inside bounds, writing the
// result into each node's Bounds. Children are placed in source order from the
// left content edge and centred vertically in the padded content box.
func Layout(root *Node, bounds Rect, measure MeasureText) error {
	if root == nil {
		return fmt.Errorf("ui: nil root")
	}
	if root.Kind != KindRow {
		return fmt.Errorf("ui: root kind %d is not a row", root.Kind)
	}
	if bounds.W < 0 || bounds.H < 0 {
		return fmt.Errorf("ui: negative bounds %dx%d", bounds.W, bounds.H)
	}

	root.Bounds = bounds

	content := Rect{
		X: bounds.X + root.Padding,
		Y: bounds.Y + root.Padding,
		W: bounds.W - 2*root.Padding,
		H: bounds.H - 2*root.Padding,
	}

	x := content.X
	for i, child := range root.Children {
		if child == nil {
			return fmt.Errorf("ui: nil child %d", i)
		}
		if i > 0 {
			x += root.Gap
		}

		w, h, err := measureNode(child, content.H, measure)
		if err != nil {
			return fmt.Errorf("ui: child %d: %w", i, err)
		}
		switch child.Kind {
		case KindColumn:
			box := Rect{X: x, Y: content.Y, W: w, H: content.H}
			if err := LayoutColumn(child, box, measure); err != nil {
				return fmt.Errorf("ui: child %d: %w", i, err)
			}
		case KindScroll, KindVirtualList:
			if child.Width <= 0 {
				w = content.X + content.W - x
			}
			box := Rect{X: x, Y: content.Y, W: w, H: content.H}
			if err := layoutScroll(child, box, measure); err != nil {
				return fmt.Errorf("ui: child %d: %w", i, err)
			}
		case KindCapsule:
			if w < 0 || h < 0 || h > content.H || x+w > content.X+content.W {
				return fmt.Errorf("ui: child %d of kind %d does not fit in %dx%d", i, child.Kind, content.W, content.H)
			}
			child.Bounds = Rect{X: x, Y: content.Y + (content.H-h)/2, W: w, H: h}
			if err := layoutCapsuleChild(child, measure); err != nil {
				return fmt.Errorf("ui: child %d: %w", i, err)
			}
		default:
			if w < 0 || h < 0 || h > content.H || x+w > content.X+content.W {
				return fmt.Errorf("ui: child %d of kind %d does not fit in %dx%d", i, child.Kind, content.W, content.H)
			}
			child.Bounds = Rect{X: x, Y: content.Y + (content.H-h)/2, W: w, H: h}
		}
		x += child.Bounds.W
	}
	return nil
}

// layoutCapsuleChild centres a capsule's single child inside its padded inner
// box. A nested row or column is arranged in that box so a capsule can hold the
// workspace dot row.
func layoutCapsuleChild(n *Node, measure MeasureText) error {
	if len(n.Children) == 0 {
		return nil
	}
	child := n.Children[0]
	if child == nil {
		return fmt.Errorf("capsule has a nil child")
	}
	inner := Rect{
		X: n.Bounds.X + n.Padding,
		Y: n.Bounds.Y + n.Padding,
		W: max(n.Bounds.W-2*n.Padding, 0),
		H: max(n.Bounds.H-2*n.Padding, 0),
	}
	switch child.Kind {
	case KindRow:
		// A section can grant a capsule less than it measured when the band is
		// tight. The row is then laid out at its natural width and clipped by
		// the capsule rather than failing the whole surface: a squeezed bar
		// should degrade, not refuse to configure.
		w, h, err := measureNode(child, inner.H, measure)
		if err != nil {
			return err
		}
		box := inner
		if w > box.W {
			box.W = w
		}
		if h > box.H {
			// Grow around the inner band's centre rather than downward from
			// its top, or members centre on the grown box and sit low in the
			// visible capsule.
			box.Y -= (h - box.H) / 2
			box.H = h
		}
		return Layout(child, box, measure)
	case KindColumn:
		return LayoutColumn(child, inner, measure)
	}
	w, h, err := measureNode(child, inner.H, measure)
	if err != nil {
		return err
	}
	child.Bounds = Rect{X: inner.X, Y: inner.Y + (inner.H-h)/2, W: w, H: h}
	return nil
}

// measureNode reports the logical size of one leaf node. A meter fills the row
// content height; a button pads its text on every side.
func measureNode(n *Node, contentHeight int, measure MeasureText) (int, int, error) {
	switch n.Kind {
	case KindText, KindTab:
		w, h := measure(n.Text, n.Tabular)
		if n.MinWidthText != "" {
			if floor, _ := measure(n.MinWidthText, n.Tabular); floor > w {
				w = floor
			}
		}
		if n.MaxWidth > 0 && w > n.MaxWidth {
			w = n.MaxWidth
		}
		return w, h, nil
	case KindMeter:
		if n.Value < 0 || n.Value > 1 {
			return 0, 0, fmt.Errorf("meter value %v is outside zero through one", n.Value)
		}
		return n.Width, contentHeight, nil
	case KindGraph:
		// A graph reserves its configured width and the full content height,
		// the way a meter does. It does not measure its data, so a bar does
		// not reflow as samples arrive.
		return n.Width, contentHeight, nil
	case KindSeparator:
		return 1, contentHeight, nil
	case KindRow:
		// A nested row is as wide as its children plus the gaps between them.
		// The outer root is arranged by Layout and never measured here; this
		// case exists for a row inside a capsule, which is how the workspace
		// pill strip is built.
		w := 2 * n.Padding
		tallest := 0
		for i, child := range n.Children {
			if child == nil {
				return 0, 0, fmt.Errorf("row child %d is nil", i)
			}
			if i > 0 {
				w += n.Gap
			}
			cw, ch, err := measureNode(child, max(contentHeight-2*n.Padding, 0), measure)
			if err != nil {
				return 0, 0, err
			}
			w += cw
			if ch > tallest {
				tallest = ch
			}
		}
		// Report the tallest child rather than the band offered. A caller that
		// clamps a nested row to the offered height would otherwise crop text
		// measured at the physical size, which rounds up.
		h := tallest + 2*n.Padding
		if h < contentHeight {
			h = contentHeight
		}
		return w, h, nil
	case KindCapsule:
		// An empty capsule is a dot: square, sized by Width.
		if len(n.Children) == 0 {
			if n.Width <= 0 {
				return 0, 0, nil
			}
			return n.Width, n.Width, nil
		}
		if len(n.Children) != 1 {
			return 0, 0, fmt.Errorf("capsule has %d children, want one", len(n.Children))
		}
		inner := max(contentHeight-2*n.Padding, 0)
		w, _, err := measureNode(n.Children[0], inner, measure)
		if err != nil {
			return 0, 0, err
		}
		// A zero-width child leaves no pill at all, so an empty window title
		// does not paint a bare capsule.
		if w == 0 {
			return 0, 0, nil
		}
		return w + 2*n.Padding, contentHeight, nil
	case KindButton:
		w, h := measure(n.Text, n.Tabular)
		return w + 2*n.Padding, h + 2*n.Padding, nil
	case KindImage:
		size := n.ImageSize
		if size <= 0 {
			size = contentHeight
		}
		return size, size, nil
	case KindToggle:
		return ToggleWidth, ToggleHeight, nil
	case KindSlider:
		w := n.Width
		if w <= 0 {
			w = 160
		}
		return w, SliderKnob, nil
	case KindMenu:
		w, h := measure(n.Text, n.Tabular)
		if n.Width > w {
			w = n.Width
		}
		return w, h, nil
	case KindTextField:
		sample := n.Text + n.Preedit
		if sample == "" {
			sample = " "
		}
		w, h := measure(sample, n.Tabular)
		if n.Width > w {
			w = n.Width
		}
		return w, h + 2*n.Padding, nil
	case KindScroll, KindVirtualList:
		w := n.Width
		if w <= 0 {
			w = 400
		}
		return w, contentHeight, nil
	case KindColumn:
		w := n.Width
		if w <= 0 {
			w = 220
		}
		return w, contentHeight, nil
	default:
		return 0, 0, fmt.Errorf("unsupported kind %d", n.Kind)
	}
}

// Hit reports the action of the topmost arranged node containing the point.
//
// Children are searched in reverse source order, which is reverse paint order,
// and the search descends so a nested section resolves to its leaf rather than
// stopping at the container.
func Hit(root *Node, x, y int) (string, bool) {
	if root == nil || !root.Bounds.Contains(x, y) {
		return "", false
	}
	for i := len(root.Children) - 1; i >= 0; i-- {
		if action, ok := Hit(root.Children[i], x, y); ok {
			return action, true
		}
	}
	return root.Action, root.Action != ""
}
