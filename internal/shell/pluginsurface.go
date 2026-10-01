package shell

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

const (
	pluginSurfaceMinWidth  = 200
	pluginSurfaceMinHeight = 180
	pluginSurfaceMaxExtent = 2048
	pluginSurfaceIDPrefix  = "plugin-floating:"
	pluginSurfaceStateNS   = "floating."
)

type pluginSurfaceState struct {
	X, Y, Width, Height int
	Pinned              bool
}

type pluginSurfaceHost struct {
	host      *pluginHost
	panel     *PanelHost
	plugin    string
	key       string
	title     string
	viewID    string
	connector string
	global    uint32
	surfaceID string
	// content is the last plugin root, kept so a resize can re-wrap it and
	// the body can take the new height. Guarded by the registry lock.
	content *ui.Node

	mu                         sync.Mutex
	x, y, width, height        int
	outputWidth, outputHeight  int
	pinned                     bool
	dragging, resizing, closed bool
	startX, startY             int
	startRect                  pluginSurfaceState
}

func (h *pluginHost) openFloatingSurface(ctx context.Context, pluginID string, params v1.SurfaceOpenParams) (v1.SurfaceResult, error) {
	if err := ctx.Err(); err != nil {
		return v1.SurfaceResult{}, err
	}
	h.surfaceMu.Lock()
	defer h.surfaceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return v1.SurfaceResult{}, err
	}

	h.mu.Lock()
	slot := h.slots[pluginID]
	h.mu.Unlock()
	if slot == nil {
		return v1.SurfaceResult{}, errors.New("plugin is not running")
	}

	h.r.mu.Lock()
	connector, global, err := h.resolveOutputLocked(v1.OutputContextParams{Output: params.Output, Generation: params.Generation})
	if err != nil {
		h.r.mu.Unlock()
		return v1.SurfaceResult{}, err
	}
	outputW, outputH := pluginOutputSizeLocked(h.r, global)
	theme := h.r.panelThemeFor(global)
	font := h.r.panelFontFamily(global)
	h.r.mu.Unlock()
	if outputW <= 0 || outputH <= 0 {
		return v1.SurfaceResult{}, fmt.Errorf("output %s geometry is not ready", connector)
	}
	if outputW < pluginSurfaceMinWidth || outputH < pluginSurfaceMinHeight {
		return v1.SurfaceResult{}, fmt.Errorf("output %s is too small for a sticky note", connector)
	}
	h.mu.Lock()
	for id, surface := range h.surfaces {
		if surface.plugin == pluginID && surface.key == params.Key && surface.connector == connector && surface.global == global {
			h.mu.Unlock()
			h.r.publishSurface(surface.global, surface.surfaceID)
			return v1.SurfaceResult{ViewID: id, AlreadyOpen: true}, nil
		}
	}
	h.mu.Unlock()

	state := pluginSurfaceState{X: params.X, Y: params.Y, Width: params.Width, Height: params.Height}
	stateKey := pluginSurfaceStateKey(params.Key, connector)
	if raw, found := slot.store.Get(stateKey); found {
		var saved pluginSurfaceState
		if json.Unmarshal(raw, &saved) == nil && saved.Width > 0 && saved.Height > 0 {
			state = clampPluginSurface(saved, outputW, outputH)
		}
	} else if !pluginSurfaceRectFits(params.X, params.Y, params.Width, params.Height, outputW, outputH) {
		return v1.SurfaceResult{}, fmt.Errorf("surface rectangle %dx%d+%d+%d leaves output %s (%dx%d)", params.Width, params.Height, params.X, params.Y, connector, outputW, outputH)
	}
	state = clampPluginSurface(state, outputW, outputH)
	title := params.Title
	if title == "" {
		title = params.Key
	}

	surface := &pluginSurfaceHost{
		host: h, plugin: pluginID, key: params.Key, title: title, connector: connector,
		global: global, surfaceID: pluginSurfaceID(pluginID, params.Key, connector),
		x: state.X, y: state.Y, width: state.Width, height: state.Height,
		outputWidth: outputW, outputHeight: outputH, pinned: state.Pinned,
	}
	surface.panel = &PanelHost{
		id: PanelPlugin, output: global,
		// rect is where the sticky sits on the output; hover hints are
		// placed against it.
		rect:  ui.Rect{X: state.X, Y: state.Y, W: state.Width, H: state.Height},
		place: Placement{Panel: ui.Rect{W: state.Width, H: state.Height}, Output: ui.Rect{W: outputW, H: outputH}, CenterY: true},
		theme: theme, fontFamily: font,
	}
	surface.panel.root = surface.loadingTree()
	surface.panel.focus = ui.Focusables(surface.panel.root)
	surface.panel.roving = ui.Roving{Count: len(surface.panel.focus)}

	view, openedSlot, err := h.reserveView(hostedView{
		Plugin: pluginID, Entry: params.Key, SurfaceKey: params.Key,
		Output: connector, Generation: global, Kind: v1.ViewFloating,
		Width: state.Width, Height: state.Height, Title: title,
	})
	if err != nil {
		return v1.SurfaceResult{}, err
	}
	surface.viewID = view.ID
	h.mu.Lock()
	h.surfaces[view.ID] = surface
	h.mu.Unlock()
	if err := h.r.sendAuxWait(ctx, wayland.AuxRequest{Output: global, ID: surface.surfaceID, Open: surface.spec()}); err != nil {
		h.mu.Lock()
		delete(h.surfaces, view.ID)
		delete(h.views, view.ID)
		h.mu.Unlock()
		return v1.SurfaceResult{}, err
	}
	isOpen := func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		registered := h.views[view.ID]
		return registered != nil && registered.Plugin == pluginID && registered.Kind == v1.ViewFloating && h.surfaces[view.ID] == surface
	}
	closeOpenedSurface := func() {
		// outputLost can retire the view on the Wayland owner while this open
		// waits for its reply. Queue the close after the acknowledged open.
		h.r.sendAux(wayland.AuxRequest{Output: global, ID: surface.surfaceID})
	}
	if !isOpen() {
		closeOpenedSurface()
		return v1.SurfaceResult{}, errors.New("floating surface closed while opening")
	}
	surface.persist()
	h.announceView(view, openedSlot, false)
	if !isOpen() {
		closeOpenedSurface()
		_ = openedSlot.rt.Send(&v1.ViewClose{ViewID: view.ID})
		return v1.SurfaceResult{}, errors.New("floating surface closed while opening")
	}
	return v1.SurfaceResult{ViewID: view.ID}, nil
}

func (h *pluginHost) closeFloatingSurface(ctx context.Context, pluginID string, params v1.SurfaceCloseParams) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h.surfaceMu.Lock()
	defer h.surfaceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	h.mu.Lock()
	view := h.views[params.View]
	owned := view != nil && view.Plugin == pluginID && view.Kind == v1.ViewFloating
	h.mu.Unlock()
	if !owned {
		return fmt.Errorf("floating view %q is not owned by %s", params.View, pluginID)
	}
	h.closeView(params.View)
	return nil
}

func (h *pluginHost) pinFloatingSurface(ctx context.Context, pluginID string, params v1.SurfacePinParams) error {
	h.mu.Lock()
	view, surface := h.views[params.View], h.surfaces[params.View]
	owned := view != nil && view.Plugin == pluginID && view.Kind == v1.ViewFloating && surface != nil
	h.mu.Unlock()
	if !owned {
		return fmt.Errorf("floating view %q is not owned by %s", params.View, pluginID)
	}
	return surface.setPinned(ctx, params.Pinned)
}

func (h *pluginHost) dropFloatingAux(output uint32, id string) bool {
	if output == 0 {
		return false
	}
	h.mu.Lock()
	var dropped *pluginSurfaceHost
	for _, surface := range h.surfaces {
		if surface.global == output && surface.surfaceID == id {
			dropped = surface
			break
		}
	}
	h.mu.Unlock()
	if dropped == nil {
		return false
	}
	h.dropFloatingSurface(dropped)
	return true
}

func (h *pluginHost) dropFloatingSurface(surface *pluginSurfaceHost) {
	if surface == nil {
		return
	}
	id := surface.viewID
	h.mu.Lock()
	if h.surfaces[id] != surface {
		h.mu.Unlock()
		return
	}
	view := h.views[id]
	slot := (*pluginSlot)(nil)
	if view != nil {
		slot = h.slots[view.Plugin]
		h.closed = append(h.closed, id)
	}
	delete(h.views, id)
	delete(h.surfaces, id)
	if h.panel != nil && h.panel.ID == id {
		h.panel = nil
	}
	h.mu.Unlock()

	// Wayland already removed this instance. An ID-only close here could land
	// after a replacement opens and remove that replacement instead.
	surface.mu.Lock()
	surface.closed = true
	surface.mu.Unlock()
	if slot != nil {
		_ = slot.rt.Send(&v1.ViewClose{ViewID: id})
	}
}

func (h *pluginHost) outputLost(output uint32) {
	h.mu.Lock()
	var ids []string
	for id, surface := range h.surfaces {
		if surface.global == output {
			ids = append(ids, id)
		}
	}
	h.mu.Unlock()
	for _, id := range ids {
		h.closeView(id)
	}
}

func (h *pluginHost) refreshFloatingSurface(viewID string) {
	h.mu.Lock()
	surface, view := h.surfaces[viewID], h.views[viewID]
	if surface == nil || view == nil {
		h.mu.Unlock()
		return
	}
	root, label, failed := view.Root, view.Label, view.Failed
	h.mu.Unlock()
	if failed || root == nil {
		root = surface.failureTree(label)
	}
	h.r.mu.Lock()
	surface.install(root)
	h.r.mu.Unlock()
	h.r.publishSurface(surface.global, surface.surfaceID)
}

// install wraps content in the sticky chrome and lays it out, keeping the
// focused control by its stable key. A tree the layout refuses is replaced by
// the failure card rather than painted unarranged. The caller holds the
// registry lock.
func (p *pluginSurfaceHost) install(content *ui.Node) {
	panel := p.panel
	focusKey := ""
	if n := panel.focused(); n != nil {
		focusKey = n.StableKey()
	}
	place := func(content *ui.Node) error {
		panel.root = p.wrapTree(content)
		panel.focus = ui.Focusables(panel.root)
		panel.roving = ui.Roving{Count: len(panel.focus)}
		for i, n := range panel.focus {
			if focusKey != "" && n.StableKey() == focusKey {
				panel.roving.Set(i)
				break
			}
		}
		if panel.logicalW > 0 && panel.logicalH > 0 {
			return panel.configure(panel.logicalW, panel.logicalH, panel.scale120)
		}
		return nil
	}
	// content is kept even when it is refused: a resize re-installs it, and
	// at the new size it may fit.
	p.content = content
	if err := place(content); err != nil {
		slog.Warn("sticky note layout refused", "plugin", p.plugin, "view", p.viewID, "err", err)
		_ = place(p.failureTree(err.Error()))
	}
}

func (p *pluginSurfaceHost) failureTree(detail string) *ui.Node {
	metrics := p.panel.theme.Metrics
	return &ui.Node{Kind: ui.KindColumn, Padding: metrics.PanelPadding, Gap: metrics.CardGap, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "This sticky note could not be rendered", Tone: ui.ToneError},
		{Kind: ui.KindText, Text: detail, Tone: ui.ToneSubtle},
	}}
}

func (p *pluginSurfaceHost) loadingTree() *ui.Node {
	return &ui.Node{Kind: ui.KindColumn, Padding: p.panel.theme.Metrics.PanelPadding, Gap: p.panel.theme.Metrics.CardGap, Children: []*ui.Node{{Kind: ui.KindText, Text: "Loading note…"}}}
}

func (p *pluginSurfaceHost) wrapTree(content *ui.Node) *ui.Node {
	return p.wrapTreeMeasured(content, p.panel.measureText())
}

// wrapTreeMeasured frames plugin content in the shell's sticky chrome. The
// chrome rows are sized from the theme's control metrics so they fit at every
// surface size, the content's fill becomes the surface paper rather than a
// child fill (a column paints no chrome of its own), and the first multiline
// field absorbs whatever height the chrome and the rest of the content leave,
// so resizing a sticky resizes its writing area.
func (p *pluginSurfaceHost) wrapTreeMeasured(content *ui.Node, measure ui.MeasureText) *ui.Node {
	content = copyNode(content)
	p.panel.paper, content.Fill = content.Fill, ui.FillNone
	state := p.snapshot()
	m := p.panel.theme.Metrics
	pinFill, pinName := ui.FillNone, "Keep sticky note above other windows"
	if state.Pinned {
		pinFill, pinName = ui.FillAccent, "Unpin sticky note"
	}
	header, inset, controlsW := p.titleBar()
	grip := m.CompactControl / 2
	titleW := max(state.Width-2*inset-controlsW-m.BarSpacing, m.StandardControl)
	chrome := &ui.Node{Kind: ui.KindRow, Height: header, Padding: inset, Gap: m.BarSpacing, PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindText, Text: p.title, MaxWidth: titleW, TextRole: theme.RoleLabel},
		{Kind: ui.KindRow, Width: controlsW, Height: m.CompactControl, Gap: m.BarSpacing, Children: []*ui.Node{
			{Kind: ui.KindButton, Action: pluginActionPrefix + p.viewID + ":surface-pin", Width: m.CompactControl, Height: m.CompactControl, Fill: pinFill, Name: pinName, Role: "button", Focusable: true,
				Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "push_pin"}}},
			{Kind: ui.KindButton, Action: pluginActionPrefix + p.viewID + ":surface-close", Width: m.CompactControl, Height: m.CompactControl, Name: "Close sticky note", Role: "button", Focusable: true,
				Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "close"}}},
		}},
	}}
	gripRow := &ui.Node{Kind: ui.KindRow, Height: grip, PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindColumn},
		{Kind: ui.KindIcon, Icon: "drag_indicator", IconSize: grip},
	}}
	if body := firstMultiline(content); body != nil {
		if natural, err := ui.ContentHeight(content, state.Width, measure); err == nil {
			slack := state.Height - header - grip - natural
			body.Height = max(body.Height+slack, 2*minLineHeight(measure))
		}
	}
	return &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{chrome, content, gripRow}}
}

func firstMultiline(n *ui.Node) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Kind == ui.KindTextField && n.Multiline {
		return n
	}
	for _, c := range n.Children {
		if f := firstMultiline(c); f != nil {
			return f
		}
	}
	return nil
}

func minLineHeight(measure ui.MeasureText) int {
	_, h := measure(" ", ui.TextAttrs{})
	return max(h, 1)
}

func (p *pluginSurfaceHost) snapshot() pluginSurfaceState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return pluginSurfaceState{X: p.x, Y: p.y, Width: p.width, Height: p.height, Pinned: p.pinned}
}

func (p *pluginSurfaceHost) spec() *wayland.AuxSpec {
	s := p.snapshot()
	layer := layershell.ZwlrLayerShellV1LayerTop
	if s.Pinned {
		layer = layershell.ZwlrLayerShellV1LayerOverlay
	}
	anchor := uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorLeft)
	// blur-exempt: sticky notes use opaque paper fills; sampling the desktop behind them would reduce text contrast.
	return &wayland.AuxSpec{
		ID: p.surfaceID, Namespace: "sysc-shell-plugin-sticky", Layer: layer, Anchor: anchor,
		MarginTop: int32(s.Y), MarginLeft: int32(s.X), Width: int32(s.Width), Height: int32(s.Height),
		ExclusiveZone:             -1,
		Keyboard:                  uint32(layershell.ZwlrLayerSurfaceV1KeyboardInteractivityOnDemand),
		RequiredLayerShellVersion: 4,
		OnDrop:                    func() { p.host.dropFloatingSurface(p) },
		Callbacks: wayland.HostCallbacks{
			Configure:  p.panel.configureLocking(p.host.r),
			OutputSize: p.outputSize,
			Render:     p.panel.renderLocking(p.host.r),
			Handle:     p.handle,
			WantIME: func() bool {
				p.host.r.mu.Lock()
				defer p.host.r.mu.Unlock()
				n := p.panel.focused()
				return n != nil && n.Kind == ui.KindTextField
			},
			IBeamAt: func(x, y float64) bool {
				p.host.r.mu.Lock()
				defer p.host.r.mu.Unlock()
				n := p.panel.hitFocusable(int(x), int(y))
				return n != nil && n.Kind == ui.KindTextField
			},
			OpaqueBackground: p.panel.theme.BackgroundOpaque(),
			Radius:           p.panel.theme.Radius,
		},
	}
}

func (p *pluginSurfaceHost) outputSize(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	p.mu.Lock()
	p.outputWidth, p.outputHeight = width, height
	old := pluginSurfaceState{X: p.x, Y: p.y, Width: p.width, Height: p.height, Pinned: p.pinned}
	next := clampPluginSurface(old, width, height)
	p.x, p.y, p.width, p.height = next.X, next.Y, next.Width, next.Height
	changed := old != next
	p.mu.Unlock()
	p.host.r.mu.Lock()
	p.panel.place.Output.W, p.panel.place.Output.H = width, height
	p.host.r.mu.Unlock()
	if changed {
		p.setGeometry(next, true)
	}
}

func (p *pluginSurfaceHost) handle(e wayland.Event) bool {
	if e.Kind == wayland.EventKeyPress && e.Key == keyEsc {
		p.host.r.mu.Lock()
		left := p.panel.leaveTextField()
		p.host.r.mu.Unlock()
		if left {
			return true
		}
		return p.host.r.deliverPluginText(pluginActionPrefix+p.viewID+":surface-close", "", v1.EventActivate)
	}
	if e.Kind == wayland.EventPointerPress && (e.Button == 0 || e.Button == btnLeft) {
		p.mu.Lock()
		p.startX, p.startY = int(e.X), int(e.Y)
		p.startRect = pluginSurfaceState{X: p.x, Y: p.y, Width: p.width, Height: p.height, Pinned: p.pinned}
		switch {
		case int(e.X) >= p.width-p.panel.theme.Metrics.CompactControl/2 && int(e.Y) >= p.height-p.panel.theme.Metrics.CompactControl/2:
			p.resizing = true
			p.mu.Unlock()
			return true
		case p.inDragZoneLocked(int(e.X), int(e.Y)):
			p.dragging = true
			p.mu.Unlock()
			return true
		}
		p.mu.Unlock()
	}
	if e.Kind == wayland.EventPointerMotion || e.Kind == wayland.EventPointerEnter {
		p.mu.Lock()
		dragging, resizing, start, sx, sy := p.dragging, p.resizing, p.startRect, p.startX, p.startY
		originX, originY := p.x, p.y
		outW, outH := p.outputWidth, p.outputHeight
		p.mu.Unlock()
		if dragging || resizing {
			// Pointer coordinates are surface-local. Include the current origin
			// to keep measuring the cursor against its position at press time
			// while the surface follows it.
			dx, dy := surfacePointerDelta(start.X, start.Y, sx, sy, originX, originY, int(e.X), int(e.Y))
			state := start
			if resizing {
				state.Width, state.Height = start.Width+dx, start.Height+dy
			} else {
				state.X, state.Y = start.X+dx, start.Y+dy
			}
			p.setGeometry(clampPluginSurface(state, outW, outH), resizing)
			return true
		}
	}
	if e.Kind == wayland.EventPointerRelease {
		p.mu.Lock()
		wasMoving := p.dragging || p.resizing
		p.dragging, p.resizing = false, false
		p.mu.Unlock()
		if wasMoving {
			p.persist()
			return true
		}
	}
	return p.panel.handle(p.host.r)(e)
}

// titleBar is the sticky title bar's geometry: a control height plus an inset
// on every side, so the title clears the corner and the two controls pin to
// the right edge.
func (p *pluginSurfaceHost) titleBar() (height, inset, controlsW int) {
	m := p.panel.theme.Metrics
	inset = m.BarSpacing + m.BarSpacing/2
	return m.CompactControl + 2*inset, inset, 2*m.CompactControl + m.BarSpacing
}

// inDragZone reports whether a surface-local point starts a move: anywhere in
// the title bar except over its controls.
func (p *pluginSurfaceHost) inDragZone(x, y int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inDragZoneLocked(x, y)
}

func (p *pluginSurfaceHost) inDragZoneLocked(x, y int) bool {
	height, inset, controlsW := p.titleBar()
	return y >= 0 && y < height && x < p.width-inset-controlsW
}

func surfacePointerDelta(startX, startY, startLocalX, startLocalY, currentX, currentY, currentLocalX, currentLocalY int) (int, int) {
	return currentX + currentLocalX - (startX + startLocalX), currentY + currentLocalY - (startY + startLocalY)
}

func (p *pluginSurfaceHost) setGeometry(next pluginSurfaceState, resized bool) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.x, p.y, p.width, p.height = next.X, next.Y, next.Width, next.Height
	p.mu.Unlock()
	p.host.r.mu.Lock()
	p.panel.rect = ui.Rect{X: next.X, Y: next.Y, W: next.Width, H: next.Height}
	if resized {
		p.panel.place.Panel.W, p.panel.place.Panel.H = next.Width, next.Height
		if p.panel.logicalW > 0 {
			p.panel.logicalW, p.panel.logicalH = next.Width, next.Height
			if p.content != nil {
				// Re-wrap so the body takes the new height.
				p.install(p.content)
			} else {
				_ = p.panel.configure(next.Width, next.Height, p.panel.scale120)
			}
		}
	}
	p.host.r.mu.Unlock()
	p.applyGeometry(resized)
}

func (p *pluginSurfaceHost) applyGeometry(resized bool) {
	state := p.snapshot()
	w, h := uint32(state.Width), uint32(state.Height)
	x, y := int32(state.X), int32(state.Y)
	upd := &wayland.AuxUpdate{MarginLeft: &x, MarginTop: &y}
	if resized {
		upd.Width, upd.Height = &w, &h
	}
	p.host.r.sendAux(wayland.AuxRequest{Output: p.global, ID: p.surfaceID, Update: upd})
	p.host.r.publishSurface(p.global, p.surfaceID)
}

func (p *pluginSurfaceHost) setPinned(ctx context.Context, on bool) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errors.New("floating surface is closed")
	}
	if p.pinned == on {
		p.mu.Unlock()
		return nil
	}
	wasPinned := p.pinned
	p.pinned = on
	p.mu.Unlock()
	layer := layershell.ZwlrLayerShellV1LayerTop
	if on {
		layer = layershell.ZwlrLayerShellV1LayerOverlay
	}
	if err := p.host.r.sendAuxWait(ctx, wayland.AuxRequest{Output: p.global, ID: p.surfaceID, Update: &wayland.AuxUpdate{Layer: &layer}}); err != nil {
		p.mu.Lock()
		if !p.closed {
			p.pinned = wasPinned
		}
		p.mu.Unlock()
		p.host.refreshFloatingSurface(p.viewID)
		return err
	}
	p.host.refreshFloatingSurface(p.viewID)
	p.persist()
	return nil
}

func (p *pluginSurfaceHost) persist() {
	state := p.snapshot()
	p.host.mu.Lock()
	slot := p.host.slots[p.plugin]
	p.host.mu.Unlock()
	if slot == nil || slot.store == nil {
		return
	}
	value, err := json.Marshal(state)
	if err != nil {
		return
	}
	if err := slot.store.Set(context.Background(), pluginSurfaceStateKey(p.key, p.connector), json.RawMessage(value)); err != nil {
		// Geometry is best effort; the live note remains open and editable.
		return
	}
}

func (p *pluginSurfaceHost) closeWayland() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()
	p.host.r.sendAux(wayland.AuxRequest{Output: p.global, ID: p.surfaceID})
}

func pluginOutputSizeLocked(r *Registry, global uint32) (int, int) {
	bar := r.bars[global]
	if bar == nil {
		return 0, 0
	}
	bar.mu.Lock()
	defer bar.mu.Unlock()
	return bar.output.width, bar.output.height
}

func clampPluginSurface(state pluginSurfaceState, outputW, outputH int) pluginSurfaceState {
	state.Width = min(max(state.Width, pluginSurfaceMinWidth), min(pluginSurfaceMaxExtent, outputW))
	state.Height = min(max(state.Height, pluginSurfaceMinHeight), min(pluginSurfaceMaxExtent, outputH))
	state.X = min(max(state.X, 0), max(outputW-state.Width, 0))
	state.Y = min(max(state.Y, 0), max(outputH-state.Height, 0))
	return state
}

func pluginSurfaceRectFits(x, y, width, height, outputW, outputH int) bool {
	return x >= 0 && y >= 0 && width > 0 && height > 0 && width <= outputW && height <= outputH &&
		x <= outputW-width && y <= outputH-height
}

func pluginSurfaceID(pluginID, key, output string) string {
	return pluginSurfaceIDPrefix + pluginID + ":" + shortHash(key) + ":" + shortHash(output)
}

func pluginSurfaceStateKey(key, output string) string {
	return pluginSurfaceStateNS + shortHash(key) + "." + shortHash(output)
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:12])
}
