package ui

import "math"

const (
	dragThreshold = 8
	dropSlop      = 8
)

// Drag is the in-progress pointer drag owned by a panel, not by a wire node.
type Drag struct {
	Source  *Node
	Type    string
	Payload string
	startX  float64
	startY  float64
	X       float64
	Y       float64
	Moved   bool
}

func (d *Drag) Begin(src *Node, x, y float64) {
	if d == nil || src == nil {
		return
	}
	*d = Drag{Source: src, Type: src.DragType, Payload: src.Payload, startX: x, startY: y, X: x, Y: y}
}

func (d *Drag) Move(x, y float64) {
	if d == nil || d.Source == nil {
		return
	}
	d.X, d.Y = x, y
	if math.Hypot(x-d.startX, y-d.startY) >= dragThreshold {
		d.Moved = true
	}
}

func (d *Drag) Cancel() {
	if d == nil {
		return
	}
	*d = Drag{}
}

func (d *Drag) Active() bool { return d != nil && d.Source != nil && d.Moved }

func (d *Drag) Accepts(zone *Node) bool {
	if d == nil || zone == nil || !d.Moved {
		return false
	}
	if len(zone.Accept) == 0 {
		return d.Type == ""
	}
	for _, t := range zone.Accept {
		if t == d.Type {
			return true
		}
	}
	return false
}

func (d *Drag) Hits(zone *Node) bool {
	if d == nil || zone == nil {
		return false
	}
	r := zone.Bounds
	x, y := int(d.X), int(d.Y)
	r.X -= dropSlop
	r.Y -= dropSlop
	r.W += 2 * dropSlop
	r.H += 2 * dropSlop
	return r.Contains(x, y)
}

func (d *Drag) Drop(zone *Node) (payload string, ok bool) {
	if !d.Active() || !d.Hits(zone) || !d.Accepts(zone) {
		return "", false
	}
	payload = d.Payload
	d.Cancel()
	return payload, true
}

func FindDropZone(root *Node, d *Drag) *Node {
	if root == nil || d == nil {
		return nil
	}
	var found *Node
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil || found != nil {
			return
		}
		if n.Kind == KindDropZone && d.Hits(n) {
			found = n
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return found
}
