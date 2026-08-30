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
		if w < 0 || h < 0 || h > content.H || x+w > content.X+content.W {
			return fmt.Errorf("ui: child %d of kind %d does not fit in %dx%d", i, child.Kind, content.W, content.H)
		}

		child.Bounds = Rect{X: x, Y: content.Y + (content.H-h)/2, W: w, H: h}
		x += w
	}
	return nil
}

// measureNode reports the logical size of one leaf node. A meter fills the row
// content height; a button pads its text on every side.
func measureNode(n *Node, contentHeight int, measure MeasureText) (int, int, error) {
	switch n.Kind {
	case KindText:
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
	case KindButton:
		w, h := measure(n.Text, n.Tabular)
		return w + 2*n.Padding, h + 2*n.Padding, nil
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
