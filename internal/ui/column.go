package ui

import "fmt"

// LayoutColumn arranges a column root: padding inset, children fill the
// content width, stacked top to bottom with Gap between them.
func LayoutColumn(root *Node, bounds Rect, measure MeasureText) error {
	if root == nil {
		return fmt.Errorf("ui: nil root")
	}
	if root.Kind != KindColumn && root.Kind != KindScroll && root.Kind != KindVirtualList {
		return fmt.Errorf("ui: root kind %d is not a column", root.Kind)
	}
	if bounds.W < 0 || bounds.H < 0 {
		return fmt.Errorf("ui: negative bounds %dx%d", bounds.W, bounds.H)
	}
	root.Bounds = bounds
	if root.Kind == KindScroll || root.Kind == KindVirtualList {
		return layoutScroll(root, bounds, measure)
	}
	content := Rect{
		X: bounds.X + root.Padding,
		Y: bounds.Y + root.Padding,
		W: bounds.W - 2*root.Padding,
		H: bounds.H - 2*root.Padding,
	}
	y := content.Y
	for i, child := range root.Children {
		if child == nil {
			return fmt.Errorf("ui: nil child %d", i)
		}
		if i > 0 {
			y += root.Gap
		}
		h, err := columnChildHeight(child, content.W, measure)
		if err != nil {
			return fmt.Errorf("ui: child %d: %w", i, err)
		}
		box := Rect{X: content.X, Y: y, W: content.W, H: h}
		if err := placeColumnChild(child, box, measure); err != nil {
			return fmt.Errorf("ui: child %d: %w", i, err)
		}
		y += h
	}
	return nil
}

func columnChildHeight(n *Node, width int, measure MeasureText) (int, error) {
	switch n.Kind {
	case KindText, KindTab:
		_, h := measure(n.Text, n.Tabular)
		return h, nil
	case KindSeparator:
		return 1, nil
	case KindButton:
		_, h := measure(n.Text, n.Tabular)
		return h + 2*n.Padding, nil
	case KindImage:
		if n.ImageSize > 0 {
			return n.ImageSize, nil
		}
		_, h := measure("", n.Tabular)
		return h, nil
	case KindToggle:
		return ToggleHeight, nil
	case KindSlider:
		return SliderKnob, nil
	case KindMenu:
		_, h := measure(n.Text, n.Tabular)
		for _, c := range n.Children {
			if c == nil {
				continue
			}
			ch, err := columnChildHeight(c, width, measure)
			if err != nil {
				return 0, err
			}
			h += ch
		}
		return h, nil
	case KindTextField:
		sample := n.Text + n.Preedit
		if sample == "" {
			sample = " "
		}
		_, h := measure(sample, n.Tabular)
		return h + 2*n.Padding, nil
	case KindScroll, KindVirtualList:
		if n.Bounds.H > 0 {
			return n.Bounds.H, nil
		}
		return 240, nil
	case KindRow:
		maxH := 0
		for _, c := range n.Children {
			if c == nil {
				continue
			}
			_, h, err := measureNode(c, 1<<20, measure)
			if err != nil {
				return 0, err
			}
			if h > maxH {
				maxH = h
			}
		}
		return maxH + 2*n.Padding, nil
	case KindColumn:
		h := 2 * n.Padding
		for i, c := range n.Children {
			if c == nil {
				continue
			}
			ch, err := columnChildHeight(c, width-2*n.Padding, measure)
			if err != nil {
				return 0, err
			}
			if i > 0 {
				h += n.Gap
			}
			h += ch
		}
		return h, nil
	default:
		return 0, fmt.Errorf("unsupported kind %d", n.Kind)
	}
}

func placeColumnChild(n *Node, box Rect, measure MeasureText) error {
	switch n.Kind {
	case KindRow:
		return Layout(n, box, measure)
	case KindColumn:
		return LayoutColumn(n, box, measure)
	case KindScroll, KindVirtualList:
		return layoutScroll(n, box, measure)
	case KindMenu:
		n.Bounds = box
		_, fh := measure(n.Text, n.Tabular)
		y := box.Y + fh
		for _, c := range n.Children {
			if c == nil {
				continue
			}
			h, err := columnChildHeight(c, box.W, measure)
			if err != nil {
				return err
			}
			c.Bounds = Rect{X: box.X, Y: y, W: box.W, H: h}
			y += h
		}
		return nil
	default:
		n.Bounds = box
		return nil
	}
}

func layoutScroll(root *Node, bounds Rect, measure MeasureText) error {
	content := Rect{
		X: bounds.X + root.Padding,
		Y: bounds.Y + root.Padding,
		W: bounds.W - 2*root.Padding,
		H: bounds.H - 2*root.Padding,
	}
	if root.Kind == KindVirtualList {
		if root.ItemHeight <= 0 {
			root.ItemHeight = 1
		}
		root.ContentH = root.ItemCount * root.ItemHeight
		clampScroll(root)
		lo, hi := VisibleRange(root)
		if root.Item != nil {
			root.Children = root.Children[:0]
			for i := lo; i < hi; i++ {
				child := root.Item(i)
				if child == nil {
					continue
				}
				root.Children = append(root.Children, child)
			}
		}
		y := content.Y - (root.ScrollOffset - lo*root.ItemHeight)
		for i, child := range root.Children {
			if child == nil {
				return fmt.Errorf("ui: nil child %d", i)
			}
			box := Rect{X: content.X, Y: y, W: content.W, H: root.ItemHeight}
			if err := placeColumnChild(child, box, measure); err != nil {
				return err
			}
			y += root.ItemHeight
		}
		return nil
	}
	h := 0
	for i, child := range root.Children {
		if child == nil {
			return fmt.Errorf("ui: nil child %d", i)
		}
		ch, err := columnChildHeight(child, content.W, measure)
		if err != nil {
			return err
		}
		if i > 0 {
			h += root.Gap
		}
		h += ch
	}
	root.ContentH = h
	clampScroll(root)
	y := content.Y - root.ScrollOffset
	for i, child := range root.Children {
		ch, err := columnChildHeight(child, content.W, measure)
		if err != nil {
			return err
		}
		if i > 0 {
			y += root.Gap
		}
		if err := placeColumnChild(child, Rect{X: content.X, Y: y, W: content.W, H: ch}, measure); err != nil {
			return err
		}
		y += ch
	}
	return nil
}
