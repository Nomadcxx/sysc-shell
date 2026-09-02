package shell

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

const (
	keyboardExclusive = uint32(layershell.ZwlrLayerSurfaceV1KeyboardInteractivityExclusive)
	keyboardNone      = uint32(layershell.ZwlrLayerSurfaceV1KeyboardInteractivityNone)
	layerOverlay      = layershell.ZwlrLayerShellV1LayerOverlay

	keyEsc       = 1
	keyBackspace = 14
	keyTab       = 15
	keyEnter     = 28
	keyLeftShift = 42
	keySpace     = 57
	keyHome      = 102
	keyUp        = 103
	keyPageUp    = 104
	keyLeft      = 105
	keyRight     = 106
	keyEnd       = 107
	keyDown      = 108
	keyPageDown  = 109

	btnRight = 273

	revealDuration = 200 * time.Millisecond
	revealTick     = 16 * time.Millisecond
	// shieldQuietFor drops the press that mapped the overlay, so the click
	// that opened a panel cannot dismiss it through the fullscreen shield.
	shieldQuietFor = 400 * time.Millisecond
)

// Trigger is the placement hint for opening a panel on one output.
type Trigger struct {
	BarEdge    string
	BarZone    int
	Align      string
	OutW, OutH int
}

// PanelHost is one open panel: two surfaces' callbacks, content tree, focus,
// leases, and reveal state.
type PanelHost struct {
	id             PanelID
	output         uint32
	place          Placement
	root           *ui.Node
	focus          []*ui.Node
	roving         ui.Roving
	leases         []*services.Lease
	animStart      time.Time
	shieldQuiet    time.Time
	stopAnim       chan struct{}
	stopOnce       sync.Once
	theme          Theme
	text           *render.TextRenderer
	fontFamily     string
	logicalW       int
	logicalH       int
	scale120       int
	shift          bool
	pressed        *ui.Node
	drag           ui.Drag
	lastAction     string
	hoverX, hoverY int
	monthDelta     int
	errLabel       string
	menu           *Menu
	menuPath       string
	menus          map[string]*Menu
	sliderDrag     *ui.Node
	scrollDrag     *ui.Node
	set            *settings.Registry
	draft          config.Config
	query          string
	section        string
	search         *ui.Field
	fields         map[string]*ui.Field
	editors        map[string]*retainedEditor

	launcherResults []launcher.Result
	launcherSel     int
	launcherScroll  int
	launcherMenuID  string
	launcherActions []launcher.Action

	notifyTab    int
	notifyFilter string
	notifyExpand string
	notifyMenu   bool

	profiles      []string
	profileActive string
	profilesOK    bool
}

func parsePanelName(name string) (PanelID, error) {
	switch name {
	case "clock":
		return PanelClock, nil
	case "system-monitor":
		return PanelMonitor, nil
	case "session", "power":
		return PanelSession, nil
	case "settings":
		return PanelSettings, nil
	case "launcher":
		return PanelLauncher, nil
	case "plugin":
		return PanelPlugin, nil
	case "notifications":
		return PanelNotifications, nil
	default:
		return 0, fmt.Errorf("unknown panel")
	}
}

func (r *Registry) AuxRequests() <-chan wayland.AuxRequest { return r.aux }

func (r *Registry) TogglePanelByName(name string) error {
	id, err := parsePanelName(name)
	if err != nil {
		return err
	}
	out, trig := r.focusedTrigger()
	return r.TogglePanel(id, out, trig)
}

func (r *Registry) OpenPanelByName(name string) error {
	id, err := parsePanelName(name)
	if err != nil {
		return err
	}
	out, trig := r.focusedTrigger()
	return r.OpenPanel(id, out, trig)
}

func (r *Registry) ClosePanelByName(name string) error {
	id, err := parsePanelName(name)
	if err != nil {
		return err
	}
	r.ClosePanel(id)
	return nil
}

func (r *Registry) focusedTrigger() (uint32, Trigger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var global uint32
	var connector string
	if r.focused != "" {
		for g, bar := range r.bars {
			if bar.connector() == r.focused {
				global, connector = g, r.focused
				break
			}
		}
	}
	if global == 0 {
		for g, bar := range r.bars {
			global, connector = g, bar.connector()
			break
		}
	}
	return global, r.triggerLocked(global, connector)
}

func (r *Registry) triggerFor(global uint32) (uint32, Trigger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	connector := ""
	if bar, ok := r.bars[global]; ok {
		connector = bar.connector()
	}
	return global, r.triggerLocked(global, connector)
}

func (r *Registry) triggerLocked(global uint32, connector string) Trigger {
	policy := r.cfg.ForConnector(connector)
	trig := Trigger{BarEdge: policy.Edge, BarZone: policy.Height - policy.Gap, Align: "center"}
	if bar, ok := r.bars[global]; ok {
		w, h := bar.configuredSize()
		if w > 0 {
			trig.OutW = w
		}
		if h > 0 {
			trig.BarZone = h
		}
	}
	return trig
}

func (r *Registry) OpenPanel(id PanelID, output uint32, trig Trigger) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if where, ok := r.panels.Output(id); ok && where == output && r.roots.owns(panelRoot(id)) {
		return nil
	}
	return r.openPanelRootLocked(id, output, trig)
}

// openPanelRootLocked publishes the panel as the process-wide interactive
// root. Whatever chain was open is released first, so opening an unrelated
// panel closes the previous one and this panel on any other output.
func (r *Registry) openPanelRootLocked(id PanelID, output uint32, trig Trigger) error {
	generation := r.roots.openRoot(panelRoot(id))
	if r.panels.open == nil {
		r.panels.open = make(map[PanelID]uint32)
	}
	r.panels.open[id] = output
	if err := r.spawnPanelLocked(id, output, trig); err != nil {
		r.panels.Close(id)
		r.roots.closeRoot(generation)
		return err
	}
	if id == PanelNotifications {
		r.setCenterOpen(true)
		if ids := r.markCenterSeen(); len(ids) > 0 {
			r.sendNotify(protocol.Command{Kind: protocol.CommandHistoryMarkSeen, IDs: ids})
		}
	}
	r.roots.onClose(generation, func() {
		r.panels.Close(id)
		r.teardownPanelLocked(id)
		// A root that goes away takes any visible tooltip with it.
		r.dwell.leave()
		if id == PanelPlugin && r.plugins != nil {
			ids := r.plugins.snapshotPanelViewIDs()
			go r.plugins.dropPanelViews(ids)
		}
	})
	return nil
}

func (r *Registry) ClosePanel(id PanelID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closePanelLocked(id)
}

// closePanelLocked closes the panel through the root chain when it owns the
// chain, so every release runs exactly once and in one order.
func (r *Registry) closePanelLocked(id PanelID) {
	if _, generation, ok := r.roots.current(); ok && r.roots.owns(panelRoot(id)) {
		r.roots.closeRoot(generation)
		return
	}
	r.panels.Close(id)
	r.teardownPanelLocked(id)
	if id == PanelPlugin && r.plugins != nil {
		ids := r.plugins.snapshotPanelViewIDs()
		go r.plugins.dropPanelViews(ids)
	}
}

func (r *Registry) TogglePanel(id PanelID, output uint32, trig Trigger) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if where, ok := r.panels.Output(id); ok && where == output {
		r.closePanelLocked(id)
		return nil
	}
	// A panel asked for on a different output is a fresh root there; opening
	// releases the chain that held the old instance.
	return r.openPanelRootLocked(id, output, trig)
}

func (r *Registry) DropAux(output uint32, surfaceID string) {
	if r.DropTrayAux(output, surfaceID) {
		return
	}
	id, ok := panelIDFromAux(surfaceID)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.panelHosts[id]
	if h == nil || h.output != output {
		return
	}
	r.closePanelLocked(id)
}

func panelIDFromAux(surfaceID string) (PanelID, bool) {
	name, ok := strings.CutPrefix(surfaceID, "panel:")
	if !ok {
		name, ok = strings.CutPrefix(surfaceID, "shield:")
		if !ok {
			return 0, false
		}
	}
	switch name {
	case "clock":
		return PanelClock, true
	case "system-monitor":
		return PanelMonitor, true
	case "session", "power":
		return PanelSession, true
	case "settings":
		return PanelSettings, true
	case "launcher":
		return PanelLauncher, true
	case "plugin":
		return PanelPlugin, true
	case "notifications":
		return PanelNotifications, true
	default:
		return 0, false
	}
}

func (r *Registry) spawnPanelLocked(id PanelID, output uint32, trig Trigger) error {
	outW, outH := trig.OutW, trig.OutH
	if outW <= 0 {
		outW = 1920
	}
	if outH <= 0 {
		outH = 1080
	}
	size := panelTargetSize(id)
	if id == PanelPlugin && r.plugins != nil {
		size = r.plugins.panelSize()
	}
	gap := r.cfg.Panels.Gap
	if id == PanelPlugin {
		gap = 0
	}
	place := Placement{
		BarEdge: trig.BarEdge,
		Output:  ui.Rect{W: outW, H: outH},
		BarZone: trig.BarZone,
		Gap:     gap,
		Padding: r.cfg.Panels.Padding,
		Panel:   size,
		Align:   trig.Align,
	}
	if id == PanelSettings && place.Align == "" {
		place.Align = "center"
	}
	if id == PanelSession || id == PanelNotifications {
		place.Align = "right"
	}
	if id == PanelLauncher {
		place.CenterY = true
	}

	h := &PanelHost{
		id:          id,
		output:      output,
		place:       place,
		stopAnim:    make(chan struct{}),
		shieldQuiet: time.Now().Add(shieldQuietFor),
		theme:       ThemeFromTokens(r.tokens, 12),
		fontFamily:  r.panelFontFamily(output),
	}
	if bar, ok := r.bars[output]; ok {
		h.scale120 = bar.scale120()
	}
	if id == PanelSettings {
		h.set = settings.DefaultFor(r.cfg)
		h.draft = r.cfg
		h.section = "Bar"
		h.search = ui.NewField("")
		h.menus = map[string]*Menu{}
		h.fields = map[string]*ui.Field{}
	}
	if id == PanelLauncher {
		h.search = ui.NewField("")
		svc := r.launcherServiceLocked()
		svc.Open()
		svc.Query("")
	}
	h.root = r.panelTree(h)
	h.focus = ui.Focusables(h.root)
	h.roving = ui.Roving{Count: len(h.focus)}
	if id == PanelMonitor || id == PanelNotifications {
		_ = h.ensureText()
		if id == PanelNotifications {
			h.place.Panel.H = notificationsSurfaceHeight(h)
		} else {
			h.place.Panel.H = monitorSurfaceHeight(h.root, h.place.Panel.W, h.theme.Radius, h.measureText())
		}
	}
	w, hgt := h.place.FittedSize()
	h.place.Panel.W, h.place.Panel.H = w, hgt
	margins := h.place.Margins()
	if err := r.acquirePanelLeases(h); err != nil {
		return err
	}

	r.panelHosts[id] = h

	r.sendAux(wayland.AuxRequest{Output: output, Open: r.shieldSpec(h)})
	r.sendAux(wayland.AuxRequest{Output: output, Open: r.panelSpec(h, margins)})

	if id == PanelSession {
		r.scheduleLoadProfiles(h)
	}

	if r.cfg.Accessibility.ReducedMotion {
		r.publishSurface(output, panelSurfaceID(id))
		return nil
	}
	h.animStart = time.Now()
	go r.revealLoop(h)
	return nil
}

func (r *Registry) acquirePanelLeases(h *PanelHost) error {
	switch h.id {
	case PanelClock:
		lease, err := r.clock.Acquire(time.Second)
		if err != nil {
			return err
		}
		h.leases = []*services.Lease{lease}
	case PanelMonitor:
		connector := ""
		if bar, ok := r.bars[h.output]; ok {
			connector = bar.connector()
		}
		for _, sel := range monitorSelectors(r.cfg.ForConnector(connector)) {
			lease, err := r.metrics.Acquire(sel, time.Second)
			if err != nil {
				releaseAll(h.leases)
				h.leases = nil
				return err
			}
			h.leases = append(h.leases, lease)
		}
	case PanelSession:
		lease, err := r.metrics.Acquire(services.Selector{Source: services.SourceBattery}, time.Second)
		if err != nil {
			return err
		}
		h.leases = []*services.Lease{lease}
	}
	return nil
}

func monitorSelectors(bar config.Bar) []services.Selector {
	out := []services.Selector{
		{Source: services.SourceCPU},
		{Source: services.SourceMemory},
		{Source: services.SourceGPU},
	}
	seenFS, seenBlock, seenNet := false, false, false
	for _, item := range append(append(append([]config.Item{}, bar.Left...), bar.Center...), bar.Right...) {
		sel, ok := metricSelector(item)
		if !ok {
			continue
		}
		switch sel.Source {
		case services.SourceFilesystem:
			if seenFS {
				continue
			}
			seenFS = true
		case services.SourceBlock:
			if seenBlock {
				continue
			}
			seenBlock = true
		case services.SourceNetwork:
			if seenNet {
				continue
			}
			seenNet = true
		default:
			continue
		}
		out = append(out, sel)
	}
	return out
}

func placeholderTree() *ui.Node {
	btn := func(text, action string) *ui.Node {
		return &ui.Node{
			Kind: ui.KindButton, Text: text, Action: action,
			Name: text, Role: "button", Focusable: true,
		}
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: 8, Padding: 12, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "Panel"},
		btn("Lock", "lock"),
		btn("Two", "two"),
		btn("Three", "three"),
	}}
}

func panelSurfaceID(id PanelID) string  { return "panel:" + id.String() }
func shieldSurfaceID(id PanelID) string { return "shield:" + id.String() }

func (r *Registry) shieldSpec(h *PanelHost) *wayland.AuxSpec {
	return &wayland.AuxSpec{
		ID:            shieldSurfaceID(h.id),
		Namespace:     "sysc-shell-shield",
		Layer:         layerOverlay,
		Anchor:        uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorBottom | layershell.ZwlrLayerSurfaceV1AnchorLeft | layershell.ZwlrLayerSurfaceV1AnchorRight),
		ExclusiveZone: -1,
		Keyboard:      keyboardNone,
		Callbacks: wayland.HostCallbacks{
			Configure: func(int, int, int) error { return nil },
			Render:    func([]byte, int, int, int) error { return nil },
			Handle: func(e wayland.Event) bool {
				if e.Kind == wayland.EventPointerPress {
					if time.Now().Before(h.shieldQuiet) {
						return false
					}
					r.ClosePanel(h.id)
					return true
				}
				return false
			},
		},
	}
}

func (r *Registry) panelSpec(h *PanelHost, m Margins) *wayland.AuxSpec {
	anchor := uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorLeft)
	if h.place.BarEdge == "bottom" {
		anchor = uint32(layershell.ZwlrLayerSurfaceV1AnchorBottom | layershell.ZwlrLayerSurfaceV1AnchorLeft)
	}
	return &wayland.AuxSpec{
		ID:            panelSurfaceID(h.id),
		Namespace:     "sysc-shell-panel",
		Layer:         layerOverlay,
		Anchor:        anchor,
		MarginTop:     int32(m.Top),
		MarginBottom:  int32(m.Bottom),
		MarginLeft:    int32(m.Left),
		MarginRight:   int32(m.Right),
		Width:         int32(h.place.Panel.W),
		Height:        int32(h.place.Panel.H),
		ExclusiveZone: -1,
		Keyboard:      keyboardExclusive,
		Callbacks: wayland.HostCallbacks{
			OpaqueBackground: h.theme.BackgroundOpaque(),
			Configure:        h.configureLocking(r),
			Render:           h.renderLocking(r),
			Handle:           h.handle(r),
			WantIME: func() bool {
				n := h.focused()
				return n != nil && n.Kind == ui.KindTextField
			},
			IBeamAt: func(x, y float64) bool {
				n := h.hitFocusable(int(math.Floor(x)), int(math.Floor(y)))
				return n != nil && n.Kind == ui.KindTextField
			},
		},
	}
}

// panelFontFamily resolves the font of the output the panel opens on. A panel
// is per-output, so a connector with its own bar font must not open a panel in
// the global family.
func (r *Registry) panelFontFamily(output uint32) string {
	connector := ""
	if bar, ok := r.bars[output]; ok {
		connector = bar.connector()
	}
	return r.cfg.ForConnector(connector).FontFamily
}

func (h *PanelHost) ensureText() error {
	if h.text != nil {
		return nil
	}
	fonts, err := render.NewSystemFontMap(h.fontFamily, render.DefaultFontCacheDir())
	if err != nil {
		return err
	}
	h.text = render.NewTextRendererWithFontMap(fonts)
	return nil
}

// The configure and render callbacks take the registry lock, like handle
// already does. Panel geometry and the panel tree are written from relay
// goroutines — the launcher's result relay rebuilds an open panel, which
// re-lays it out at this geometry — so the compositor's callbacks cannot read
// or write it unlocked. rebuildPanel already runs under the lock and calls
// configure directly, which is why the locking wrapper is separate.
func (h *PanelHost) configureLocking(r *Registry) func(int, int, int) error {
	return func(w, height, scale120 int) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		return h.configure(w, height, scale120)
	}
}

func (h *PanelHost) renderLocking(r *Registry) func([]byte, int, int, int) error {
	return func(pixels []byte, width, height, stride int) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		return h.render(pixels, width, height, stride)
	}
}

func (h *PanelHost) configure(w, height, scale120 int) error {
	h.logicalW, h.logicalH, h.scale120 = w, height, scale120
	if err := h.ensureText(); err != nil {
		return err
	}
	box := ui.Rect{W: w, H: height}
	if h.root != nil && h.root.Kind == ui.KindRow {
		return ui.Layout(h.root, box, h.measureText())
	}
	return ui.LayoutColumn(h.root, box, h.measureText())
}

func (h *PanelHost) measureText() ui.MeasureText {
	scale := ui.Scale120(h.scale120)
	if !scale.Valid() {
		scale = ui.ScaleUnit
	}
	size := scale.Physical(h.theme.TextSize)
	if size <= 0 {
		size = h.theme.TextSize
	}
	return func(s string, tabular bool) (int, int) {
		if h.text != nil && size > 0 {
			mw, mh, err := h.text.Measure(s, size, tabular)
			if err == nil {
				return scale.Logical(mw), scale.Logical(mh)
			}
		}
		return len(s) * 8, 16
	}
}

func (h *PanelHost) render(pixels []byte, width, height, stride int) error {
	if err := h.ensureText(); err != nil {
		return err
	}
	c, err := render.NewCanvas(pixels, width, height, stride)
	if err != nil {
		return err
	}
	scale := ui.Scale120(h.scale120)
	if !scale.Valid() {
		scale = ui.ScaleUnit
	}
	body := ui.Rect{W: h.logicalW, H: h.logicalH}
	if body.W <= 0 || body.H <= 0 {
		body = ui.Rect{W: h.place.Panel.W, H: h.place.Panel.H}
	}
	style := h.theme.ProofStyle()
	style.Scale120 = scale
	style.Body = body
	if !h.place.CenterY {
		style.AttachEdge = h.place.BarEdge
	}
	if err := render.Paint(c, h.root, h.text, style); err != nil {
		return err
	}
	if h.roving.Count > 0 {
		n := h.focus[h.roving.Index()]
		if n != nil && n.Bounds.W > 0 && n.Kind != ui.KindTextField {
			// The ring follows the node's own silhouette rather than boxing a
			// stadium in square corners, and stays independent of hover: a
			// focused control that is not hovered still shows it. A text field
			// is excluded because it paints its own focused well.
			ring := scale.PhysicalRect(n.Bounds)
			radius := min(scale.Physical(h.theme.Radius), min(ring.W, ring.H)/2)
			c.StrokeRounded(ring, radius, 2, h.theme.Accent)
		}
	}
	return nil
}

func (h *PanelHost) handle(r *Registry) func(wayland.Event) bool {
	return func(e wayland.Event) bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		switch e.Kind {
		case wayland.EventKeyPress:
			return h.keyPress(r, e.Key)
		case wayland.EventIME:
			return h.applyIME(r, e)
		case wayland.EventPointerAxis:
			return h.scrollAxis(r, e)
		case wayland.EventKeyRelease:
			if e.Key == keyLeftShift {
				h.shift = false
			}
			return false
		case wayland.EventPointerEnter, wayland.EventPointerMotion:
			h.hoverX, h.hoverY = int(math.Floor(e.X)), int(math.Floor(e.Y))
			if h.drag.Source != nil {
				h.drag.Move(e.X, e.Y)
				return h.drag.Active()
			}
			if h.sliderDrag != nil {
				ui.SliderAt(h.sliderDrag, h.hoverX)
				return true
			}
			if h.scrollDrag != nil {
				ui.ScrollSetFromY(h.scrollDrag, h.hoverY)
				if h.logicalW > 0 {
					_ = h.configure(h.logicalW, h.logicalH, h.scale120)
				}
				return true
			}
			return false
		case wayland.EventPointerLeave:
			h.pressed = nil
			h.sliderDrag = nil
			h.scrollDrag = nil
			return false
		case wayland.EventPointerPress:
			h.hoverX, h.hoverY = int(math.Floor(e.X)), int(math.Floor(e.Y))
			if h.id == PanelLauncher && h.launcherPointerPress(r, e) {
				return true
			}
			if s := findScroll(h.root); s != nil && ui.ScrollTrack(s).Contains(h.hoverX, h.hoverY) {
				h.scrollDrag = s
				ui.ScrollSetFromY(s, h.hoverY)
				if h.logicalW > 0 {
					_ = h.configure(h.logicalW, h.logicalH, h.scale120)
				}
				return true
			}
			if n := h.hitFocusable(h.hoverX, h.hoverY); n != nil {
				h.pressed = n
				h.setFocus(n)
				if n.Kind == ui.KindDragSource {
					h.drag.Begin(n, e.X, e.Y)
				}
				if n.Kind == ui.KindSlider {
					ui.SliderAt(n, h.hoverX)
					h.sliderDrag = n
				}
				return true
			}
			return false
		case wayland.EventPointerRelease:
			h.hoverX, h.hoverY = int(math.Floor(e.X)), int(math.Floor(e.Y))
			if h.sliderDrag != nil {
				n := h.sliderDrag
				ui.SliderAt(n, h.hoverX)
				h.sliderDrag = nil
				h.pressed = nil
				if strings.HasPrefix(n.Action, "plugin-set:") {
					return r.handlePluginManager(h, n)
				}
				h.applySetting(r, n)
				return true
			}
			if h.scrollDrag != nil {
				h.scrollDrag = nil
				return true
			}
			if h.drag.Active() {
				zone := ui.FindDropZone(h.root, &h.drag)
				payload, ok := h.drag.Drop(zone)
				h.drag.Cancel()
				if ok && zone != nil {
					return r.deliverPluginText(zone.Action, payload, v1.EventDrop)
				}
				return true
			}
			n := h.hitFocusable(h.hoverX, h.hoverY)
			pressed := h.pressed
			h.pressed = nil
			if n != nil && n == pressed {
				return h.activate(r)
			}
			return false
		}
		return false
	}
}

func (h *PanelHost) keyPress(r *Registry, key uint32) bool {
	if h.menu != nil && h.menu.Opened() {
		if !h.menu.Handle(key) {
			return false
		}
		if !h.menu.Opened() && key != keyEsc {
			if h.id == PanelLauncher {
				h.applyLauncherMenu(r)
			} else if strings.HasPrefix(h.menuPath, "plugin-set:") {
				_ = r.handlePluginManager(h, &ui.Node{
					Kind: ui.KindMenu, Action: h.menuPath, Text: h.menu.Value(),
				})
			} else {
				h.applyMenu(r, h.menuPath)
			}
		}
		r.rebuildPanel(h)
		return true
	}
	if key == keyBackspace {
		return h.editField(r, func(f *ui.Field) { f.Backspace() })
	}
	if ch, ok := ui.EvdevText(key, h.shift); ok {
		return h.editField(r, func(f *ui.Field) { f.Insert(ch) })
	}
	if h.id == PanelLauncher && h.launcherKeyPress(r, key) {
		return true
	}
	switch key {
	case keyLeftShift:
		h.shift = true
		return false
	case keyEsc:
		if h.id == PanelSettings && h.query != "" {
			h.query = ""
			h.search = ui.NewField("")
			r.rebuildPanel(h)
			return true
		}
		r.closePanelLocked(h.id)
		return true
	case keyTab:
		if h.shift {
			h.roving.Prev()
		} else {
			h.roving.Next()
		}
		h.afterFocusChange(r)
		return true
	case keyLeft, keyUp:
		if h.adjustSlider(r, keyLeft) {
			return true
		}
		h.roving.Prev()
		h.afterFocusChange(r)
		return true
	case keyRight, keyDown:
		if h.adjustSlider(r, keyRight) {
			return true
		}
		h.roving.Next()
		h.afterFocusChange(r)
		return true
	case keyHome, keyEnd:
		if h.adjustSlider(r, key) {
			return true
		}
		if key == keyHome {
			return h.scrollTo(0)
		}
		return h.scrollTo(1 << 30)
	case keyPageUp:
		return h.scrollBy(-max(h.logicalH, 1))
	case keyPageDown:
		return h.scrollBy(max(h.logicalH, 1))
	case keySpace, keyEnter:
		return h.activate(r)
	}
	return false
}

func (h *PanelHost) scrollAxis(r *Registry, e wayland.Event) bool {
	delta := 0
	switch {
	case e.AxisDiscrete != 0:
		delta = int(e.AxisDiscrete) * 40
	case e.AxisValue120 != 0:
		delta = int(e.AxisValue120) * 40 / 120
	default:
		delta = int(e.AxisValue)
	}
	if delta == 0 {
		return false
	}
	if h.id == PanelLauncher {
		rows := delta / launcherSlotHeight
		if rows == 0 {
			if delta > 0 {
				rows = 1
			} else {
				rows = -1
			}
		}
		h.launcherMoveSel(r, rows)
		return true
	}
	return h.scrollBy(delta)
}

func (h *PanelHost) scrollBy(delta int) bool {
	s := findScroll(h.root)
	if s == nil {
		return false
	}
	ui.ScrollBy(s, delta)
	if h.id == PanelLauncher {
		h.launcherScroll = s.ScrollOffset
	}
	if h.logicalW > 0 {
		_ = h.configure(h.logicalW, h.logicalH, h.scale120)
	}
	return true
}

func (h *PanelHost) scrollTo(off int) bool {
	s := findScroll(h.root)
	if s == nil {
		return false
	}
	s.ScrollOffset = off
	ui.ScrollBy(s, 0)
	if h.logicalW > 0 {
		_ = h.configure(h.logicalW, h.logicalH, h.scale120)
	}
	return true
}

func findScroll(n *ui.Node) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Kind == ui.KindScroll || n.Kind == ui.KindVirtualList {
		return n
	}
	for _, c := range n.Children {
		if got := findScroll(c); got != nil {
			return got
		}
	}
	return nil
}

func (h *PanelHost) applyIME(r *Registry, e wayland.Event) bool {
	return h.editField(r, func(f *ui.Field) {
		f.DeleteSurrounding(int(e.IMEDeleteBefore), int(e.IMEDeleteAfter))
		if e.IMECommit != "" {
			f.Commit(e.IMECommit)
		}
		f.Preedit(e.IMEPreedit)
	})
}

func (h *PanelHost) editField(r *Registry, fn func(*ui.Field)) bool {
	n := h.focused()
	if n == nil || n.Kind != ui.KindTextField {
		return false
	}
	var f *ui.Field
	if n.Name == "Search" {
		if h.search == nil {
			h.search = ui.NewField("")
		}
		h.search.SyncFrom(n)
		f = h.search
	} else if _, ok := parsePluginAction(n.Action); ok {
		k := n.StableKey()
		if h.editors == nil {
			h.editors = map[string]*retainedEditor{}
		}
		slot := h.editors[k]
		if slot == nil {
			slot = &retainedEditor{field: &ui.Field{
				Text: n.Text, PreeditText: n.Preedit, Cursor: n.Cursor,
				Multiline: n.Multiline, SubmitOnEnter: n.SubmitOnEnter,
			}, reseed: n.Reseed}
			h.editors[k] = slot
		}
		slot.field.SyncFrom(n)
		f = slot.field
	} else if store := pluginSettingStoreKey(n.Action); store != "" {
		if h.fields == nil {
			h.fields = map[string]*ui.Field{}
		}
		f = h.fields[store]
		if f == nil {
			f = &ui.Field{Text: n.Text, PreeditText: n.Preedit, Cursor: n.Cursor}
			h.fields[store] = f
		} else {
			f.SyncFrom(n)
		}
	} else {
		path, _ := strings.CutPrefix(n.Action, "set:")
		if h.fields == nil {
			h.fields = map[string]*ui.Field{}
		}
		f = h.fields[path]
		if f == nil {
			f = &ui.Field{Text: n.Text, PreeditText: n.Preedit, Cursor: n.Cursor}
			h.fields[path] = f
		} else {
			f.SyncFrom(n)
		}
	}
	fn(f)
	f.SyncTo(n)
	if _, ok := parsePluginAction(n.Action); ok {
		r.deliverPluginText(n.Action, n.Text, v1.EventChange)
		return true
	}
	if n.Name == "Search" {
		h.query = f.Text
		if h.id == PanelLauncher {
			h.launcherSel = 0
			h.launcherScroll = 0
			r.launcherServiceLocked().Query(h.query)
		}
		idx := h.roving.Index()
		r.rebuildPanel(h)
		h.roving.Set(idx)
		return true
	}
	if strings.HasPrefix(n.Action, "plugin-set:") {
		return r.handlePluginManager(h, n)
	}
	h.applySetting(r, n)
	return true
}

func (h *PanelHost) focused() *ui.Node {
	if h.roving.Count == 0 {
		return nil
	}
	return h.focus[h.roving.Index()]
}

func (h *PanelHost) adjustSlider(r *Registry, key uint32) bool {
	n := h.focused()
	if n == nil || n.Kind != ui.KindSlider {
		return false
	}
	if !ui.ControlKey(n, key) {
		return false
	}
	if strings.HasPrefix(n.Action, "plugin-set:") {
		return r.handlePluginManager(h, n)
	}
	h.applySetting(r, n)
	return true
}

func (h *PanelHost) activate(r *Registry) bool {
	n := h.focused()
	if n == nil {
		return false
	}
	switch n.Action {
	case "plugin-close", "plugin-retry", "plugin-disable":
		if r.plugins != nil {
			r.plugins.retryOrDisable(n.Action)
		}
		return true
	}
	if _, ok := parsePluginAction(n.Action); ok {
		if n.Kind == ui.KindTextField {
			return r.deliverPluginText(n.Action, n.Text, v1.EventSubmit)
		}
		return r.handlePluginBar(n.Action, wayland.Event{Kind: wayland.EventPointerRelease, Button: 272})
	}
	if strings.HasPrefix(n.Action, "plugin-set:") {
		switch n.Kind {
		case ui.KindToggle:
			ui.Activate(n)
			return r.handlePluginManager(h, n)
		case ui.KindText:
			for _, f := range h.focus {
				if f != nil && f.Kind == ui.KindToggle && f.Action == n.Action {
					ui.Activate(f)
					return r.handlePluginManager(h, f)
				}
			}
			return false
		case ui.KindMenu:
			store := pluginSettingStoreKey(n.Action)
			if m := h.menus[store]; m != nil {
				h.menu = m
				h.menuPath = n.Action
				if !m.Opened() {
					m.Open()
					r.rebuildPanel(h)
					return true
				}
				m.PickAt(n, h.hoverX, h.hoverY)
				m.Select()
				n.Text = m.Value()
				return r.handlePluginManager(h, n)
			}
			return false
		default:
			return r.handlePluginManager(h, n)
		}
	}
	if r.handlePluginManager(h, n) {
		return true
	}
	if h.id == PanelLauncher {
		return h.activateLauncher(r, n)
	}
	if n.Kind == ui.KindToggle {
		changed := ui.Activate(n)
		h.applySetting(r, n)
		return changed
	}
	if n.Kind == ui.KindMenu {
		path, _ := strings.CutPrefix(n.Action, "set:")
		if m := h.menus[path]; m != nil {
			h.menu = m
			h.menuPath = path
			if !m.Opened() {
				m.Open()
				r.rebuildPanel(h)
				return true
			}
			m.PickAt(n, h.hoverX, h.hoverY)
			m.Select()
			h.applyMenu(r, path)
			r.rebuildPanel(h)
			return true
		}
		if h.menu != nil && !h.menu.Opened() {
			h.menu.Open()
			return true
		}
		return false
	}
	if strings.HasPrefix(n.Action, "notify:") {
		return h.activateNotify(r, n)
	}
	if strings.HasPrefix(n.Action, "section:") {
		h.section = strings.TrimPrefix(n.Action, "section:")
		r.rebuildPanel(h)
		return true
	}
	if path, ok := strings.CutPrefix(n.Action, "goto:"); ok {
		if e := h.set.ByPath(path); e != nil {
			h.section = e.Section
			h.query = ""
			h.search = ui.NewField("")
			r.rebuildPanel(h)
			h.focusByName(e.Label)
		}
		return true
	}
	if name, ok := strings.CutPrefix(n.Action, "profile:"); ok {
		r.setSessionProfile(h, name)
		return true
	}
	h.lastAction = n.Action
	switch n.Action {
	case "cal-prev":
		h.monthDelta--
		r.rebuildPanel(h)
	case "cal-next":
		h.monthDelta++
		r.rebuildPanel(h)
	case "session-lock", "session-logout", "session-suspend", "session-reboot", "session-poweroff":
		r.runSessionAction(h, n.Action)
	}
	return true
}

func (h *PanelHost) activateNotify(r *Registry, n *ui.Node) bool {
	action := n.Action
	h.lastAction = action
	if rest, ok := strings.CutPrefix(action, "notify:center:"); ok {
		switch {
		case rest == "dismiss-all":
			r.sendNotify(protocol.Command{Kind: protocol.CommandDismissAll})
		case rest == "clear-history":
			r.sendNotify(protocol.Command{Kind: protocol.CommandHistoryClear})
		case rest == "dnd":
			_, on := r.notify.dndState(r.clockNow())
			r.notify.setDND(!on)
			if r.toasts != nil {
				r.toasts.recompute()
			}
			r.rebuildPanel(h)
		case rest == "schedule":
			h.notifyMenu = !h.notifyMenu
			r.rebuildPanel(h)
		case strings.HasPrefix(rest, "tab:"):
			h.notifyTab, _ = strconv.Atoi(strings.TrimPrefix(rest, "tab:"))
			r.rebuildPanel(h)
		case strings.HasPrefix(rest, "filter:"):
			h.notifyFilter = strings.TrimPrefix(rest, "filter:")
			r.rebuildPanel(h)
		case strings.HasPrefix(rest, "expand:"):
			key := strings.TrimPrefix(rest, "expand:")
			if h.notifyExpand == key {
				h.notifyExpand = ""
			} else {
				h.notifyExpand = key
			}
			r.rebuildPanel(h)
		case strings.HasPrefix(rest, "dismiss-group:"):
			key := strings.TrimPrefix(rest, "dismiss-group:")
			for _, id := range r.notify.idsForGroup(key) {
				r.sendNotify(protocol.Command{Kind: protocol.CommandDismiss, ID: id})
			}
		case strings.HasPrefix(rest, "preset:"):
			id := strings.TrimPrefix(rest, "preset:")
			now := r.clockNow()
			if d, untilOff, ok := dndPresetDuration(id, now); ok {
				if untilOff {
					r.notify.setDND(true)
				} else {
					r.notify.setDNDPreset(now, d)
				}
				if r.toasts != nil {
					r.toasts.recompute()
				}
			}
			h.notifyMenu = false
			r.rebuildPanel(h)
		}
		return true
	}
	id, parts, ok := parseCardAction(action)
	if !ok || len(parts) == 0 {
		return true
	}
	switch parts[0] {
	case "dismiss":
		r.sendNotify(protocol.Command{Kind: protocol.CommandDismiss, ID: id})
	case "default":
		r.sendNotify(protocol.Command{Kind: protocol.CommandAction, ID: id, ActionKey: "default"})
	case "action":
		if len(parts) == 2 {
			r.sendNotify(protocol.Command{Kind: protocol.CommandAction, ID: id, ActionKey: parts[1]})
		}
	}
	return true
}

func (h *PanelHost) afterFocusChange(r *Registry) {
	if h.id == PanelMonitor {
		r.rebuildPanel(h)
	}
}

func (r *Registry) rebuildPanel(h *PanelHost) {
	idx := h.roving.Index()
	h.root = r.panelTree(h)
	if h.id == PanelPlugin {
		if h.editors == nil {
			h.editors = map[string]*retainedEditor{}
		}
		overlayEditors(h.root, h.editors)
	}
	h.focus = ui.Focusables(h.root)
	h.roving.Count = len(h.focus)
	h.roving.Set(idx)
	if h.id == PanelNotifications {
		r.syncNotificationsSize(h)
	}
	if h.logicalW > 0 {
		_ = h.configure(h.logicalW, h.logicalH, h.scale120)
	}
}

func (r *Registry) panelTree(h *PanelHost) *ui.Node {
	switch h.id {
	case PanelClock:
		now := r.now
		if now.IsZero() {
			now = time.Now()
		}
		return clockTree(now, h.monthDelta)
	case PanelMonitor:
		connector := ""
		if bar, ok := r.bars[h.output]; ok {
			connector = bar.connector()
		}
		return monitorTree(monitorSelectors(r.cfg.ForConnector(connector)), r.sample, r.historyLocked(), readMachineFacts())
	case PanelSession:
		return sessionTree(h, r.sample, r.cfg.Session.Locker)
	case PanelSettings:
		return settingsTree(r, h)
	case PanelLauncher:
		return launcherTree(r, h)
	case PanelPlugin:
		if r.plugins != nil {
			return r.plugins.panelTree(h)
		}
		return pluginPanelError("starting", false)
	case PanelNotifications:
		return r.centerTreeFor(h)
	default:
		return placeholderTree()
	}
}

func panelTargetSize(id PanelID) ui.Rect {
	switch id {
	case PanelClock:
		return ui.Rect{W: 360, H: 420}
	case PanelMonitor:
		return ui.Rect{W: 640, H: 480}
	case PanelSettings:
		return ui.Rect{W: 900, H: 620}
	case PanelLauncher:
		return ui.Rect{W: 560, H: 500}
	case PanelSession:
		return ui.Rect{W: 420, H: 360}
	case PanelPlugin:
		// Fallback when no plugin view has declared a size yet.
		return ui.Rect{W: 320, H: 280}
	case PanelNotifications:
		return ui.Rect{W: 416, H: 300}
	default:
		return ui.Rect{W: 280, H: 200}
	}
}

// monitorSurfaceHeight is the tree's intrinsic height plus two radii of
// empty chrome so the rounded bottom clears the last row. One radius still
// clipped Uptime on the 1.25 laptop.
func monitorSurfaceHeight(root *ui.Node, width, radius int, measure ui.MeasureText) int {
	fallback := panelTargetSize(PanelMonitor).H
	if root == nil || measure == nil {
		return fallback
	}
	ht, err := ui.ContentHeight(root, width, measure)
	if err != nil || ht <= 0 {
		return fallback
	}
	if radius < 0 {
		radius = 0
	}
	return ht + 2*radius
}

func notificationsSurfaceHeight(h *PanelHost) int {
	ht := monitorSurfaceHeight(h.root, h.place.Panel.W, h.theme.Radius, h.measureText())
	maxH := min(h.place.Output.H*8/10, 648)
	return max(300, min(ht, maxH))
}

func (r *Registry) syncNotificationsSize(h *PanelHost) {
	_ = h.ensureText()
	h.place.Panel.H = notificationsSurfaceHeight(h)
	w, hgt := h.place.FittedSize()
	h.place.Panel.W, h.place.Panel.H = w, hgt
}

func (h *PanelHost) applySetting(r *Registry, n *ui.Node) {
	if h.set == nil || n == nil {
		return
	}
	path, ok := strings.CutPrefix(n.Action, "set:")
	if !ok {
		return
	}
	e := h.set.ByPath(path)
	if e == nil {
		return
	}
	var v string
	switch n.Kind {
	case ui.KindToggle:
		v = "false"
		if n.Value != 0 {
			v = "true"
		}
	case ui.KindSlider:
		v = strconv.Itoa(int(n.Value))
	case ui.KindTextField:
		v = n.Text
	case ui.KindMenu:
		v = n.Text
	}
	if err := e.Set(&h.draft, v); err != nil {
		h.errLabel = err.Error()
		r.rebuildPanel(h)
		return
	}
	h.persistDraft(r)
}

func (h *PanelHost) applyMenu(r *Registry, path string) {
	if h.set == nil || path == "" {
		return
	}
	e := h.set.ByPath(path)
	m := h.menus[path]
	if e == nil || m == nil {
		return
	}
	if err := e.Set(&h.draft, m.Value()); err != nil {
		h.errLabel = err.Error()
		r.rebuildPanel(h)
		return
	}
	h.persistDraft(r)
}

func (h *PanelHost) focusByName(name string) {
	for i, n := range h.focus {
		if n != nil && n.Name == name {
			h.roving.Set(i)
			return
		}
	}
}

func (h *PanelHost) setFocus(n *ui.Node) {
	for i, f := range h.focus {
		if f == n {
			h.roving.Set(i)
			return
		}
	}
}

func (h *PanelHost) hitFocusable(x, y int) *ui.Node {
	for _, n := range h.focus {
		if n.Bounds.Contains(x, y) {
			return n
		}
	}
	return nil
}

func (h *PanelHost) stopAnimation() {
	h.stopOnce.Do(func() { close(h.stopAnim) })
}

func (r *Registry) revealLoop(h *PanelHost) {
	tick := time.NewTicker(revealTick)
	defer tick.Stop()
	for {
		select {
		case <-h.stopAnim:
			return
		case <-tick.C:
			if time.Since(h.animStart) >= revealDuration {
				r.publishSurface(h.output, panelSurfaceID(h.id))
				return
			}
			r.publishSurface(h.output, panelSurfaceID(h.id))
		}
	}
}

func (r *Registry) teardownPanelLocked(id PanelID) {
	if id == PanelNotifications {
		r.setCenterOpen(false)
	}
	h := r.panelHosts[id]
	if h == nil {
		return
	}
	h.stopAnimation()
	h.drag.Cancel()
	delete(r.panelHosts, id)
	r.sendAux(wayland.AuxRequest{Output: h.output, ID: panelSurfaceID(id)})
	r.sendAux(wayland.AuxRequest{Output: h.output, ID: shieldSurfaceID(id)})
	releaseAll(h.leases)
	h.leases = nil
	h.editors = nil
}

func (r *Registry) sendAux(req wayland.AuxRequest) {
	select {
	case r.aux <- req:
	case <-r.closed:
	}
}

func (r *Registry) publishSurface(global uint32, surfaceID string) {
	select {
	case r.invalidations <- wayland.Invalidation{Global: global, SurfaceID: surfaceID}:
	case <-r.closed:
	default:
		// Drop when the owner is behind rather than stalling the caller.
	}
}

func (r *Registry) runSessionAction(h *PanelHost, action string) {
	argv := sessionArgv(action, r.cfg.Session.Locker)
	if err := r.runArgv(argv); err != nil {
		h.errLabel = err.Error()
		r.rebuildPanel(h)
		return
	}
	r.closePanelLocked(h.id)
}

func (r *Registry) closeAllPanelsLocked() {
	r.roots.release()
	ids := make([]PanelID, 0, len(r.panelHosts))
	for id := range r.panelHosts {
		ids = append(ids, id)
	}
	for _, id := range ids {
		r.panels.Close(id)
		r.teardownPanelLocked(id)
	}
}

type retainedEditor struct {
	field  *ui.Field
	reseed uint64
}

func overlayEditors(root *ui.Node, eds map[string]*retainedEditor) {
	if eds == nil {
		return
	}
	seen := map[string]bool{}
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindTextField {
			k := n.StableKey()
			if k != "" {
				seen[k] = true
				slot := eds[k]
				if slot == nil || n.Reseed > slot.reseed {
					eds[k] = &retainedEditor{
						field: &ui.Field{
							Text: n.Text, PreeditText: n.Preedit, Cursor: n.Cursor,
							Multiline: n.Multiline, SubmitOnEnter: n.SubmitOnEnter,
						},
						reseed: n.Reseed,
					}
				} else {
					slot.field.Multiline = n.Multiline
					slot.field.SubmitOnEnter = n.SubmitOnEnter
					slot.field.SyncTo(n)
				}
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	for k := range eds {
		if !seen[k] {
			delete(eds, k)
		}
	}
}
