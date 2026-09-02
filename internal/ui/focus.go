package ui

// Focusables flattens the tree in traversal order, returning focusable nodes.
func Focusables(root *Node) []*Node {
	var out []*Node
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if n.Focusable {
			out = append(out, n)
		}
		if n.Kind == KindVirtualList && n.Item != nil {
			if !virtualListHasFocusable(n) {
				return
			}
			for i := 0; i < n.ItemCount; i++ {
				walk(n.Item(i))
			}
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

func hasFocusable(n *Node) bool {
	if n == nil {
		return false
	}
	if n.Focusable {
		return true
	}
	for _, c := range n.Children {
		if hasFocusable(c) {
			return true
		}
	}
	return false
}

// virtualListHasFocusable probes a few rows so a heading-only prefix cannot
// hide later controls, without instantiating a thousand inert launcher rows.
func virtualListHasFocusable(n *Node) bool {
	if n == nil || n.Item == nil || n.ItemCount <= 0 {
		return false
	}
	limit := min(n.ItemCount, 8)
	for i := 0; i < limit; i++ {
		if hasFocusable(n.Item(i)) {
			return true
		}
	}
	return false
}

// Roving tracks the single focus index for one panel.
type Roving struct {
	idx   int
	Count int
}

func (r *Roving) Index() int { return r.idx }

func (r *Roving) Next() {
	if r.Count <= 0 {
		return
	}
	r.idx = (r.idx + 1) % r.Count
}

func (r *Roving) Prev() {
	if r.Count <= 0 {
		return
	}
	r.idx = (r.idx - 1 + r.Count) % r.Count
}

func (r *Roving) Set(i int) {
	if r.Count <= 0 {
		r.idx = 0
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= r.Count {
		i = r.Count - 1
	}
	r.idx = i
}
