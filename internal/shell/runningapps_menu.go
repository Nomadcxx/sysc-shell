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
)

type runningAppMenuHost struct {
	r        *Registry
	request  func(wayland.AuxRequest)
	open_    bool
	closed   bool
	output   uint32
	rootGen  uint64
	slot     runningAppSlot
	rows     []runningAppMenuRow
	menu     *Menu
	root     *ui.Node
	logicalW int
	logicalH int
	scale120 int
	text     *render.TextRenderer
	style    render.ProofStyle
}

func newRunningAppMenuHost(r *Registry) *runningAppMenuHost {
	return &runningAppMenuHost{
		r:       r,
		request: func(req wayland.AuxRequest) { r.sendAux(req) },
	}
}

func (h *runningAppMenuHost) openLocked(output uint32, slot runningAppSlot) {
	if h.open_ {
		h.closeLocked()
	}
	h.open_ = true
	h.closed = false
	h.output = output
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
	marginTop := int32(0)
	if bar, ok := h.r.bars[h.output]; ok {
		_, bh := bar.configuredSize()
		marginTop = int32(bh)
	}
	return &wayland.AuxSpec{
		ID: runningAppMenuSurfaceID, Namespace: runningAppMenuNamespace,
		Layer: layershell.ZwlrLayerShellV1LayerOverlay,
		Anchor: uint32(layershell.ZwlrLayerSurfaceV1AnchorTop |
			layershell.ZwlrLayerSurfaceV1AnchorRight),
		MarginTop: marginTop, Width: int32(width), Height: int32(height),
		ExclusiveZone: -1, Keyboard: keyboardOnDemand,
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
	return trayMenuWidth, n*trayMenuRowHeight + 2*trayMenuPadding
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
	measure := func(text string, tabular bool) (int, int) {
		if h.text != nil {
			if w, textHeight, err := h.text.Measure(text, h.style.Size, tabular); err == nil {
				return w, textHeight
			}
		}
		return len([]rune(text)) * 8, 16
	}
	return ui.LayoutColumn(h.root, ui.Rect{W: h.logicalW, H: h.logicalH}, measure)
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
		h.style = theme.ProofStyle()
		h.style.Scale120, h.style.Body = scale, body
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
	case wayland.EventPointerRelease:
		x, y := int(math.Floor(event.X)), int(math.Floor(event.Y))
		if i, ok := h.hitRow(x, y); ok {
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
	for i, c := range h.root.Children {
		if c != nil && c.Bounds.Contains(x, y) {
			return i, true
		}
	}
	return 0, false
}

func (h *runningAppMenuHost) rebuild() {
	col := &ui.Node{Kind: ui.KindColumn, Padding: trayMenuPadding}
	if h.menu != nil {
		for i, opt := range h.menu.options {
			row := &ui.Node{
				Kind: ui.KindButton, Text: opt, Padding: 4, Height: trayMenuRowHeight,
				Focusable: true, Role: "menuitem",
			}
			if i == h.menu.cursor {
				row.Fill = ui.FillAccent
			}
			col.Children = append(col.Children, row)
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
}
