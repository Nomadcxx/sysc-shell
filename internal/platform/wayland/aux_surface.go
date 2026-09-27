package wayland

import (
	"errors"
	"fmt"
	"slices"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/fractionalscale"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
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
	// RequiredLayerShellVersion prevents an auxiliary surface from issuing
	// requests the compositor did not advertise, such as on-demand focus.
	RequiredLayerShellVersion uint32
	// BlurRegion is the output-logical rect to capture behind this surface
	// before it is created. Nil disables the backdrop and all of its cost.
	BlurRegion *ui.Rect
	BlurRadius int
	// InputRects, when non-nil, limit pointer input to these surface-local
	// rectangles from the first frame. Nil leaves the whole surface.
	InputRects []ui.Rect
	Callbacks  HostCallbacks
}

// AuxRequest opens (Open != nil), updates (Update != nil), or closes (both nil,
// ID set) one aux surface on the output identified by its wl_registry global.
type AuxRequest struct {
	Output uint32
	ID     string
	Open   *AuxSpec
	Update *AuxUpdate
	Reply  chan error
}

// AuxUpdate changes policy on an already-open auxiliary surface without
// recreating it. A nil Keyboard leaves keyboard interactivity alone; the input
// region is replaced only when SetInputRegion is true.
type AuxUpdate struct {
	Keyboard                                         *uint32
	Layer                                            *layershell.ZwlrLayerShellV1Layer
	MarginTop, MarginBottom, MarginLeft, MarginRight *int32
	// Width and Height resize the surface in surface-local coordinates. A nil
	// pointer leaves that axis alone; both nil leaves the size alone.
	Width  *uint32
	Height *uint32
	// SetInputRegion replaces the surface input region. An empty InputRects
	// means the surface accepts no pointer input, which is not the same as
	// leaving the region unset: an unset region covers the whole surface.
	SetInputRegion bool
	InputRects     []ui.Rect
}

// auxPolicy is the mutable policy of one open auxiliary surface.
type auxPolicy struct {
	keyboard                                         uint32
	layer                                            layershell.ZwlrLayerShellV1Layer
	marginTop, marginBottom, marginLeft, marginRight int32
	width, height                                    uint32
	inputRects                                       []ui.Rect
	hasInputRegion                                   bool
}

func (o *owner) handleAux(req AuxRequest) {
	h, ok := o.hosts.get(req.Output)
	if !ok || !h.alive {
		if req.Reply != nil {
			req.Reply <- fmt.Errorf("wayland: output %d is not available for aux surface %s", req.Output, req.ID)
		}
		// ponytail: asynchronous closes can trail output removal; dropping those
		// stale requests is safe, while open/update callers receive the error.
		return
	}
	var err error
	switch {
	case req.Open != nil:
		err = o.openAux(h, req.Open)
	case req.Update != nil:
		err = o.updateAux(h, req.ID, req.Update)
	default:
		o.closeAux(h, req.ID)
	}
	if req.Reply != nil {
		req.Reply <- err
	} else {
		o.fail(err)
	}
}

func (o *owner) openAux(h *OutputHost, spec *AuxSpec) error {
	if spec == nil || spec.ID == "" {
		return errors.New("wayland: aux spec has no id")
	}
	if spec.Namespace == "" {
		return fmt.Errorf("wayland: aux %s has no namespace", spec.ID)
	}
	version := o.rs.singletons["zwlr_layer_shell_v1"].version
	if spec.RequiredLayerShellVersion > version {
		return fmt.Errorf("wayland: %s needs layer-shell version %d for on-demand focus; compositor provides %d", spec.ID, spec.RequiredLayerShellVersion, version)
	}
	if err := spec.Callbacks.validate(spec.ID); err != nil {
		return err
	}
	if _, exists := h.aux[spec.ID]; exists {
		o.closeAux(h, spec.ID)
	}

	u := newSurfaceUnit(spec.ID)
	u.app = spec.Callbacks

	// Before any surface exists. Screencopy captures the composited output, so
	// a capture taken once this panel or its shield had mapped would blur the
	// panel into its own backdrop. The shield opens first but paints nothing --
	// its Render returns immediately, leaving a cleared, fully transparent
	// buffer -- so it cannot show up in the copy either.
	if spec.BlurRegion != nil && spec.Callbacks.Backdrop != nil {
		if shot := o.captureRegion(h.proxy, *spec.BlurRegion); shot != nil {
			spec.Callbacks.Backdrop(render.Blur(shot, backdropDownsample, spec.BlurRadius))
		}
	}

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
	// The policy starts at the size the surface was opened with, so a
	// later one-axis update resolves the other axis against reality.
	u.policy.width, u.policy.height = uint32(max(spec.Width, 0)), uint32(max(spec.Height, 0))
	u.policy.layer = spec.Layer
	u.policy.marginTop, u.policy.marginBottom = spec.MarginTop, spec.MarginBottom
	u.policy.marginLeft, u.policy.marginRight = spec.MarginLeft, spec.MarginRight
	if spec.InputRects != nil {
		u.policy.inputRects, u.policy.hasInputRegion = append([]ui.Rect(nil), spec.InputRects...), true
	}
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

// applyAuxRegions sets the input and opaque regions for one auxiliary surface.
//
// The radius is the surface's own painted radius, not zero: a panel clears its
// buffer and fills a rounded body, so its corners are transparent, and claiming
// the whole rectangle is opaque tells the compositor not to blend them. It then
// composites the cleared pixels straight to the screen and the corners read as
// black squares behind the border rather than as wallpaper.
//
// A panel attached to a bar edge squares the two corners on that edge, so
// excluding all four corner squares gives up a little compositor optimisation
// there. That is the safe direction to err: too small an opaque region only
// costs blending, while too large a one is a visible artefact.
func (o *owner) applyAuxRegions(u *surfaceUnit) error {
	r := ui.Rect{W: u.ss.logicalWidth, H: u.ss.logicalHeight}
	if u.policy.hasInputRegion {
		if err := o.applyInputRects(u.surface, u.policy.inputRects); err != nil {
			return err
		}
		return o.applyOpaqueRegion(u.surface, r, u.app.Radius, u.app.OpaqueBackground)
	}
	return o.applyRegions(u.surface, r, r, u.app.Radius, u.app.OpaqueBackground)
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
	if upd.Layer != nil {
		if *upd.Layer > layershell.ZwlrLayerShellV1LayerOverlay {
			return auxPolicy{}, fmt.Errorf("wayland: aux %s has invalid layer %d", u.id, *upd.Layer)
		}
		next.layer = *upd.Layer
	}
	if upd.MarginTop != nil {
		next.marginTop = *upd.MarginTop
	}
	if upd.MarginBottom != nil {
		next.marginBottom = *upd.MarginBottom
	}
	if upd.MarginLeft != nil {
		next.marginLeft = *upd.MarginLeft
	}
	if upd.MarginRight != nil {
		next.marginRight = *upd.MarginRight
	}
	if upd.Width != nil || upd.Height != nil {
		w, hgt := next.width, next.height
		if upd.Width != nil {
			w = *upd.Width
		}
		if upd.Height != nil {
			hgt = *upd.Height
		}
		if w == 0 || hgt == 0 {
			return auxPolicy{}, fmt.Errorf("wayland: aux %s size %dx%d is empty", u.id, w, hgt)
		}
		next.width, next.height = w, hgt
	}
	if !upd.SetInputRegion {
		return next, nil
	}
	// The region applies to the surface this update asks for, so a resize is
	// checked against its new size, not the one it replaces.
	bounds := ui.Rect{W: u.ss.logicalWidth, H: u.ss.logicalHeight}
	if upd.Width != nil || upd.Height != nil {
		bounds.W, bounds.H = int(next.width), int(next.height)
	}
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
	if next.width != u.policy.width || next.height != u.policy.height {
		if err := u.layer.SetSize(next.width, next.height); err != nil {
			return fmt.Errorf("wayland: aux %s size: %w", u.id, err)
		}
	}
	if next.layer != u.policy.layer {
		if err := u.layer.SetLayer(uint32(next.layer)); err != nil {
			return fmt.Errorf("wayland: aux %s layer: %w", u.id, err)
		}
	}
	if next.marginTop != u.policy.marginTop || next.marginBottom != u.policy.marginBottom || next.marginLeft != u.policy.marginLeft || next.marginRight != u.policy.marginRight {
		if err := u.layer.SetMargin(next.marginTop, next.marginRight, next.marginBottom, next.marginLeft); err != nil {
			return fmt.Errorf("wayland: aux %s margins: %w", u.id, err)
		}
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
