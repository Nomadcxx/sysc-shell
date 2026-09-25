package ui

import (
	"fmt"
	"strconv"
)

const defaultIconSize = 20

// label names a node for a rejection message: the kind, the text it carries
// when it carries any, and the wire path the converter stamped when there is
// one. It is the string a plugin author reads in the journal to find the node
// the host refused.
func label(n *Node) string {
	if n == nil {
		return "nil"
	}
	out := n.Kind.String()
	if n.Text != "" {
		text := n.Text
		if len(text) > 24 {
			text = text[:24] + "…"
		}
		out += " " + strconv.Quote(text)
	}
	switch {
	case n.Path != "":
		return out + " at " + n.Path
	case n.Key != "":
		return out + " keyed " + strconv.Quote(n.Key)
	}
	return out
}

// fitError is the one wording for "this child cannot live in the box its
// parent offered": the parent that offered it, the child that missed, and the
// tail the older docs quote kept byte-identical.
func fitError(parent *Node, i int, child *Node, content Rect) error {
	return fmt.Errorf("ui: %s: child %d of kind %d (%s) does not fit in %dx%d",
		label(parent), i, child.Kind, label(child), content.W, content.H)
}

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
		remain := content.X + content.W - x
		if remain < 0 && (child.Kind == KindText || child.Kind == KindTab) {
			// Text may be clipped after a fixed leading child already consumed
			// the row. Controls keep the error so an invisible action cannot be
			// laid out as if it were usable.
			remain = 0
		}
		if root.PinEnd && len(root.Children) == 2 && i == 0 {
			if root.Children[1] == nil {
				return fmt.Errorf("ui: nil child 1")
			}
			end := root.Children[1]
			endW, _, err := measureNode(end, content.H, measure)
			if err != nil {
				return fmt.Errorf("ui: child 1: %w", err)
			}
			// Reserve the trailing child before clipping its leading content.
			// Text may be clipped when it is wider than the row; controls keep
			// their natural width so an invisible action still fails layout.
			if end.Kind == KindText || end.Kind == KindTab {
				available := max(remain-root.Gap, 0)
				endW = min(endW, available)
			}
			remain -= root.Gap + endW
			if remain < 0 && (end.Kind == KindText || end.Kind == KindTab) {
				remain = 0
			}
		}
		switch child.Kind {
		case KindScheduleGrid:
			if w <= 0 {
				w = remain
			}
			child.Bounds = Rect{X: x, Y: content.Y, W: min(w, remain), H: content.H}
			if err := layoutScheduleGrid(child, child.Bounds, measure); err != nil {
				return fmt.Errorf("ui: child %d: %w", i, err)
			}
		case KindColumn:
			if child.Width <= 0 && (i == len(root.Children)-1 || root.PinEnd) {
				w = remain
			}
			box := Rect{X: x, Y: content.Y, W: min(w, remain), H: content.H}
			if err := LayoutColumn(child, box, measure); err != nil {
				return fmt.Errorf("ui: child %d: %w", i, err)
			}
		case KindStack:
			if child.Width <= 0 && (i == len(root.Children)-1 || root.PinEnd) {
				w = remain
			}
			box := Rect{X: x, Y: content.Y, W: min(w, remain), H: content.H}
			child.Bounds = box
			if err := layoutStackChildren(child, measure); err != nil {
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
				return fitError(root, i, child, content)
			}
			child.Bounds = Rect{X: x, Y: content.Y + (content.H-h)/2, W: w, H: h}
			if err := layoutCapsuleChild(child, measure); err != nil {
				return fmt.Errorf("ui: child %d: %w", i, err)
			}
		case KindButton, KindDragSource:
			if w < 0 || h < 0 || h > content.H || x+w > content.X+content.W {
				return fitError(root, i, child, content)
			}
			child.Bounds = Rect{X: x, Y: content.Y + (content.H-h)/2, W: w, H: h}
			if err := layoutButtonContent(child, measure, child.Height > 0); err != nil {
				return fmt.Errorf("ui: child %d: %w", i, err)
			}
		case KindSegmented:
			if w < 0 || h < 0 || h > content.H || x+w > content.X+content.W {
				return fitError(root, i, child, content)
			}
			child.Bounds = Rect{X: x, Y: content.Y + (content.H-h)/2, W: w, H: h}
			if err := layoutSegmented(child, measure); err != nil {
				return fmt.Errorf("ui: child %d: %w", i, err)
			}
		case KindMenu:
			if w < 0 || h < 0 || h > content.H || x+w > content.X+content.W {
				return fitError(root, i, child, content)
			}
			box := Rect{X: x, Y: content.Y, W: w, H: h}
			if err := placeColumnChild(child, box, measure); err != nil {
				return fmt.Errorf("ui: child %d: %w", i, err)
			}
		default:
			if w < 0 || h < 0 || h > content.H {
				return fitError(root, i, child, content)
			}
			// Nested rows in a column of known width (a System card cell)
			// must clip overflowing text rather than close the surface.
			if child.Kind == KindRow && child.PinEnd {
				w = remain
			}
			if w > remain {
				w = remain
			}
			if w < 0 {
				return fitError(root, i, child, content)
			}
			child.Bounds = Rect{X: x, Y: content.Y + (content.H-h)/2, W: w, H: h}
			if child.Kind == KindRow {
				if err := placeColumnChild(child, child.Bounds, measure); err != nil {
					return fmt.Errorf("ui: child %d: %w", i, err)
				}
			}
		}
		x += child.Bounds.W
	}
	return nil
}

// IconSize is the logical square a KindIcon occupies. Layout and paint both
// resolve it here so the glyph is rasterised at the size that was measured.
func IconSize(n *Node) int {
	if n == nil {
		return 0
	}
	if n.IconSize > 0 {
		return n.IconSize
	}
	return defaultIconSize
}

func inlineContentSize(n *Node, measure MeasureText) (int, int, error) {
	if len(n.Children) == 0 {
		w, h := measure(n.Text, TextAttrsOf(n))
		return w, h, nil
	}
	w, h := 0, 0
	for i, child := range n.Children {
		if child == nil {
			return 0, 0, fmt.Errorf("button child %d is nil", i)
		}
		cw, ch, err := measureNode(child, max(n.Height-2*n.Padding, 0), measure)
		if err != nil {
			return 0, 0, err
		}
		if i > 0 {
			w += n.Gap
		}
		w += cw
		h = max(h, ch)
	}
	return w, h, nil
}

func measureButton(n *Node, measure MeasureText) (int, int, error) {
	w, h, err := inlineContentSize(n, measure)
	if err != nil {
		return 0, 0, err
	}
	w += 2 * n.Padding
	h += 2 * n.Padding
	if n.Width > 0 {
		w = n.Width
	}
	if n.Height > 0 {
		h = n.Height
	}
	return w, h, nil
}

func layoutButtonContent(n *Node, measure MeasureText, fixedHeight bool) error {
	if len(n.Children) == 0 {
		return nil
	}
	verticalPadding := n.Padding
	if fixedHeight {
		verticalPadding = 0
	}
	inner := Rect{X: n.Bounds.X + n.Padding, Y: n.Bounds.Y + verticalPadding,
		W: max(n.Bounds.W-2*n.Padding, 0), H: max(n.Bounds.H-2*verticalPadding, 0)}
	if len(n.Children) == 1 {
		child := n.Children[0]
		if child == nil {
			return fmt.Errorf("button child 0 is nil")
		}
		switch child.Kind {
		case KindRow:
			if err := Layout(child, inner, measure); err != nil {
				return err
			}
			pinRowEnd(child, inner)
			return nil
		case KindColumn:
			return LayoutColumn(child, inner, measure)
		}
	}
	w, h, err := inlineContentSize(n, measure)
	if err != nil {
		return err
	}
	if w > inner.W || h > inner.H {
		return fmt.Errorf("button content %dx%d does not fit in %dx%d", w, h, inner.W, inner.H)
	}
	x := inner.X + (inner.W-w)/2
	for i, child := range n.Children {
		if i > 0 {
			x += n.Gap
		}
		cw, ch, err := measureNode(child, inner.H, measure)
		if err != nil {
			return err
		}
		child.Bounds = Rect{X: x, Y: inner.Y + (inner.H-ch)/2, W: cw, H: ch}
		x += cw
	}
	return nil
}

func validateSegments(n *Node) error {
	selected := 0
	for i, child := range n.Children {
		if child == nil || child.Kind != KindButton {
			return fmt.Errorf("segment %d is not a button", i)
		}
		if child.State.Has(StateSelected) {
			selected++
		}
	}
	if selected > 1 {
		return fmt.Errorf("segmented control has %d selected children", selected)
	}
	return nil
}

func measureSegmented(n *Node, measure MeasureText) (int, int, error) {
	if err := validateSegments(n); err != nil {
		return 0, 0, err
	}
	// layoutSegmented allocates every segment the same width, so the row needs
	// the widest segment repeated -- not the sum of natural widths. Summing
	// under-measures whenever the labels differ, and the row then fails to lay
	// out inside the very box it asked for.
	widest, h := 0, 0
	for _, child := range n.Children {
		cw, ch, err := measureButton(child, measure)
		if err != nil {
			return 0, 0, err
		}
		widest = max(widest, cw)
		h = max(h, ch)
	}
	w := 2 * n.Padding
	if len(n.Children) > 0 {
		w += widest*len(n.Children) + n.Gap*(len(n.Children)-1)
	}
	h += 2 * n.Padding
	if n.Width > 0 {
		w = n.Width
	}
	if n.Height > 0 {
		h = n.Height
	}
	return w, h, nil
}

func layoutSegmented(n *Node, measure MeasureText) error {
	if err := validateSegments(n); err != nil {
		return err
	}
	if len(n.Children) == 0 {
		return nil
	}
	inner := Rect{X: n.Bounds.X + n.Padding, Y: n.Bounds.Y + n.Padding,
		W: max(n.Bounds.W-2*n.Padding, 0), H: max(n.Bounds.H-2*n.Padding, 0)}
	available := inner.W - n.Gap*(len(n.Children)-1)
	if available < 0 {
		return fmt.Errorf("segmented gaps do not fit in width %d", inner.W)
	}
	base, extra := available/len(n.Children), available%len(n.Children)
	x := inner.X
	for i, child := range n.Children {
		w := base
		if i < extra {
			w++
		}
		child.Bounds = Rect{X: x, Y: inner.Y, W: w, H: inner.H}
		if err := layoutButtonContent(child, measure, true); err != nil {
			return fmt.Errorf("segment %d: %w", i, err)
		}
		x += w + n.Gap
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
	case KindStack:
		child.Bounds = inner
		return layoutStackChildren(child, measure)
	}
	w, h, err := measureNode(child, inner.H, measure)
	if err != nil {
		return err
	}
	child.Bounds = Rect{X: inner.X + max((inner.W-w)/2, 0), Y: inner.Y + (inner.H-h)/2, W: w, H: h}
	return nil
}

// measureNode reports the logical size of one leaf node. A meter fills the row
// content height; a button pads its text on every side.
func measureNode(n *Node, contentHeight int, measure MeasureText) (int, int, error) {
	switch n.Kind {
	case KindEffect:
		if err := n.Effect.Validate(); err != nil {
			return 0, 0, err
		}
		return 0, 0, nil
	case KindText, KindTab:
		w, h := measure(n.Text, TextAttrsOf(n))
		if n.MinWidthText != "" {
			if floor, _ := measure(n.MinWidthText, TextAttrsOf(n)); floor > w {
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
		return n.Width, ownHeight(n, contentHeight), nil
	case KindRadialGauge:
		size := n.Width
		if size <= 0 {
			size = contentHeight
		}
		h := n.Height
		if h <= 0 {
			h = size
		}
		return size, h, nil
	case KindGraph:
		// A graph reserves its configured width and the full content height,
		// the way a meter does. It does not measure its data, so a bar does
		// not reflow as samples arrive. One that names a height keeps it,
		// so a row can set a short sparkline beside two lines of text.
		return n.Width, ownHeight(n, contentHeight), nil
	case KindSeparator:
		return 1, contentHeight, nil
	case KindEdgeFade:
		// A fade is placed over content that is already positioned, never
		// measured into a run: it carries its own Bounds and takes no space of
		// its own. Measuring as zero keeps a row that happens to hold one from
		// reserving width the fade does not use.
		return 0, contentHeight, nil
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
		// An empty capsule is a dot: square, sized by Width. An explicit
		// height overrides the square, so a wider pill can stay glyph-height.
		if len(n.Children) == 0 {
			if n.Width <= 0 {
				return 0, 0, nil
			}
			h := n.Width
			if n.Height > 0 {
				h = n.Height
			}
			return n.Width, h, nil
		}
		if len(n.Children) != 1 {
			return 0, 0, fmt.Errorf("capsule has %d children, want one", len(n.Children))
		}
		inner := max(contentHeight-2*n.Padding, 0)
		w, _, err := measureNode(n.Children[0], inner, measure)
		if err != nil {
			return 0, 0, err
		}
		// An explicit width is a grid cell: two cards share a row evenly and
		// neither is sized by whichever happens to hold the longer figure. A
		// bar pill sets no width and is sized by its content, as before.
		if n.Width > 0 {
			h := contentHeight
			if n.Height > 0 {
				h = n.Height
			}
			return n.Width, h, nil
		}
		// A zero-width child leaves no pill at all, so an empty window title
		// does not paint a bare capsule.
		if w == 0 {
			return 0, 0, nil
		}
		return w + 2*n.Padding, contentHeight, nil
	// A drag source measures exactly as a button does, including composing
	// children. render.paint already draws the two through one path; measuring
	// them differently left a drag source able to carry children that were
	// never given a box, which is an invisible control rather than an error.
	case KindButton, KindDragSource:
		return measureButton(n, measure)
	case KindIcon:
		size := IconSize(n)
		return size, size, nil
	case KindSegmented:
		return measureSegmented(n, measure)
	case KindScheduleGrid:
		if n.Schedule == nil {
			return 0, 0, fmt.Errorf("schedule grid has no range")
		}
		return n.Width, n.Height, nil
	case KindWordmark:
		// The mark is always given an explicit box: the shell derives its
		// width from render.WordmarkAspect so the asset owns its proportions.
		if w, h, ok := imageBox(n); ok {
			return w, h, nil
		}
		return 0, 0, fmt.Errorf("ui: wordmark has no box")
	case KindImage:
		if w, h, ok := imageBox(n); ok {
			return w, h, nil
		}
		size := n.ImageSize
		if size <= 0 {
			size = contentHeight
		}
		return size, size, nil
	case KindToggle:
		if n.Role == "checkbox" {
			return CheckboxSize, CheckboxSize, nil
		}
		return ToggleWidth, ToggleHeight, nil
	case KindSlider:
		w := n.Width
		if w <= 0 {
			w = 160
		}
		return w, SliderKnob, nil
	case KindMenu:
		w, h := measure(n.Text, TextAttrsOf(n))
		if n.Width > w {
			w = n.Width
		}
		for _, c := range n.Children {
			if c == nil {
				continue
			}
			_, ch := measure(c.Text, TextAttrsOf(c))
			h += ch
		}
		return w, h, nil
	case KindTextField:
		// Measured on the displayed runes, not the stored ones: a masked field
		// draws bullets, whose advance differs from the letters behind them.
		sample := DisplayText(n) + DisplayPreedit(n)
		if sample == "" {
			sample = " "
		}
		w, h := measure(sample, TextAttrsOf(n))
		if n.Width > w {
			w = n.Width
		}
		return w, max(n.Height, h+2*n.Padding), nil
	case KindScroll, KindVirtualList:
		w := n.Width
		if w <= 0 {
			w = 400
		}
		return w, contentHeight, nil
	case KindColumn, KindDropZone:
		w := n.Width
		if w <= 0 && n.Kind == KindColumn && len(n.Children) > 0 {
			// A column's intrinsic width is its widest child, not a fixed
			// guess: rows that pack several columns otherwise reserve far
			// more than they need and push trailing controls out of bounds.
			for _, c := range n.Children {
				if c == nil {
					continue
				}
				cw, _, err := measureNode(c, contentHeight, measure)
				if err != nil {
					return 0, 0, err
				}
				w = max(w, cw+2*n.Padding)
			}
		}
		if w <= 0 {
			w = 220
		}
		return w, contentHeight, nil
	case KindStack:
		if len(n.Children) == 0 {
			return 0, 0, nil
		}
		// A stack shares one box, so its intrinsic size is the maximum of its
		// children rather than their sum. Explicit dimensions reserve that
		// dimension, keeping measurement and placement in agreement.
		w, h := 2*n.Padding, 2*n.Padding
		for i, child := range n.Children {
			if child == nil {
				return 0, 0, fmt.Errorf("stack child %d is nil", i)
			}
			cw, ch, err := measureNode(child, contentHeight, measure)
			if err != nil {
				return 0, 0, err
			}
			w = max(w, cw+2*n.Padding)
			h = max(h, ch+2*n.Padding)
		}
		if n.Width > 0 {
			w = n.Width
		}
		if n.Height > 0 {
			h = n.Height
		}
		return w, h, nil
	default:
		return 0, 0, fmt.Errorf("ui: %s: unsupported kind %s", label(n), n.Kind)
	}
}

// layoutStackChildren lays every child into the stack's content box. They
// overlap by design; paint order decides what is visible and Hit's reverse
// walk decides what is clicked.
func layoutStackChildren(n *Node, measure MeasureText) error {
	if len(n.Children) == 0 {
		return nil
	}
	inner := Rect{
		X: n.Bounds.X + n.Padding,
		Y: n.Bounds.Y + n.Padding,
		W: max(n.Bounds.W-2*n.Padding, 0),
		H: max(n.Bounds.H-2*n.Padding, 0),
	}
	for i, child := range n.Children {
		if child == nil {
			return fmt.Errorf("ui: stack child %d is nil", i)
		}
		switch child.Kind {
		case KindColumn:
			if err := LayoutColumn(child, inner, measure); err != nil {
				return err
			}
		case KindRow:
			if err := Layout(child, inner, measure); err != nil {
				return err
			}
		case KindEffect:
			if err := child.Effect.Validate(); err != nil {
				return fmt.Errorf("ui: stack child %d: %w", i, err)
			}
			child.Bounds = inner
		default:
			child.Bounds = inner
		}
	}
	return nil
}

// Hit reports the action of the topmost arranged node containing the point.
//
// Children are searched in reverse source order, which is reverse paint order,
// and the search descends so a nested section resolves to its leaf rather than
// stopping at the container.
func Hit(root *Node, x, y int) (string, bool) {
	if root == nil || root.Kind == KindEffect || !root.Bounds.Contains(x, y) {
		return "", false
	}
	for i := len(root.Children) - 1; i >= 0; i-- {
		if action, ok := Hit(root.Children[i], x, y); ok {
			return action, true
		}
	}
	return root.Action, root.Action != ""
}

// ownHeight is a band node's height in a row: its own when it names one,
// never taller than the row, and the row's content height otherwise.
func ownHeight(n *Node, contentHeight int) int {
	if n.Height > 0 {
		return min(n.Height, contentHeight)
	}
	return contentHeight
}
