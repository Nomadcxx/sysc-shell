package shell

import (
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	runningAppMenuSurfaceID = "running-app-menu"
	runningAppMenuNamespace = "sysc-shell-running-app-menu"
	runningAppMenuShieldID  = "shield:running-app-menu"
	runningAppMenuPad       = 4
	runningAppMenuRadius    = 6
	runningAppMenuSep       = 9
)

type runningAppMenuHost struct {
	r          *Registry
	request    func(wayland.AuxRequest)
	open_      bool
	closed     bool
	output     uint32
	rootGen    uint64
	slot       runningAppSlot
	rows       []runningAppMenuRow
	menu       *Menu
	root       *ui.Node
	logicalW   int
	logicalH   int
	scale120   int
	place      trayMenuPlacement
	pressed    int
	pointerRow int
	keyed      bool
	text       *render.TextRenderer
	style      render.Style
}

func newRunningAppMenuHost(r *Registry) *runningAppMenuHost {
	return &runningAppMenuHost{
		r:       r,
		request: func(req wayland.AuxRequest) { r.sendAux(req) },
	}
}

func (h *runningAppMenuHost) openLocked(output uint32, slot runningAppSlot, anchor ui.Rect) {
	if h.open_ {
		h.closeLocked()
	}
	h.open_ = true
	h.closed = false
	h.output = output
	h.place = trayMenuUnderBar(anchor)
	h.pressed = -1
	h.pointerRow = -1
	h.keyed = false
	h.slot = slot
	h.rows = runningAppMenu(slot)
	labels := make([]string, len(h.rows))
	for i, row := range h.rows {
		labels[i] = row.Label
	}
	h.menu = NewMenu(labels, 0)
	h.menu.Open()
	h.rebuild()
	h.rootGen = h.r.roots.openRoot(runningAppsMenuRoot(output))
	h.r.roots.onClose(h.rootGen, h.releaseForChainClose)
	h.r.dwell.leave()
	h.request(wayland.AuxRequest{Output: output, Open: h.shieldSpec()})
	h.request(wayland.AuxRequest{Output: output, Open: h.spec()})
}

func (h *runningAppMenuHost) releaseForChainClose() {
	if !h.open_ {
		return
	}
	h.open_ = false
	h.closeSurface()
}

func (h *runningAppMenuHost) spec() *wayland.AuxSpec {
	width, height := h.size()
	place := h.place
	if place.anchor == 0 {
		place = trayMenuUnderBar(ui.Rect{})
	}
	return &wayland.AuxSpec{
		ID: runningAppMenuSurfaceID, Namespace: runningAppMenuNamespace,
		Layer:       layershell.ZwlrLayerShellV1LayerOverlay,
		Anchor:      place.anchor,
		MarginTop:   place.marginTop,
		MarginLeft:  place.marginLeft,
		MarginRight: place.marginRight,
		Width:       int32(width), Height: int32(height),
		ExclusiveZone: -1, Keyboard: keyboardExclusive,
		Callbacks: wayland.HostCallbacks{
			Configure: h.configureLocking, Render: h.renderLocking, Handle: h.handleLocking,
		},
	}
}

func (h *runningAppMenuHost) size() (int, int) {
	n := len(h.rows)
	if n == 0 {
		n = 1
	}
	height := n*trayMenuRowHeight + 2*runningAppMenuPad
	if h.hasCloseSeparator() {
		height += runningAppMenuSep
	}
	return h.menuWidth(), height
}

func (h *runningAppMenuHost) hasCloseSeparator() bool {
	return len(h.rows) > 1 && h.rows[len(h.rows)-1].CloseAll
}

func (h *runningAppMenuHost) menuWidth() int {
	maxW := 0
	for _, row := range h.rows {
		w := len([]rune(row.Label)) * 8
		if h.text != nil && h.style.Size > 0 {
			if mw, _, err := h.text.Measure(row.Label, render.SpecFor(h.style, ui.TextAttrs{}), false); err == nil {
				w = mw
			}
		}
		if w > maxW {
			maxW = w
		}
	}
	return min(max(maxW+2*6+2*runningAppMenuPad, 140), 220)
}

func (h *runningAppMenuHost) configureLocking(width, height, scale120 int) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.configure(width, height, scale120)
}

func (h *runningAppMenuHost) renderLocking(pixels []byte, width, height, stride int) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.render(pixels, width, height, stride)
}

func (h *runningAppMenuHost) handleLocking(event wayland.Event) bool {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.handle(event)
}

func (h *runningAppMenuHost) configure(width, height, scale120 int) error {
	h.logicalW, h.logicalH, h.scale120 = width, height, scale120
	h.style.Scale120 = ui.Scale120(scale120)
	h.style.Body = ui.Rect{W: width, H: height}
	return h.relayout()
}

func (h *runningAppMenuHost) relayout() error {
	if h.root == nil {
		return nil
	}
	measure := func(text string, attrs ui.TextAttrs) (int, int) {
		if h.text != nil {
			if w, textHeight, err := h.text.Measure(text, render.SpecFor(h.style, attrs), attrs.Tabular); err == nil {
				return w, textHeight
			}
		}
		return len([]rune(text)) * 8, 16
	}
	return ui.LayoutColumn(h.root, ui.Rect{W: h.logicalW, H: h.logicalH}, measure)
}

func (h *runningAppMenuHost) shieldSpec() *wayland.AuxSpec {
	return &wayland.AuxSpec{
		ID:        runningAppMenuShieldID,
		Namespace: "sysc-shell-shield",
		Layer:     layerOverlay,
		Anchor: uint32(layershell.ZwlrLayerSurfaceV1AnchorTop |
			layershell.ZwlrLayerSurfaceV1AnchorBottom |
			layershell.ZwlrLayerSurfaceV1AnchorLeft |
			layershell.ZwlrLayerSurfaceV1AnchorRight),
		ExclusiveZone: -1,
		Keyboard:      keyboardNone,
		Callbacks: wayland.HostCallbacks{
			Configure: func(int, int, int) error { return nil },
			Render:    func([]byte, int, int, int) error { return nil },
			Handle:    h.shieldHandleLocking,
		},
	}
}

func (h *runningAppMenuHost) shieldHandleLocking(event wayland.Event) bool {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	if !h.open_ || event.Kind != wayland.EventPointerPress {
		return false
	}
	h.closeLocked()
	return true
}

func (h *runningAppMenuHost) render(pixels []byte, width, height, stride int) error {
	createdText := false
	if h.text == nil {
		fonts, err := render.NewSystemFontMap(h.r.cfg.Bar.FontFamily, render.DefaultFontCacheDir())
		if err != nil {
			return err
		}
		h.text = render.NewTextRendererWithFontMap(fonts)
		createdText = true
		theme := h.r.surfaceTheme()
		scale, body := h.style.Scale120, h.style.Body
		h.style = theme.OverlayStyle()
		h.style.Scale120, h.style.Body = scale, body
		h.style.Background = h.style.Capsule
	}
	if createdText && h.logicalW > 0 {
		if err := h.configure(h.logicalW, h.logicalH, h.scale120); err != nil {
			return err
		}
	}
	canvas, err := render.NewCanvas(pixels, width, height, stride)
	if err != nil {
		return err
	}
	return render.Paint(canvas, h.root, h.text, h.style)
}

func (h *runningAppMenuHost) handle(event wayland.Event) bool {
	if !h.open_ {
		return false
	}
	switch event.Kind {
	case wayland.EventKeyPress:
		if h.menu == nil {
			return false
		}
		if !h.menu.Handle(event.Key) {
			return false
		}
		h.keyed = true
		h.pointerRow = -1
		if !h.menu.Opened() {
			if event.Key == keyEsc {
				h.closeLocked()
				return true
			}
			h.chooseLocked(h.menu.Index())
			return true
		}
		h.rebuild()
		h.r.publishSurface(h.output, runningAppMenuSurfaceID)
		return true
	case wayland.EventPointerEnter, wayland.EventPointerMotion:
		i, ok := h.hitRow(int(math.Floor(event.X)), int(math.Floor(event.Y)))
		next := -1
		if ok {
			next = i
		}
		if next == h.pointerRow && !h.keyed {
			return false
		}
		h.pointerRow = next
		h.keyed = false
		h.rebuild()
		h.r.publishSurface(h.output, runningAppMenuSurfaceID)
		return true
	case wayland.EventPointerLeave:
		if h.pointerRow < 0 {
			return false
		}
		h.pointerRow = -1
		h.rebuild()
		h.r.publishSurface(h.output, runningAppMenuSurfaceID)
		return true
	case wayland.EventPointerPress:
		i, ok := h.hitRow(int(math.Floor(event.X)), int(math.Floor(event.Y)))
		if !ok {
			h.pressed = -1
			return false
		}
		h.pressed = i
		return true
	case wayland.EventPointerRelease:
		i, ok := h.hitRow(int(math.Floor(event.X)), int(math.Floor(event.Y)))
		pressed := h.pressed
		h.pressed = -1
		if ok && i == pressed {
			h.chooseLocked(i)
			return true
		}
		return false
	}
	return false
}

func (h *runningAppMenuHost) hitRow(x, y int) (int, bool) {
	if h.root == nil {
		return 0, false
	}
	n := 0
	for _, c := range h.root.Children {
		if c == nil || c.Kind != ui.KindCapsule {
			continue
		}
		if c.Bounds.Contains(x, y) {
			return n, true
		}
		n++
	}
	return 0, false
}

func (h *runningAppMenuHost) highlight() int {
	if h.pointerRow >= 0 {
		return h.pointerRow
	}
	if h.keyed && h.menu != nil {
		return h.menu.cursor
	}
	return -1
}

// Hallmark · component: menu · genre: modern-minimal · theme: shell tokens
// states: default · hover · focus (keyboard) · active · (no loading/error/success)
// Rows are KindCapsule + KindText, the launcher list language. KindButton is a
// CTA stadium; KindMenu is a panel combobox. Neither is a popup list.
func (h *runningAppMenuHost) rebuild() {
	col := &ui.Node{Kind: ui.KindColumn, Padding: runningAppMenuPad}
	on := h.highlight()
	if h.menu != nil {
		for i, opt := range h.menu.options {
			if i > 0 && i == len(h.rows)-1 && h.rows[i].CloseAll {
				col.Children = append(col.Children, &ui.Node{
					Kind: ui.KindRow, Padding: 4,
					Children: []*ui.Node{{Kind: ui.KindSeparator}},
				})
			}
			fill := ui.FillNone
			if i == on {
				fill = ui.FillSoft
			}
			label := &ui.Node{Kind: ui.KindText, Text: opt}
			if i < len(h.rows) && h.rows[i].CloseAll {
				label.Tone = ui.ToneError
			}
			col.Children = append(col.Children, &ui.Node{
				Kind: ui.KindCapsule, Radius: runningAppMenuRadius, Padding: 6,
				Fill: fill, Focusable: true, Role: "menuitem",
				Children: []*ui.Node{label},
			})
		}
	}
	h.root = col
	if h.logicalW > 0 {
		_ = h.relayout()
	}
}

func (h *runningAppMenuHost) choose(i int) {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	h.chooseLocked(i)
}

func (h *runningAppMenuHost) chooseLocked(i int) {
	if i < 0 || i >= len(h.rows) {
		return
	}
	row := h.rows[i]
	slot := h.slot
	h.closeLocked()
	if row.CloseAll {
		for _, w := range slot.Members {
			h.r.sendNiriLocked(niri.CloseWindow{ID: w.ID})
		}
		return
	}
	execLine := ""
	for _, a := range slot.Actions {
		if a.ID == row.ActionID {
			execLine = a.Exec
			break
		}
	}
	h.r.spawnDesktopExecLocked(execLine)
}

func (h *runningAppMenuHost) closeLocked() {
	if !h.open_ {
		return
	}
	h.open_ = false
	h.closeSurface()
	gen := h.rootGen
	h.rootGen = 0
	if gen != 0 {
		h.r.roots.closeRoot(gen)
	}
}

func (h *runningAppMenuHost) closeSurface() {
	if h.closed || h.output == 0 {
		return
	}
	h.closed = true
	h.request(wayland.AuxRequest{Output: h.output, ID: runningAppMenuSurfaceID})
	h.request(wayland.AuxRequest{Output: h.output, ID: runningAppMenuShieldID})
}
