package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-shell/internal/config"
)

func launcherDisplayPNGEntries() []launcher.Entry {
	entries := make([]launcher.Entry, 130)
	for i := range entries {
		name := fmt.Sprintf("Application %03d", i)
		if i%4 == 0 {
			name += " with a longer display name"
		}
		entries[i] = launcher.Entry{
			ID:      fmt.Sprintf("org.example.app-%03d.desktop", i),
			Name:    name,
			Comment: fmt.Sprintf("Application comment %03d", i),
			Argv:    []string{"true"},
		}
	}
	for i := 0; i < 10; i++ {
		entries[i].Comment = ""
	}
	entries[6].Name = strings.Repeat("W", 60)
	for _, i := range []int{2, 6, 9} {
		entries[i].Actions = []launcher.Action{{ID: "new-window", Name: "New Window", Argv: []string{"true", "--new-window"}}}
	}
	return entries
}

// TestLauncherDisplayRendersToPNG keeps the visual gate beside the renderer:
// every state is configured at its AuxSpec size and painted at desktop/laptop scales.
func TestLauncherDisplayRendersToPNG(t *testing.T) {
	dir := os.Getenv("SYSC_LAUNCHER_PNG_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	entries := launcherDisplayPNGEntries()
	outputs := []int{120, 150}
	states := []struct {
		name string
		mode string
		menu bool
	}{
		{name: "list", mode: config.LauncherDisplayList},
		{name: "compact", mode: config.LauncherDisplayCompact},
		{name: "grid", mode: config.LauncherDisplayGrid},
		{name: "grid-menu", mode: config.LauncherDisplayGrid, menu: true},
	}
	trigger := Trigger{BarEdge: "top", BarZone: 40, OutW: 1536, OutH: 864}
	for _, scale120 := range outputs {
		for _, state := range states {
			name := fmt.Sprintf("launcher-%s-%d.png", state.name, scale120)
			t.Run(name, func(t *testing.T) {
				reg, _, reqs := openLauncherPanelWithTrigger(t, entries, trigger)
				panel := reqs[1].Open
				h := launcherHost(t, reg)
				reg.mu.Lock()
				reg.cfg.Launcher.Display = state.mode
				longIndex, actionIndex := -1, -1
				var actionID string
				for i, res := range h.launcherResults {
					if res.Entry.ID == entries[6].ID {
						longIndex = i
					}
					if actionIndex < 0 && i < launcherGridColumns*3 && len(res.Entry.Actions) > 0 {
						actionIndex, actionID = i, res.Entry.ID
					}
				}
				if longIndex < 0 || state.menu && actionIndex < 0 {
					reg.mu.Unlock()
					t.Fatalf("fixture results: long name index %d, visible action index %d", longIndex, actionIndex)
				}
				if state.menu {
					h.launcherSel = actionIndex
					if !h.openLauncherActions(reg, actionID) || h.menu == nil || !h.menu.Opened() {
						reg.mu.Unlock()
						t.Fatal("could not open the fixture desktop actions menu")
					}
				} else {
					h.launcherSel = longIndex
					reg.rebuildPanel(h)
				}
				reg.mu.Unlock()

				settleHostAnimation(reg, h)
				paintPanelPNG(t, panel, scale120, filepath.Join(dir, name))
			})
		}
	}
}
