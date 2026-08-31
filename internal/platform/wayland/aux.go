package wayland

import (
	"errors"
	"fmt"
	"slices"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/fractionalscale"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// AuxSpec describes one auxiliary layer surface. Callbacks travel with the
// spec because the app supplies them per surface at open time.
type AuxSpec struct {
	ID                                               string
	Namespace                                        string
	Layer                                            layershell.ZwlrLayerShellV1Layer
	Anchor                                           uint32
	MarginTop, MarginBottom, MarginLeft, MarginRight int32
	Width, Height                                    int32
	ExclusiveZone                                    int32
	Keyboard                                         uint32
	Callbacks                                        HostCallbacks
}

// AuxRequest opens (Open != nil) or closes (Open == nil, ID set) one aux
// surface on the output identified by its wl_registry global.
type AuxRequest struct {
	Output uint32
	ID     string
	Open   *AuxSpec
}

func (o *owner) handleAux(req AuxRequest) {
	h, ok := o.hosts.get(req.Output)
	if !ok || !h.alive {
		return
	}
	if req.Open == nil {
		o.closeAux(h, req.ID)
		return
	}
	o.fail(o.openAux(h, req.Open))
}

func (o *owner) openAux(h *OutputHost, spec *AuxSpec) error {
	if spec == nil || spec.ID == "" {
		return errors.New("wayland: aux spec has no id")
	}
	if spec.Namespace == "" {
		return fmt.Errorf("wayland: aux %s has no namespace", spec.ID)
	}
	if err := spec.Callbacks.validate(spec.ID); err != nil {
		return err
	}
	if _, exists := h.aux[spec.ID]; exists {
		o.closeAux(h, spec.ID)
	}

	u := newSurfaceUnit(spec.ID)
	u.app = spec.Callbacks

	surface, err := o.compositor.CreateSurface()
	if err != nil {
		return fmt.Errorf("wayland: create aux surface %s: %w", spec.ID, err)
	}
	u.surface = surface
	u.cleanup.push("surface", surface.Destroy)

	layer, err := o.layerShell.GetLayerSurface(surface, h.proxy, uint32(spec.Layer), spec.Namespace)
	if err != nil {
		_, _ = u.cleanup.unwind()
		return fmt.Errorf("wayland: get aux layer surface %s: %w", spec.ID, err)
	}
	u.layer = layer
	u.cleanup.push("layer-surface", layer.Destroy)

	if err := o.applyAuxGeometry(u, spec); err != nil {
		_, _ = u.cleanup.unwind()
		return err
	}
	layer.SetConfigureHandler(func(e layershell.ZwlrLayerSurfaceV1ConfigureEvent) {
		if h.alive {
			o.onConfigure(h, u, e)
		}
	})
	id := spec.ID
	layer.SetClosedHandler(func(layershell.ZwlrLayerSurfaceV1ClosedEvent) {
		if _, ok := h.aux[id]; ok {
			o.closeAux(h, id)
		}
	})

	scale, err := o.scaleMgr.GetFractionalScale(surface)
	if err != nil {
		_, _ = u.cleanup.unwind()
		return fmt.Errorf("wayland: get aux fractional scale %s: %w", spec.ID, err)
	}
	u.scale = scale
	u.cleanup.push("fractional-scale", scale.Destroy)
	scale.SetPreferredScaleHandler(func(e fractionalscale.WpFractionalScaleV1PreferredScaleEvent) {
		if h.alive {
			o.onPreferredScale(h, u, e)
		}
	})

	viewport, err := o.viewporter.GetViewport(surface)
	if err != nil {
		_, _ = u.cleanup.unwind()
		return fmt.Errorf("wayland: get aux viewport %s: %w", spec.ID, err)
	}
	u.viewport = viewport
	u.cleanup.push("viewport", viewport.Destroy)

	if err := surface.Commit(); err != nil {
		_, _ = u.cleanup.unwind()
		return fmt.Errorf("wayland: initial aux commit %s: %w", spec.ID, err)
	}
	h.aux[spec.ID] = u
	return nil
}

func (o *owner) applyAuxGeometry(u *surfaceUnit, spec *AuxSpec) error {
	if err := u.layer.SetSize(uint32(max(spec.Width, 0)), uint32(max(spec.Height, 0))); err != nil {
		return err
	}
	if err := u.layer.SetAnchor(spec.Anchor); err != nil {
		return err
	}
	if err := u.layer.SetMargin(spec.MarginTop, spec.MarginRight, spec.MarginBottom, spec.MarginLeft); err != nil {
		return err
	}
	if err := u.layer.SetExclusiveZone(spec.ExclusiveZone); err != nil {
		return err
	}
	return u.layer.SetKeyboardInteractivity(spec.Keyboard)
}

func (o *owner) applyAuxRegions(u *surfaceUnit) error {
	r := ui.Rect{W: u.ss.logicalWidth, H: u.ss.logicalHeight}
	return o.applyRegions(u.surface, r, r, 0, u.app.OpaqueBackground)
}

func (o *owner) closeAux(h *OutputHost, id string) {
	u, ok := h.aux[id]
	if !ok {
		return
	}
	delete(h.aux, id)
	if o.focus.unit == u {
		o.clearFocus()
	}
	if o.keyFocus.unit == u {
		o.leaveKeyboard()
	}
	_ = o.teardownUnit(u)
	if o.cb.DropAux != nil {
		o.cb.DropAux(h.global, id)
	}
}

func (o *owner) closeAllAux(h *OutputHost) {
	ids := make([]string, 0, len(h.aux))
	for id := range h.aux {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		o.closeAux(h, id)
	}
}

func (o *owner) teardownUnit(u *surfaceUnit) error {
	if u == nil {
		return nil
	}
	var errs []error
	u.sched.Close()
	if err := u.dropFrameCallback(); err != nil {
		errs = append(errs, err)
	}
	if u.current != nil {
		u.current.retire.destroy()
		u.retiring = append(u.retiring, u.current)
		u.current = nil
	}

	// The surface goes first. A wl_buffer destroyed while its wl_surface is
	// still alive can still be sent wl_buffer.release, and dispatching an
	// event for a destroyed id panics the client with an invalid server
	// object ID. Destroying the surface makes the compositor drop its
	// references, after which the generations are safe to free.
	if _, err := u.cleanup.unwind(); err != nil {
		errs = append(errs, err)
	}
	for _, gen := range u.retiring {
		gen.retire.destroy()
		if err := gen.destroy(); err != nil {
			errs = append(errs, err)
		}
	}
	u.retiring = nil
	u.surface, u.layer, u.scale, u.viewport = nil, nil, nil, nil
	return errors.Join(errs...)
}
