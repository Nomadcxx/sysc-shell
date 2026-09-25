package ui

import "fmt"

// FitProblem is one place the host would refuse a tree, named the way a
// rejection names it. Path is the offending child's wire path when the
// converter stamped one.
type FitProblem struct {
	Path    string
	Message string
}

// CheckFit walks a converted tree and reports every child that cannot live in
// the box its parent would offer, using the same arithmetic Layout refuses
// with. Layout stops at the first rejection because a surface that cannot be
// drawn must not half-draw; a checker wants the whole list, so it mirrors the
// placement rules instead of running them.
//
// The mirror is pinned by TestCheckFitAgreesWithLayout: for any tree, a
// violation here exists exactly when Layout returns an error.
func CheckFit(root *Node, bounds Rect, measure MeasureText) []FitProblem {
	var out []FitProblem
	if root == nil {
		return []FitProblem{{Message: "ui: nil root"}}
	}
	if bounds.W < 0 || bounds.H < 0 {
		return []FitProblem{{Message: fmt.Sprintf("ui: negative bounds %dx%d", bounds.W, bounds.H)}}
	}
	checkNode(root, bounds, measure, &out)
	return out
}

func checkNode(n *Node, box Rect, measure MeasureText, out *[]FitProblem) {
	switch n.Kind {
	case KindRow:
		checkRow(n, box, measure, out)
	case KindColumn, KindDropZone:
		checkColumn(n, box, measure, out)
	case KindScroll, KindVirtualList:
		checkScroll(n, box, measure, out)
	case KindScheduleGrid:
		copyOfNode := *n
		copyOfNode.Children = make([]*Node, len(n.Children))
		for i, child := range n.Children {
			if child != nil {
				childCopy := *child
				copyOfNode.Children[i] = &childCopy
			}
		}
		if err := layoutScheduleGrid(&copyOfNode, box, measure); err != nil {
			*out = append(*out, FitProblem{Path: n.Path, Message: "ui: " + err.Error()})
		}
	case KindCapsule, KindStack:
		inner := Rect{X: box.X + n.Padding, Y: box.Y + n.Padding,
			W: max(box.W-2*n.Padding, 0), H: max(box.H-2*n.Padding, 0)}
		for _, c := range n.Children {
			if c != nil {
				checkNode(c, inner, measure, out)
			}
		}
	}
}

// checkRow mirrors Layout's loop, including the two exceptions it grants:
// text clips at the row's edge instead of failing the row, and PinEnd reserves
// the trailing child before the leading text is clipped.
func checkRow(n *Node, box Rect, measure MeasureText, out *[]FitProblem) {
	content := Rect{X: box.X + n.Padding, Y: box.Y + n.Padding,
		W: box.W - 2*n.Padding, H: box.H - 2*n.Padding}
	x := content.X
	for i, child := range n.Children {
		if child == nil {
			*out = append(*out, FitProblem{Message: fmt.Sprintf("ui: %s: nil child %d", label(n), i)})
			continue
		}
		if i > 0 {
			x += n.Gap
		}
		w, h, err := measureNode(child, content.H, measure)
		if err != nil {
			*out = append(*out, FitProblem{Path: child.Path, Message: "ui: " + err.Error()})
			continue
		}
		remain := content.X + content.W - x
		if remain < 0 && (child.Kind == KindText || child.Kind == KindTab) {
			remain = 0
		}
		if n.PinEnd && len(n.Children) == 2 && i == 0 && n.Children[1] != nil {
			end := n.Children[1]
			endW, _, err := measureNode(end, content.H, measure)
			if err == nil {
				if end.Kind == KindText || end.Kind == KindTab {
					endW = min(endW, max(remain-n.Gap, 0))
				}
				remain -= n.Gap + endW
				if remain < 0 && (end.Kind == KindText || end.Kind == KindTab) {
					remain = 0
				}
			}
		}
		switch child.Kind {
		case KindScheduleGrid:
			if err := measureScheduleGridFits(child, Rect{X: x, Y: content.Y, W: remain, H: content.H}, measure); err != nil {
				*out = append(*out, FitProblem{Path: child.Path, Message: "ui: " + err.Error()})
			}
			continue
		case KindColumn, KindStack:
			// A column or a stack is handed the row's content box whatever it
			// measures: an overrun inside it is the column's business.
		case KindScroll, KindVirtualList:
			// Layout gives a scroll its declared width and the row's whole
			// content height; it is never clipped to the remainder the way a
			// column is, so it takes its own descent.
			checkNode(child, Rect{X: x, Y: content.Y, W: w, H: content.H}, measure, out)
			continue
		case KindCapsule, KindButton, KindDragSource, KindSegmented, KindMenu:
			if w < 0 || h < 0 || h > content.H || x+w > content.X+content.W {
				*out = append(*out, FitProblem{Path: child.Path, Message: fitError(n, i, child, content).Error()})
				continue
			}
		default:
			if w < 0 || h < 0 || h > content.H {
				*out = append(*out, FitProblem{Path: child.Path, Message: fitError(n, i, child, content).Error()})
				continue
			}
			if child.Kind == KindRow && child.PinEnd {
				w = remain
			}
			if w > remain {
				w = remain
			}
			if w < 0 {
				*out = append(*out, FitProblem{Path: child.Path, Message: fitError(n, i, child, content).Error()})
				continue
			}
		}
		// Layout hands a column min(w, remain) and clips a nested row the same
		// way; one clamp before the descent reproduces both.
		if w > remain {
			w = remain
		}
		if w < 0 {
			w = 0
		}
		checkNode(child, Rect{X: x, Y: content.Y, W: w, H: h}, measure, out)
		x += w
	}
}

func measureScheduleGridFits(n *Node, bounds Rect, measure MeasureText) error {
	copyOfNode := *n
	copyOfNode.Children = make([]*Node, len(n.Children))
	for i, child := range n.Children {
		if child != nil {
			childCopy := *child
			copyOfNode.Children[i] = &childCopy
		}
	}
	return layoutScheduleGrid(&copyOfNode, bounds, measure)
}

// checkColumn mirrors LayoutColumn, which rejects nothing: a child taller or
// wider than the column simply overflows or truncates. Only the descent is
// needed, because a violation deeper in the tree is still a violation.
func checkColumn(n *Node, box Rect, measure MeasureText, out *[]FitProblem) {
	content := Rect{X: box.X + n.Padding, Y: box.Y + n.Padding,
		W: box.W - 2*n.Padding, H: box.H - 2*n.Padding}
	y := content.Y
	for i, child := range n.Children {
		if child == nil {
			*out = append(*out, FitProblem{Message: fmt.Sprintf("ui: %s: nil child %d", label(n), i)})
			continue
		}
		if i > 0 {
			y += n.Gap
		}
		h, err := columnChildHeight(child, content.W, measure)
		if err != nil {
			*out = append(*out, FitProblem{Path: child.Path, Message: "ui: " + err.Error()})
			continue
		}
		checkNode(child, Rect{X: content.X, Y: y, W: content.W, H: h}, measure, out)
		y += h
	}
}

// checkScroll mirrors layoutScroll: children stack inside the padded content
// box, each at its natural height, and the viewport clips them.
func checkScroll(n *Node, box Rect, measure MeasureText, out *[]FitProblem) {
	content := Rect{X: box.X + n.Padding, Y: box.Y + n.Padding,
		W: box.W - 2*n.Padding, H: box.H - 2*n.Padding}
	y := content.Y
	for i, child := range n.Children {
		if child == nil {
			*out = append(*out, FitProblem{Message: fmt.Sprintf("ui: %s: nil child %d", label(n), i)})
			continue
		}
		h, err := columnChildHeight(child, content.W, measure)
		if err != nil {
			*out = append(*out, FitProblem{Path: child.Path, Message: "ui: " + err.Error()})
			continue
		}
		if i > 0 {
			y += n.Gap
		}
		checkNode(child, Rect{X: content.X, Y: y, W: content.W, H: h}, measure, out)
		y += h
	}
}
