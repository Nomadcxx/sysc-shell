package shell

import (
	"context"
	"sync"

	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/trayclient"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

// trayState is the shell's projection of service-owned tray items. Items are
// keyed by their full ItemKey — owner, path, and generation — so a
// re-registered owner never aliases its replacement and a stale command never
// reaches it.
type trayState struct {
	mu         sync.Mutex
	generation uint64
	items      map[tray.ItemKey]tray.Item
	order      []tray.ItemKey
	menus      map[tray.ItemKey]tray.Menu
}

func newTrayState() *trayState {
	return &trayState{items: map[tray.ItemKey]tray.Item{}, menus: map[tray.ItemKey]tray.Menu{}}
}

// applyTray applies one immutable client message. A stale generation is
// discarded; a disconnect drops everything.
func (s *trayState) applyTray(m trayclient.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch m.Kind {
	case trayclient.KindSnapshot:
		s.generation = m.Generation
		s.items = make(map[tray.ItemKey]tray.Item, len(m.Snapshot.Items))
		s.order = s.order[:0]
		for _, it := range m.Snapshot.Items {
			s.items[it.Key] = it
			s.order = append(s.order, it.Key)
		}
	case trayclient.KindItemAdded, trayclient.KindItemChanged:
		if m.Generation != s.generation {
			return
		}
		if _, exists := s.items[m.Item.Key]; !exists {
			s.order = append(s.order, m.Item.Key)
		}
		s.items[m.Item.Key] = m.Item
	case trayclient.KindItemRemoved:
		if m.Generation != s.generation {
			return
		}
		delete(s.items, m.Removed.Key)
		delete(s.menus, m.Removed.Key)
		for i, key := range s.order {
			if key == m.Removed.Key {
				s.order = append(s.order[:i], s.order[i+1:]...)
				break
			}
		}
	case trayclient.KindMenuUpdated:
		if m.Generation != s.generation {
			return
		}
		s.menus[m.Menu.Key] = m.Menu.Menu
	case trayclient.KindDisconnected:
		s.generation = 0
		s.items = map[tray.ItemKey]tray.Item{}
		s.order = nil
		s.menus = map[tray.ItemKey]tray.Menu{}
	}
}

func (s *trayState) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

func (s *trayState) title(key tray.ItemKey) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.items[key].Title
}

// iconName resolves the effective named icon: NeedsAttention replaces the
// normal icon with the attention icon.
func (s *trayState) iconName(key tray.ItemKey) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[key]
	if !ok {
		return "", false
	}
	if it.Status == tray.StatusNeedsAttention && it.AttentionIcon.Name != "" {
		return it.AttentionIcon.Name, true
	}
	return it.Icon.Name, it.Icon.Name != ""
}

// items returns the projection for one output. Projection is
// output-independent: every output reads the same set.
func (s *trayState) itemsList() []tray.Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]tray.Item, 0, len(s.items))
	for _, key := range s.order {
		if it, ok := s.items[key]; ok {
			out = append(out, it)
		}
	}
	return out
}

// has reports whether the exact key — owner, path, generation — is live.
func (s *trayState) has(key tray.ItemKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.items[key]
	return ok
}

func (s *trayState) itemFor(key tray.ItemKey) (tray.Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[key]
	return item, ok
}

// menuFor returns the service-owned menu for one item, when one has arrived.
func (s *trayState) menuFor(key tray.ItemKey) (tray.Menu, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.menus[key]
	return m, ok
}

// tooltipText flattens the service-owned tooltip into one dwell string. The
// service already bounded the fields; the shell clamps again at the protocol
// bound so a compromised service cannot grow the bar's hover surface.
func (s *trayState) tooltipText(key tray.ItemKey) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[key]
	if !ok {
		return ""
	}
	text := it.Tooltip.Title
	if it.Tooltip.Description != "" {
		if text != "" {
			text += "\n"
		}
		text += it.Tooltip.Description
	}
	if len(text) > tray.MaxTooltipBytes {
		text = text[:tray.MaxTooltipBytes]
	}
	return text
}

// Registry wrappers.
func (r *Registry) applyTray(m trayclient.Message)             { r.tray.applyTray(m) }
func (r *Registry) trayItemCount() int                         { return r.tray.count() }
func (r *Registry) trayTitle(k tray.ItemKey) string            { return r.tray.title(k) }
func (r *Registry) trayIconName(k tray.ItemKey) (string, bool) { return r.tray.iconName(k) }
func (r *Registry) trayItemsFor(string) []tray.Item            { return r.tray.itemsList() }
func (r *Registry) trayTooltipText(k tray.ItemKey) string      { return r.tray.tooltipText(k) }

// Linux evdev pointer button codes, as wl_pointer reports them. The tray is
// the first feature that distinguishes buttons, so they are named here.
const (
	buttonLeft   = 0x110
	buttonRight  = 0x111
	buttonMiddle = 0x112
)

// BindTray installs the presentation hosts, the command sender, and the one
// bounded icon worker. The wiring layer calls it; tests that drive applyTray
// directly do not, so nothing starts a goroutine they did not ask for.
func (r *Registry) BindTray(sender trayCommandSender) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.traySender = sender
	r.trayMenu = newTrayMenuHost(r, nil)
	r.trayDrawer = newTrayDrawerHost(r, nil)
	r.trayDrawer.itemAction = r.drawerItemAction
	// A stale-item reply means the service replaced the key under a click that
	// was already in flight. The projection is authoritative, so the retry is
	// to reproject: the next command carries the live key or is not sent.
	r.trayReplies = newTrayReplyTracker(r, func(tray.ItemKey) { r.reprojectTray() })
	workerContext, cancel := context.WithCancel(context.Background())
	r.trayIconCancel = cancel
	r.trayIcons = icons.NewWorker(icons.NewResolver("", nil), r.applyTrayIcon)
	go func() { _ = r.trayIcons.Run(workerContext) }()
	r.attachRunningIconsLocked()
}

// TrayMessages is the channel the trayclient publishes to and main drains
// onto ApplyTray.
func (r *Registry) TrayMessages() chan trayclient.Message { return r.trayCh }

// ApplyTray applies one client message: the projection first, then every
// surface that shows it. Each kind states which correlations it breaks.
func (r *Registry) ApplyTray(m trayclient.Message) {
	r.tray.applyTray(m)

	r.mu.Lock()
	switch m.Kind {
	case trayclient.KindItemRemoved:
		// Only this item's surfaces go. A second item's menu is untouched.
		r.clearPendingTrayMenuFor(m.Removed.Key)
		if r.trayMenu != nil {
			r.trayMenu.itemLost(m.Removed.Key)
		}
	case trayclient.KindMenuUpdated:
		if r.trayMenu != nil {
			r.trayMenu.applyMenu()
		}
		r.resumePendingTrayMenuLocked(m.Menu.Key)
	case trayclient.KindReply:
		if r.trayReplies != nil {
			r.trayReplies.apply(m)
		}
		if r.pendingTrayMenu.active && r.pendingTrayMenu.requestID == m.RequestID && !m.Reply.OK {
			// The menu will never arrive for this request.
			r.pendingTrayMenu = pendingTrayMenu{}
		}
	case trayclient.KindDisconnected, trayclient.KindSnapshot:
		// A reconnect republishes every item under a fresh generation, so
		// every key a surface was correlated against is now dead.
		r.pendingTrayMenu = pendingTrayMenu{}
		if r.trayMenu != nil && !r.tray.has(r.trayMenu.item) {
			r.trayMenu.close()
		}
		if m.Kind == trayclient.KindDisconnected && r.trayDrawer != nil {
			r.trayDrawer.disconnect()
		}
	}
	changed := r.syncTrayLocked()
	r.mu.Unlock()

	r.publish(changed)
}

// reprojectTray republishes tray state onto every surface without a client
// message behind it. The icon worker and the reply tracker both reach it.
func (r *Registry) reprojectTray() {
	r.mu.Lock()
	changed := r.syncTrayLocked()
	r.mu.Unlock()
	r.publish(changed)
}

// syncTrayLocked pushes the projection onto every bar and the open drawer,
// and reports the outputs whose bars must repaint.
//
// Icons are decoded once per physical size, not once per item: two outputs at
// the same scale share one raster, and an output at another scale gets its
// own rather than an upscaled copy of somebody else's.
func (r *Registry) syncTrayLocked() []uint32 {
	items := r.tray.itemsList()
	bySize := map[int]map[tray.ItemKey]*ui.Image{}
	imagesFor := func(scale120 int) map[tray.ItemKey]*ui.Image {
		size := trayIconPixelSize(scale120)
		if cached, ok := bySize[size]; ok {
			return cached
		}
		built := r.trayImagesLocked(items, size)
		bySize[size] = built
		return built
	}

	changed := make([]uint32, 0, len(r.bars))
	for global, bar := range r.bars {
		bar.setTray(items, r.cfg.Tray, imagesFor(bar.scale120()))
		changed = append(changed, global)
	}
	if r.trayDrawer != nil && r.trayDrawer.open_ {
		if bar, ok := r.bars[r.trayDrawer.output]; ok {
			r.trayDrawer.refresh(bar.trayArrangement(), imagesFor(r.trayDrawer.scale120))
		} else {
			// The drawer's output has gone; its own cleanup closes it.
			r.trayDrawer.outputLost(r.trayDrawer.output)
		}
	}
	return changed
}

// trayIconPixelSize is the raster edge one logical tray slot needs at a scale.
// A zero or invalid scale means the bar has not been configured yet, in which
// case the logical size is the honest answer.
func trayIconPixelSize(scale120 int) int {
	scale := ui.Scale120(scale120)
	if !scale.Valid() {
		return trayItemSize
	}
	return max(scale.Physical(trayItemSize), 1)
}

// trayImagesLocked resolves one raster per item at size. A pixmap the service
// sent is used as-is; a named icon is decoded off this goroutine and appears
// on a later pass, so a slow icon theme never stalls the pump.
func (r *Registry) trayImagesLocked(items []tray.Item, size int) map[tray.ItemKey]*ui.Image {
	images := make(map[tray.ItemKey]*ui.Image, len(items))
	for _, item := range items {
		if image := trayPixmapImage(item, size); image != nil {
			images[item.Key] = image
			continue
		}
		key, ok := trayNamedIconKey(item, size)
		if !ok || r.trayIcons == nil {
			continue
		}
		if image, cached := r.trayIcons.Lookup(key); cached {
			images[item.Key] = image
			continue
		}
		_, _, _ = r.trayIcons.Request(key)
	}
	return images
}

// applyTrayIcon runs on the icon worker. A failed decode publishes nil, which
// leaves the item's box reserved and empty rather than reflowing the bar.
func (r *Registry) applyTrayIcon(_ icons.Key, image *ui.Image) {
	if image == nil {
		return
	}
	r.reprojectTray()
	r.reprojectRunningApps()
	r.mu.Lock()
	h := r.panelHosts[PanelLauncher]
	if h == nil {
		r.mu.Unlock()
		return
	}
	r.rebuildPanel(h)
	out := h.output
	r.mu.Unlock()
	r.publishSurface(out, panelSurfaceID(PanelLauncher))
}

// sendTrayLocked sends one command for a live item and remembers the request
// so its reply can be correlated. A key the projection no longer holds never
// leaves the shell.
func (r *Registry) sendTrayLocked(command tray.Command) (uint64, error) {
	if r.traySender == nil || !r.tray.has(command.Item) {
		return 0, trayclient.ErrBusy
	}
	requestID, err := r.traySender.Send(command)
	if err == nil && r.trayReplies != nil {
		r.trayReplies.note(requestID, command.Item)
	}
	return requestID, err
}

// pendingTrayMenu is one menu.open waiting for the service to publish the
// menu it asked for.
//
// chainGen is the root generation at request time. If the chain has moved on
// when the menu arrives — a panel opened, the drawer closed, another menu took
// it — the request describes a gesture that is over and opens nothing.
type pendingTrayMenu struct {
	active    bool
	child     bool
	requestID uint64
	item      tray.ItemKey
	connector string
	output    uint32
	serial    uint32
	anchor    ui.Rect
	chainGen  uint64
}

func (r *Registry) clearPendingTrayMenuFor(key tray.ItemKey) {
	if r.pendingTrayMenu.active && r.pendingTrayMenu.item == key {
		r.pendingTrayMenu = pendingTrayMenu{}
	}
}

// resumePendingTrayMenuLocked opens the menu a click asked for once it lands.
func (r *Registry) resumePendingTrayMenuLocked(key tray.ItemKey) {
	pending := r.pendingTrayMenu
	if !pending.active || pending.item != key {
		return
	}
	r.pendingTrayMenu = pendingTrayMenu{}
	if r.roots.gen() != pending.chainGen {
		return
	}
	r.openTrayMenuLocked(pending)
}

// handleTrayBar routes one bar gesture. A zero key is the overflow control,
// which toggles the drawer; anything else names an item.
func (r *Registry) handleTrayBar(
	global uint32, connector string, key tray.ItemKey,
	arranged trayArrangement, anchor ui.Rect, event wayland.Event,
) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if key.IsZero() {
		return r.toggleTrayDrawerLocked(global, connector, arranged)
	}
	return r.handleTrayItemLocked(key, connector, global, anchor, false, event)
}

func (r *Registry) toggleTrayDrawerLocked(global uint32, connector string, arranged trayArrangement) bool {
	if r.trayDrawer == nil {
		return false
	}
	if r.trayDrawer.open_ && r.trayDrawer.output == global {
		r.trayDrawer.close()
		return true
	}
	r.trayDrawer.close()
	images := map[tray.ItemKey]*ui.Image{}
	if bar, ok := r.bars[global]; ok {
		images = r.trayImagesLocked(r.tray.itemsList(), trayIconPixelSize(bar.scale120()))
	}
	return r.trayDrawer.open(global, connector, arranged, images)
}

// drawerItemAction is the drawer's activation seam. A menu opened from the
// drawer attaches to the drawer's own root instead of replacing it, so the
// drawer stays up behind its menu.
func (r *Registry) drawerItemAction(item tray.Item, connector string, output uint32, event wayland.Event) bool {
	return r.handleTrayItemLocked(item.Key, connector, output, ui.Rect{}, true, event)
}

// handleTrayItemLocked turns one gesture on an item into one protocol command.
// Left opens a menu when the item declares itself a menu, middle secondary-
// activates, right always opens the menu, and a wheel scrolls.
func (r *Registry) handleTrayItemLocked(
	key tray.ItemKey, connector string, output uint32,
	anchor ui.Rect, fromDrawer bool, event wayland.Event,
) bool {
	item, ok := r.tray.itemFor(key)
	if !ok {
		return false
	}
	r.dwell.leave()

	if event.Kind == wayland.EventPointerAxis {
		_, err := r.sendTrayLocked(tray.Command{
			Kind: tray.CommandScroll, Item: key, Output: output,
			Delta: trayScrollDelta(event), Orientation: tray.ScrollVertical,
		})
		return err == nil
	}

	switch {
	case event.Button == buttonMiddle:
		_, err := r.sendTrayLocked(tray.Command{
			Kind: tray.CommandSecondaryActivate, Item: key, Output: output,
			X: int32(event.X), Y: int32(event.Y),
		})
		return err == nil
	case event.Button == buttonRight || item.ItemIsMenu:
		return r.requestTrayMenuLocked(pendingTrayMenu{
			item: key, connector: connector, output: output,
			serial: event.Serial, anchor: anchor, child: fromDrawer,
		})
	}
	_, err := r.sendTrayLocked(tray.Command{
		Kind: tray.CommandActivate, Item: key, Output: output,
		X: int32(event.X), Y: int32(event.Y),
	})
	return err == nil
}

// trayScrollDelta normalises a wheel event. Discrete steps are the reliable
// signal; the continuous value is the fallback for a touchpad.
func trayScrollDelta(event wayland.Event) int32 {
	if event.AxisDiscrete != 0 {
		return event.AxisDiscrete * 120
	}
	if event.AxisValue120 != 0 {
		return event.AxisValue120
	}
	return int32(event.AxisValue)
}

// requestTrayMenuLocked tells the service the menu is about to show, then
// opens it. A menu the shell already holds opens now; anything else waits for
// the service to publish one.
func (r *Registry) requestTrayMenuLocked(pending pendingTrayMenu) bool {
	requestID, err := r.sendTrayLocked(tray.Command{
		Kind: tray.CommandMenuOpen, Item: pending.item,
		Output: pending.output, Serial: pending.serial,
	})
	if err != nil {
		return false
	}
	if _, ok := r.tray.menuFor(pending.item); ok {
		r.pendingTrayMenu = pendingTrayMenu{}
		return r.openTrayMenuLocked(pending)
	}
	pending.active = true
	pending.requestID = requestID
	pending.chainGen = r.roots.gen()
	r.pendingTrayMenu = pending
	return true
}

func (r *Registry) openTrayMenuLocked(pending pendingTrayMenu) bool {
	if r.trayMenu == nil {
		return false
	}
	// One menu at a time: a second gesture replaces the first rather than
	// stacking two grabs on one output.
	r.trayMenu.close()
	if pending.child {
		return r.trayMenu.openAsChild(pending.item, pending.connector, pending.output, pending.serial)
	}
	return r.trayMenu.openAt(pending.item, pending.connector, pending.output, pending.serial, pending.anchor)
}

// trayOutputLostLocked releases every tray surface bound to one output. It is
// the hotplug path: a second output's menu and drawer keep running.
func (r *Registry) trayOutputLostLocked(global uint32) {
	if r.pendingTrayMenu.active && r.pendingTrayMenu.output == global {
		r.pendingTrayMenu = pendingTrayMenu{}
	}
	if r.trayMenu != nil {
		r.trayMenu.outputLost(global)
	}
	if r.trayDrawer != nil {
		r.trayDrawer.outputLost(global)
	}
}

// closeTrayLocked tears every tray surface down: reload, shutdown, and the
// configuration commit that replaces every bar all reach it.
func (r *Registry) closeTrayLocked() {
	r.pendingTrayMenu = pendingTrayMenu{}
	if r.trayMenu != nil {
		r.trayMenu.close()
	}
	if r.trayDrawer != nil {
		r.trayDrawer.close()
	}
}

// stopTrayIcons ends the icon worker. Called once, from Close.
func (r *Registry) stopTrayIconsLocked() {
	if r.trayIconCancel != nil {
		r.trayIconCancel()
		r.trayIconCancel = nil
	}
}

// DropTrayAux handles a compositor-side close of a tray surface: the
// compositor destroyed it, so the shell releases its root without asking the
// compositor to close it again.
func (r *Registry) DropTrayAux(output uint32, surfaceID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch surfaceID {
	case trayMenuSurfaceID:
		if r.trayMenu != nil && r.trayMenu.open_ && r.trayMenu.output == output {
			r.trayMenu.closed = true // the surface is already gone
			r.trayMenu.close()
		}
		return true
	case trayDrawerSurfaceID:
		if r.trayDrawer != nil && r.trayDrawer.open_ && r.trayDrawer.output == output {
			r.trayDrawer.closed = true
			r.trayDrawer.close()
		}
		return true
	}
	return false
}
