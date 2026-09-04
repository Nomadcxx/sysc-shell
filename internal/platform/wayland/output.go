package wayland

import (
	"github.com/Nomadcxx/sysc-shell/internal/ui"

	"github.com/Nomadcxx/sysc-wayland/client"
)

// applyGeometry records wl_output.geometry. Only the transform is retained;
// physical size and subpixel layout have no consumer in this milestone.
func (h *OutputHost) applyGeometry(transform int32) { h.transform = transform }

// applyMode records wl_output.mode. The mode is metadata only: a surface size
// is never derived from it, because the layer-surface configure reports what
// remains after other clients' exclusive zones.
func (h *OutputHost) applyMode(width, height int32) {
	h.modeWidth, h.modeHeight = width, height
}

// logicalSize converts the reported mode to the coordinate space surfaces are
// configured in. A 1920x1080 panel at scale 1.25 is a 1536x864 screen, and a
// popout placed against the raw mode would run off the bottom of it.
func (h *OutputHost) logicalSize(scale120 ui.Scale120) (w, hgt int) {
	if h.modeWidth <= 0 || h.modeHeight <= 0 {
		return 0, 0
	}
	scale := int(scale120)
	if scale <= 0 {
		scale = 120
	}
	return int(h.modeWidth) * 120 / scale, int(h.modeHeight) * 120 / scale
}

// applyName records wl_output.name, which version 4 supplies directly. It is
// the configuration-match and Niri-join attribute, never the host identity.
func (h *OutputHost) applyName(connector string) { h.connector = connector }

// applyDone commits the accumulated metadata and reports whether the host
// became ready on this event.
//
// A host is ready only with both a done and a non-empty name: done is the
// atomic commit point for geometry, mode and transform, and the name selects
// the per-output configuration override a bar cannot be created without.
func (h *OutputHost) applyDone() bool {
	h.doneSeen = true
	if h.state != hostBound || !h.ready() {
		return false
	}
	h.state = hostReady
	return true
}

// attachOutputHandlers wires a freshly bound wl_output to its host. The
// handlers run on the owner goroutine, because they are dispatched from its own
// Dispatch call, so creating a bar from one needs no second goroutine.
func (o *owner) attachOutputHandlers(h *OutputHost) {
	h.proxy.SetGeometryHandler(func(e client.OutputGeometryEvent) {
		if h.alive {
			h.applyGeometry(int32(e.Transform))
		}
	})
	h.proxy.SetModeHandler(func(e client.OutputModeEvent) {
		if h.alive {
			h.applyMode(e.Width, e.Height)
		}
	})
	h.proxy.SetNameHandler(func(e client.OutputNameEvent) {
		if !h.alive {
			return
		}
		h.applyName(e.Name)
		o.rs.setOutputName(h.global, e.Name)
	})
	h.proxy.SetDoneHandler(func(client.OutputDoneEvent) {
		if !h.alive {
			return
		}
		if h.applyDone() {
			o.fail(o.hostBecameReady(h))
		}
	})
}
