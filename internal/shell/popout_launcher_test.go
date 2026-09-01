package shell

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/launcher"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func launcherTestEntries() []launcher.Entry {
	return []launcher.Entry{
		{
			ID: "firefox.desktop", Name: "Firefox", Argv: []string{"firefox"},
			Actions: []launcher.Action{{ID: "new-window", Name: "New Window", Argv: []string{"firefox", "--new-window"}}},
		},
		{ID: "foot.desktop", Name: "Foot", Argv: []string{"foot"}},
		{ID: "nautilus.desktop", Name: "Files", Argv: []string{"nautilus"}},
	}
}

type recordedSpawn struct {
	mu   sync.Mutex
	argv []string
	err  error
}

func (r *recordedSpawn) run(_ context.Context, argv []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.argv = append([]string(nil), argv...)
	return r.err
}

func (r *recordedSpawn) waitArgv(t *testing.T) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		argv := r.argv
		r.mu.Unlock()
		if argv != nil {
			return argv
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no spawn recorded")
	return nil
}

// openLauncherPanel injects a stub-backed launcher service, opens the panel,
// lays it out at its target size, and waits for the first result publish.
func openLauncherPanel(t *testing.T, entries []launcher.Entry) (*Registry, *recordedSpawn, []wayland.AuxRequest) {
	t.Helper()
	reg := newPanelRegistry(t)
	run := &recordedSpawn{}
	svc := launcher.NewService(launcher.ServiceConfig{
		Scan: func() []launcher.Entry { return entries },
		Run:  run.run,
	})
	reg.mu.Lock()
	reg.launcherSvc = svc
	reg.mu.Unlock()
	go reg.relayLauncher(svc)

	if err := reg.OpenPanel(PanelLauncher, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	if err := reqs[1].Open.Callbacks.Configure(560, 500, 120); err != nil {
		t.Fatal(err)
	}
	waitForLauncherResults(t, reg, len(entries))
	return reg, run, reqs
}

func launcherHost(t *testing.T, reg *Registry) *PanelHost {
	t.Helper()
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelLauncher]
	if h == nil {
		t.Fatal("launcher panel is not hosted")
	}
	return h
}

func waitForLauncherResults(t *testing.T, reg *Registry, want int) {
	t.Helper()
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && len(h.launcherResults) == want
	})
}

func waitForLauncherState(t *testing.T, reg *Registry, ok func(*PanelHost) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		done := ok(reg.panelHosts[PanelLauncher])
		reg.mu.Unlock()
		if done {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for launcher panel state")
}

func walkActionBounds(n *ui.Node, action string) (ui.Rect, bool) {
	if n == nil {
		return ui.Rect{}, false
	}
	if n.Action == action && n.Bounds.W > 0 {
		return n.Bounds, true
	}
	for _, c := range n.Children {
		if b, ok := walkActionBounds(c, action); ok {
			return b, true
		}
	}
	return ui.Rect{}, false
}

func pressLauncherKey(reqs []wayland.AuxRequest, key uint32) {
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventKeyPress, Key: key})
}

func TestParsePanelNameLauncher(t *testing.T) {
	t.Parallel()

	id, err := parsePanelName("launcher")
	if err != nil || id != PanelLauncher {
		t.Fatalf("parsePanelName(launcher) = %v, %v", id, err)
	}
	if got := PanelLauncher.String(); got != "launcher" {
		t.Fatalf("String() = %q", got)
	}
}

func TestLauncherPanelGeometry(t *testing.T) {
	t.Parallel()

	if got := panelTargetSize(PanelLauncher); got.W != 560 || got.H != 500 {
		t.Fatalf("target size = %dx%d, want 560x500", got.W, got.H)
	}

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	panel := reqs[1].Open
	if panel.Width != 560 || panel.Height != 500 {
		t.Fatalf("surface = %dx%d, want 560x500", panel.Width, panel.Height)
	}

	reg.mu.Lock()
	gap, pad := reg.cfg.Panels.Gap, reg.cfg.Panels.Padding
	reg.mu.Unlock()
	anchor := 40 + gap
	avail := 1080 - anchor - pad
	wantTop := anchor + (avail-500)/2
	if int(panel.MarginTop) != wantTop {
		t.Fatalf("margin top = %d, want vertically centred %d", panel.MarginTop, wantTop)
	}
	if panel.MarginLeft != 680 {
		t.Fatalf("margin left = %d, want horizontally centred 680", panel.MarginLeft)
	}
}

func TestLauncherTreeShape(t *testing.T) {
	t.Parallel()

	reg, _, _ := openLauncherPanel(t, launcherTestEntries())
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelLauncher]

	root := h.root
	if root.Kind != ui.KindColumn {
		t.Fatalf("root kind = %v, want KindColumn", root.Kind)
	}
	if root.Children[0].Kind != ui.KindTextField {
		t.Fatalf("first child kind = %v, want KindTextField", root.Children[0].Kind)
	}
	list := root.Children[len(root.Children)-1]
	if list.Kind != ui.KindVirtualList {
		t.Fatalf("list kind = %v, want KindVirtualList", list.Kind)
	}
	if list.ItemHeight != 48 {
		t.Fatalf("row height = %d, want 48", list.ItemHeight)
	}
	if list.ItemCount != 3 {
		t.Fatalf("item count = %d, want 3", list.ItemCount)
	}
	selected := list.Item(0)
	if selected.Kind != ui.KindButton || selected.Text != "Files" {
		t.Fatalf("selected row = %v %q, want KindButton Files", selected.Kind, selected.Text)
	}
	plain := list.Item(1)
	if plain.Kind != ui.KindText || plain.Text != "Firefox" {
		t.Fatalf("unselected row = %v %q, want KindText Firefox", plain.Kind, plain.Text)
	}
}

func TestLauncherTypingReprojects(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "nau"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && len(h.launcherResults) == 1 && h.launcherResults[0].Entry.Name == "Files"
	})
	h := launcherHost(t, reg)
	reg.mu.Lock()
	query := h.query
	reg.mu.Unlock()
	if query != "nau" {
		t.Fatalf("query = %q, want nau", query)
	}
}

func TestLauncherArrowsClampSelection(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	for range 5 {
		pressLauncherKey(reqs, keyDown)
	}
	reg.mu.Lock()
	sel := reg.panelHosts[PanelLauncher].launcherSel
	reg.mu.Unlock()
	if sel != 2 {
		t.Fatalf("selection after 5 downs = %d, want clamped 2", sel)
	}
	pressLauncherKey(reqs, keyUp)
	pressLauncherKey(reqs, keyUp)
	pressLauncherKey(reqs, keyUp)
	reg.mu.Lock()
	sel = reg.panelHosts[PanelLauncher].launcherSel
	reg.mu.Unlock()
	if sel != 0 {
		t.Fatalf("selection after 3 ups = %d, want clamped 0", sel)
	}
}

func TestLauncherEnterActivatesSelectedEntry(t *testing.T) {
	t.Parallel()

	reg, run, reqs := openLauncherPanel(t, launcherTestEntries())
	pressLauncherKey(reqs, keyDown)
	pressLauncherKey(reqs, keyEnter)
	if got := run.waitArgv(t); !slices.Equal(got, []string{"niri", "msg", "action", "spawn", "--", "firefox"}) {
		t.Fatalf("spawn argv = %v", got)
	}
	waitForLauncherState(t, reg, func(h *PanelHost) bool { return h == nil })
}

func TestLauncherEnterOnOverviewRowNavigates(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "/ap"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && len(h.launcherResults) == 1 && h.launcherResults[0].Entry.ID == "/apps"
	})
	pressLauncherKey(reqs, keyEnter)
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && h.query == "/apps" && len(h.launcherResults) == 3
	})
}

func TestLauncherRightClickOpensActionsMenu(t *testing.T) {
	t.Parallel()

	reg, run, reqs := openLauncherPanel(t, launcherTestEntries())
	h := launcherHost(t, reg)
	reg.mu.Lock()
	bounds, ok := walkActionBounds(h.root, "launch:firefox.desktop")
	reg.mu.Unlock()
	if !ok {
		t.Fatal("firefox row has no laid-out bounds")
	}

	reqs[1].Open.Callbacks.Handle(wayland.Event{
		Kind:   wayland.EventPointerPress,
		Button: btnRight,
		X:      float64(bounds.X + 4),
		Y:      float64(bounds.Y + 4),
	})
	reg.mu.Lock()
	menuOpen := reg.panelHosts[PanelLauncher].menu.Opened()
	reg.mu.Unlock()
	if !menuOpen {
		t.Fatal("right-click did not open the actions menu")
	}

	pressLauncherKey(reqs, keyEnter)
	if got := run.waitArgv(t); !slices.Equal(got, []string{"niri", "msg", "action", "spawn", "--", "firefox", "--new-window"}) {
		t.Fatalf("action argv = %v", got)
	}
	waitForLauncherState(t, reg, func(h *PanelHost) bool { return h == nil })
}

func TestLauncherEscapeCloses(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	pressLauncherKey(reqs, keyEsc)
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	_, hosted := reg.panelHosts[PanelLauncher]
	reg.mu.Unlock()
	if hosted {
		t.Fatal("escape left the launcher hosted")
	}
}

func TestLauncherSpawnFailureKeepsPanelOpen(t *testing.T) {
	t.Parallel()

	reg, run, reqs := openLauncherPanel(t, launcherTestEntries())
	run.err = errors.New("niri refused")
	pressLauncherKey(reqs, keyEnter)
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && h.errLabel != ""
	})
}
