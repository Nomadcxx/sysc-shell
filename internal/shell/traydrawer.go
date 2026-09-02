package shell

import (
	"log"
	"math"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

const (
	trayDrawerSurfaceID = "tray-drawer"
	trayDrawerWidth     = 420
	trayDrawerHeight    = 480
	trayDrawerRowHeight = 40
	trayItemSize        = 24
	// trayDrawerAction is the bar's overflow control. It opens this drawer.
	trayDrawerAction = "tray-drawer"
)

func trayDrawerTree(arranged trayArrangement, images map[tray.ItemKey]*ui.Image) (*ui.Node, map[*ui.Node]tray.Item) {
	rows := make([]*ui.Node, 0, len(arranged.Overflow)+len(arranged.Hidden)+2)
	items := make(map[*ui.Node]tray.Item, len(arranged.Overflow)+len(arranged.Hidden))
	if len(arranged.Overflow) > 0 {
		rows = append(rows, &ui.Node{Kind: ui.KindText, Text: "Overflow", Bold: true})
		for i := range arranged.Overflow {
			row, itemNode := trayDrawerItemRow(arranged.Overflow[i], false, arranged.Pinned[arranged.Overflow[i].Key])
			itemNode.Image = images[arranged.Overflow[i].Key]
			rows = append(rows, row)
			items[itemNode] = arranged.Overflow[i]
		}
	}
	if len(arranged.Hidden) > 0 {
		rows = append(rows, &ui.Node{Kind: ui.KindText, Text: "Hidden", Bold: true})
		for i := range arranged.Hidden {
			row, itemNode := trayDrawerItemRow(arranged.Hidden[i], true, false)
			itemNode.Image = images[arranged.Hidden[i].Key]
			rows = append(rows, row)
			items[itemNode] = arranged.Hidden[i]
		}
	}
	root := &ui.Node{
		Kind: ui.KindVirtualList, ItemCount: len(rows), ItemHeight: trayDrawerRowHeight, Padding: 8,
		Item: func(i int) *ui.Node {
			if i < 0 || i >= len(rows) {
				return nil
			}
			return rows[i]
		},
	}
	return root, items
}

func trayDrawerItemRow(item tray.Item, hidden, pinned bool) (*ui.Node, *ui.Node) {
	name := strings.TrimSpace(item.Title)
	if name == "" {
		name = strings.TrimSpace(item.ID)
	}
	if name == "" {
		name = "Tray item"
	}
	itemNode := &ui.Node{
		Kind: ui.KindImage, ImageSize: trayItemSize, Action: "tray-item",
		Focusable: true, Name: name, Role: "button",
	}
	row := &ui.Node{Kind: ui.KindRow, Gap: 6, Children: []*ui.Node{
		itemNode,
		{Kind: ui.KindText, Text: name, MaxWidth: 140},
	}}
	token, ok := stableTrayToken(item)
	if !ok {
		return row, itemNode
	}
	button := func(label, action string) *ui.Node {
		return &ui.Node{Kind: ui.KindButton, Text: label, Padding: 4,
			Action:    "tray-pref:" + action + ":" + token,
			Focusable: true, Name: label + " " + name, Role: "button"}
	}
	if hidden {
		row.Children = append(row.Children, button("Show", "show"))
		return row, itemNode
	}
	pinLabel, pinAction := "Pin", "pin"
	if pinned {
		pinLabel, pinAction = "Unpin", "unpin"
	}
	row.Children = append(row.Children,
		button("Hide", "hide"), button(pinLabel, pinAction),
		button("Earlier", "earlier"), button("Later", "later"))
	return row, itemNode
}

type trayDrawerHost struct {
	r          *Registry
	request    func(wayland.AuxRequest)
	open_      bool
	closed     bool
	output     uint32
	connector  string
	rootGen    uint64
	arranged   trayArrangement
	images     map[tray.ItemKey]*ui.Image
	root       *ui.Node
	focus      []*ui.Node
	roving     ui.Roving
	logicalW   int
	logicalH   int
	scale120   int
	hoverX     int
	hoverY     int
	pressed    *ui.Node
	itemNodes  map[*ui.Node]tray.Item
	focusRows  map[*ui.Node]int
	itemAction func(tray.Item, string, uint32, wayland.Event) bool
	diagnostic func(string)
	text       *render.TextRenderer
	style      render.ProofStyle
	// pointer is the resolved hover/press state, kept as stable keys.
	pointer    interaction
	harnessRef *hostHarness
}

func newTrayDrawerHost(r *Registry, harness *hostHarness) *trayDrawerHost {
	h := &trayDrawerHost{r: r, diagnostic: func(message string) { log.Print(message) }}
	if harness != nil {
		h.request = harness.request
		h.harnessRef = harness
	} else {
		h.request = func(req wayland.AuxRequest) { r.sendAux(req) }
	}
	return h
}

func (h *trayDrawerHost) open(output uint32, connector string, arranged trayArrangement, images map[tray.ItemKey]*ui.Image) bool {
	if h.open_ || output == 0 {
		return false
	}
	h.open_, h.closed = true, false
	h.output, h.connector, h.arranged, h.images = output, connector, arranged, images
	for _, token := range arranged.Collisions {
		h.diagnostic("sysc-shell: tray preference collision for " + token + "; ignoring its preferences")
	}
	h.rebuild()
	h.rootGen = h.r.roots.openRoot(trayDrawerRoot(output))
	h.r.roots.onClose(h.rootGen, h.releaseForChainClose)
	h.r.dwell.leave()
	h.request(wayland.AuxRequest{Output: output, Open: h.spec()})
	return true
}

// refresh reprojects an open drawer against new tray state. A drawer whose
// item has gone keeps running with the rest: the row disappears, the surface
// does not.
func (h *trayDrawerHost) refresh(arranged trayArrangement, images map[tray.ItemKey]*ui.Image) {
	if !h.open_ {
		return
	}
	h.arranged, h.images = arranged, images
	h.rebuild()
	h.r.publishSurface(h.output, trayDrawerSurfaceID)
}

func (h *trayDrawerHost) rebuild() {
	h.root, h.itemNodes = trayDrawerTree(h.arranged, h.images)
	h.focus = ui.Focusables(h.root)
	h.roving.Count = len(h.focus)
	h.focusRows = make(map[*ui.Node]int, len(h.focus))
	for i := 0; i < h.root.ItemCount; i++ {
		for _, node := range ui.Focusables(h.root.Item(i)) {
			h.focusRows[node] = i
		}
	}
	if h.logicalW > 0 {
		h.relayout()
	}
}

func (h *trayDrawerHost) spec() *wayland.AuxSpec {
	return &wayland.AuxSpec{
		ID: trayDrawerSurfaceID, Namespace: "sysc-shell-tray-drawer",
		Layer:  layershell.ZwlrLayerShellV1LayerOverlay,
		Anchor: uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorRight),
		Width:  trayDrawerWidth, Height: trayDrawerHeight, ExclusiveZone: -1, Keyboard: keyboardOnDemand,
		Callbacks: wayland.HostCallbacks{
			Configure: h.configureLocking, Render: h.renderLocking, Handle: h.handleLocking,
		},
	}
}

// The three Wayland callbacks take the registry lock, because the same host
// state is written by the client pump through refresh.
func (h *trayDrawerHost) configureLocking(width, height, scale120 int) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.configure(width, height, scale120)
}

func (h *trayDrawerHost) renderLocking(pixels []byte, width, height, stride int) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.render(pixels, width, height, stride)
}

func (h *trayDrawerHost) handleLocking(event wayland.Event) bool {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.handle(event)
}

func (h *trayDrawerHost) configure(width, height, scale120 int) error {
	h.logicalW, h.logicalH, h.scale120 = width, height, scale120
	h.style.Scale120 = ui.Scale120(scale120)
	h.style.Body = ui.Rect{W: width, H: height}
	measure := func(text string, tabular bool) (int, int) {
		if h.text != nil {
			if w, height, err := h.text.Measure(text, h.style.Size, tabular); err == nil {
				return w, height
			}
		}
		return len([]rune(text)) * 8, 16
	}
	return ui.LayoutColumn(h.root, ui.Rect{W: width, H: height}, measure)
}

func (h *trayDrawerHost) render(pixels []byte, width, height, stride int) error {
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

func (h *trayDrawerHost) handle(event wayland.Event) bool {
	switch event.Kind {
	case wayland.EventKeyPress:
		switch event.Key {
		case keyEsc:
			h.close()
			return true
		case keyTab, keyDown:
			h.roving.Next()
			h.ensureFocusVisible()
			return true
		case keyUp:
			h.roving.Prev()
			h.ensureFocusVisible()
			return true
		case keyEnter, keySpace:
			return h.activateFocused()
		case keyPageDown:
			ui.ScrollBy(h.root, max(h.logicalH, 1))
			h.relayout()
			return true
		case keyPageUp:
			ui.ScrollBy(h.root, -max(h.logicalH, 1))
			h.relayout()
			return true
		}
	case wayland.EventPointerEnter, wayland.EventPointerMotion:
		h.hoverX, h.hoverY = int(math.Floor(event.X)), int(math.Floor(event.Y))
		// Only a change of resolved row repaints; motion within one row does
		// not.
		if h.pointer.setHover(hoverKeyAt(h.root, h.hoverX, h.hoverY)) {
			h.pointer.apply(h.root, nil)
			return true
		}
	case wayland.EventPointerLeave:
		h.pressed = nil
		if h.pointer.clear() {
			h.pointer.apply(h.root, nil)
			return true
		}
	case wayland.EventPointerPress:
		h.pressed = h.hitFocusable(h.hoverX, h.hoverY)
		h.pointer.setPress(h.pressed.StableKey())
		h.pointer.apply(h.root, nil)
		return h.pressed != nil
	case wayland.EventPointerRelease:
		n := h.hitFocusable(h.hoverX, h.hoverY)
		pressed := h.pressed
		h.pressed = nil
		cleared := h.pointer.setPress("")
		h.pointer.apply(h.root, nil)
		if n != nil && n == pressed {
			return h.activate(n, event)
		}
		if cleared {
			return true
		}
	case wayland.EventPointerAxis:
		delta := int(event.AxisValue)
		if event.AxisDiscrete != 0 {
			delta = int(event.AxisDiscrete) * 40
		}
		ui.ScrollBy(h.root, delta)
		h.relayout()
		return true
	}
	return false
}

func (h *trayDrawerHost) hitFocusable(x, y int) *ui.Node {
	for _, row := range h.root.Children {
		for _, node := range ui.Focusables(row) {
			if node.Bounds.Contains(x, y) {
				return node
			}
		}
	}
	return nil
}

func (h *trayDrawerHost) activateFocused() bool {
	if len(h.focus) == 0 {
		return false
	}
	return h.activate(h.focus[h.roving.Index()], wayland.Event{Kind: wayland.EventPointerRelease})
}

// activate runs one row control. An item row forwards the whole event, so a
// row answers the same buttons the bar does: the drawer is another way to
// reach an item, not a second set of gestures to learn.
func (h *trayDrawerHost) activate(node *ui.Node, event wayland.Event) bool {
	if node == nil {
		return false
	}
	if item, ok := h.itemNodes[node]; ok {
		if h.itemAction == nil {
			return false
		}
		return h.itemAction(item, h.connector, h.output, event)
	}
	rest, ok := strings.CutPrefix(node.Action, "tray-pref:")
	if !ok {
		return false
	}
	action, token, ok := strings.Cut(rest, ":")
	if !ok || token == "" {
		return false
	}
	edits := map[string]trayPreferenceEdit{
		"hide": trayPreferenceHide, "show": trayPreferenceShow,
		"pin": trayPreferencePin, "unpin": trayPreferenceUnpin,
		"earlier": trayPreferenceEarlier, "later": trayPreferenceLater,
	}
	edit, ok := edits[action]
	if !ok {
		return false
	}
	next := h.r.cfg
	next.Tray = editTrayPreferences(next.Tray, edit, token, h.liveOrder())
	if err := h.r.writeConfig(next); err != nil {
		return false
	}
	return true
}

func (h *trayDrawerHost) liveOrder() []string {
	colliding := stringSet(h.arranged.Collisions)
	items := append(append([]tray.Item(nil), h.arranged.Bar...), h.arranged.Overflow...)
	order := make([]string, 0, len(items))
	for _, item := range items {
		token, ok := stableTrayToken(item)
		if ok && !colliding[token] {
			order = addUnique(order, token)
		}
	}
	return order
}

func (h *trayDrawerHost) relayout() {
	if h.logicalW > 0 {
		_ = h.configure(h.logicalW, h.logicalH, h.scale120)
	}
}

func (h *trayDrawerHost) ensureFocusVisible() {
	if len(h.focus) == 0 || h.root == nil {
		return
	}
	row, ok := h.focusRows[h.focus[h.roving.Index()]]
	if !ok {
		return
	}
	view := h.root.Bounds.H - 2*h.root.Padding
	top, lower := row*h.root.ItemHeight, (row+1)*h.root.ItemHeight
	if top < h.root.ScrollOffset {
		h.root.ScrollOffset = top
	} else if lower > h.root.ScrollOffset+view {
		h.root.ScrollOffset = lower - view
	}
	ui.ScrollBy(h.root, 0)
	h.relayout()
}

func (h *trayDrawerHost) closeSurface() {
	if h.closed || h.output == 0 {
		return
	}
	h.closed = true
	h.request(wayland.AuxRequest{Output: h.output, ID: trayDrawerSurfaceID})
}

func (h *trayDrawerHost) releaseForChainClose() {
	h.open_ = false
	h.closeSurface()
}

func (h *trayDrawerHost) close() {
	if !h.open_ {
		return
	}
	h.open_ = false
	h.closeSurface()
	h.r.roots.closeRoot(h.rootGen)
}

func (h *trayDrawerHost) outputLost(global uint32) {
	if h.open_ && h.output == global {
		h.close()
	}
}

func (h *trayDrawerHost) disconnect() { h.close() }
