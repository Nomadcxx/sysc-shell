package wayland

import "github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"

// barAnchor is the layer-shell anchor for a bar on the named edge: the edge
// itself, plus the two edges of the cross axis so the surface spans the output.
//
// Composition is a pure function of the edge so it can be table tested without
// a compositor connection, which is the only way the geometry requests become
// testable at all.
//
// All four edges are answered even though config admits only the horizontal
// pair in this milestone. A partial function would need a failure path for
// input that validation has already made unreachable; a total one cannot
// return an unplaceable surface. An unrecognised edge anchors to the top for
// the same reason: a zero anchor is a surface the compositor will not place.
func barAnchor(edge string) uint32 {
	const (
		top    = uint32(layershell.ZwlrLayerSurfaceV1AnchorTop)
		bottom = uint32(layershell.ZwlrLayerSurfaceV1AnchorBottom)
		left   = uint32(layershell.ZwlrLayerSurfaceV1AnchorLeft)
		right  = uint32(layershell.ZwlrLayerSurfaceV1AnchorRight)
	)
	switch edge {
	case "bottom":
		return bottom | left | right
	case "left":
		return left | top | bottom
	case "right":
		return right | top | bottom
	default:
		return top | left | right
	}
}

// barSize is the SetSize argument for a bar of the given extent on the named
// edge. The dimension the compositor decides is zero: a horizontal bar asks
// for the anchored width, a vertical one for the anchored height.
func barSize(edge string, extent int) (w, h uint32) {
	switch edge {
	case "left", "right":
		return uint32(extent), 0
	default:
		return 0, uint32(extent)
	}
}
