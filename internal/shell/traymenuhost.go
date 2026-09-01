package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

const (
	trayMenuNamespace = "sysc-shell-tray-menu"
	trayMenuSurfaceID = "tray-menu"
	// keyboardOnDemand is the layer-shell keyboard mode a menu takes: focus
	// arrives with the grab and leaves when the menu closes.
	keyboardOnDemand = uint32(layershell.ZwlrLayerSurfaceV1KeyboardInteractivityOnDemand)
)

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

	// refreshAsked records refresh requests in place of a wired client; the
	// wiring task swaps this for a menu refresh command.
	refreshAsked []tray.ItemKey
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

// open shows the item's menu on one output. A dead item or an item with no
// menu refuses; the root chain and the compositor are left alone.
func (h *trayMenuHost) open(item tray.ItemKey, connector string, output, serial uint32) bool {
	if h.open_ || !h.r.tray.has(item) {
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
	h.rootGen = h.r.roots.openRoot(trayMenuRoot(output))
	h.r.roots.onClose(h.rootGen, h.releaseForChainClose)
	h.request(wayland.AuxRequest{Output: output, Open: h.spec()})
	return true
}

// releaseForChainClose runs when the chain is replaced or closed by the
// chain's owner: the surface goes away and host state resets, but the chain
// itself is already mid-release and must not be closed again.
func (h *trayMenuHost) releaseForChainClose() {
	h.open_ = false
	h.serial = 0
	h.deferred = nil
	h.closeSurface()
}

func (h *trayMenuHost) spec() *wayland.AuxSpec {
	return &wayland.AuxSpec{
		ID:            trayMenuSurfaceID,
		Namespace:     trayMenuNamespace,
		Layer:         layershell.ZwlrLayerShellV1LayerOverlay,
		Anchor:        uint32(layershell.ZwlrLayerSurfaceV1AnchorBottom),
		ExclusiveZone: -1,
		Keyboard:      keyboardOnDemand,
	}
}

// closeSurface requests the compositor close exactly once.
func (h *trayMenuHost) closeSurface() {
	if h.closed || h.output == 0 {
		return
	}
	h.closed = true
	h.request(wayland.AuxRequest{Output: h.output, ID: trayMenuSurfaceID})
}

// close ends the menu from the shell side: surface, serial, root chain.
func (h *trayMenuHost) close() {
	if !h.open_ {
		return
	}
	h.open_ = false
	h.serial = 0
	h.deferred = nil
	h.closeSurface()
	if _, gen, ok := h.r.roots.current(); ok && gen == h.rootGen {
		h.r.roots.closeRoot(gen)
	}
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
