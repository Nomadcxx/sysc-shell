package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/trayclient"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

// wiringHarness binds the tray with a recording sender and one live item that
// has a menu. The hosts capture aux requests instead of reaching a compositor.
func wiringHarness(t *testing.T) (*Registry, *recordSender, tray.ItemKey) {
	t.Helper()
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	sender := &recordSender{}
	r.BindTray(sender)
	r.trayMenu = newTrayMenuHost(r, &hostHarness{})
	r.trayDrawer = newTrayDrawerHost(r, &hostHarness{})
	r.trayDrawer.itemAction = r.drawerItemAction

	key := tray.ItemKey{Owner: "org.x", ObjectPath: "/org/x/1"}
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindSnapshot,
		Snapshot: tray.Snapshot{Items: []tray.Item{{Key: key, ID: "mail", Title: "Mail"}}}})
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: key, Menu: flatMenu(3)}})
	return r, sender, key
}

func liveItem(r *Registry, key tray.ItemKey) tray.Item {
	item, _ := r.tray.itemFor(key)
	return item
}

// A menu opened from the drawer hangs off the drawer's root as its child, so
// the drawer keeps the chain and stays on screen behind its menu.
func TestTrayMenuFromDrawerAttachesToTheDrawerRoot(t *testing.T) {
	r, _, key := wiringHarness(t)
	item := liveItem(r, key)

	r.mu.Lock()
	if !r.trayDrawer.open(7, "eDP-1", trayArrangement{Overflow: []tray.Item{item}}, nil) {
		r.mu.Unlock()
		t.Fatal("the drawer refused to open")
	}
	drawerGen := r.trayDrawer.rootGen
	r.mu.Unlock()

	if !r.drawerItemAction(item, "eDP-1", 7,
		wayland.Event{Kind: wayland.EventPointerRelease, Button: buttonRight, Serial: 9}) {
		t.Fatal("a right click on a drawer row opened no menu")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.trayMenu.open_ || !r.trayMenu.child {
		t.Fatalf("menu open=%v child=%v, want an attached child", r.trayMenu.open_, r.trayMenu.child)
	}
	if !r.trayDrawer.open_ {
		t.Fatal("the drawer closed when its own menu opened")
	}
	if !r.roots.owns(trayDrawerRoot(7)) {
		t.Fatal("the menu replaced the drawer's root instead of attaching to it")
	}
	if child, ok := r.roots.currentChild(); !ok || child != trayMenuRoot(7) {
		t.Fatalf("chain child = %+v (present %v)", child, ok)
	}
	if r.trayMenu.rootGen != drawerGen {
		t.Fatal("the child menu took a generation of its own")
	}
}

// Closing a drawer-attached menu releases only the child. The drawer keeps
// the chain, so Escape in a menu returns to the drawer rather than dismissing
// both.
func TestTrayMenuFromDrawerClosesOnlyItself(t *testing.T) {
	r, _, key := wiringHarness(t)
	item := liveItem(r, key)

	r.mu.Lock()
	r.trayDrawer.open(7, "eDP-1", trayArrangement{Overflow: []tray.Item{item}}, nil)
	r.mu.Unlock()
	r.drawerItemAction(item, "eDP-1", 7,
		wayland.Event{Kind: wayland.EventPointerRelease, Button: buttonRight, Serial: 9})

	r.mu.Lock()
	defer r.mu.Unlock()
	r.trayMenu.close()
	if r.trayMenu.open_ {
		t.Fatal("the menu stayed open")
	}
	if !r.trayDrawer.open_ || !r.roots.owns(trayDrawerRoot(7)) {
		t.Fatal("closing the child menu took the drawer down with it")
	}
	if _, ok := r.roots.currentChild(); ok {
		t.Fatal("the chain still carries a child")
	}
}

// A menu opened from a bar owns a fresh root, replacing whatever held the
// chain before it.
func TestTrayMenuFromBarOwnsAFreshRoot(t *testing.T) {
	r, _, key := wiringHarness(t)

	r.mu.Lock()
	r.trayDrawer.open(7, "eDP-1", trayArrangement{}, nil)
	opened := r.requestTrayMenuLocked(pendingTrayMenu{
		item: key, connector: "eDP-1", output: 7, serial: 11,
		anchor: ui.Rect{X: 100, Y: 4, W: 24, H: 24},
	})
	defer r.mu.Unlock()
	if !opened || !r.trayMenu.open_ {
		t.Fatal("the bar menu did not open")
	}
	if r.trayMenu.child {
		t.Fatal("a bar menu attached itself to another root")
	}
	if !r.roots.owns(trayMenuRoot(7)) {
		t.Fatal("the bar menu did not take the chain")
	}
	if r.trayDrawer.open_ {
		t.Fatal("the drawer survived an unrelated root replacing the chain")
	}
}

// The surface is placed under the icon that opened it, so the menu appears
// where the user clicked rather than at the output origin.
func TestTrayMenuIsPlacedUnderItsIcon(t *testing.T) {
	r, _, key := wiringHarness(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trayMenu.openAt(key, "eDP-1", 7, 11, ui.Rect{X: 1600, Y: 4, W: 24, H: 24})

	opens := r.trayMenu.harness().opens
	if len(opens) != 1 {
		t.Fatalf("opens = %d", len(opens))
	}
	spec := opens[0]
	if spec.MarginLeft != 1600 || spec.MarginTop != 28 {
		t.Fatalf("margins = left %d top %d, want the icon's left and bottom edges",
			spec.MarginLeft, spec.MarginTop)
	}
	if spec.Width != trayMenuWidth || spec.Height <= 0 {
		t.Fatalf("size = %dx%d, want a menu-sized surface", spec.Width, spec.Height)
	}
	if spec.Callbacks.Configure == nil || spec.Callbacks.Render == nil || spec.Callbacks.Handle == nil {
		t.Fatal("the menu surface carries no callbacks")
	}
}

// The surface is tall enough for the deepest level, because a layer surface
// cannot resize when the user enters a submenu.
func TestTrayMenuSurfaceHoldsTheWidestLevel(t *testing.T) {
	parent := tray.MenuNode{ID: 1, Label: "More", Enabled: true, Visible: true,
		ChildrenDisplay: "submenu"}
	for i := range 6 {
		parent.Children = append(parent.Children, tray.MenuNode{
			ID: int32(10 + i), Label: "Deep", Enabled: true, Visible: true})
	}
	menu := newTrayMenu(tray.Menu{Revision: 1,
		Root: tray.MenuNode{ID: 0, Visible: true, Children: []tray.MenuNode{parent}}})
	if got := menu.len(); got != 1 {
		t.Fatalf("root level rows = %d", got)
	}
	if got := menu.widestLevel(); got != 6 {
		t.Fatalf("widest level = %d, want the submenu's 6 rows", got)
	}
}

// Opening a menu closes any tooltip first: the menu takes the grab, and a
// tooltip left up would outlive the hover that produced it.
func TestTrayMenuOpenClosesTheTooltip(t *testing.T) {
	r, _, key := wiringHarness(t)
	r.dwell.enter(7, ui.Rect{W: 10, H: 10}, "Mail", wayland.TooltipStyle{})

	r.mu.Lock()
	r.trayMenu.openAt(key, "eDP-1", 7, 11, ui.Rect{})
	r.mu.Unlock()

	select {
	case req := <-r.dwell.requests():
		if req.Text != "" {
			t.Fatalf("dwell request = %+v, want a hide", req)
		}
	default:
		// A dwell that had not yet fired is cancelled rather than hidden; the
		// requirement is only that no tooltip survives the menu.
	}
	if r.dwell.shown {
		t.Fatal("a tooltip is still shown behind the menu")
	}
}

// Selecting an entry sends menu.select for the visible revision and closes
// the menu; the service is told the menu is gone.
func TestTrayMenuSelectionSendsAndCloses(t *testing.T) {
	r, sender, key := wiringHarness(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trayMenu.openAt(key, "eDP-1", 7, 11, ui.Rect{})
	if !r.trayMenu.activateFocused() {
		t.Fatal("activating the focused entry did nothing")
	}
	kinds := make([]tray.CommandKind, 0, len(sender.sent))
	for _, command := range sender.sent {
		kinds = append(kinds, command.Kind)
	}
	if len(kinds) != 2 || kinds[0] != tray.CommandMenuSelect || kinds[1] != tray.CommandMenuClose {
		t.Fatalf("commands = %v, want a select then a close", kinds)
	}
	if r.trayMenu.open_ {
		t.Fatal("the menu stayed open after a selection")
	}
}

// A selection against a superseded revision sends no select and asks the
// service to republish the menu instead. about-to-show is that request.
func TestTrayMenuStaleSelectionAsksForARefresh(t *testing.T) {
	r, sender, key := wiringHarness(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trayMenu.openAt(key, "eDP-1", 7, 11, ui.Rect{})
	r.trayMenu.noteInteraction()
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: key, Menu: tray.Menu{Revision: 9,
			Root: tray.MenuNode{ID: 0, Visible: true, Children: []tray.MenuNode{
				{ID: 77, Label: "New", Enabled: true, Visible: true}}}}}})
	r.trayMenu.applyMenu()

	if !r.trayMenu.activateFocused() {
		t.Fatal("a stale selection reported no action at all")
	}
	for _, command := range sender.sent {
		if command.Kind == tray.CommandMenuSelect {
			t.Fatal("a stale selection reached the service")
		}
	}
	if len(sender.sent) != 1 || sender.sent[0].Kind != tray.CommandAboutToShow {
		t.Fatalf("commands = %+v, want one about-to-show", sender.sent)
	}
	if !r.trayMenu.open_ {
		t.Fatal("a stale selection closed the menu")
	}
}

// Icons are decoded per physical size, so an output at scale 2 asks for a
// 48-pixel raster rather than upscaling a 24-pixel one.
func TestTrayIconSizeFollowsTheOutputScale(t *testing.T) {
	if got := trayIconPixelSize(int(ui.ScaleUnit)); got != trayItemSize {
		t.Fatalf("scale 1 size = %d, want %d", got, trayItemSize)
	}
	if got := trayIconPixelSize(240); got != 2*trayItemSize {
		t.Fatalf("scale 2 size = %d, want %d", got, 2*trayItemSize)
	}
	if got := trayIconPixelSize(0); got != trayItemSize {
		t.Fatalf("unconfigured scale = %d, want the logical size", got)
	}
	item := tray.Item{Icon: tray.Icon{Name: "mail"}}
	small, ok := trayNamedIconKey(item, trayItemSize)
	if !ok {
		t.Fatal("a named icon produced no key")
	}
	large, _ := trayNamedIconKey(item, 2*trayItemSize)
	if small == large {
		t.Fatal("two scales share one cache key, so one would upscale the other")
	}
}

// An open drawer follows tray state: an item that goes away leaves its row,
// and the surface keeps running.
func TestTrayDrawerFollowsTrayState(t *testing.T) {
	r, _, key := wiringHarness(t)
	item := liveItem(r, key)
	second := preferenceItem("chat", "Chat", 1)

	r.mu.Lock()
	r.trayDrawer.open(7, "eDP-1", trayArrangement{Overflow: []tray.Item{item, second}}, nil)
	before := r.trayDrawer.root.ItemCount
	r.trayDrawer.refresh(trayArrangement{Overflow: []tray.Item{second}}, nil)
	after := r.trayDrawer.root.ItemCount
	open := r.trayDrawer.open_
	r.mu.Unlock()

	if before != 3 || after != 2 {
		t.Fatalf("rows before %d after %d, want one heading plus its items", before, after)
	}
	if !open {
		t.Fatal("refreshing closed the drawer")
	}
}

// Rows carry the raster the registry resolved, so a drawer row shows the same
// icon the bar does rather than an empty box.
func TestTrayDrawerRowsCarryResolvedImages(t *testing.T) {
	item := preferenceItem("mail", "Mail", 1)
	image := &ui.Image{Width: 24, Height: 24, Stride: 96, Pix: make([]byte, 24*96)}
	tree, _ := trayDrawerTree(trayArrangement{Overflow: []tray.Item{item}},
		map[tray.ItemKey]*ui.Image{item.Key: image})
	row := tree.Item(1)
	icon := ui.Focusables(row)[0]
	if icon.Image != image {
		t.Fatalf("row icon = %+v, want the resolved raster", icon.Image)
	}
}
