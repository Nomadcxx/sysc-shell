package shell

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

func TestTrayDrawerIsAVirtualListWithKeyboardAccessibleControls(t *testing.T) {
	arranged := trayArrangement{
		Overflow: []tray.Item{preferenceItem("mail", "Mail", 1)},
		Hidden:   []tray.Item{preferenceItem("chat", "Chat", 1)},
	}
	tree, _ := trayDrawerTree(arranged, nil)
	if tree.Kind != ui.KindVirtualList || tree.ItemCount != 4 {
		t.Fatalf("tree = kind %d count %d, want virtual list with two headings and two items", tree.Kind, tree.ItemCount)
	}
	for i := 0; i < tree.ItemCount; i++ {
		row := tree.Item(i)
		for _, n := range ui.Focusables(row) {
			if n.Name == "" || n.Role == "" || n.Action == "" {
				t.Fatalf("row %d has inaccessible control %+v", i, n)
			}
		}
	}
	hidden := tree.Item(3)
	if action, ok := ui.Hit(hidden, hidden.Bounds.X, hidden.Bounds.Y); ok || action != "" {
		// Bounds are assigned only by layout; this also proves the model does not
		// fake a pointer target before the owner lays it out.
		t.Fatalf("unlaid hidden row unexpectedly hit %q", action)
	}
	if got := ui.Focusables(hidden); len(got) < 2 || got[1].Name != "Show Chat" {
		t.Fatalf("hidden controls = %+v, want recoverable Show action", got)
	}
}

func TestTrayDrawerPinnedItemOffersUnpin(t *testing.T) {
	item := preferenceItem("mail", "Mail", 1)
	tree, _ := trayDrawerTree(trayArrangement{
		Overflow: []tray.Item{item}, Pinned: map[tray.ItemKey]bool{item.Key: true},
	}, nil)
	controls := ui.Focusables(tree.Item(1))
	if len(controls) < 3 || controls[2].Name != "Unpin Mail" || controls[2].Action != "tray-pref:unpin:id:mail" {
		t.Fatalf("pin control = %+v", controls)
	}
}

func TestTrayDrawerItemRowsRouteTheirDistinctGenerationSafeIdentity(t *testing.T) {
	r := NewRegistry(config.Default())
	h := newTrayDrawerHost(r, &hostHarness{})
	first := preferenceItem("mail", "Mail", 1)
	second := preferenceItem("chat", "Chat", 2)
	var got []tray.ItemKey
	h.itemAction = func(item tray.Item, connector string, output uint32, event wayland.Event) bool {
		if connector != "eDP-1" || output != 7 || event.Serial != 42 {
			t.Fatalf("correlation = %q/%d/%d", connector, output, event.Serial)
		}
		got = append(got, item.Key)
		return true
	}
	h.open(7, "eDP-1", trayArrangement{Overflow: []tray.Item{first, second}}, nil)
	release := wayland.Event{Kind: wayland.EventPointerRelease, Serial: 42}
	if !h.activate(h.focus[0], release) || !h.activate(h.focus[5], release) {
		t.Fatal("item activation was not routed")
	}
	if len(got) != 2 || got[0] != first.Key || got[1] != second.Key {
		t.Fatalf("routed keys = %+v", got)
	}
}

func TestTrayDrawerOwnsOneRootAndClosesExactlyOnce(t *testing.T) {
	r := NewRegistry(config.Default())
	hh := &hostHarness{}
	h := newTrayDrawerHost(r, hh)
	if !h.open(7, "eDP-1", trayArrangement{}, nil) {
		t.Fatal("open refused")
	}
	if !r.roots.owns(trayDrawerRoot(7)) || len(hh.opens) != 1 {
		t.Fatalf("root/open = %+v/%d", r.roots, len(hh.opens))
	}
	if hh.opens[0].Keyboard == 0 {
		t.Fatal("drawer did not request keyboard on demand")
	}
	h.close()
	h.close()
	if len(hh.closes) != 1 {
		t.Fatalf("close requests = %d, want one", len(hh.closes))
	}
}

func TestTrayDrawerLayoutAndFocusShareRetainedRows(t *testing.T) {
	r := NewRegistry(config.Default())
	h := newTrayDrawerHost(r, &hostHarness{})
	h.open(7, "eDP-1", trayArrangement{
		Overflow: []tray.Item{preferenceItem("mail", "Mail", 1)},
	}, nil)
	if err := h.configure(trayDrawerWidth, trayDrawerHeight, int(ui.ScaleUnit)); err != nil {
		t.Fatal(err)
	}
	if len(h.focus) == 0 || h.focus[0].Bounds.W == 0 || h.focus[0].Bounds.H == 0 {
		t.Fatalf("focused row was not the laid-out row: %+v", h.focus)
	}
}

func TestTrayDrawerMenuAttachesToItsRoot(t *testing.T) {
	r := NewRegistry(config.Default())
	h := newTrayDrawerHost(r, &hostHarness{})
	if !h.open(7, "eDP-1", trayArrangement{}, nil) {
		t.Fatal("open refused")
	}
	if !r.roots.attach(h.rootGen, trayMenuRoot(7)) {
		t.Fatal("menu did not attach to drawer root")
	}
	if child, ok := r.roots.currentChild(); !ok || child != trayMenuRoot(7) {
		t.Fatalf("child = %+v, %v", child, ok)
	}
}

func TestTrayDrawerScrollAndFocusMovementRelayoutVisibleRows(t *testing.T) {
	r := NewRegistry(config.Default())
	h := newTrayDrawerHost(r, &hostHarness{})
	items := make([]tray.Item, 20)
	for i := range items {
		items[i] = preferenceItem(string(rune('a'+i)), "Item", 1)
	}
	h.open(7, "eDP-1", trayArrangement{Overflow: items}, nil)
	if err := h.configure(trayDrawerWidth, 120, int(ui.ScaleUnit)); err != nil {
		t.Fatal(err)
	}
	before := h.root.Children[0]
	if !h.handle(wayland.Event{Kind: wayland.EventPointerAxis, AxisDiscrete: 3}) {
		t.Fatal("wheel was not handled")
	}
	if h.root.ScrollOffset == 0 || h.root.Children[0] == before {
		t.Fatalf("wheel did not relayout virtual rows: offset=%d", h.root.ScrollOffset)
	}
	h.roving.Set(len(h.focus) - 1)
	h.ensureFocusVisible()
	if h.root.ScrollOffset == 0 {
		t.Fatal("focused last control was not scrolled into view")
	}
}

func TestTrayDrawerEmitsPreferenceCollisionDiagnostic(t *testing.T) {
	r := NewRegistry(config.Default())
	h := newTrayDrawerHost(r, &hostHarness{})
	var got []string
	h.diagnostic = func(message string) { got = append(got, message) }
	h.open(7, "eDP-1", trayArrangement{Collisions: []string{"id:shared"}}, nil)
	if len(got) != 1 || !strings.Contains(got[0], "id:shared") {
		t.Fatalf("diagnostics = %v", got)
	}
}
