package wayland

import (
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-wayland/client"
)

// inputRect is the area the bar accepts pointer input in: the whole surface.
//
// Milestone 2 declares no click-through pixels inside the surface. The gap band
// is transparent but still clickable, so a pointer slammed to the screen edge
// lands on the bar rather than in a dead strip. A later milestone that adds a
// shadow grows the surface past the exclusive zone and excludes that band here.
func inputRect(surface ui.Rect) ui.Rect { return surface }

// hostRegionGeometry derives regions from the host's accepted configure and a
// candidate policy. Reload uses the current configure until the compositor
// sends a replacement configure for changed layer-surface geometry.
func hostRegionGeometry(h *OutputHost, policy config.Bar) (surface, body ui.Rect) {
	surface = ui.Rect{W: h.bar.ss.logicalWidth, H: h.bar.ss.logicalHeight}
	body = ui.Rect{
		X: policy.Gap, Y: policy.Gap,
		W: max(0, surface.W-2*policy.Gap),
		H: max(0, surface.H-policy.Gap),
	}
	return surface, body
}

func (o *owner) applyHostRegions(h *OutputHost, policy config.Bar, opaqueBackground bool) error {
	surface, body := hostRegionGeometry(h, policy)
	return o.applyRegions(h.bar.surface, surface, body, policy.Radius, opaqueBackground)
}

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
func (o *owner) applyRegions(surface *client.Surface, surfaceRect, body ui.Rect, radius int, opaqueBackground bool) error {
	if err := o.applyInputRects(surface, []ui.Rect{inputRect(surfaceRect)}); err != nil {
		return err
	}
	return o.applyOpaqueRegion(surface, body, radius, opaqueBackground)
}

// applyInputRects sets the surface input region to exactly these rectangles.
// An empty slice yields an empty region, which accepts no pointer input; that
// differs from leaving the region unset, which accepts the whole surface.
func (o *owner) applyInputRects(surface *client.Surface, rects []ui.Rect) error {
	input, err := o.compositor.CreateRegion()
	if err != nil {
		return err
	}
	for _, r := range rects {
		if err := input.Add(int32(r.X), int32(r.Y), int32(r.W), int32(r.H)); err != nil {
			return err
		}
	}
	if err := surface.SetInputRegion(input); err != nil {
		return err
	}
	return input.Destroy()
}

func (o *owner) applyOpaqueRegion(surface *client.Surface, body ui.Rect, radius int, opaqueBackground bool) error {
	rects := opaqueRects(body, radius, opaqueBackground)
	if len(rects) == 0 {
		// A nil region means "no opaque area", which is what a translucent bar
		// needs; it is not the same as leaving the region unset.
		return surface.SetOpaqueRegion(nil)
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
	if err := surface.SetOpaqueRegion(opaque); err != nil {
		return err
	}
	return opaque.Destroy()
}
