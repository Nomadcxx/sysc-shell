package wayland

import "github.com/Nomadcxx/sysc-shell/internal/ui"

// inputRect is the area the bar accepts pointer input in: the whole surface.
//
// Milestone 2 declares no click-through pixels inside the surface. The gap band
// is transparent but still clickable, so a pointer slammed to the screen edge
// lands on the bar rather than in a dead strip. A later milestone that adds a
// shadow grows the surface past the exclusive zone and excludes that band here.
func inputRect(surface ui.Rect) ui.Rect { return surface }

// opaqueRects decomposes the painted body into rectangles the bar fills with
// fully opaque pixels.
//
// The opaque region is a hint that lets the compositor skip drawing behind the
// bar, so the transparent gap band and the rounded corners must be excluded or
// they render as corruption. Two overlapping bands cover the body minus its
// four corner squares: one spanning the full width inset vertically by the
// radius, one spanning the full height inset horizontally by it.
func opaqueRects(body ui.Rect, radius int, opaqueBackground bool) []ui.Rect {
	if !opaqueBackground || body.W <= 0 || body.H <= 0 {
		return nil
	}
	if radius <= 0 {
		return []ui.Rect{body}
	}
	if limit := min(body.W, body.H) / 2; radius > limit {
		radius = limit
	}
	if radius <= 0 {
		return []ui.Rect{body}
	}
	return []ui.Rect{
		{X: body.X, Y: body.Y + radius, W: body.W, H: body.H - 2*radius},
		{X: body.X + radius, Y: body.Y, W: body.W - 2*radius, H: body.H},
	}
}

// applyRegions sets the input and opaque regions for one bar. Regions are in
// logical surface coordinates, which is the viewport destination space.
func (o *owner) applyRegions(h *OutputHost, surface, body ui.Rect, radius int, opaqueBackground bool) error {
	input, err := o.compositor.CreateRegion()
	if err != nil {
		return err
	}
	r := inputRect(surface)
	if err := input.Add(int32(r.X), int32(r.Y), int32(r.W), int32(r.H)); err != nil {
		return err
	}
	if err := h.surface.SetInputRegion(input); err != nil {
		return err
	}
	if err := input.Destroy(); err != nil {
		return err
	}

	rects := opaqueRects(body, radius, opaqueBackground)
	if len(rects) == 0 {
		// A nil region means "no opaque area", which is what a translucent bar
		// needs; it is not the same as leaving the region unset.
		return h.surface.SetOpaqueRegion(nil)
	}
	opaque, err := o.compositor.CreateRegion()
	if err != nil {
		return err
	}
	for _, rect := range rects {
		if err := opaque.Add(int32(rect.X), int32(rect.Y), int32(rect.W), int32(rect.H)); err != nil {
			return err
		}
	}
	if err := h.surface.SetOpaqueRegion(opaque); err != nil {
		return err
	}
	return opaque.Destroy()
}
