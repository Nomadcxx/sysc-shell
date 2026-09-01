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

// AuxRequest opens (Open != nil), updates (Update != nil), or closes (both nil,
// ID set) one aux surface on the output identified by its wl_registry global.
type AuxRequest struct {
	Output uint32
	ID     string
	Open   *AuxSpec
	Update *AuxUpdate
}

// AuxUpdate changes policy on an already-open auxiliary surface without
// recreating it. A nil Keyboard leaves keyboard interactivity alone; the input
// region is replaced only when SetInputRegion is true.
type AuxUpdate struct {
	Keyboard *uint32
	// SetInputRegion replaces the surface input region. An empty InputRects
	// means the surface accepts no pointer input, which is not the same as
	// leaving the region unset: an unset region covers the whole surface.
	SetInputRegion bool
	InputRects     []ui.Rect
}

// auxPolicy is the mutable policy of one open auxiliary surface.
type auxPolicy struct {
	keyboard       uint32
	inputRects     []ui.Rect
	hasInputRegion bool
}

func (o *owner) handleAux(req AuxRequest) {
	h, ok := o.hosts.get(req.Output)
	if !ok || !h.alive {
		return
	}
	switch {
	case req.Open != nil:
		o.fail(o.openAux(h, req.Open))
	case req.Update != nil:
		o.fail(o.updateAux(h, req.ID, req.Update))
	default:
		o.closeAux(h, req.ID)
	}
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
	if u.policy.hasInputRegion {
		if err := o.applyInputRects(u.surface, u.policy.inputRects); err != nil {
			return err
		}
		return o.applyOpaqueRegion(u.surface, r, 0, u.app.OpaqueBackground)
	}
	return o.applyRegions(u.surface, r, r, 0, u.app.OpaqueBackground)
}

// updateAux changes policy on an open surface in place. The request is
// validated before any compositor call, so a bad update disturbs nothing.
func (o *owner) updateAux(h *OutputHost, id string, upd *AuxUpdate) error {
	u, ok := h.aux[id]
	if !ok {
		return fmt.Errorf("wayland: aux %s is not open", id)
	}
	next, err := planAuxUpdate(u, upd)
	if err != nil {
		return err
	}
	if err := o.applyAuxPolicy(u, next); err != nil {
		return err
	}
	u.policy = next
	return nil
}

// planAuxUpdate folds an update into the surface's policy. It makes no
// compositor calls and copies every submitted rectangle, so the caller cannot
// mutate the region afterwards.
func planAuxUpdate(u *surfaceUnit, upd *AuxUpdate) (auxPolicy, error) {
	if upd == nil {
		return auxPolicy{}, errors.New("wayland: aux update is empty")
	}
	next := u.policy
	if upd.Keyboard != nil {
		next.keyboard = *upd.Keyboard
	}
	if !upd.SetInputRegion {
		return next, nil
	}
	bounds := ui.Rect{W: u.ss.logicalWidth, H: u.ss.logicalHeight}
	rects := make([]ui.Rect, 0, len(upd.InputRects))
	for _, r := range upd.InputRects {
		if r.W <= 0 || r.H <= 0 || r.X < 0 || r.Y < 0 {
			return auxPolicy{}, fmt.Errorf("wayland: aux %s input rect %+v is empty or negative", u.id, r)
		}
		if r.X+r.W > bounds.W || r.Y+r.H > bounds.H {
			return auxPolicy{}, fmt.Errorf("wayland: aux %s input rect %+v leaves the surface", u.id, r)
		}
		rects = append(rects, r)
	}
	next.inputRects = rects
	next.hasInputRegion = true
	return next, nil
}

// applyAuxPolicy performs the compositor calls for one update and commits once.
func (o *owner) applyAuxPolicy(u *surfaceUnit, next auxPolicy) error {
	if u.layer == nil || u.surface == nil {
		return fmt.Errorf("wayland: aux %s has no surface", u.id)
	}
	if next.keyboard != u.policy.keyboard {
		if err := u.layer.SetKeyboardInteractivity(next.keyboard); err != nil {
			return fmt.Errorf("wayland: aux %s keyboard interactivity: %w", u.id, err)
		}
	}
	if next.hasInputRegion {
		if err := o.applyInputRects(u.surface, next.inputRects); err != nil {
			return fmt.Errorf("wayland: aux %s input region: %w", u.id, err)
		}
	}
	return u.surface.Commit()
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
	for _, gen := range u.retiring {
		gen.retire.destroy()
		if err := gen.destroy(); err != nil {
			errs = append(errs, err)
		}
	}
	u.retiring = nil
	if _, err := u.cleanup.unwind(); err != nil {
		errs = append(errs, err)
	}
	u.surface, u.layer, u.scale, u.viewport = nil, nil, nil, nil
	return errors.Join(errs...)
}
