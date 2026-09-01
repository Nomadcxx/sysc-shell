package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/trayclient"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

// menuHostHarness builds a registry with one live tray item that has a menu,
// and a host capturing aux requests.
func menuHostHarness(t *testing.T) (*Registry, *trayMenuHost, tray.ItemKey) {
	t.Helper()
	r := NewRegistry(config.Default())
	key := tray.ItemKey{Owner: "org.x", ObjectPath: "/org/x/1"}
	menu := flatMenu(3)
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindSnapshot,
		Snapshot: tray.Snapshot{Items: []tray.Item{{Key: key, Title: "Chat"}}}})
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: key, Menu: menu}})
	h := newTrayMenuHost(r, &hostHarness{})
	return r, h, key
}

// Opening emits one Overlay aux surface with OnDemand keyboard and saves the
// correlation fields: serial, output, item, revision, root generation.
func TestTrayMenuOpenSavesCorrelation(t *testing.T) {
	r, h, key := menuHostHarness(t)
	if !h.open(key, "eDP-1", 7, 42) {
		t.Fatal("open refused a live item with a menu")
	}
	if h.serial != 42 || h.output != 7 || h.item != key || h.revision != 1 {
		t.Fatalf("correlation = serial %d output %d item %+v revision %d",
			h.serial, h.output, h.item, h.revision)
	}
	if len(h.harness().opens) != 1 {
		t.Fatalf("opens = %d", len(h.harness().opens))
	}
	spec := h.harness().opens[0]
	if spec.Keyboard != keyboardOnDemand {
		t.Fatalf("keyboard = %d, want OnDemand", spec.Keyboard)
	}
	if spec.Layer != 3 { // overlay
		t.Fatalf("layer = %d, want overlay", spec.Layer)
	}
	if _, gen, ok := r.roots.current(); !ok || gen != h.rootGen {
		t.Fatal("the menu did not take the root chain")
	}
	if !r.roots.owns(trayMenuRoot(7)) {
		t.Fatal("the chain owner is not the menu root")
	}
}

// Opening against a dead item or an item with no menu refuses and leaves the
// chain alone.
func TestTrayMenuOpenRefusesADeadItem(t *testing.T) {
	r, h, key := menuHostHarness(t)
	r.applyTray(trayclient.Message{Kind: trayclient.KindDisconnected})
	if h.open(key, "eDP-1", 7, 1) {
		t.Fatal("open succeeded after disconnect")
	}
	if len(h.harness().opens) != 0 {
		t.Fatal("a surface opened for a dead item")
	}
}

// Closing releases the surface, drops the serial, and frees the root chain.
func TestTrayMenuCloseReleasesEverything(t *testing.T) {
	r, h, key := menuHostHarness(t)
	h.open(key, "eDP-1", 7, 42)
	h.close()
	if len(h.harness().closes) != 1 {
		t.Fatalf("closes = %d", len(h.harness().closes))
	}
	if h.serial != 0 || h.open_ {
		t.Fatal("serial or open state survived close")
	}
	if _, _, ok := r.roots.current(); ok {
		t.Fatal("the root chain stayed open after close")
	}
}

// Losing the owning item closes only a menu owned by that item.
func TestTrayMenuItemLossClosesOnlyItsOwnMenu(t *testing.T) {
	_, h, key := menuHostHarness(t)
	h.open(key, "eDP-1", 7, 42)
	h.itemLost(tray.ItemKey{Owner: "other", ObjectPath: "/org/y/9"})
	if !h.open_ {
		t.Fatal("an unrelated item's loss closed the menu")
	}
	h.itemLost(key)
	if h.open_ {
		t.Fatal("the owning item's loss left the menu open")
	}
}

// Output loss closes a menu on that output.
func TestTrayMenuOutputLossCloses(t *testing.T) {
	_, h, key := menuHostHarness(t)
	h.open(key, "eDP-1", 7, 42)
	h.outputLost(99)
	if !h.open_ {
		t.Fatal("an unrelated output's loss closed the menu")
	}
	h.outputLost(7)
	if h.open_ {
		t.Fatal("the menu survived its output going away")
	}
}

// Service disconnect closes the menu: the menu is service state.
func TestTrayMenuServiceLossCloses(t *testing.T) {
	_, h, key := menuHostHarness(t)
	h.open(key, "eDP-1", 7, 42)
	h.disconnect()
	if h.open_ {
		t.Fatal("the menu survived service loss")
	}
}

// A property-only revision — same structure, same IDs — applies immediately
// even while the user is interacting, and keeps focus when the focused entry
// still exists.
func TestTrayMenuPropertyOnlyUpdateKeepsFocus(t *testing.T) {
	r, h, key := menuHostHarness(t)
	h.open(key, "eDP-1", 7, 42)
	h.menu.move(1) // focus row 2
	next := flatMenu(3)
	next.Revision = 2
	next.Root.Children[2].Label = "renamed"
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: key, Menu: next}})
	h.applyMenu()
	if h.revision != 2 {
		t.Fatalf("revision = %d, want 2 applied", h.revision)
	}
	if h.menu.focusedID() != 2 {
		t.Fatalf("focus = %d, want row 2 kept", h.menu.focusedID())
	}
}

// A structural revision while the user is interacting waits; when the user
// goes idle the newest deferred revision replaces the tree and restores
// focus by entry ID.
func TestTrayMenuStructuralUpdateDefersWhileInteracting(t *testing.T) {
	r, h, key := menuHostHarness(t)
	h.open(key, "eDP-1", 7, 42)
	h.menu.move(1) // focus row 2
	h.noteInteraction()

	structural := tray.Menu{Revision: 3, Root: tray.MenuNode{ID: 0, Visible: true,
		Children: []tray.MenuNode{
			{ID: 2, Label: "Item 2", Enabled: true, Visible: true}, // focused row survives
			{ID: 9, Label: "New", Enabled: true, Visible: true},
		}}}
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: key, Menu: structural}})
	h.applyMenu()
	if h.revision != 1 {
		t.Fatalf("revision = %d, want the structural update deferred", h.revision)
	}
	h.idle()
	if h.revision != 3 {
		t.Fatalf("revision = %d after idle, want 3", h.revision)
	}
	if h.menu.focusedID() != 2 {
		t.Fatalf("focus = %d, want restoration by entry ID", h.menu.focusedID())
	}
}

// Only the newest structural revision is kept: two updates while interacting
// leave one tree to apply.
func TestTrayMenuKeepsOnlyTheNewestDeferredRevision(t *testing.T) {
	r, h, key := menuHostHarness(t)
	h.open(key, "eDP-1", 7, 42)
	h.noteInteraction()
	for _, rev := range []uint32{2, 3, 4} {
		r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
			Menu: tray.MenuUpdate{Key: key, Menu: tray.Menu{Revision: rev,
				Root: tray.MenuNode{ID: 0, Visible: true, Children: []tray.MenuNode{
					{ID: int32(rev), Label: "v", Enabled: true, Visible: true}}}}}})
		h.applyMenu()
	}
	h.idle()
	if h.revision != 4 {
		t.Fatalf("revision = %d, want only the newest deferred revision", h.revision)
	}
}

// Selection against an old revision sends nothing, reports stale, and asks
// the service to refresh; the menu stays usable.
func TestTrayMenuStaleSelectionSendsNothing(t *testing.T) {
	r, h, key := menuHostHarness(t)
	snd := &recordSender{}
	h.open(key, "eDP-1", 7, 42)
	// A newer revision arrives structurally while interacting.
	h.noteInteraction()
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: key, Menu: tray.Menu{Revision: 9,
			Root: tray.MenuNode{ID: 0, Visible: true, Children: []tray.MenuNode{
				{ID: 1, Label: "v", Enabled: true, Visible: true}}}}}})
	h.applyMenu()
	stale := h.selectFocused(snd)
	if !stale {
		t.Fatal("a selection against an old revision was not reported stale")
	}
	if len(snd.sent) != 0 {
		t.Fatal("a stale selection reached the service")
	}
	if len(h.refreshAsked) == 0 {
		t.Fatal("no refresh was requested after a stale selection")
	}
	if !h.open_ {
		t.Fatal("a stale selection closed the menu")
	}
}

// A selection against the current revision carries the menu revision and the
// focused entry ID.
func TestTrayMenuSelectCarriesRevisionAndID(t *testing.T) {
	_, h, key := menuHostHarness(t)
	snd := &recordSender{}
	h.open(key, "eDP-1", 7, 42)
	h.menu.move(1)
	if h.selectFocused(snd) {
		t.Fatal("a current selection reported stale")
	}
	c := snd.sent[0]
	if c.Kind != tray.CommandMenuSelect || c.MenuRevision != 1 ||
		c.MenuID != 2 || c.Item != key || c.Serial != 42 {
		t.Fatalf("command = %+v", c)
	}
}

// An unrelated root replacing the chain closes the menu through its cleanup.
func TestTrayMenuRootReplacementCloses(t *testing.T) {
	r, h, key := menuHostHarness(t)
	h.open(key, "eDP-1", 7, 42)
	r.roots.openRoot(panelRoot(1)) // an unrelated root replaces the chain
	if h.open_ {
		t.Fatal("the menu survived its chain being replaced")
	}
	if len(h.harness().closes) != 1 {
		t.Fatal("the replacement never closed the surface")
	}
}

var _ = wayland.AuxRequest{} // keep the import for the harness type
