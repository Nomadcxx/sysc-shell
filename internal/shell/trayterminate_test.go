package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/trayclient"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

// terminateHost opens a menu for an item the service says it can close.
func terminateHost(t *testing.T, supported bool) (*trayMenuHost, *recordSender) {
	t.Helper()
	r := NewRegistry(config.Default())
	key := tray.ItemKey{Owner: "org.x", ObjectPath: "/org/x/1"}
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindSnapshot,
		Snapshot: tray.Snapshot{Items: []tray.Item{{Key: key, Title: "Chat", CloseSupported: supported}}}})
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: key, Menu: tray.Menu{Revision: 1, Root: tray.MenuNode{
			ID: 0, Visible: true, Children: []tray.MenuNode{
				{ID: 1, Label: "Open", Visible: true, Enabled: true},
				{ID: 2, Label: "Settings", Visible: true, Enabled: true},
			}}}}})
	h := newTrayMenuHost(r, &hostHarness{})
	if !h.open(key, "eDP-1", 7, 42) {
		t.Fatal("open refused a live item with a menu")
	}
	return h, &recordSender{}
}

func closeMenu(t *testing.T, supported bool) *trayMenu {
	t.Helper()
	m := newTrayMenu(tray.Menu{Revision: 3, Root: tray.MenuNode{Children: []tray.MenuNode{
		{ID: 1, Label: "Open", Visible: true, Enabled: true},
		{ID: 2, Label: "Settings", Visible: true, Enabled: true},
	}}})
	m.closeRow = supported
	return m
}

// The reserved id cannot be -1. focusedID already returns -1 for "nothing here
// is focusable", and activateFocused gates on id >= 0, so a Close row numbered
// -1 would be indistinguishable from an empty menu and could never activate.
func TestReservedCloseIDDoesNotCollideWithTheNoFocusSentinel(t *testing.T) {
	t.Parallel()
	if trayCloseMenuID == -1 {
		t.Fatal("the reserved Close id is the same value focusedID returns for no focus")
	}
	if trayCloseMenuID >= 0 {
		t.Fatalf("reserved Close id %d is in the range a real menu node can use", trayCloseMenuID)
	}
}

// A service that established a same-UID owner advertises CloseSupported, and
// only then does the shell offer to close the application.
func TestCloseRowAppearsOnlyWhenTheServiceSupportsIt(t *testing.T) {
	t.Parallel()
	with := closeMenu(t, true)
	if got := with.len(); got != 3 {
		t.Fatalf("rows with close support = %d, want the two menu rows plus Close", got)
	}
	last := with.row(with.len() - 1)
	if last.id != trayCloseMenuID || last.name != "Close" {
		t.Fatalf("last row = %+v, want the reserved Close row", last)
	}

	without := closeMenu(t, false)
	if got := without.len(); got != 2 {
		t.Fatalf("rows without close support = %d, want only the menu's own", got)
	}
	for i := range without.len() {
		if without.row(i).id == trayCloseMenuID {
			t.Fatal("a Close row was offered for an item the service cannot close")
		}
	}
}

// The reserved row takes focus and activates like any other, because a row
// that cannot be reached by keyboard is not a row.
func TestCloseRowFocusesAndActivates(t *testing.T) {
	t.Parallel()
	m := closeMenu(t, true)
	m.move(1)
	m.move(1) // onto Close
	id, ok := m.activateFocused()
	if !ok || id != trayCloseMenuID {
		t.Fatalf("activate on the Close row = (%d, %v), want the reserved id", id, ok)
	}
}

// The reserved row is shell-owned: it is never sent to the application as a
// menu selection, which would address a node the application never published.
func TestSelectingCloseSendsTerminateAndNeverMenuSelect(t *testing.T) {
	t.Parallel()
	h, sender := terminateHost(t, true)
	h.menu.move(1)
	h.menu.move(1)
	if stale := h.selectFocused(sender); stale {
		t.Fatal("selecting Close reported stale")
	}
	if len(sender.sent) != 1 {
		t.Fatalf("commands = %+v, want exactly one", sender.sent)
	}
	cmd := sender.sent[0]
	if cmd.Kind != tray.CommandTerminate {
		t.Fatalf("command kind = %q, want terminate", cmd.Kind)
	}
	if cmd.Item != h.item {
		t.Fatalf("terminate addressed %+v, want the live key %+v", cmd.Item, h.item)
	}
	if cmd.MenuID != 0 || cmd.MenuRevision != 0 {
		t.Fatalf("terminate carried menu fields %+v; it addresses the item, not a node", cmd)
	}
}

// An ordinary row still goes to the application untouched.
func TestSelectingAnOrdinaryRowStillSendsMenuSelect(t *testing.T) {
	t.Parallel()
	h, sender := terminateHost(t, true)
	// Focus already sits on the first focusable row after open.
	if stale := h.selectFocused(sender); stale {
		t.Fatal("selecting a menu row reported stale")
	}
	if len(sender.sent) != 1 || sender.sent[0].Kind != tray.CommandMenuSelect {
		t.Fatalf("commands = %+v, want one menu.select", sender.sent)
	}
	if sender.sent[0].MenuID != 1 {
		t.Fatalf("menu.select addressed id %d, want 1", sender.sent[0].MenuID)
	}
}

// Termination is finished when the item goes, not when the service accepts
// the request: the process is signalled and given a window, and it may refuse.
func TestPendingCloseSurvivesTheReplyAndEndsWithTheRemoval(t *testing.T) {
	t.Parallel()
	key := tray.ItemKey{Owner: "org.x", ObjectPath: "/org/x/1", Generation: 4}
	tracker := newTrayCloseTracker()
	tracker.begin(key)
	if !tracker.pendingFor(key) {
		t.Fatal("a sent terminate left nothing pending")
	}
	// An accepted request is not a closed application.
	if tracker.count() != 1 {
		t.Fatalf("pending = %d after acceptance, want the request still open", tracker.count())
	}
	tracker.settle(key)
	if tracker.pendingFor(key) {
		t.Fatal("the removal left the request pending")
	}
}

// A refusal or a timeout clears the request and leaves the item alone: the row
// stays, because the application is still running.
func TestRefusedCloseClearsPendingAndKeepsTheItem(t *testing.T) {
	t.Parallel()
	key := tray.ItemKey{Owner: "org.x", ObjectPath: "/org/x/1", Generation: 4}
	tracker := newTrayCloseTracker()
	tracker.begin(key)
	tracker.settle(key) // busy, or any failure
	if tracker.pendingFor(key) {
		t.Fatal("a refused terminate stayed pending; the row would never recover")
	}
}

// A generation is one connection's numbering. A terminate sent before a
// reconnect must not be matched against an item the new connection published,
// even when the owner and path are reused.
func TestPendingCloseIsPerGenerationAndClearedOnDisconnect(t *testing.T) {
	t.Parallel()
	old := tray.ItemKey{Owner: "org.x", ObjectPath: "/org/x/1", Generation: 1}
	fresh := tray.ItemKey{Owner: "org.x", ObjectPath: "/org/x/1", Generation: 2}
	tracker := newTrayCloseTracker()
	tracker.begin(old)
	if tracker.pendingFor(fresh) {
		t.Fatal("a pending close matched a different generation of the same address")
	}
	tracker.reset()
	if tracker.count() != 0 {
		t.Fatalf("pending = %d after a disconnect, want none", tracker.count())
	}
}

// End to end through the real message path: the Close row sends terminate, the
// request stays pending while the service works, and only the item's removal
// settles it.
func TestCloseRowPendingClearsOnlyOnRemoval(t *testing.T) {
	t.Parallel()
	h, sender := terminateHost(t, true)
	h.menu.move(1)
	h.menu.move(1)
	h.selectFocused(sender)
	if !h.r.trayCloses.pendingFor(h.item) {
		t.Fatal("no pending close after the Close row was chosen")
	}

	// The service accepts; the application has not gone yet.
	h.r.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindReply,
		RequestID: 1, Reply: tray.Reply{OK: true}})
	if !h.r.trayCloses.pendingFor(h.item) {
		t.Fatal("acceptance settled the close; the application may still be running")
	}

	h.r.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindItemRemoved,
		Removed: tray.ItemRemoved{Key: h.item}})
	if h.r.trayCloses.pendingFor(h.item) {
		t.Fatal("the removal did not settle the close")
	}
}

// A disconnect invalidates every key, so nothing may stay pending across one.
func TestDisconnectClearsPendingCloses(t *testing.T) {
	t.Parallel()
	h, sender := terminateHost(t, true)
	h.menu.move(1)
	h.menu.move(1)
	h.selectFocused(sender)
	h.r.ApplyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindDisconnected})
	if h.r.trayCloses.count() != 0 {
		t.Fatalf("pending = %d after a disconnect, want none", h.r.trayCloses.count())
	}
}
