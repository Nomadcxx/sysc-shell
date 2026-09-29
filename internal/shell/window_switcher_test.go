package shell

import (
	"reflect"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
)

func TestWindowSwitcherOrder(t *testing.T) {
	t.Parallel()
	snapshot := niri.Snapshot{
		FocusedOutput: "DP-1",
		Workspaces: []niri.Workspace{
			{ID: 10, Output: "DP-1"},
			{ID: 20, Output: "DP-2"},
		},
		Windows: []niri.Window{
			{ID: 80, WorkspaceID: 10, HasWorkspace: true},
			{ID: 4, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 100},
			{ID: 2, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 100},
			{ID: 7, WorkspaceID: 10, HasWorkspace: true},
			{ID: 1, WorkspaceID: 20, HasWorkspace: true, FocusTimestamp: 200},
			{ID: 3, WorkspaceID: 99, HasWorkspace: true, FocusTimestamp: 300},
			{ID: 5, WorkspaceID: 10, HasWorkspace: false, FocusTimestamp: 400},
		},
	}
	model := newWindowSwitcherModel(snapshot)
	if got, want := windowIDs(model.windows), []uint64{2, 4, 7, 80}; !reflect.DeepEqual(got, want) {
		t.Fatalf("MRU IDs = %v, want %v", got, want)
	}
}

func TestWindowSwitcherCycle(t *testing.T) {
	t.Parallel()
	model := newWindowSwitcherModel(niri.Snapshot{
		FocusedOutput: "DP-1",
		Workspaces:    []niri.Workspace{{ID: 10, Output: "DP-1"}},
		Windows: []niri.Window{
			{ID: 1, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 30},
			{ID: 2, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 20},
			{ID: 3, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 10},
		},
	})
	if got, ok := model.selectedWindow(); !ok || got.ID != 1 {
		t.Fatalf("initial selection = %+v, %v; want window 1", got, ok)
	}
	model.cycle(1)
	model.cycle(1)
	model.cycle(1)
	if got, _ := model.selectedWindow(); got.ID != 1 {
		t.Fatalf("forward wrap selected %d, want 1", got.ID)
	}
	model.cycle(-1)
	if got, _ := model.selectedWindow(); got.ID != 3 {
		t.Fatalf("backward wrap selected %d, want 3", got.ID)
	}

	empty := newWindowSwitcherModel(niri.Snapshot{})
	if empty.cycle(1) {
		t.Fatal("cycling an empty model reported a selection change")
	}
	if _, ok := empty.selectedWindow(); ok {
		t.Fatal("empty model returned a selected window")
	}
}

func TestWindowSwitcherRefreshPreservesIdentity(t *testing.T) {
	t.Parallel()
	model := newWindowSwitcherModel(switcherSnapshot([]niri.Window{
		{ID: 1, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 30},
		{ID: 2, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 20},
	}, false))
	model.cycle(1)
	model.refresh(switcherSnapshot([]niri.Window{
		{ID: 2, WorkspaceID: 10, HasWorkspace: true, Title: "Updated", FocusTimestamp: 20},
		{ID: 1, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 30},
	}, false))
	if got, ok := model.selectedWindow(); !ok || got.ID != 2 || got.Title != "Updated" {
		t.Fatalf("refresh selection = %+v, %v; want refreshed window 2", got, ok)
	}
	model.refresh(switcherSnapshot([]niri.Window{
		{ID: 1, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 30},
	}, false))
	if got, ok := model.selectedWindow(); !ok || got.ID != 1 {
		t.Fatalf("removed selection fallback = %+v, %v; want window 1", got, ok)
	}
}

func TestWindowSwitcherOpenGuards(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	valid := switcherSnapshot([]niri.Window{{ID: 1, WorkspaceID: 10, HasWorkspace: true}}, false)

	reg.UpdateNiri(switcherSnapshot(valid.Windows, true))
	if err := reg.ShowWindowSwitcher(); err == nil {
		t.Fatal("switcher opened during overview")
	}
	reg.UpdateNiri(valid)
	reg.mu.Lock()
	reg.roots.openRoot(panelRoot(1))
	reg.mu.Unlock()
	if err := reg.ShowWindowSwitcher(); err == nil {
		t.Fatal("switcher opened while another root owned the keyboard")
	}
	reg.mu.Lock()
	reg.roots.release()
	reg.mu.Unlock()

	reg.UpdateNiri(niri.Snapshot{Workspaces: valid.Workspaces, Windows: valid.Windows})
	if err := reg.ShowWindowSwitcher(); err == nil {
		t.Fatal("switcher opened without a focused output")
	}
	reg.UpdateNiri(switcherSnapshot(nil, false))
	if err := reg.ShowWindowSwitcher(); err == nil {
		t.Fatal("switcher opened with no focused-output windows")
	}
	reg.UpdateNiri(valid)
	if err := reg.ShowWindowSwitcher(); err != nil {
		t.Fatalf("open with a focused-output window: %v", err)
	}
	req := <-reg.AuxRequests()
	if req.Open == nil || req.Output != 1 || req.Open.Keyboard != keyboardExclusive || req.Open.Layer != layershell.ZwlrLayerShellV1LayerOverlay {
		t.Fatalf("open request = %+v; want exclusive overlay on output 1", req)
	}
	host := reg.windowSwitcher
	if !host.handleLocking(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEsc}) {
		t.Fatal("Escape did not close the switcher")
	}
}

func TestWindowSwitcherNavigationFocusAndCancel(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	var sent []any
	reg.niriSend = func(action any) error { sent = append(sent, action); return nil }
	reg.UpdateNiri(switcherSnapshot([]niri.Window{
		{ID: 40, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 40},
		{ID: 20, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 20},
		{ID: 30, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 30},
	}, false))
	if err := reg.ShowWindowSwitcher(); err != nil {
		t.Fatal(err)
	}
	host := reg.windowSwitcher
	if !host.handleLocking(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab}) {
		t.Fatal("Tab did not advance selection")
	}
	if got, _ := host.model.selectedWindow(); got.ID != 30 {
		t.Fatalf("Tab selected %d, want 30", got.ID)
	}
	if !host.handleLocking(wayland.Event{Kind: wayland.EventKeyPress, Key: keyUp}) {
		t.Fatal("Up did not move selection backwards")
	}
	if got, _ := host.model.selectedWindow(); got.ID != 40 {
		t.Fatalf("Up selected %d, want 40", got.ID)
	}
	if !host.handleLocking(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEnter}) {
		t.Fatal("Enter did not focus the selected window")
	}
	if len(sent) != 1 {
		t.Fatalf("Niri actions = %v, want one FocusWindow", sent)
	}
	if got, ok := sent[0].(niri.FocusWindow); !ok || got.ID != 40 {
		t.Fatalf("focused action = %#v, want FocusWindow{ID:40}", sent[0])
	}

	if err := reg.ShowWindowSwitcher(); err != nil {
		t.Fatal(err)
	}
	host = reg.windowSwitcher
	if !host.handleLocking(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEsc}) || host.open_ {
		t.Fatal("Escape did not close the switcher")
	}
	if len(sent) != 1 {
		t.Fatalf("Escape sent a Niri action: %v", sent[1:])
	}
}

func TestWindowSwitcherPointerAndStaleSnapshot(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	var sent []any
	reg.niriSend = func(action any) error { sent = append(sent, action); return nil }
	reg.UpdateNiri(switcherSnapshot([]niri.Window{
		{ID: 1, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 30},
		{ID: 2, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 20},
	}, false))
	if err := reg.ShowWindowSwitcher(); err != nil {
		t.Fatal(err)
	}
	host := reg.windowSwitcher
	if err := host.spec().Callbacks.Configure(800, 600, 120); err != nil {
		t.Fatal(err)
	}
	row := host.rows[1]
	x, y := float64(row.Bounds.X+row.Bounds.W/2), float64(row.Bounds.Y+row.Bounds.H/2)
	host.handleLocking(wayland.Event{Kind: wayland.EventPointerMotion, X: x, Y: y})
	if got, _ := host.model.selectedWindow(); got.ID != 2 {
		t.Fatalf("pointer selection = %d, want 2", got.ID)
	}
	// Niri removes the selected ID before the click arrives. Refresh by ID and
	// activate only the surviving exact window.
	reg.UpdateNiri(switcherSnapshot([]niri.Window{
		{ID: 1, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 30},
	}, false))
	if got, _ := host.model.selectedWindow(); got.ID != 1 {
		t.Fatalf("stale selection did not move to surviving window: %d", got.ID)
	}
	host.handleLocking(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEnter})
	if len(sent) != 1 {
		t.Fatalf("actions = %v, want one focus", sent)
	}
	if got, ok := sent[0].(niri.FocusWindow); !ok || got.ID != 1 {
		t.Fatalf("stale focus action = %#v, want FocusWindow{ID:1}", sent[0])
	}

	reg.UpdateNiri(switcherSnapshot([]niri.Window{
		{ID: 1, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 30},
	}, false))
	if err := reg.ShowWindowSwitcher(); err != nil {
		t.Fatal(err)
	}
	reg.UpdateNiri(switcherSnapshot(nil, true))
	if reg.windowSwitcher.open_ {
		t.Fatal("overview opening left the switcher open")
	}
	if len(sent) != 1 {
		t.Fatalf("overview close sent a Niri action: %v", sent[1:])
	}
}

func TestWindowSwitcherPointerClickFocusesExactWindow(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	var sent []any
	reg.niriSend = func(action any) error { sent = append(sent, action); return nil }
	reg.UpdateNiri(switcherSnapshot([]niri.Window{
		{ID: 91, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 30},
		{ID: 47, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 20},
	}, false))
	if err := reg.ShowWindowSwitcher(); err != nil {
		t.Fatal(err)
	}
	host := reg.windowSwitcher
	if err := host.spec().Callbacks.Configure(800, 600, 120); err != nil {
		t.Fatal(err)
	}
	row := host.rows[1]
	x, y := float64(row.Bounds.X+row.Bounds.W/2), float64(row.Bounds.Y+row.Bounds.H/2)
	host.handleLocking(wayland.Event{Kind: wayland.EventPointerMotion, X: x, Y: y})
	if !host.handleLocking(wayland.Event{Kind: wayland.EventPointerPress, Button: buttonLeft, X: x, Y: y}) {
		t.Fatal("pointer press on a window row was ignored")
	}
	if !host.handleLocking(wayland.Event{Kind: wayland.EventPointerRelease, Button: buttonLeft, X: x, Y: y}) {
		t.Fatal("pointer release did not focus the window row")
	}
	if len(sent) != 1 {
		t.Fatalf("Niri actions = %v, want one focus", sent)
	}
	if got, ok := sent[0].(niri.FocusWindow); !ok || got.ID != 47 {
		t.Fatalf("pointer focused %#v, want FocusWindow{ID:47}", sent[0])
	}
}

func TestWindowSwitcherEmptyAndRemovedSnapshotRefuseFocus(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	var sent []any
	reg.niriSend = func(action any) error { sent = append(sent, action); return nil }
	reg.UpdateNiri(switcherSnapshot([]niri.Window{{ID: 1, WorkspaceID: 10, HasWorkspace: true}}, false))
	if err := reg.ShowWindowSwitcher(); err != nil {
		t.Fatal(err)
	}
	reg.UpdateNiri(switcherSnapshot(nil, false))
	if reg.windowSwitcher.open_ {
		t.Fatal("empty snapshot left switcher open")
	}
	if len(sent) != 0 {
		t.Fatalf("removed selected window was focused: %v", sent)
	}
}

func switcherSnapshot(windows []niri.Window, overview bool) niri.Snapshot {
	return niri.Snapshot{
		FocusedOutput: "DP-1",
		OverviewOpen:  overview,
		Workspaces:    []niri.Workspace{{ID: 10, Output: "DP-1"}},
		Windows:       windows,
	}
}

func windowIDs(windows []niri.Window) []uint64 {
	ids := make([]uint64, len(windows))
	for i, w := range windows {
		ids[i] = w.ID
	}
	return ids
}
