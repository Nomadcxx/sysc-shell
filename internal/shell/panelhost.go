package shell

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
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
	lastAction     string
	hoverX, hoverY int
	monthDelta     int
	errLabel       string
	menu           *Menu
	menuPath       string
	menus          map[string]*Menu
	set            *settings.Registry
	draft          config.Config
	query          string
	section        string
	search         *ui.Field
	fields         map[string]*ui.Field

	launcherResults []launcher.Result
	launcherSel     int
	launcherScroll  int
	launcherMenuID  string
	launcherActions []launcher.Action
}

func parsePanelName(name string) (PanelID, error) {
	switch name {
	case "clock":
		return PanelClock, nil
	case "system-monitor":
		return PanelMonitor, nil
	case "session":
		return PanelSession, nil
	case "settings":
		return PanelSettings, nil
	case "launcher":
		return PanelLauncher, nil
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
	policy := r.cfg.ForConnector(connector)
	return global, Trigger{BarEdge: policy.Edge, BarZone: policy.Height, Align: ""}
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
	r.roots.onClose(generation, func() {
		r.panels.Close(id)
		r.teardownPanelLocked(id)
		// A root that goes away takes any visible tooltip with it.
		r.dwell.leave()
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
	case "session":
		return PanelSession, true
	case "settings":
		return PanelSettings, true
	case "launcher":
		return PanelLauncher, true
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
	place := Placement{
		BarEdge: trig.BarEdge,
		Output:  ui.Rect{W: outW, H: outH},
		BarZone: trig.BarZone,
		Gap:     r.cfg.Panels.Gap,
		Padding: r.cfg.Panels.Padding,
		Panel:   panelTargetSize(id),
		Align:   trig.Align,
	}
	if id == PanelSettings && place.Align == "" {
		place.Align = "center"
	}
	if id == PanelLauncher {
		place.CenterY = true
	}
	w, hgt := place.FittedSize()
	place.Panel.W, place.Panel.H = w, hgt
	margins := place.Margins()

	h := &PanelHost{
		id:         id,
		output:     output,
		place:      place,
		stopAnim:   make(chan struct{}),
		theme:      ThemeFromTokens(r.tokens, 12),
		fontFamily: r.panelFontFamily(output),
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
	if err := r.acquirePanelLeases(h); err != nil {
		return err
	}

	r.panelHosts[id] = h

	r.sendAux(wayland.AuxRequest{Output: output, Open: r.shieldSpec(h)})
	r.sendAux(wayland.AuxRequest{Output: output, Open: r.panelSpec(h, margins)})

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
	}
	return nil
}

func monitorSelectors(bar config.Bar) []services.Selector {
	out := []services.Selector{
		{Source: services.SourceCPU},
		{Source: services.SourceMemory},
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

func logicalFromPhysical(scale ui.Scale120, phys int) int {
	if !scale.Valid() || scale == ui.ScaleUnit {
		return phys
	}
	return phys * 120 / int(scale)
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
	scale := ui.Scale120(scale120)
	if !scale.Valid() {
		scale = ui.ScaleUnit
	}
	size := scale.Physical(h.theme.TextSize)
	if size <= 0 {
		size = h.theme.TextSize
	}
	measure := func(s string, tabular bool) (int, int) {
		if h.text != nil && size > 0 {
			mw, mh, err := h.text.Measure(s, size, tabular)
			if err == nil {
				return logicalFromPhysical(scale, mw), logicalFromPhysical(scale, mh)
			}
		}
		return len(s) * 8, 16
	}
	box := ui.Rect{W: w, H: height}
	if h.root != nil && h.root.Kind == ui.KindRow {
		return ui.Layout(h.root, box, measure)
	}
	return ui.LayoutColumn(h.root, box, measure)
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
	style := render.ProofStyle{
		Size:       h.theme.TextSize,
		Scale120:   scale,
		Body:       body,
		Radius:     12,
		Background: h.theme.Background,
		Foreground: h.theme.Foreground,
		Track:      h.theme.Muted,
		Accent:     h.theme.Accent,
		AccentOn:   h.theme.Error,
		Error:      h.theme.Error,
	}
	if err := render.Paint(c, h.root, h.text, style); err != nil {
		return err
	}
	if h.roving.Count > 0 {
		n := h.focus[h.roving.Index()]
		if n != nil && n.Bounds.W > 0 {
			ring := scale.PhysicalRect(n.Bounds)
			c.FillRounded(ui.Rect{X: ring.X, Y: ring.Y, W: ring.W, H: 2}, 0, h.theme.Accent)
			c.FillRounded(ui.Rect{X: ring.X, Y: ring.Y + ring.H - 2, W: ring.W, H: 2}, 0, h.theme.Accent)
			c.FillRounded(ui.Rect{X: ring.X, Y: ring.Y, W: 2, H: ring.H}, 0, h.theme.Accent)
			c.FillRounded(ui.Rect{X: ring.X + ring.W - 2, Y: ring.Y, W: 2, H: ring.H}, 0, h.theme.Accent)
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
			return h.scrollAxis(e)
		case wayland.EventKeyRelease:
			if e.Key == keyLeftShift {
				h.shift = false
			}
			return false
		case wayland.EventPointerEnter, wayland.EventPointerMotion:
			h.hoverX, h.hoverY = int(math.Floor(e.X)), int(math.Floor(e.Y))
			return false
		case wayland.EventPointerLeave:
			h.pressed = nil
			return false
		case wayland.EventPointerPress:
			if h.id == PanelLauncher && h.launcherPointerPress(r, e) {
				return true
			}
			if n := h.hitFocusable(h.hoverX, h.hoverY); n != nil {
				h.pressed = n
				h.setFocus(n)
				return true
			}
			return false
		case wayland.EventPointerRelease:
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

func (h *PanelHost) scrollAxis(e wayland.Event) bool {
	delta := int(e.AxisValue)
	if e.AxisDiscrete != 0 {
		delta = int(e.AxisDiscrete) * 40
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
	h.applySetting(r, n)
	return true
}

func (h *PanelHost) activate(r *Registry) bool {
	n := h.focused()
	if n == nil {
		return false
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

func (h *PanelHost) afterFocusChange(r *Registry) {
	if h.id == PanelMonitor {
		r.rebuildPanel(h)
	}
}

func (r *Registry) rebuildPanel(h *PanelHost) {
	h.root = r.panelTree(h)
	h.focus = ui.Focusables(h.root)
	h.roving.Count = len(h.focus)
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
		return monitorTree(monitorSelectors(r.cfg.ForConnector(connector)), r.sample, r.historyLocked(), h.roving.Index())
	case PanelSession:
		return sessionTree(r.cfg.Session.Locker, h.errLabel)
	case PanelSettings:
		return settingsTree(h)
	case PanelLauncher:
		return launcherTree(h)
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
	default:
		return ui.Rect{W: 280, H: 200}
	}
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
	h := r.panelHosts[id]
	if h == nil {
		return
	}
	h.stopAnimation()
	delete(r.panelHosts, id)
	r.sendAux(wayland.AuxRequest{Output: h.output, ID: panelSurfaceID(id)})
	r.sendAux(wayland.AuxRequest{Output: h.output, ID: shieldSurfaceID(id)})
	releaseAll(h.leases)
	h.leases = nil
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
