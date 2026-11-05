package render

import "github.com/Nomadcxx/sysc-shell/internal/ui"

// FocusOutset is the logical space outside a control for its gap and ring.
const FocusOutset = 5 // 2px clear gap plus the 3px inward stroke.

// focusGeometry follows the painter's silhouette, not the pointer allocation.
// The returned box is the outer stroke edge in physical surface coordinates.
func focusGeometry(style Style, n *ui.Node) (ui.Rect, int) {
	logical := n.Bounds
	if n.Kind == ui.KindButton || n.Kind == ui.KindCapsule {
		logical = ChromeBounds(n)
	}
	if n.Kind == ui.KindMenu {
		if popup := ui.MenuPopupBounds(n); popup.H > 0 && popup.Y > logical.Y {
			logical.H = popup.Y - logical.Y
		}
	}
	box := style.Scale120.PhysicalRect(logical)
	radius := 0
	switch n.Kind {
	case ui.KindTextField:
		radius = FieldRadius(n, box, style.Scale120)
	case ui.KindToggle:
		if n.Role == "checkbox" {
			radius = style.Scale120.Physical(3)
		} else {
			box = toggleBounds(n, style)
			radius = box.H / 2
		}
	case ui.KindRow:
		if n.Shape != ui.ShapeInherit || n.Fill == ui.FillContainerHigh && n.Shape == ui.ShapeCard {
			radius = chromeRadius(style, nodeRadius(style, n, capsuleInherit(style, n)), box)
		}
	case ui.KindTab:
		radius = chromeRadius(style, nodeRadius(style, n, 0), box)
	case ui.KindButton, ui.KindDragSource:
		radius = chromeRadius(style, nodeRadius(style, n, 0), box)
		radius = ui.MorphRadius(radius, n.PressProgress)
	case ui.KindMenu:
		radius = chromeRadius(style, nodeRadius(style, n, 6), box)
	default:
		radius = chromeRadius(style, nodeRadius(style, n, capsuleInherit(style, n)), box)
		radius = ui.MorphRadius(radius, n.PressProgress)
	}
	outer := logical
	outer.X -= FocusOutset
	outer.Y -= FocusOutset
	outer.W += 2 * FocusOutset
	outer.H += 2 * FocusOutset
	expanded := style.Scale120.PhysicalRect(outer)
	target := style.Scale120.PhysicalRect(logical)
	// A switch's visual is centred inside its declared hit target.
	expanded.X += box.X - target.X
	expanded.Y += box.Y - target.Y
	expanded.W += box.W - target.W
	expanded.H += box.H - target.H
	return expanded, min(radius+style.Scale120.Physical(FocusOutset), min(expanded.W, expanded.H)/2)
}

func paintFocus(c *Canvas, n *ui.Node, style Style) {
	if !n.State.Has(ui.StateFocused) || n.State.Has(ui.StateDisabled) && !n.AriaDisabled {
		return
	}
	box, radius := focusGeometry(style, n)
	ring := *c
	// Opacity groups clip ordinary content to their allocation. The ring alone
	// may use the extended scratch frame; scroll, explicit and damage clips stay.
	ring.contentClip = nil
	ring.StrokeRounded(box, radius, max(1, style.Scale120.Physical(3)), style.Secondary)
}

// focusPaintBounds extends only the focused subtree's bounded scratch/cull
// envelope. Real viewport and surface clips still apply while painting.
func focusPaintBounds(n *ui.Node, style Style) ui.Rect {
	bounds := style.Scale120.PhysicalRect(n.Bounds)
	// ponytail: scan the visible subtree per opacity group; cache its focus
	// envelope on the paint copy if deeply nested groups become a measured cost.
	var include func(*ui.Node)
	include = func(child *ui.Node) {
		if child == nil {
			return
		}
		if child.State.Has(ui.StateFocused) {
			ring, _ := focusGeometry(style, child)
			x, y := min(bounds.X, ring.X), min(bounds.Y, ring.Y)
			bounds = ui.Rect{X: x, Y: y, W: max(bounds.X+bounds.W, ring.X+ring.W) - x, H: max(bounds.Y+bounds.H, ring.Y+ring.H) - y}
		}
		for _, c := range child.Children {
			include(c)
		}
	}
	include(n)
	return bounds
}
