package shell

import "github.com/Nomadcxx/sysc-shell/internal/ui"

// overflowFades builds the chrome for sysc-313's other half. Dropping whole
// items stopped the mid-glyph clip, but the drop itself was silent: from the
// user's seat the widgets were simply absent. Each section that gave way gets
// one fade over the trailing edge of the run that survived, so the row reads as
// continuing past the cut rather than ending there.
//
// It is built here, on the render view, rather than in the arrangement: the
// fade takes no width and displaces nothing, which is the whole reason this
// treatment was chosen over a control. An indicator that reserves room to
// announce overflow can cause the overflow it announces.
//
// A section drops the tail of its declaration order, so the cut is always at
// the trailing edge of what is left, for every section.
func overflowFades(sections [][]*ui.Node, over ui.BarOverflow, extent int) []*ui.Node {
	if !over.Any() || extent <= 0 {
		return nil
	}
	counts := [3]int{over.Left, over.Center, over.Right}
	var out []*ui.Node
	for i, section := range sections {
		if i >= len(counts) || counts[i] == 0 {
			continue
		}
		last := lastPlaced(section)
		if last == nil {
			// Every item dropped. There is no edge to soften, and a box over
			// bare bar would report the loss where no content ever sat.
			continue
		}
		box := last.Bounds
		width := min(extent, box.W)
		out = append(out, &ui.Node{
			Kind:   ui.KindEdgeFade,
			Bounds: ui.Rect{X: box.X + box.W - width, Y: box.Y, W: width, H: box.H},
		})
	}
	return out
}

// lastPlaced reports the final node a section actually placed. A dropped item
// keeps the zero Rect, so it is not the trailing edge of anything.
func lastPlaced(section []*ui.Node) *ui.Node {
	for i := len(section) - 1; i >= 0; i-- {
		if n := section[i]; n != nil && n.Bounds.W > 0 && n.Bounds.H > 0 {
			return n
		}
	}
	return nil
}

// clearHoverLocked drops the hover state interaction.apply resolved onto the
// bar's render copy; see renderViewLocked for why the bar paints its
// clickables at rest.
func clearHoverLocked(n *ui.Node) {
	if n == nil {
		return
	}
	n.State &^= ui.StateHovered
	for _, c := range n.Children {
		clearHoverLocked(c)
	}
}
