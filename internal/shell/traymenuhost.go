package shell

import (
	"math"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

const (
	trayMenuNamespace = "sysc-shell-tray-menu"
	trayMenuSurfaceID = "tray-menu"
	// keyboardOnDemand is the layer-shell keyboard mode a menu takes: focus
	// arrives with the grab and leaves when the menu closes.
	keyboardOnDemand = uint32(layershell.ZwlrLayerSurfaceV1KeyboardInteractivityOnDemand)

	// The menu surface is sized from its own rows. A layer surface with no
	// size takes the whole anchored edge, which a tray menu must not do; the
	// height is capped and the list scrolls past the cap.
	trayMenuRowHeight = 28
	trayMenuWidth     = 280
	trayMenuMaxHeight = 480
	trayMenuPadding   = 6
)

// trayMenuActionPrefix marks a row action. The suffix is the protocol entry
// ID, which is what menu.select carries.
const trayMenuActionPrefix = "tray-menu:"

func trayMenuAction(id int32) string {
	return trayMenuActionPrefix + strconv.FormatInt(int64(id), 10)
}

func trayMenuActionID(action string) (int32, bool) {
	rest, ok := strings.CutPrefix(action, trayMenuActionPrefix)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(id), true
}

// trayMenuHost owns the one open tray menu surface. The documented primary
// path is an xdg_popup parented to the triggering layer surface; the live
// Niri probe of that sequence is still pending, so this host implements the
// design's named fallback: one Overlay auxiliary surface with the same root
// and revision rules. Switching paths changes only spec().
//
// Opening saves the serial, output, item key (which embeds the service
// generation), menu revision, and root generation. Close restores keyboard,
// drops the serial, and releases the surface exactly once, whichever terminal
// path arrives first.
type trayMenuHost struct {
	r          *Registry
	harnessRef *hostHarness
	request    func(wayland.AuxRequest)

	open_     bool
	closed    bool // surface close already requested
	connector string
	output    uint32 // wl_registry global
	serial    uint32
	item      tray.ItemKey
	revision  uint32 // revision of the visible tree
	knownRev  uint32 // newest revision the service has sent
	rootGen   uint64
	menu      *trayMenu
	interact  bool
	deferred  *tray.Menu // newest structural revision while interacting

	// refreshAsked records the items a stale selection asked the service to
	// refresh. The command itself goes out through the registry sender; this
	// keeps the record readable without one.
	refreshAsked []tray.ItemKey

	// place is where on the output the surface sits. A bar menu hangs under
	// its icon; a drawer menu sits beside the drawer that opened it.
	place trayMenuPlacement
	// child records that this menu hangs off an open drawer rather than
	// owning the chain: closing it must release only the child.
	child bool

	root     *ui.Node
	focus    []*ui.Node
	roving   ui.Roving
	logicalW int
	logicalH int
	scale120 int
	hoverX   int
	hoverY   int
	pressed  *ui.Node
	text     *render.TextRenderer
	style    render.ProofStyle
}

func newTrayMenuHost(r *Registry, harness *hostHarness) *trayMenuHost {
	h := &trayMenuHost{r: r}
	if harness != nil {
		h.request = harness.request
		h.harnessRef = harness
	} else {
		h.request = func(req wayland.AuxRequest) { r.sendAux(req) }
	}
	return h
}

func (h *trayMenuHost) harness() *hostHarness { return h.harnessRef }

// open shows the item's menu on one output as a fresh root. A dead item or an
// item with no menu refuses; the root chain and the compositor are left alone.
func (h *trayMenuHost) open(item tray.ItemKey, connector string, output, serial uint32) bool {
	if !h.prepare(item, connector, output, serial) {
		return false
	}
	h.child = false
	h.rootGen = h.r.roots.openRoot(trayMenuRoot(output))
	h.r.roots.onClose(h.rootGen, h.releaseForChainClose)
	h.publishOpen()
	return true
}

// openAt is open with the bar bounds of the item that triggered it, so the
// surface lands under its icon rather than at the output origin.
func (h *trayMenuHost) openAt(item tray.ItemKey, connector string, output, serial uint32, anchor ui.Rect) bool {
	h.place = trayMenuUnderBar(anchor)
	return h.open(item, connector, output, serial)
}

// openAsChild attaches the menu to the root that is already open — the drawer
// a menu was launched from. The drawer keeps the chain and keeps its surface;
// closing the menu releases only the child.
func (h *trayMenuHost) openAsChild(item tray.ItemKey, connector string, output, serial uint32) bool {
	_, generation, ok := h.r.roots.current()
	if !ok {
		return false
	}
	h.place = trayMenuBesideDrawer()
	if !h.prepare(item, connector, output, serial) {
		return false
	}
	if !h.r.roots.attach(generation, trayMenuRoot(output)) {
		h.open_ = false
		return false
	}
	h.child = true
	h.rootGen = generation
	h.r.roots.onChildClose(generation, h.releaseForChainClose)
	h.publishOpen()
	return true
}

// prepare validates the item and installs the model. It publishes nothing, so
// a caller that cannot take the chain leaves the compositor untouched.
func (h *trayMenuHost) prepare(item tray.ItemKey, connector string, output, serial uint32) bool {
	if h.open_ || output == 0 || !h.r.tray.has(item) {
		return false
	}
	menu, ok := h.r.tray.menuFor(item)
	if !ok {
		return false
	}
	h.open_ = true
	h.closed = false
	h.connector = connector
	h.output = output
	h.serial = serial
	h.item = item
	h.revision = menu.Revision
	h.knownRev = menu.Revision
	h.menu = newTrayMenu(menu)
	h.interact = false
	h.deferred = nil
	h.logicalW, h.logicalH = 0, 0
	h.pressed = nil
	h.rebuild()
	return true
}

// publishOpen closes any tooltip before the surface appears: a menu takes the
// pointer grab, and a tooltip left up would outlive the hover that made it.
func (h *trayMenuHost) publishOpen() {
	h.r.dwell.leave()
	h.request(wayland.AuxRequest{Output: h.output, Open: h.spec()})
}

// releaseForChainClose runs when the chain is replaced or closed by the
// chain's owner: the surface goes away and host state resets, but the chain
// itself is already mid-release and must not be closed again.
func (h *trayMenuHost) releaseForChainClose() {
	if !h.open_ {
		return
	}
	h.tellServiceClosed()
	h.open_ = false
	h.serial = 0
	h.deferred = nil
	h.child = false
	h.closeSurface()
}

// spec describes the fallback surface: an Overlay layer surface sized to the
// menu and anchored under the item that opened it. The documented primary
// path is an xdg_popup parented to the bar; switching to it changes only this
// method, because every callback below works in surface-local coordinates.
func (h *trayMenuHost) spec() *wayland.AuxSpec {
	width, height := h.size()
	place := h.place
	if place.anchor == 0 {
		place = trayMenuUnderBar(ui.Rect{})
	}
	return &wayland.AuxSpec{
		ID:            trayMenuSurfaceID,
		Namespace:     trayMenuNamespace,
		Layer:         layershell.ZwlrLayerShellV1LayerOverlay,
		Anchor:        place.anchor,
		MarginTop:     place.marginTop,
		MarginLeft:    place.marginLeft,
		MarginRight:   place.marginRight,
		Width:         int32(width),
		Height:        int32(height),
		ExclusiveZone: -1,
		Keyboard:      keyboardOnDemand,
		Callbacks: wayland.HostCallbacks{
			Configure: h.configureLocking, Render: h.renderLocking, Handle: h.handleLocking,
		},
	}
}

// trayMenuPlacement is the layer-shell anchor and margins for one menu. It is
// computed by whoever opened the menu, because only that caller knows where on
// the output the trigger was.
type trayMenuPlacement struct {
	anchor                  uint32
	marginTop               int32
	marginLeft, marginRight int32
}

// trayMenuUnderBar hangs the menu from the bar, left-aligned with the icon
// that opened it. The bar node's bounds are already output coordinates,
// because the bar surface spans the output.
func trayMenuUnderBar(anchor ui.Rect) trayMenuPlacement {
	return trayMenuPlacement{
		anchor:     uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorLeft),
		marginTop:  int32(max(anchor.Y+anchor.H, 0)),
		marginLeft: int32(max(anchor.X, 0)),
	}
}

// trayMenuBesideDrawer puts the menu immediately left of the open drawer, on
// the same top-right corner the drawer anchors to, so the two never overlap.
func trayMenuBesideDrawer() trayMenuPlacement {
	return trayMenuPlacement{
		anchor:      uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorRight),
		marginRight: trayDrawerWidth,
	}
}

// size reports the logical surface size.
//
// It is measured against the widest level in the whole tree, not the level on
// screen: a layer surface's size is fixed when it is created, and entering a
// submenu must not clip rows the parent level had no room for. A level with
// fewer rows leaves the surplus empty.
func (h *trayMenuHost) size() (int, int) {
	rows := 0
	if h.menu != nil {
		rows = h.menu.widestLevel()
	}
	height := rows*trayMenuRowHeight + 2*trayMenuPadding
	return trayMenuWidth, min(max(height, trayMenuRowHeight+2*trayMenuPadding), trayMenuMaxHeight)
}

// The three Wayland callbacks take the registry lock, because the same host
// state is written by the client pump through applyMenu.
func (h *trayMenuHost) configureLocking(width, height, scale120 int) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.configure(width, height, scale120)
}

func (h *trayMenuHost) renderLocking(pixels []byte, width, height, stride int) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.render(pixels, width, height, stride)
}

func (h *trayMenuHost) handleLocking(event wayland.Event) bool {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.handle(event)
}

func (h *trayMenuHost) configure(width, height, scale120 int) error {
	h.logicalW, h.logicalH, h.scale120 = width, height, scale120
	h.style.Scale120 = ui.Scale120(scale120)
	h.style.Body = ui.Rect{W: width, H: height}
	measure := func(text string, tabular bool) (int, int) {
		if h.text != nil {
			if w, textHeight, err := h.text.Measure(text, h.style.Size, tabular); err == nil {
				return w, textHeight
			}
		}
		return len([]rune(text)) * 8, 16
	}
	return ui.LayoutColumn(h.root, ui.Rect{W: width, H: height}, measure)
}

func (h *trayMenuHost) render(pixels []byte, width, height, stride int) error {
	createdText := false
	if h.text == nil {
		fonts, err := render.NewSystemFontMap(h.r.cfg.Bar.FontFamily, render.DefaultFontCacheDir())
		if err != nil {
			return err
		}
		h.text = render.NewTextRendererWithFontMap(fonts)
		createdText = true
		theme := ThemeFrom(h.r.cfg, h.r.cfg.Bar)
		h.style.Size = theme.TextSize
		h.style.Radius = theme.Radius
		h.style.Background, h.style.Foreground = theme.Background, theme.Foreground
		h.style.Track, h.style.Accent, h.style.AccentOn, h.style.Error =
			theme.Muted, theme.Accent, theme.Error, theme.Error
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

// handle is the menu's whole input contract. Escape pops one level before it
// closes anything, so a submenu never costs the user the menu itself.
func (h *trayMenuHost) handle(event wayland.Event) bool {
	if !h.open_ {
		return false
	}
	switch event.Kind {
	case wayland.EventKeyPress:
		h.noteInteraction()
		switch event.Key {
		case keyEsc, keyLeft:
			if h.menu != nil && h.menu.back() {
				h.rebuild()
				h.idle()
				return true
			}
			h.close()
			return true
		case keyTab, keyDown:
			h.moveFocus(1)
			return true
		case keyUp:
			h.moveFocus(-1)
			return true
		case keyRight:
			return h.pushFocused()
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
	case wayland.EventKeyRelease:
		h.idle()
		return false
	case wayland.EventPointerEnter, wayland.EventPointerMotion:
		h.hoverX, h.hoverY = int(math.Floor(event.X)), int(math.Floor(event.Y))
		return false
	case wayland.EventPointerLeave:
		h.pressed = nil
		h.idle()
		return false
	case wayland.EventPointerPress:
		h.noteInteraction()
		h.pressed = h.hitFocusable(h.hoverX, h.hoverY)
		if h.pressed == nil {
			return false
		}
		h.focusNode(h.pressed)
		return true
	case wayland.EventPointerRelease:
		node := h.hitFocusable(h.hoverX, h.hoverY)
		pressed := h.pressed
		h.pressed = nil
		if node == nil || node != pressed {
			h.idle()
			return false
		}
		h.focusNode(node)
		changed := h.activateFocused()
		h.idle()
		return changed
	case wayland.EventPointerAxis:
		delta := int(event.AxisValue)
		if event.AxisDiscrete != 0 {
			delta = int(event.AxisDiscrete) * trayMenuRowHeight
		}
		ui.ScrollBy(h.root, delta)
		h.relayout()
		return true
	}
	return false
}

// rebuild reprojects the visible level and restores the roving index from the
// model's own focus, which replaceTree already carried across revisions.
func (h *trayMenuHost) rebuild() {
	h.root = trayMenuTree(h.menu)
	h.focus = ui.Focusables(h.root)
	h.roving.Count = len(h.focus)
	h.syncRovingFromModel()
	if h.logicalW > 0 {
		h.relayout()
	}
}

// syncRovingFromModel points the roving index at the node whose action names
// the model's focused entry. The model owns focus; the tree only shows it.
func (h *trayMenuHost) syncRovingFromModel() {
	if h.menu == nil {
		return
	}
	want := h.menu.focusedID()
	for i, node := range h.focus {
		if id, ok := trayMenuActionID(node.Action); ok && id == want {
			h.roving.Set(i)
			return
		}
	}
}

// moveFocus walks the model, then mirrors the result into the tree, so the
// two never disagree about which entry a selection would send.
func (h *trayMenuHost) moveFocus(delta int) {
	if h.menu == nil {
		return
	}
	h.menu.move(delta)
	h.syncRovingFromModel()
}

func (h *trayMenuHost) focusNode(node *ui.Node) {
	id, ok := trayMenuActionID(node.Action)
	if !ok || h.menu == nil {
		return
	}
	nodes := h.menu.visible()
	for i, n := range nodes {
		if n.ID == id && focusableRow(n) {
			h.menu.top().focus = i
			h.syncRovingFromModel()
			return
		}
	}
}

func (h *trayMenuHost) hitFocusable(x, y int) *ui.Node {
	for _, node := range h.focus {
		if node.Bounds.Contains(x, y) {
			return node
		}
	}
	return nil
}

// pushFocused enters a submenu in place. Nothing is sent: the service already
// published the whole tree, and a submenu is a level of this same surface.
func (h *trayMenuHost) pushFocused() bool {
	if h.menu == nil || !h.menu.push("") {
		return false
	}
	h.rebuild()
	return true
}

// activateFocused sends the focused entry, or enters it when it is a submenu.
// A menu the service has already superseded sends nothing and refreshes.
func (h *trayMenuHost) activateFocused() bool {
	if h.menu == nil {
		return false
	}
	if h.submenuFocused() {
		return h.pushFocused()
	}
	if h.r.traySender == nil {
		return false
	}
	stale := h.selectFocused(h.r.traySender)
	if stale {
		h.askRefresh()
		return true
	}
	h.close()
	return true
}

func (h *trayMenuHost) submenuFocused() bool {
	nodes := h.menu.visible()
	focus := h.menu.top().focus
	if focus < 0 || focus >= len(nodes) {
		return false
	}
	node := nodes[focus]
	return node.ChildrenDisplay == "submenu" && len(node.Children) > 0
}

// askRefresh asks the service to republish the item's menu. about-to-show is
// the protocol's refresh: there is no separate refresh command.
func (h *trayMenuHost) askRefresh() {
	if h.r.traySender == nil || !h.r.tray.has(h.item) {
		return
	}
	_, _ = h.r.traySender.Send(tray.Command{
		Kind: tray.CommandAboutToShow, Item: h.item, Output: h.output, Serial: h.serial,
	})
}

// relayout re-arranges and asks for a repaint. Before the first configure
// there is no surface to repaint, so nothing is published.
func (h *trayMenuHost) relayout() {
	if h.logicalW <= 0 {
		return
	}
	_ = h.configure(h.logicalW, h.logicalH, h.scale120)
	h.r.publishSurface(h.output, trayMenuSurfaceID)
}

// trayMenuTree projects the visible level into rows. Separators and disabled
// entries render but never take focus, so keyboard travel skips exactly what
// a pointer cannot press.
func trayMenuTree(m *trayMenu) *ui.Node {
	root := &ui.Node{Kind: ui.KindVirtualList, ItemHeight: trayMenuRowHeight, Padding: trayMenuPadding}
	if m == nil {
		root.Item = func(int) *ui.Node { return nil }
		return root
	}
	visible := m.visible()
	rows := make([]*ui.Node, 0, len(visible))
	for i := range visible {
		rows = append(rows, trayMenuRowNode(m.row(i)))
	}
	root.ItemCount = len(rows)
	root.Item = func(i int) *ui.Node {
		if i < 0 || i >= len(rows) {
			return nil
		}
		return rows[i]
	}
	return root
}

func trayMenuRowNode(row trayMenuRow) *ui.Node {
	if row.role == "separator" {
		return &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{Kind: ui.KindSeparator}}}
	}
	label := strings.TrimSpace(row.name)
	if label == "" {
		label = "Item"
	}
	name := label
	switch {
	case row.checked:
		name = label + " (checked)"
	case row.submenu:
		name = label + " (submenu)"
	}
	entry := &ui.Node{
		Kind: ui.KindButton, Text: trayMenuRowText(row, label), Padding: 4,
		Action: trayMenuAction(row.id), Name: name, Role: row.role,
		Focusable: row.enabled,
	}
	return &ui.Node{Kind: ui.KindRow, Gap: 6, Children: []*ui.Node{entry}}
}

// trayMenuRowText folds toggle and submenu state into the text. The fallback
// surface paints one button per row, so state a native menu would draw as a
// glyph has to live in the label and the accessible name alike.
func trayMenuRowText(row trayMenuRow, label string) string {
	switch {
	case row.role == "checkmenuitem" || row.role == "radiomenuitem":
		if row.checked {
			return "\u2713 " + label
		}
		return "  " + label
	case row.submenu:
		return label + " \u203a"
	}
	return label
}

// closeSurface requests the compositor close exactly once.
func (h *trayMenuHost) closeSurface() {
	if h.closed || h.output == 0 {
		return
	}
	h.closed = true
	h.request(wayland.AuxRequest{Output: h.output, ID: trayMenuSurfaceID})
}

// close ends the menu from the shell side: the service is told, then the
// surface, the serial, and the part of the chain this menu owns go away. A
// menu attached to a drawer releases only the child, leaving the drawer up.
func (h *trayMenuHost) close() {
	if !h.open_ {
		return
	}
	h.tellServiceClosed()
	h.open_ = false
	h.serial = 0
	h.deferred = nil
	h.closeSurface()
	if _, gen, ok := h.r.roots.current(); !ok || gen != h.rootGen {
		return
	}
	if h.child {
		h.r.roots.closeChild(h.rootGen)
		return
	}
	h.r.roots.closeRoot(h.rootGen)
}

// tellServiceClosed releases the service's own menu state. A dead item is
// skipped: its key is already stale and the command could not be delivered.
func (h *trayMenuHost) tellServiceClosed() {
	if h.r.traySender == nil || !h.r.tray.has(h.item) {
		return
	}
	_, _ = h.r.traySender.Send(tray.Command{
		Kind: tray.CommandMenuClose, Item: h.item, Output: h.output, Serial: h.serial,
	})
}

func (h *trayMenuHost) itemLost(key tray.ItemKey) {
	if h.open_ && h.item == key {
		h.close()
	}
}

func (h *trayMenuHost) outputLost(global uint32) {
	if h.open_ && h.output == global {
		h.close()
	}
}

func (h *trayMenuHost) disconnect() { h.close() }

// noteInteraction marks pointer or keyboard activity; structural revisions
// wait until idle.
func (h *trayMenuHost) noteInteraction() { h.interact = true }

// applyMenu pulls the current service menu for the open item and applies the
// revision rules: property-only updates apply in place, structural updates
// defer while interacting, and only the newest deferred revision survives.
func (h *trayMenuHost) applyMenu() {
	if !h.open_ {
		return
	}
	menu, ok := h.r.tray.menuFor(h.item)
	if !ok || menu.Revision <= h.knownRev {
		return
	}
	h.knownRev = menu.Revision
	if h.interact && !sameMenuShape(h.menu, &menu) {
		h.deferred = &menu // newest wins
		return
	}
	h.replaceTree(menu)
}

// idle ends interaction: the newest deferred structural revision replaces
// the tree, restoring focus by entry ID when the focused entry still exists.
func (h *trayMenuHost) idle() {
	h.interact = false
	if h.deferred == nil {
		return
	}
	menu := h.deferred
	h.deferred = nil
	h.replaceTree(*menu)
}

func (h *trayMenuHost) replaceTree(menu tray.Menu) {
	focused := int32(-1)
	if h.menu != nil {
		focused = h.menu.focusedID()
	}
	next := newTrayMenu(menu)
	if focused >= 0 {
		// Restore focus by entry ID: property changes and reorders keep the
		// user's place.
		nodes := next.visible()
		for i, n := range nodes {
			if n.ID == focused && focusableRow(n) {
				next.top().focus = i
				break
			}
		}
	}
	h.menu = next
	h.revision = menu.Revision
	h.rebuild()
}

// sameMenuShape reports whether the update changes no structure: identical
// visible ID sequences at the current level. Only property changes keep the
// user's place.
func sameMenuShape(cur *trayMenu, next *tray.Menu) bool {
	if cur == nil {
		return false
	}
	old := cur.visible()
	fresh := make([]int32, 0, len(old))
	var walk func(nodes []tray.MenuNode)
	walk = func(nodes []tray.MenuNode) {
		for _, n := range nodes {
			if !n.Visible {
				continue
			}
			fresh = append(fresh, n.ID)
			if len(fresh) >= menuMaxRows {
				return
			}
		}
	}
	walk(next.Root.Children)
	if len(old) != len(fresh) {
		return false
	}
	for i := range old {
		if old[i].ID != fresh[i] {
			return false
		}
	}
	return true
}

// selectFocused activates the focused entry. A selection against an old
// revision sends nothing, reports stale, and asks the service for a refresh;
// the menu stays open and usable.
func (h *trayMenuHost) selectFocused(s trayCommandSender) (stale bool) {
	if !h.open_ {
		return false
	}
	if h.revision != h.knownRev {
		h.refreshAsked = append(h.refreshAsked, h.item)
		return true
	}
	id, ok := h.menu.activateFocused()
	if !ok {
		return false
	}
	_, _ = s.Send(tray.Command{
		Kind:         tray.CommandMenuSelect,
		Item:         h.item,
		MenuRevision: h.revision,
		MenuID:       id,
		Output:       h.output,
		Serial:       h.serial,
	})
	return false
}
