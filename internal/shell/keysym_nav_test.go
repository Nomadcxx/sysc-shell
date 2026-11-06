package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/services/polkit"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

// A virtual keyboard or another layout can put a symbol on any code: wtype
// sends Tab on code 1, the US Escape key. Panels navigate by the symbol.
func TestPanelHostNavigatesByResolvedSymbol(t *testing.T) {
	for name, id := range map[string]PanelID{"settings": PanelSettings, "control centre": PanelControlCenter} {
		t.Run(name, func(t *testing.T) {
			r := newPanelRegistry(t)
			if err := r.OpenPanel(id, 7, Trigger{}); err != nil {
				t.Fatal(err)
			}
			drainAuxQueue(r)
			r.mu.Lock()
			defer r.mu.Unlock()
			h := r.panelHosts[id]
			if len(h.focus) < 3 {
				t.Fatalf("%s has %d focus stops, need 3", id, len(h.focus))
			}
			start := h.roving.Index()
			if !h.keyInput(r, ui.KeyInput{Code: 1, Sym: ui.SymTab}) || r.panelHosts[id] != h {
				t.Fatal("Tab on code 1 closed the panel")
			}
			if h.roving.Index() == start || !h.focusVisible {
				t.Fatal("Tab on code 1 did not move a visible focus")
			}
			moved := h.roving.Index()
			h.keyInput(r, ui.KeyInput{Code: 23, Sym: ui.SymISOLeftTab})
			if h.roving.Index() == moved {
				t.Fatal("ISO_Left_Tab without a held Shift did not move focus back")
			}
			h.keyInput(r, ui.KeyInput{Code: 1, Sym: 'a', Text: "a"})
			if r.panelHosts[id] != h {
				t.Fatal("a letter on code 1 closed the panel as Escape")
			}
			// Settings types the letter into its search, and Escape clears a
			// query before it closes.
			for i := 0; i < 3 && r.panelHosts[id] == h; i++ {
				h.keyInput(r, ui.KeyInput{Code: 15, Sym: ui.SymEscape})
			}
			if r.panelHosts[id] == h {
				t.Fatal("Escape on code 15 did not close the panel")
			}
		})
	}
}

func TestSurfacesNavigateByResolvedSymbol(t *testing.T) {
	tabOnEsc := wayland.Event{Kind: wayland.EventKeyPress, Key: keyEsc, Sym: ui.SymTab}
	escOnTab := wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab, Sym: ui.SymEscape}

	t.Run("window switcher", func(t *testing.T) {
		reg := newPanelRegistry(t)
		newHosts(t, reg, map[uint32]string{1: "DP-1"})
		reg.niriSend = func(any) error { return nil }
		reg.UpdateNiri(switcherSnapshot([]niri.Window{
			{ID: 40, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 40},
			{ID: 20, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 20},
			{ID: 30, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 30},
		}, false))
		if err := reg.ShowWindowSwitcher(); err != nil {
			t.Fatal(err)
		}
		host := reg.windowSwitcher
		if !host.handleLocking(tabOnEsc) || !host.open_ {
			t.Fatal("Tab on code 1 closed the switcher")
		}
		if got, _ := host.model.selectedWindow(); got.ID != 30 {
			t.Fatalf("Tab on code 1 selected %d, want 30", got.ID)
		}
		host.handleLocking(wayland.Event{Kind: wayland.EventKeyPress, Key: 23, Sym: ui.SymISOLeftTab})
		if got, _ := host.model.selectedWindow(); got.ID != 40 {
			t.Fatalf("ISO_Left_Tab selected %d, want 40", got.ID)
		}
		if !host.handleLocking(escOnTab) || host.open_ {
			t.Fatal("Escape on code 15 did not close the switcher")
		}
	})

	t.Run("tray drawer", func(t *testing.T) {
		r := NewRegistry(config.Default())
		h := newTrayDrawerHost(r, &hostHarness{})
		items := []tray.Item{preferenceItem("a", "A", 1), preferenceItem("b", "B", 1)}
		if !h.open(7, "eDP-1", trayArrangement{Overflow: items}, nil) {
			t.Fatal("open refused")
		}
		if err := h.configure(trayDrawerWidth, 400, int(ui.ScaleUnit)); err != nil {
			t.Fatal(err)
		}
		start := h.roving.Index()
		h.handle(tabOnEsc)
		if !h.open_ || h.roving.Index() == start {
			t.Fatal("Tab on code 1 did not move focus in an open drawer")
		}
		h.handle(escOnTab)
		if h.open_ {
			t.Fatal("Escape on code 15 did not close the drawer")
		}
	})

	t.Run("tray menu", func(t *testing.T) {
		_, h, key := menuHostHarness(t)
		if !h.open(key, "eDP-1", 7, 42) {
			t.Fatal("open refused")
		}
		h.handle(tabOnEsc)
		if !h.open_ {
			t.Fatal("Tab on code 1 closed the tray menu")
		}
		h.handle(escOnTab)
		if h.open_ {
			t.Fatal("Escape on code 15 did not close the tray menu")
		}
	})

	t.Run("running apps menu", func(t *testing.T) {
		_, _, host := openRunningAppsWindowMenu(t, []niri.Window{
			{ID: 80, AppID: "steam", Title: "Older", FocusTimestamp: 10},
			{ID: 81, AppID: "steam", Title: "Newest", FocusTimestamp: 20},
		})
		host.handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEsc, Sym: ui.SymDown})
		if !host.open_ {
			t.Fatal("Down on code 1 closed the menu")
		}
		host.handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyDown, Sym: ui.SymEscape})
		if host.open_ {
			t.Fatal("Escape on code 108 did not close the menu")
		}
	})

	t.Run("media strip", func(t *testing.T) {
		r, strip, _ := stripTestRegistry(t, services.MediaState{Available: true, Title: "Track", CanPause: true})
		r.mu.Lock()
		defer r.mu.Unlock()
		openStripForTest(t, strip, ui.Rect{X: 800, W: 160, H: 32}, true)
		strip.handle(tabOnEsc)
		if !strip.open_ {
			t.Fatal("Tab on code 1 closed the strip")
		}
		strip.handle(escOnTab)
		if strip.open_ {
			t.Fatal("Escape on code 15 did not close the strip")
		}
	})

	t.Run("polkit", func(t *testing.T) {
		h := &polkitHost{open_: true, phase: polkitAwaitingPrompt}
		h.setPromptLocked(polkit.Prompt{Text: "Password:", Secret: true})
		h.handleLocked(tabOnEsc)
		if !h.open_ {
			t.Fatal("Tab on code 1 cancelled authentication")
		}
	})
}
