package ui

import "fmt"

// LayoutColumn arranges a column root: padding inset, children fill the
// content width, stacked top to bottom with Gap between them.
func LayoutColumn(root *Node, bounds Rect, measure MeasureText) error {
	if root == nil {
		return fmt.Errorf("ui: nil root")
	}
	if root.Kind != KindColumn && root.Kind != KindScroll && root.Kind != KindVirtualList && root.Kind != KindDropZone {
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

// ContentHeight is the intrinsic height of a column (or a column child) at
// width. Panel surfaces use it so the window matches the tree instead of a
// guessed size that clips the last card.
func ContentHeight(n *Node, width int, measure MeasureText) (int, error) {
	return columnChildHeight(n, width, measure)
}

func columnChildHeight(n *Node, width int, measure MeasureText) (int, error) {
	switch n.Kind {
	case KindText, KindTab:
		_, h := measure(n.Text, n.Tabular)
		return h, nil
	case KindMeter:
		if n.Value < 0 || n.Value > 1 {
			return 0, fmt.Errorf("meter value %v is outside zero through one", n.Value)
		}
		if n.Height > 0 {
			return n.Height, nil
		}
		return MeterHeight, nil
	case KindCapsule:
		// A capsule in a column is its child plus padding. The design does not
		// use one here yet; the case exists so placing one cannot crash a
		// surface the way an unmeasurable kind did.
		if len(n.Children) == 0 {
			return n.Width, nil
		}
		if n.Width > 0 {
			width = n.Width
		}
		h, err := columnChildHeight(n.Children[0], max(width-2*n.Padding, 0), measure)
		if err != nil {
			return 0, err
		}
		return h + 2*n.Padding, nil
	case KindGraph:
		// Width is the graph's measured width in a row. Reusing it as a height
		// makes the monitor popout's 240-wide sparkline 240 tall.
		return GraphHeight, nil
	case KindSeparator:
		return 1, nil
	case KindButton:
		_, h, err := measureButton(n, measure)
		return h, err
	case KindDragSource:
		_, h := measure(n.Text, n.Tabular)
		return h + 2*n.Padding, nil
	case KindIcon:
		return IconSize(n), nil
	case KindSegmented:
		_, h, err := measureSegmented(n, measure)
		return h, err
	case KindImage:
		if n.ImageSize > 0 {
			return n.ImageSize, nil
		}
		_, h := measure("", n.Tabular)
		return h, nil
	case KindToggle:
		if n.Role == "checkbox" {
			return CheckboxSize, nil
		}
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
		if n.Height > 0 {
			return n.Height, nil
		}
		sample := n.Text + n.Preedit
		if sample == "" {
			sample = " "
		}
		if n.Multiline {
			lines := 1
			for i := 0; i < len(sample); i++ {
				if sample[i] == '\n' {
					lines++
				}
			}
			_, lh := measure(" ", n.Tabular)
			if lh <= 0 {
				lh = 16
			}
			return lines*lh + 2*n.Padding, nil
		}
		_, h := measure(sample, n.Tabular)
		return h + 2*n.Padding, nil
	case KindScroll, KindVirtualList:
		if n.Height > 0 {
			return n.Height, nil
		}
		if n.Bounds.H > 0 {
			return n.Bounds.H, nil
		}
		return 240, nil
	case KindRow:
		// Ask for each child's intrinsic height, not measureNode's. That one
		// answers "how tall is this in the band offered", and a column has no
		// band to offer: passing a sentinel made every kind that fills its
		// band -- capsule, meter, graph, separator, nested column -- report the
		// sentinel itself, so a card in a two-up grid measured 1048576 tall.
		maxH := 0
		for _, c := range n.Children {
			if c == nil {
				continue
			}
			h, err := columnChildHeight(c, width, measure)
			if err != nil {
				return 0, err
			}
			if h > maxH {
				maxH = h
			}
		}
		return maxH + 2*n.Padding, nil
	case KindColumn, KindDropZone:
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

func pinRowEnd(n *Node, box Rect) {
	if len(n.Children) != 2 {
		return
	}
	first, last := n.Children[0], n.Children[1]
	if first == nil || last == nil || first.Kind != KindText {
		return
	}
	right := box.X + box.W - n.Padding
	if last.Bounds.X+last.Bounds.W >= right {
		return
	}
	last.Bounds.X = right - last.Bounds.W
}

func placeColumnChild(n *Node, box Rect, measure MeasureText) error {
	switch n.Kind {
	case KindRow:
		if err := Layout(n, box, measure); err != nil {
			return err
		}
		pinRowEnd(n, box)
		return nil
	case KindCapsule:
		// A capsule in a column is a card: its child fills the padded inner
		// box. Measuring already accounted for the child, so a capsule that
		// placed only itself painted a rounded fill with nothing inside it.
		n.Bounds = box
		if len(n.Children) == 0 || n.Children[0] == nil {
			return nil
		}
		inner := Rect{
			X: box.X + n.Padding, Y: box.Y + n.Padding,
			W: max(box.W-2*n.Padding, 0), H: max(box.H-2*n.Padding, 0),
		}
		return placeColumnChild(n.Children[0], inner, measure)
	case KindButton:
		n.Bounds = box
		return layoutButtonContent(n, measure, n.Height > 0)
	case KindSegmented:
		n.Bounds = box
		return layoutSegmented(n, measure)
	case KindColumn, KindDropZone:
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
	root.Bounds = bounds
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
