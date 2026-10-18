package shell

import (
	"errors"
	"log"
	"math"
	"strconv"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	runningAppMenuSurfaceID = "running-app-menu"
	runningAppMenuNamespace = "sysc-shell-running-app-menu"
	runningAppMenuShieldID  = "shield:running-app-menu"
)

type runningAppMenuHost struct {
	r                   *Registry
	request             func(wayland.AuxRequest)
	open_               bool
	closed              bool
	output              uint32
	fontFamily          string
	rootGen             uint64
	slot                runningAppSlot
	rows                []runningAppMenuRow
	optionRows          []int
	menu                *Menu
	root                *ui.Node
	logicalW            int
	logicalH            int
	scale120            int
	place               trayMenuPlacement
	anchor              ui.Rect
	pressed             string
	pointer             string
	keyed               bool
	hitRows             map[string]int
	hitCloses           map[string]uint64
	forceQuitState      runningAppForceQuitState
	forceQuitMessage    string
	forceQuitGeneration uint64
	floatPending        map[uint64]runningAppFloatingRequest
	floatUnavailable    map[uint64]bool
	floatGeneration     uint64
	text                *render.TextRenderer
	style               render.Style
}

type runningAppFloatingRequest struct {
	target     bool
	generation uint64
}

func newRunningAppMenuHost(r *Registry) *runningAppMenuHost {
	return &runningAppMenuHost{
		r:       r,
		request: func(req wayland.AuxRequest) { r.sendAux(req) },
	}
}

func (h *runningAppMenuHost) openLocked(output uint32, slot runningAppSlot, anchor ui.Rect) error {
	if h.open_ {
		h.closeLocked()
	}
	if h.text == nil || h.fontFamily != h.r.cfg.Bar.FontFamily {
		text, err := h.r.textRenderer(h.r.cfg.Bar.FontFamily)
		if err != nil {
			return err
		}
		h.text = text
		h.fontFamily = h.r.cfg.Bar.FontFamily
	}
	h.style = h.r.surfaceTheme().OverlayStyle()
	h.style.Background = h.style.Capsule
	h.scale120 = 120
	if bar := h.r.bars[output]; bar != nil {
		if scale := bar.scale120(); scale > 0 {
			h.scale120 = scale
		}
	}
	h.style.Scale120 = ui.Scale120(h.scale120)
	h.open_ = true
	h.closed = false
	h.output = output
	h.anchor = anchor
	h.pressed = ""
	h.pointer = ""
	h.keyed = true
	h.slot = slot
	h.forceQuitGeneration++
	h.forceQuitState = runningAppForceQuitIdle
	h.forceQuitMessage = ""
	h.rows = h.menuRowsLocked(slot, h.r.niriSnapshot)
	h.buildMenu(0)
	w, height, err := h.remeasureLocked()
	if err != nil {
		h.open_ = false
		h.closed = true
		return err
	}
	h.place = h.r.barMenuPlacementLocked(output, anchor, w, height)
	h.rootGen = h.r.roots.openRoot(runningAppsMenuRoot(output))
	h.r.roots.onClose(h.rootGen, h.releaseForChainClose)
	h.r.dwell.leave()
	h.request(wayland.AuxRequest{Output: output, Open: h.shieldSpec()})
	h.request(wayland.AuxRequest{Output: output, Open: h.spec()})
	return nil
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
	if place.width > 0 {
		width = place.width
	}
	if place.height > 0 {
		height = place.height
	}
	// blur-exempt: a menu, not a panel. Design D13 scopes the backdrop to panels
	// and does not name this surface either way, so it keeps today's paint until
	// the owner rules on the menu and drawer surfaces; see
	// docs/plans/2026-09-13-parity-tranche-continuation-handover.md.
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
	width := h.logicalW
	if width <= 0 {
		width = h.menuWidth()
	}
	if h.root == nil {
		return width, 0
	}
	height, err := ui.ContentHeight(h.root, width, h.measureText)
	if err != nil {
		log.Printf("running-app menu measure: %v", err)
		height = h.style.Metrics.BaseWidget + 2*theme.MarginS
	}
	height = min(height, h.maxHeight())
	return width, height
}

func (h *runningAppMenuHost) maxHeight() int {
	height := trayMenuMaxHeight
	if _, _, _, output := h.r.barGeometryOnOutputLocked(h.output, h.anchor); output.H > 0 {
		height = min(height, output.H)
	}
	return height
}

func (h *runningAppMenuHost) remeasureLocked() (int, int, error) {
	h.logicalW = h.menuWidth()
	h.logicalH = 0
	width := h.logicalW
	if h.menu != nil {
		h.menu.setVisibleRows(menuVisibleRows)
	}
	var height int
	for {
		h.rebuild()
		var err error
		height, err = ui.ContentHeight(h.root, width, h.measureText)
		if err != nil {
			return 0, 0, err
		}
		if h.menu == nil || h.menu.visibleRows <= 1 || height <= h.maxHeight() {
			break
		}
		h.menu.setVisibleRows(h.menu.visibleRows - 1)
	}
	height = min(height, h.maxHeight())
	h.logicalW, h.logicalH = width, height
	h.style.Body = ui.Rect{W: width, H: height}
	if err := h.relayout(); err != nil {
		return 0, 0, err
	}
	return width, height, nil
}

func (h *runningAppMenuHost) resizeVisibleContentLocked() bool {
	oldWidth, oldHeight := h.logicalW, h.logicalH
	width, height, err := h.remeasureLocked()
	if err != nil {
		log.Printf("running-app menu scroll layout: %v", err)
		h.closeLocked()
		return false
	}
	h.place = h.r.barMenuPlacementLocked(h.output, h.anchor, width, height)
	if width == oldWidth && height == oldHeight {
		return true
	}
	spec := h.spec()
	h.updateSurfaceGeometryLocked(spec)
	return true
}

func (h *runningAppMenuHost) updateSurfaceGeometryLocked(spec *wayland.AuxSpec) {
	width, height := uint32(spec.Width), uint32(spec.Height)
	top, bottom := spec.MarginTop, spec.MarginBottom
	left, right := spec.MarginLeft, spec.MarginRight
	h.request(wayland.AuxRequest{
		Output: h.output,
		ID:     runningAppMenuSurfaceID,
		Update: &wayland.AuxUpdate{
			Width: &width, Height: &height,
			MarginTop: &top, MarginBottom: &bottom,
			MarginLeft: &left, MarginRight: &right,
		},
	})
}

func (h *runningAppMenuHost) menuWidth() int {
	maxW := 0
	for _, row := range h.rows {
		w, _ := h.measureText(h.displayLabel(row), ui.TextAttrs{})
		if row.Kind == runningAppMenuHeader || row.Kind == runningAppMenuSection {
			w, _ = h.measureText(row.Label, ui.TextAttrs{Role: theme.RoleTitle})
		}
		if row.Kind == runningAppMenuWindow {
			w += h.closeButtonWidth() + theme.MarginXS
		}
		if w > maxW {
			maxW = w
		}
	}
	width := maxW + 2*theme.MarginS + 2*theme.MarginS
	_, _, _, output := h.r.barGeometryOnOutputLocked(h.output, h.anchor)
	maxWidth := 420
	if output.W > 0 {
		maxWidth = min(maxWidth, max(200, output.W*2/5))
	}
	width = min(max(width, 200), maxWidth)
	if output.W > 0 {
		width = min(width, output.W)
	}
	return max(width, 1)
}

func (h *runningAppMenuHost) closeButtonWidth() int {
	return max(h.style.Metrics.IconButton, 24)
}

func (h *runningAppMenuHost) displayLabel(row runningAppMenuRow) string {
	if row.Kind == runningAppMenuWindow && row.Focused {
		return "● " + row.Label
	}
	return row.Label
}

func (h *runningAppMenuHost) buildMenu(selectedWindow uint64) {
	labels := make([]string, 0, len(h.rows))
	h.optionRows = make([]int, 0, len(h.rows))
	initial := 0
	windowOption := -1
	for i, row := range h.rows {
		if row.Kind == runningAppMenuHeader || row.Kind == runningAppMenuSection {
			continue
		}
		if row.Kind == runningAppMenuWindow {
			if windowOption < 0 {
				windowOption = len(labels)
			}
			if selectedWindow != 0 && row.WindowID == selectedWindow {
				initial = len(labels)
			}
		}
		labels = append(labels, row.Label)
		h.optionRows = append(h.optionRows, i)
	}
	if selectedWindow != 0 {
		for _, rowIndex := range h.optionRows {
			if h.rows[rowIndex].Kind == runningAppMenuWindow && h.rows[rowIndex].WindowID == selectedWindow {
				windowOption = -1
				break
			}
		}
		if windowOption >= 0 {
			initial = windowOption
		}
	} else if windowOption >= 0 {
		initial = windowOption
	}
	h.menu = NewMenu(labels, initial)
	h.menu.Open()
}

func (h *runningAppMenuHost) refreshLocked(slot runningAppSlot, snapshot niri.Snapshot) bool {
	if !h.open_ {
		return false
	}
	var selectedWindow uint64
	if row, ok := h.optionRow(h.menu.cursor); ok &&
		(h.rows[row].Kind == runningAppMenuWindow || h.rows[row].Kind == runningAppMenuFloating) {
		selectedWindow = h.rows[row].WindowID
	}
	h.slot = slot
	h.rows = h.menuRowsLocked(slot, snapshot)
	h.buildMenu(selectedWindow)
	h.pressed = ""
	h.pointer = ""
	h.keyed = true
	if !h.resizeVisibleContentLocked() {
		return false
	}
	return true
}

func (h *runningAppMenuHost) measureText(text string, attrs ui.TextAttrs) (int, int) {
	if h.text != nil {
		if width, height, err := h.text.Measure(text, render.SpecFor(h.style, attrs), attrs.Tabular); err == nil {
			return width, height
		}
	}
	return len([]rune(text)) * 8, 16
}

func (h *runningAppMenuHost) truncate(text string, width int, attrs ui.TextAttrs) string {
	if h.text == nil || width <= 0 {
		return text
	}
	fitted, _, err := h.text.Truncate(text, render.SpecFor(h.style, attrs), width, attrs.Tabular)
	if err != nil {
		return text
	}
	return fitted
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
	h.scale120 = scale120
	h.style.Scale120 = ui.Scale120(scale120)
	measuredW, measuredH, err := h.remeasureLocked()
	if err != nil {
		return err
	}
	if measuredW != width || measuredH != height {
		h.place = h.r.barMenuPlacementLocked(h.output, h.anchor, measuredW, measuredH)
		h.updateSurfaceGeometryLocked(h.spec())
	}
	return nil
}

func (h *runningAppMenuHost) rethemeLocked(next Theme, fontFamily string) bool {
	if !h.open_ {
		return false
	}
	if fontFamily != h.fontFamily {
		text, err := h.r.textRenderer(fontFamily)
		if err != nil {
			log.Printf("running-app menu font: %v", err)
			h.closeLocked()
			return false
		}
		h.text = text
		h.fontFamily = fontFamily
	}
	scale := h.style.Scale120
	h.style = next.OverlayStyle()
	h.style.Scale120 = scale
	h.style.Background = h.style.Capsule
	oldWidth, oldHeight := h.logicalW, h.logicalH
	width, height, err := h.remeasureLocked()
	if err != nil {
		log.Printf("running-app menu retheme layout: %v", err)
		h.closeLocked()
		return false
	}
	h.place = h.r.barMenuPlacementLocked(h.output, h.anchor, width, height)
	if width != oldWidth || height != oldHeight {
		h.updateSurfaceGeometryLocked(h.spec())
	}
	return true
}

func (h *runningAppMenuHost) relayout() error {
	if h.root == nil {
		return nil
	}
	return ui.LayoutColumn(h.root, ui.Rect{W: h.logicalW, H: h.logicalH}, h.measureText)
}

// blur-exempt: the shield paints nothing, exactly as the panel shield does.
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
	if h.text == nil {
		return errors.New("running-app menu has no text renderer")
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
		if runningAppMenuCloseShortcut(event) {
			option := h.menu.cursor
			if row, ok := h.optionRow(option); ok && h.rows[row].Kind == runningAppMenuWindow {
				h.closeWindowLocked(h.rows[row].WindowID)
				return true
			}
			return false
		}
		if !h.menu.Handle(event.Key) {
			return false
		}
		h.keyed = true
		h.pointer = ""
		if !h.menu.Opened() {
			if event.Key == keyEsc {
				h.closeLocked()
				return true
			}
			if row, ok := h.optionRow(h.menu.Index()); ok {
				h.chooseLocked(row)
			} else {
				h.closeLocked()
			}
			return true
		}
		if !h.resizeVisibleContentLocked() {
			return true
		}
		h.r.publishSurface(h.output, runningAppMenuSurfaceID)
		return true
	case wayland.EventPointerEnter, wayland.EventPointerMotion:
		action := h.hitAction(int(math.Floor(event.X)), int(math.Floor(event.Y)))
		if action == h.pointer && !h.keyed {
			return false
		}
		previous := h.pointer
		h.pointer = action
		h.keyed = false
		h.rebuild()
		h.r.publishSurface(h.output, runningAppMenuSurfaceID)
		return action != "" || previous != ""
	case wayland.EventPointerLeave:
		if h.pointer == "" {
			return false
		}
		h.pointer = ""
		h.rebuild()
		h.r.publishSurface(h.output, runningAppMenuSurfaceID)
		return true
	case wayland.EventPointerPress:
		if event.Button != 0 && event.Button != buttonLeft {
			h.pressed = ""
			return false
		}
		action := h.hitAction(int(math.Floor(event.X)), int(math.Floor(event.Y)))
		h.pressed = action
		return action != ""
	case wayland.EventPointerRelease:
		if event.Button != 0 && event.Button != buttonLeft {
			h.pressed = ""
			return false
		}
		action := h.hitAction(int(math.Floor(event.X)), int(math.Floor(event.Y)))
		pressed := h.pressed
		h.pressed = ""
		if action != "" && action == pressed {
			h.activateAction(action)
			return true
		}
		return false
	case wayland.EventPointerAxis:
		if event.Axis != 0 || h.menu == nil {
			return false
		}
		steps := int(event.AxisDiscrete)
		if steps == 0 && event.AxisValue120 != 0 {
			steps = int(event.AxisValue120 / 120)
			if steps == 0 {
				steps = signInt(int(event.AxisValue120))
			}
		}
		if steps == 0 && event.AxisValue != 0 {
			steps = int(math.Round(event.AxisValue))
			if steps == 0 {
				steps = signInt(int(math.Copysign(1, event.AxisValue)))
			}
		}
		if steps == 0 {
			return false
		}
		if h.menu.wheel(steps) {
			h.keyed = true
			h.pointer = ""
			if !h.resizeVisibleContentLocked() {
				return true
			}
			h.r.publishSurface(h.output, runningAppMenuSurfaceID)
		}
		return true
	}
	return false
}

func runningAppMenuCloseShortcut(event wayland.Event) bool {
	if event.Key == keyDelete && event.Mods == 0 {
		return true
	}
	return (event.Sym == uint32('x') && event.Mods == 0) ||
		(event.Sym == uint32('X') && event.Mods == ui.ModShift)
}

func signInt(value int) int {
	if value < 0 {
		return -1
	}
	if value > 0 {
		return 1
	}
	return 0
}

func (h *runningAppMenuHost) optionRow(option int) (int, bool) {
	if option < 0 || option >= len(h.optionRows) {
		return 0, false
	}
	return h.optionRows[option], true
}

func (h *runningAppMenuHost) hitAction(x, y int) string {
	if action, ok := ui.Hit(h.root, x, y); ok {
		return action
	}
	return ""
}

func (h *runningAppMenuHost) activateAction(action string) {
	if id, ok := h.hitCloses[action]; ok {
		h.closeWindowLocked(id)
		return
	}
	if row, ok := h.hitRows[action]; ok {
		h.chooseLocked(row)
	}
}

func (h *runningAppMenuHost) actionForRow(row runningAppMenuRow) string {
	switch row.Kind {
	case runningAppMenuWindow:
		return "running-app-window:" + strconv.FormatUint(row.WindowID, 10)
	case runningAppMenuFloating:
		return "running-app-floating:" + strconv.FormatUint(row.WindowID, 10)
	case runningAppMenuAction:
		return "running-app-action:" + row.ActionID
	case runningAppMenuWorkspace:
		return "running-app-workspace:" + strconv.FormatUint(row.MoveWindowID, 10) + ":" + strconv.FormatUint(row.MoveWorkspaceID, 10)
	case runningAppMenuForceQuit:
		return "running-app-force-quit"
	case runningAppMenuForceQuitConfirm:
		return "running-app-force-quit-confirm"
	case runningAppMenuForceQuitCancel:
		return "running-app-force-quit-cancel"
	case runningAppMenuForceQuitDismiss:
		return "running-app-force-quit-dismiss"
	default:
		return ""
	}
}

func (h *runningAppMenuHost) highlightAction() string {
	if !h.keyed {
		return h.pointer
	}
	if h.menu == nil {
		return ""
	}
	if row, ok := h.optionRow(h.menu.cursor); ok {
		return h.actionForRow(h.rows[row])
	}
	return ""
}

func (h *runningAppMenuHost) rebuild() {
	col := &ui.Node{Kind: ui.KindColumn, Padding: theme.MarginS, Gap: theme.MarginXS}
	h.hitRows = make(map[string]int)
	h.hitCloses = make(map[string]uint64)
	visible := make(map[int]bool)
	start, end := 0, 0
	if h.menu != nil {
		start, end = h.menu.visibleRange()
		for option := start; option < end; option++ {
			if row, ok := h.optionRow(option); ok {
				visible[row] = true
			}
		}
	}
	on := h.highlightAction()
	for index, row := range h.rows {
		switch row.Kind {
		case runningAppMenuHeader:
			col.Children = append(col.Children, &ui.Node{
				Kind: ui.KindText, Text: h.truncate(row.Label, max(h.logicalW-2*theme.MarginS, 0), ui.TextAttrs{Role: theme.RoleTitle}),
				TextRole: theme.RoleTitle,
			})
		case runningAppMenuSection:
			if h.sectionVisible(index, visible) {
				col.Children = append(col.Children, &ui.Node{Kind: ui.KindText, Text: row.Label,
					TextRole: theme.RoleCaption})
			}
		default:
			if !visible[index] {
				continue
			}
			if row.Kind == runningAppMenuForceQuit && len(visible) > 1 {
				col.Children = append(col.Children, &ui.Node{Kind: ui.KindSeparator})
			}
			action := h.actionForRow(row)
			h.hitRows[action] = index
			fill := ui.FillNone
			if action == on {
				fill = ui.FillSoft
			}
			if row.Kind == runningAppMenuWindow {
				col.Children = append(col.Children, h.windowRowNode(row, action, fill))
				continue
			}
			attrs := ui.TextAttrs{}
			text := row.Label
			if row.Kind == runningAppMenuForceQuit || row.Kind == runningAppMenuForceQuitConfirm {
				attrs.Role = theme.RoleLabel
			}
			contentWidth := max(h.logicalW-2*theme.MarginS-2*theme.MarginS, 0)
			label := &ui.Node{Kind: ui.KindText, Text: h.truncate(text, contentWidth, attrs), Tone: ui.ToneNormal}
			if row.Kind == runningAppMenuForceQuit || row.Kind == runningAppMenuForceQuitConfirm {
				label.Tone = ui.ToneError
			}
			col.Children = append(col.Children, &ui.Node{
				Kind: ui.KindCapsule, Radius: h.style.Shapes.Small, Padding: theme.MarginS,
				Fill: fill, Action: action, Name: row.Label, Focusable: true, Role: "menuitem",
				Children: []*ui.Node{label},
			})
		}
	}
	h.root = col
	if h.logicalW > 0 {
		_ = h.relayout()
	}
}

func (h *runningAppMenuHost) sectionVisible(section int, visible map[int]bool) bool {
	for i := section + 1; i < len(h.rows); i++ {
		if h.rows[i].Kind == runningAppMenuSection || h.rows[i].Kind == runningAppMenuHeader {
			break
		}
		if visible[i] {
			return true
		}
	}
	return false
}

func (h *runningAppMenuHost) windowRowNode(row runningAppMenuRow, action string, fill ui.Fill) *ui.Node {
	contentWidth := max(h.logicalW-2*theme.MarginS, 0)
	closeWidth := h.closeButtonWidth()
	labelWidth := max(contentWidth-closeWidth-theme.MarginXS-2*theme.MarginS, 0)
	attrs := ui.TextAttrs{}
	label := &ui.Node{Kind: ui.KindText, Text: h.truncate(h.displayLabel(row), labelWidth, attrs)}
	window := &ui.Node{
		Kind: ui.KindCapsule, Radius: h.style.Shapes.Small, Padding: theme.MarginS,
		Width: max(contentWidth-closeWidth-theme.MarginXS, 0),
		Fill:  fill, Action: action, Name: row.Label, Focusable: true, Role: "menuitem",
		Children: []*ui.Node{label},
	}
	closeAction := "running-app-close:" + strconv.FormatUint(row.WindowID, 10)
	h.hitCloses[closeAction] = row.WindowID
	closeFill := ui.FillNone
	if h.highlightAction() == closeAction {
		closeFill = ui.FillSoft
	}
	close := &ui.Node{
		Kind: ui.KindButton, Width: closeWidth, Height: closeWidth, Radius: h.style.Shapes.Small,
		Text: "×", Fill: closeFill, Action: closeAction,
		Name: "Close " + row.Label, Role: "button", Focusable: true,
	}
	return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS, Children: []*ui.Node{window, close}}
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
	switch row.Kind {
	case runningAppMenuFloating:
		h.toggleFloatingLocked(row.WindowID)
		return
	case runningAppMenuForceQuit:
		h.showForceQuitStateLocked(runningAppForceQuitConfirming, "")
		return
	case runningAppMenuForceQuitConfirm:
		h.startForceQuitLocked()
		return
	case runningAppMenuForceQuitCancel, runningAppMenuForceQuitDismiss:
		h.closeLocked()
		return
	}
	slot := h.slot
	h.closeLocked()
	switch row.Kind {
	case runningAppMenuWindow:
		if currentRunningWindow(h.r.running, slot.Key, row.WindowID) {
			h.r.sendNiriLocked(niri.FocusWindow{ID: row.WindowID})
		}
	case runningAppMenuWorkspace:
		if currentRunningWindow(h.r.running, slot.Key, row.MoveWindowID) &&
			workspaceMoveTargetExists(h.r.niriSnapshot, row.MoveWindowID, row.MoveWorkspaceID) {
			h.r.sendNiriLocked(niri.MoveWindowToWorkspace{
				WindowID: row.MoveWindowID, WorkspaceID: row.MoveWorkspaceID, Focus: false,
			})
		}
	case runningAppMenuAction:
		execLine := ""
		for _, a := range slot.Actions {
			if a.ID == row.ActionID {
				execLine = a.Exec
				break
			}
		}
		h.r.spawnDesktopExecLocked(execLine)
	}
}

func (h *runningAppMenuHost) showForceQuitStateLocked(state runningAppForceQuitState, message string) {
	h.forceQuitState = state
	h.forceQuitMessage = message
	h.rows = h.menuRowsLocked(h.slot, h.r.niriSnapshot)
	h.buildMenu(0)
	h.pressed = ""
	h.pointer = ""
	h.keyed = true
	if !h.resizeVisibleContentLocked() {
		return
	}
	h.r.publishSurface(h.output, runningAppMenuSurfaceID)
}

func (h *runningAppMenuHost) menuRowsLocked(slot runningAppSlot, snapshot niri.Snapshot) []runningAppMenuRow {
	rows := runningAppForceQuitMenuRows(slot, h.forceQuitState, h.forceQuitMessage, snapshot.Workspaces)
	if h.forceQuitState != runningAppForceQuitIdle {
		return rows
	}
	filtered := rows[:0]
	for _, row := range rows {
		if row.Kind == runningAppMenuFloating {
			if _, pending := h.floatPending[row.WindowID]; pending {
				continue
			}
			if _, unavailable := h.floatUnavailable[row.WindowID]; unavailable {
				continue
			}
		}
		filtered = append(filtered, row)
	}
	return filtered
}

func (h *runningAppMenuHost) confirmFloatingTogglesLocked(snapshot niri.Snapshot) {
	if len(h.floatPending) == 0 && len(h.floatUnavailable) == 0 {
		return
	}
	windows := make(map[uint64]niri.Window, len(snapshot.Windows))
	for _, window := range snapshot.Windows {
		windows[window.ID] = window
	}
	for id, request := range h.floatPending {
		window, exists := windows[id]
		if !exists || window.Floating == request.target {
			delete(h.floatPending, id)
		}
	}
	for id, wasFloating := range h.floatUnavailable {
		window, exists := windows[id]
		if !exists || window.Floating != wasFloating {
			delete(h.floatUnavailable, id)
		}
	}
}

func (h *runningAppMenuHost) toggleFloatingLocked(windowID uint64) {
	if _, pending := h.floatPending[windowID]; pending {
		return
	}
	if _, unavailable := h.floatUnavailable[windowID]; unavailable {
		return
	}
	window, exists := currentRunningWindowValue(h.r.running, h.slot.Key, windowID)
	if !exists {
		return
	}
	h.floatGeneration++
	request := runningAppFloatingRequest{target: !window.Floating, generation: h.floatGeneration}
	if h.floatPending == nil {
		h.floatPending = make(map[uint64]runningAppFloatingRequest)
	}
	h.floatPending[windowID] = request
	if slot, ok := runningSlotByKey(h.r.running, h.slot.Key); ok {
		if h.refreshLocked(slot, h.r.niriSnapshot) {
			h.r.publishSurface(h.output, runningAppMenuSurfaceID)
		}
	}
	h.r.sendNiriResultLocked(niri.ToggleWindowFloating{ID: windowID}, func(err error) {
		h.finishFloatingToggle(windowID, request.generation, err)
	})
}

func (h *runningAppMenuHost) finishFloatingToggle(windowID, generation uint64, err error) {
	if err != nil {
		log.Printf("running-app floating toggle %d: %v", windowID, err)
	}
	if err == nil {
		return
	}
	h.r.mu.Lock()
	request, pending := h.floatPending[windowID]
	if !pending || request.generation != generation {
		h.r.mu.Unlock()
		return
	}
	recoverAmbiguous := false
	var refusal *niri.ActionRefusalError
	if errors.As(err, &refusal) {
		delete(h.floatPending, windowID)
		if h.floatUnavailable == nil {
			h.floatUnavailable = make(map[uint64]bool)
		}
		h.floatUnavailable[windowID] = !request.target
		h.confirmFloatingTogglesLocked(h.r.niriSnapshot)
	} else {
		var notSent *niri.ActionNotSentError
		if errors.As(err, &notSent) {
			delete(h.floatPending, windowID)
		} else {
			recoverAmbiguous = true
		}
	}
	updated := false
	output := h.output
	if h.open_ {
		if slot, ok := runningSlotByKey(h.r.running, h.slot.Key); ok {
			updated = h.refreshLocked(slot, h.r.niriSnapshot)
		}
	}
	h.r.mu.Unlock()
	if updated {
		h.r.publishSurface(output, runningAppMenuSurfaceID)
	}
	if !recoverAmbiguous {
		return
	}
	windows, queryErr := h.r.queryNiriWindows()
	if queryErr != nil {
		log.Printf("running-app floating toggle %d: refresh window state: %v", windowID, queryErr)
		return
	}
	for _, window := range windows {
		if window.ID == windowID {
			h.reconcileAmbiguousFloatingToggle(windowID, generation, window)
			return
		}
	}
}

func (h *runningAppMenuHost) reconcileAmbiguousFloatingToggle(windowID, generation uint64, window niri.Window) {
	r := h.r
	r.mu.Lock()
	request, pending := h.floatPending[windowID]
	if !pending || request.generation != generation {
		r.mu.Unlock()
		return
	}
	if window.Floating == request.target {
		r.mu.Unlock()
		return
	}
	delete(h.floatPending, windowID)
	updated := false
	output := h.output
	if h.open_ {
		if slot, ok := runningSlotByKey(r.running, h.slot.Key); ok {
			updated = h.refreshLocked(slot, r.niriSnapshot)
		}
	}
	r.mu.Unlock()
	if updated {
		r.publishSurface(output, runningAppMenuSurfaceID)
	}
}

func (h *runningAppMenuHost) startForceQuitLocked() {
	slot, ok := runningSlotByKey(h.r.running, h.slot.Key)
	if !ok {
		h.showForceQuitStateLocked(runningAppForceQuitFailed,
			runningAppForceQuitErrorMessage(errors.New("the app has no current Niri windows")))
		return
	}
	appIDs := runningAppScopeAppIDs(slot)
	windowPIDs := runningAppScopeWindowPIDs(slot)
	if len(appIDs) == 0 && len(windowPIDs) == 0 {
		h.showForceQuitStateLocked(runningAppForceQuitFailed,
			runningAppForceQuitErrorMessage(errors.New("the app has no current Niri window identity")))
		return
	}
	h.slot = slot
	h.showForceQuitStateLocked(runningAppForceQuitPending, "")
	generation := h.forceQuitGeneration
	go h.finishForceQuit(generation, slot.Key, appIDs, windowPIDs)
}

func (h *runningAppMenuHost) finishForceQuit(generation uint64, appKey string, appIDs []string, windowPIDs []int) {
	err := h.r.forceQuitRunningAppScopes(appIDs, windowPIDs)
	if err != nil {
		log.Printf("running-app force quit %q: %v", appKey, err)
	}

	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	if !h.open_ || h.forceQuitGeneration != generation || h.forceQuitState != runningAppForceQuitPending {
		return
	}
	if err == nil {
		h.closeLocked()
		return
	}
	h.showForceQuitStateLocked(runningAppForceQuitFailed, runningAppForceQuitErrorMessage(err))
}

func (h *runningAppMenuHost) closeWindowLocked(windowID uint64) {
	h.closeLocked()
	if currentRunningWindow(h.r.running, h.slot.Key, windowID) {
		h.r.sendNiriLocked(niri.CloseWindow{ID: windowID})
	}
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
