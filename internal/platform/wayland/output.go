package wayland

import "fmt"

// applyGeometry records wl_output.geometry. Only the transform is retained;
// physical size and subpixel layout have no consumer in this milestone.
func (h *OutputHost) applyGeometry(transform int32) { h.transform = transform }

// applyMode records wl_output.mode. The mode is metadata only: a surface size
// is never derived from it, because the layer-surface configure reports what
// remains after other clients' exclusive zones.
func (h *OutputHost) applyMode(width, height int32) {
	h.modeWidth, h.modeHeight = width, height
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

// chooseOutput resolves the requested connector to the host that will carry the
// bar, after the roundtrips that deliver wl_output.name.
//
// Host identity is the registry global name, never the connector string: a
// connector can disappear and return as a different monitor, so the connector
// is only a lookup attribute.
func (o *owner) chooseOutput() error {
	entry, err := o.rs.selectOutput(o.options.Output)
	if err != nil {
		return err
	}
	h, ok := o.hosts.get(entry.global)
	if !ok {
		return fmt.Errorf("wayland: output %q was advertised but not bound", entry.connector)
	}
	o.selected = h
	return nil
}
