package shell

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	windowSwitcherSurfaceID  = "window-switcher"
	windowSwitcherNamespace  = "sysc-shell-switcher"
	windowSwitcherRowHeight  = 44
	windowSwitcherMaxRows    = 8
	windowSwitcherCardWidth  = 720
	windowSwitcherCardMargin = 24
)

type windowSwitcherHost struct {
	r         *Registry
	request   func(wayland.AuxRequest)
	open_     bool
	closed    bool
	output    uint32
	connector string
	rootGen   uint64

	model      windowSwitcherModel
	root       *ui.Node
	rows       []*ui.Node
	rowIndices []int
	pressed    int
	logicalW   int
	logicalH   int
	scale120   int
	text       *render.TextRenderer
	style      render.Style
}

func newWindowSwitcherHost(r *Registry) *windowSwitcherHost {
	return &windowSwitcherHost{
		r:       r,
		request: func(req wayland.AuxRequest) { r.sendAux(req) },
	}
}

func (h *windowSwitcherHost) openLocked(output uint32, connector string, model windowSwitcherModel) {
	h.open_ = true
	h.closed = false
	h.output = output
	h.connector = connector
	h.model = model
	h.pressed = -1
	h.rebuild()
	h.rootGen = h.r.roots.openRoot(windowSwitcherRoot(output))
	h.r.roots.onClose(h.rootGen, h.releaseForChainClose)
	h.r.dwell.leave()
	h.request(wayland.AuxRequest{Output: output, Open: h.spec()})
}

func (h *windowSwitcherHost) releaseForChainClose() {
	if !h.open_ {
		return
	}
	h.open_ = false
	h.rootGen = 0
	h.closeSurface()
}

func (h *windowSwitcherHost) spec() *wayland.AuxSpec {
	// blur-exempt: this transparent keyboard overlay keeps the desktop visible beneath the centered list.
	return &wayland.AuxSpec{
		ID: windowSwitcherSurfaceID, Namespace: windowSwitcherNamespace,
		Layer: layershell.ZwlrLayerShellV1LayerOverlay,
		Anchor: uint32(layershell.ZwlrLayerSurfaceV1AnchorTop |
			layershell.ZwlrLayerSurfaceV1AnchorBottom |
			layershell.ZwlrLayerSurfaceV1AnchorLeft |
			layershell.ZwlrLayerSurfaceV1AnchorRight),
		ExclusiveZone: -1,
		Keyboard:      keyboardExclusive,
		Callbacks: wayland.HostCallbacks{
			Configure: h.configureLocking,
			Render:    h.renderLocking,
			Handle:    h.handleLocking,
		},
	}
}

func (h *windowSwitcherHost) configureLocking(width, height, scale120 int) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.configure(width, height, scale120)
}

func (h *windowSwitcherHost) renderLocking(pixels []byte, width, height, stride int) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.render(pixels, width, height, stride)
}

func (h *windowSwitcherHost) handleLocking(event wayland.Event) bool {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.handle(event)
}

func (h *windowSwitcherHost) configure(width, height, scale120 int) error {
	h.logicalW, h.logicalH, h.scale120 = width, height, scale120
	h.style.Scale120 = ui.Scale120(scale120)
	h.style.Body = ui.Rect{W: width, H: height}
	if width <= 0 || height <= 0 {
		return nil
	}
	return h.relayout()
}

func (h *windowSwitcherHost) relayout() error {
	if h.root == nil || h.logicalW <= 0 || h.logicalH <= 0 {
		return nil
	}
	measure := func(text string, attrs ui.TextAttrs) (int, int) {
		if h.text != nil {
			if w, height, err := h.text.Measure(text, render.SpecFor(h.style, attrs), attrs.Tabular); err == nil {
				return w, height
			}
		}
		return len([]rune(text)) * 8, 16
	}
	return ui.LayoutColumn(h.root, ui.Rect{W: h.logicalW, H: h.logicalH}, measure)
}

func (h *windowSwitcherHost) render(pixels []byte, width, height, stride int) error {
	createdText := false
	if h.text == nil {
		fonts, err := render.NewSystemFontMap(h.r.cfg.Bar.FontFamily, render.DefaultFontCacheDir())
		if err != nil {
			return err
		}
		h.text = render.NewTextRendererWithFontMap(fonts)
		createdText = true
		style := h.r.surfaceTheme().OverlayStyle()
		scale, body := h.style.Scale120, h.style.Body
		h.style = style
		h.style.NoGround = true
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

func (h *windowSwitcherHost) rebuild() {
	count := len(h.model.windows)
	visible := min(count, windowSwitcherMaxRows)
	start := max(h.model.selected-visible/2, 0)
	start = min(start, count-visible)
	end := start + visible
	cardWidth := min(windowSwitcherCardWidth, max(h.logicalW-2*windowSwitcherCardMargin, 280))
	if h.logicalW <= 0 {
		cardWidth = windowSwitcherCardWidth
	}
	rows := make([]*ui.Node, 0, visible)
	indices := make([]int, 0, visible)
	contentWidth := max(cardWidth-2*theme.MarginL, 1)
	for index := start; index < end; index++ {
		window := h.model.windows[index]
		fill := ui.FillNone
		if index == h.model.selected {
			fill = ui.FillSoft
		}
		row := &ui.Node{
			Kind: ui.KindCapsule, Width: contentWidth, Height: windowSwitcherRowHeight,
			Padding: theme.MarginS, Shape: ui.ShapeMedium, Fill: fill,
			Focusable: true, Role: "option",
			Children: []*ui.Node{{Kind: ui.KindText, Text: switcherWindowLabel(window), TextRole: theme.RoleLabel, MaxWidth: contentWidth - 2*theme.MarginS}},
		}
		rows = append(rows, row)
		indices = append(indices, index)
	}
	h.rows, h.rowIndices = rows, indices
	children := []*ui.Node{{Kind: ui.KindText, Text: fmt.Sprintf("Windows on %s", h.connector), TextRole: theme.RoleTitle}}
	children = append(children, rows...)
	children = append(children, &ui.Node{
		Kind: ui.KindText, Text: "↑/↓ or Tab Select  ·  Enter Focus  ·  Esc Cancel",
		TextRole: theme.RoleCaption,
	})
	card := &ui.Node{
		Kind: ui.KindCapsule, Width: cardWidth, Padding: theme.MarginL,
		Shape: ui.ShapeLarge, Fill: ui.FillContainer, CenterX: true, CenterY: true,
		Role: "listbox", Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginS, Children: children}},
	}
	h.root = &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{card}}
	if h.logicalW > 0 && h.logicalH > 0 {
		_ = h.relayout()
	}
}

func switcherWindowLabel(window niri.Window) string {
	title := strings.TrimSpace(window.Title)
	if title == "" {
		title = strings.TrimSpace(window.AppID)
	}
	if title == "" {
		title = "Untitled window"
	}
	if window.AppID != "" && window.AppID != title {
		title += "  ·  " + window.AppID
	}
	return fmt.Sprintf("%s  ·  #%d", title, window.ID)
}

func (h *windowSwitcherHost) handle(event wayland.Event) bool {
	if !h.open_ {
		return false
	}
	switch event.Kind {
	case wayland.EventKeyPress:
		switch event.Key {
		case keyEsc:
			h.closeLocked()
			return true
		case keyEnter:
			h.activateLocked()
			return true
		case keyTab, keyDown, keyRight:
			direction := 1
			if event.Key == keyTab && event.Mods&ui.ModShift != 0 {
				direction = -1
			}
			return h.cycleLocked(direction)
		case keyUp, keyLeft:
			return h.cycleLocked(-1)
		}
	case wayland.EventPointerEnter, wayland.EventPointerMotion:
		index, ok := h.hitRow(int(math.Floor(event.X)), int(math.Floor(event.Y)))
		if ok && h.model.selectIndex(index) {
			h.rebuild()
			h.r.publishSurface(h.output, windowSwitcherSurfaceID)
			return true
		}
	case wayland.EventPointerLeave:
		h.pressed = -1
	case wayland.EventPointerPress:
		index, ok := h.hitRow(int(math.Floor(event.X)), int(math.Floor(event.Y)))
		if ok && (event.Button == 0 || event.Button == buttonLeft) {
			h.pressed = index
			return true
		}
		h.pressed = -1
	case wayland.EventPointerRelease:
		index, ok := h.hitRow(int(math.Floor(event.X)), int(math.Floor(event.Y)))
		pressed := h.pressed
		h.pressed = -1
		if ok && index == pressed && (event.Button == 0 || event.Button == buttonLeft) {
			h.model.selectIndex(index)
			h.activateLocked()
			return true
		}
	}
	return false
}

func (h *windowSwitcherHost) cycleLocked(direction int) bool {
	if !h.model.cycle(direction) {
		return false
	}
	h.rebuild()
	h.r.publishSurface(h.output, windowSwitcherSurfaceID)
	return true
}

func (h *windowSwitcherHost) hitRow(x, y int) (int, bool) {
	for row, node := range h.rows {
		if node.Bounds.Contains(x, y) {
			return h.rowIndices[row], true
		}
	}
	return 0, false
}

func (h *windowSwitcherHost) activateLocked() {
	selected, ok := h.model.selectedWindow()
	valid := ok && !h.r.niriSnapshot.OverviewOpen && h.r.niriSnapshot.FocusedOutput == h.connector
	if valid {
		valid = false
		for _, window := range focusedOutputWindows(h.r.niriSnapshot) {
			if window.ID == selected.ID {
				valid = true
				break
			}
		}
	}
	h.closeLocked()
	if valid {
		h.r.sendNiriLocked(niri.FocusWindow{ID: selected.ID})
	}
}

func (h *windowSwitcherHost) refreshLocked(snapshot niri.Snapshot) bool {
	if !h.open_ {
		return false
	}
	if snapshot.OverviewOpen || snapshot.FocusedOutput != h.connector {
		h.closeLocked()
		return false
	}
	previous := slices.Clone(h.model.windows)
	selected, hadSelected := h.model.selectedWindow()
	h.model.refresh(snapshot)
	if len(h.model.windows) == 0 {
		h.closeLocked()
		return false
	}
	current, hasCurrent := h.model.selectedWindow()
	changed := !slices.Equal(previous, h.model.windows) ||
		hadSelected != hasCurrent || hadSelected && hasCurrent && selected.ID != current.ID
	if changed {
		h.rebuild()
	}
	return changed
}

func (h *windowSwitcherHost) closeLocked() {
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

func (h *windowSwitcherHost) closeSurface() {
	if h.closed || h.output == 0 {
		return
	}
	h.closed = true
	h.request(wayland.AuxRequest{Output: h.output, ID: windowSwitcherSurfaceID})
}

// ShowWindowSwitcher opens the per-output switcher only when no other modal
// root owns keyboard focus. Caller is the same-user IPC handler.
func (r *Registry) ShowWindowSwitcher() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.niriSnapshot.OverviewOpen {
		return fmt.Errorf("cannot open switcher while Niri overview is open")
	}
	if _, _, open := r.roots.current(); open {
		return fmt.Errorf("cannot open switcher while another interactive root is open")
	}
	connector := r.niriSnapshot.FocusedOutput
	if connector == "" {
		return fmt.Errorf("cannot open switcher without a focused output")
	}
	model := newWindowSwitcherModel(r.niriSnapshot)
	if len(model.windows) == 0 {
		return fmt.Errorf("cannot open switcher without focused-output windows")
	}
	var output uint32
	for global, bar := range r.bars {
		if bar != nil && bar.connector() == connector {
			output = global
			break
		}
	}
	if output == 0 {
		return fmt.Errorf("focused output %q has no Wayland surface", connector)
	}
	if r.windowSwitcher == nil {
		r.windowSwitcher = newWindowSwitcherHost(r)
	}
	r.windowSwitcher.openLocked(output, connector, model)
	return nil
}
