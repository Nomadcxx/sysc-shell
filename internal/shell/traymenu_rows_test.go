package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/trayclient"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

func trayRowsHarness(t *testing.T, menu tray.Menu) *trayMenuHost {
	t.Helper()
	r := NewRegistry(config.Default())
	keepInvalidationsDrained(t, r)
	key := tray.ItemKey{Owner: "org.x", ObjectPath: "/org/x/1"}
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindSnapshot,
		Snapshot: tray.Snapshot{Items: []tray.Item{{Key: key, Title: "Chat"}}}})
	r.applyTray(trayclient.Message{Generation: 1, Kind: trayclient.KindMenuUpdated,
		Menu: tray.MenuUpdate{Key: key, Menu: menu}})
	h := newTrayMenuHost(r, &hostHarness{})
	if !h.open(key, "eDP-1", 7, 42) {
		t.Fatal("open refused")
	}
	w, height := h.size()
	if err := h.configure(w, height, int(ui.ScaleUnit)); err != nil {
		t.Fatal(err)
	}
	return h
}

// Tray rows share the menu geometry: the density's row, thin separators,
// state as glyphs, and nothing focusable that a press cannot take.
func TestTrayMenuRowsUseMenuGeometry(t *testing.T) {
	menu := tray.Menu{Revision: 1, Root: tray.MenuNode{ID: 0, Visible: true, Children: []tray.MenuNode{
		{ID: 1, Label: "Mute", Enabled: true, Visible: true, ToggleType: tray.ToggleCheckmark, ToggleState: 1},
		{ID: 2, Separator: true, Visible: true},
		{ID: 3, Label: "Away", Enabled: true, Visible: true, ToggleType: tray.ToggleRadio},
		{ID: 4, Label: "More", Enabled: true, Visible: true, ChildrenDisplay: "submenu",
			Children: []tray.MenuNode{{ID: 5, Label: "Deep", Enabled: true, Visible: true}}},
		{ID: 6, Label: "Gone", Enabled: false, Visible: true},
	}}}
	h := trayRowsHarness(t, menu)
	row := h.r.surfaceTheme().Metrics.MenuItem
	if h.root.ItemHeight != row || h.root.Padding != ui.MenuSheetPadding {
		t.Fatalf("list pitch %d padding %d, want %d and %d", h.root.ItemHeight, h.root.Padding, row, ui.MenuSheetPadding)
	}
	if got := h.root.ItemExtent(1); got != ui.MenuDivider {
		t.Fatalf("separator extent = %d, want %d", got, ui.MenuDivider)
	}
	if sep := h.root.Item(1); !isMenuDivider(sep) {
		t.Fatalf("separator = %+v, want a horizontal menu divider", sep)
	}
	if _, height := h.size(); height != 4*row+ui.MenuDivider+2*ui.MenuSheetPadding {
		t.Fatalf("surface height = %d", height)
	}
	byName := map[string]*ui.Node{}
	for _, n := range h.focus {
		byName[n.Name] = n
	}
	mute, away, more := byName["Mute (checked)"], byName["Away"], byName["More (submenu)"]
	if mute == nil || away == nil || more == nil || byName["Gone"] != nil {
		t.Fatalf("focus stops = %v", byName)
	}
	if lead := mute.Children[0].Children[0]; lead.Kind != ui.KindIcon || lead.Icon != "check" {
		t.Fatalf("checked row leads with %+v, want a check", lead)
	}
	if lead := away.Children[0].Children[0]; lead.Kind != ui.KindIcon || lead.Icon != "" {
		t.Fatalf("unchecked radio leads with %+v, want an empty slot keeping labels aligned", lead)
	}
	if text := mute.Children[0].Children[1]; text.Text != "Mute" {
		t.Fatalf("label = %q, want the bare label", text.Text)
	}
	content := more.Children[0]
	if !content.PinEnd || content.Children[1].Icon != "chevron_right" {
		t.Fatalf("submenu row %+v, want a trailing chevron", content)
	}
}

// Keyboard travel past the visible rows scrolls the list, and the focused row
// is where the pointer finds it.
func TestTrayMenuKeyboardRevealsTheLastRow(t *testing.T) {
	h := trayRowsHarness(t, flatMenu(20))
	for range 19 {
		h.handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyDown, Sym: ui.SymDown})
	}
	n := h.focus[h.roving.Index()]
	if n.Name != "Item 20" {
		t.Fatalf("focused %q, want Item 20", n.Name)
	}
	if n.Bounds.H <= 0 || n.Bounds.Y < 0 || n.Bounds.Y+n.Bounds.H > h.logicalH {
		t.Fatalf("Item 20 at %+v is outside the %dpx surface", n.Bounds, h.logicalH)
	}
	if hit := h.hitFocusable(n.Bounds.X+4, n.Bounds.Y+n.Bounds.H/2); hit != n {
		t.Fatalf("pointer at the focused row found %+v", hit)
	}
}

// menuRowLabel is a ui.MenuRowNode row's label.
func menuRowLabel(row *ui.Node) *ui.Node {
	start := row.Children[0]
	if start.PinEnd {
		start = start.Children[0]
	}
	return start.Children[len(start.Children)-1]
}

// isMenuDivider reports a menu divider: a separator in its padded column.
func isMenuDivider(n *ui.Node) bool {
	return n.Kind == ui.KindColumn && len(n.Children) == 1 && n.Children[0].Kind == ui.KindSeparator
}

// Menu hosts lay out in logical pixels, so text measures the same at any
// output scale; a physical measure made 28px rows overflow at 125%.
func TestMenuHostsMeasureTextInLogicalPixels(t *testing.T) {
	h := trayRowsHarness(t, flatMenu(3))
	w, height := h.size()
	_, _, host := openRunningAppsWindowMenu(t, []niri.Window{{ID: 80, AppID: "steam", Title: "Steam", FocusTimestamp: 10}})
	measures := map[string]func(int) (int, int){
		"tray": func(scale int) (int, int) {
			if err := h.render(make([]byte, w*scale/120*height*scale/120*4), w*scale/120, height*scale/120, w*scale/120*4); err != nil {
				t.Fatal(err)
			}
			if err := h.configure(w, height, scale); err != nil {
				t.Fatal(err)
			}
			return h.measure()("Show window", ui.TextAttrs{})
		},
		"running": func(scale int) (int, int) {
			rw, rh := host.size()
			if err := host.configure(rw, rh, scale); err != nil {
				t.Fatal(err)
			}
			return host.measureText("Show window", ui.TextAttrs{})
		},
	}
	for name, measure := range measures {
		w1, h1 := measure(120)
		w2, h2 := measure(150)
		if abs(w1-w2) > 1 || abs(h1-h2) > 1 {
			t.Errorf("%s measures %dx%d at 100%% and %dx%d at 125%%", name, w1, h1, w2, h2)
		}
	}
}

// A label that fits the menu's measured width is not cut at 125%: truncation
// takes the same logical width the layout gives the row.
func TestRunningAppMenuKeepsFittingLabelsWhole(t *testing.T) {
	_, _, host := openRunningAppsWindowMenu(t, []niri.Window{
		{ID: 80, AppID: "steam", Title: "Friends List", FocusTimestamp: 10},
	})
	text, err := host.r.textRenderer(host.fontFamily)
	if err != nil {
		t.Fatal(err)
	}
	host.text = text
	w, height := host.size()
	if err := host.configure(w, height, 150); err != nil {
		t.Fatal(err)
	}
	if got := findAction(host.root, "running-app-floating:80"); got == nil || menuRowLabel(got).Text != "Move Friends List to floating" {
		t.Fatalf("placement row = %+v, want its whole label", got)
	}
}
