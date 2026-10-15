package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestRunningAppsClick(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Bar.Left, cfg.Bar.Center = nil, nil
	cfg.Bar.Right = []config.Item{{ID: "running-apps"}}
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	var sent []any
	reg.niriSend = func(body any) error {
		sent = append(sent, body)
		return nil
	}
	var spawned [][]string
	reg.runArgv = func(argv []string) error {
		spawned = append(spawned, append([]string(nil), argv...))
		return nil
	}
	reg.runningIndex = []runningAppEntry{{
		ID: "steam",
		Actions: []runningAppAction{
			{ID: "Store", Name: "Store", Exec: "steam steam://store"},
			{ID: "Library", Name: "Library", Exec: "steam steam://open/games"},
			{ID: "Friends", Name: "Friends", Exec: "steam steam://open/friends"},
		},
	}}
	newHosts(t, reg, map[uint32]string{1: "DP-9"})
	reg.runningMenu = newRunningAppMenuHost(reg)
	reg.runningMenu.request = func(wayland.AuxRequest) {}
	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{
		{ID: 80, AppID: "steam", Title: "Steam store", Focused: true, Pid: 1 << 30, FocusTimestamp: 10},
		{ID: 81, AppID: "steam", Title: "Steam library", Pid: 1 << 30, FocusTimestamp: 20},
	}})

	bar := reg.bars[1]
	if !bar.onAction("running-app:steam", buttonLeft) {
		t.Fatal("left click ignored")
	}
	if len(sent) != 1 {
		t.Fatalf("niri sends = %d, want 1 FocusWindow", len(sent))
	}
	if fw, ok := sent[0].(niri.FocusWindow); !ok || fw.ID != 81 {
		t.Fatalf("left click sent %+v, want FocusWindow 81 (cycle)", sent[0])
	}

	if !bar.onAction("running-app:steam", buttonRight) {
		t.Fatal("right click ignored")
	}
	host := reg.runningMenu
	if host == nil || !host.open_ {
		t.Fatal("right click did not open the overlay menu")
	}
	if host.spec().Layer != layershell.ZwlrLayerShellV1LayerOverlay {
		t.Fatalf("layer = %d, want Overlay", host.spec().Layer)
	}
	got := labels(host.rows)
	want := []string{"steam (2 windows)", "Steam library", "Steam store", "Store", "Library", "Friends", "Close all and force quit"}
	if len(got) != len(want) {
		t.Fatalf("menu = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %q, want %q", i, got[i], want[i])
		}
	}

	actionIndex := -1
	for i, row := range host.rows {
		if row.ActionID == "Store" {
			actionIndex = i
			break
		}
	}
	if actionIndex < 0 {
		t.Fatal("Store action missing")
	}
	host.choose(actionIndex)
	if len(spawned) != 1 {
		t.Fatalf("spawns = %d, want 1", len(spawned))
	}
	if len(spawned[0]) < 6 || spawned[0][0] != "niri" || spawned[0][3] != "spawn" {
		t.Fatalf("spawn argv = %v, want niri msg action spawn -- …", spawned[0])
	}

	bar.onAction("running-app:steam", buttonRight)
	reg.runningMenu.choose(len(reg.runningMenu.rows) - 1)
	for _, body := range sent[1:] {
		if _, ok := body.(niri.CloseWindow); ok {
			t.Fatalf("app-level action sent Niri CloseWindow: %v", body)
		}
	}
	if !reg.runningMenu.open_ || reg.runningMenu.forceQuitState != runningAppForceQuitConfirming {
		t.Fatal("app-level action did not ask for confirmation")
	}
	cancel := -1
	for i, row := range reg.runningMenu.rows {
		if row.Kind == runningAppMenuForceQuitCancel {
			cancel = i
			break
		}
	}
	if cancel < 0 {
		t.Fatal("force-quit confirmation has no cancel action")
	}
	reg.runningMenu.choose(cancel)
	if reg.runningMenu.open_ {
		t.Fatal("cancel did not close the force-quit confirmation")
	}

	bar.onAction("running-app:steam", buttonRight)
	reg.UpdateNiri(niri.Snapshot{})
	if reg.runningMenu != nil && reg.runningMenu.open_ {
		t.Fatal("menu stayed open after the slot disappeared")
	}
}

func TestRunningAppsMenuWindowRowFocusesExactNiriID(t *testing.T) {
	t.Parallel()
	reg, bar := steamMenuReg(t)
	var sent []any
	reg.niriSend = func(action any) error { sent = append(sent, action); return nil }
	reg.runArgv = func([]string) error { return nil }
	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{
		{ID: 80, AppID: "steam", Title: "Store", Focused: true, Pid: 1 << 30, FocusTimestamp: 10},
		{ID: 81, AppID: "steam", Title: "Library", Pid: 1 << 30, FocusTimestamp: 20},
	}})
	if !bar.onAction("running-app:steam", buttonRight) {
		t.Fatal("right click ignored")
	}
	index := -1
	for i, row := range reg.runningMenu.rows {
		if row.Kind == runningAppMenuWindow && row.WindowID == 81 {
			index = i
			break
		}
	}
	if index < 0 {
		t.Fatalf("window 81 missing from menu rows: %+v", reg.runningMenu.rows)
	}
	reg.runningMenu.choose(index)
	if len(sent) != 1 {
		t.Fatalf("Niri actions = %v, want one FocusWindow", sent)
	}
	if got, ok := sent[0].(niri.FocusWindow); !ok || got.ID != 81 {
		t.Fatalf("focus action = %#v, want window 81", sent[0])
	}
}

func TestRunningAppsHandleClickCycles(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Bar.Left, cfg.Bar.Center = nil, nil
	cfg.Bar.Right = []config.Item{{ID: "running-apps"}}
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	var sent []any
	reg.niriSend = func(body any) error {
		sent = append(sent, body)
		return nil
	}
	reg.runningIndex = []runningAppEntry{{ID: "steam"}}
	cb, err := reg.NewHost(1, "DP-9")
	if err != nil {
		t.Fatal(err)
	}
	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{
		{ID: 80, AppID: "steam", Focused: true},
		{ID: 81, AppID: "steam"},
	}})
	if err := cb.Configure(800, BarHeight, 120); err != nil {
		t.Fatal(err)
	}
	bar := reg.bars[1]
	tile := findAction(bar.right[0].node, "running-app:steam")
	if tile == nil || tile.Bounds.W == 0 {
		t.Fatalf("steam tile = %+v, want laid-out bounds", tile)
	}
	x, y := float64(tile.Bounds.X+tile.Bounds.W/2), float64(tile.Bounds.Y+tile.Bounds.H/2)
	bar.Handle(wayland.Event{Kind: wayland.EventPointerMotion, X: x, Y: y})
	bar.Handle(wayland.Event{Kind: wayland.EventPointerPress, Button: buttonLeft, X: x, Y: y})
	if !bar.Handle(wayland.Event{Kind: wayland.EventPointerRelease, Button: buttonLeft, X: x, Y: y}) {
		t.Fatal("press+release on the tile was not a click")
	}
	if len(sent) != 1 {
		t.Fatalf("niri sends = %d, want 1 FocusWindow", len(sent))
	}
	if fw, ok := sent[0].(niri.FocusWindow); !ok || fw.ID != 81 {
		t.Fatalf("click sent %+v, want FocusWindow 81 (cycle)", sent[0])
	}
}

func TestRunningAppsMenuIsPlacedUnderItsIcon(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Bar.Left, cfg.Bar.Center = nil, nil
	cfg.Bar.Right = []config.Item{{ID: "running-apps"}}
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	reg.runningIndex = []runningAppEntry{{ID: "steam"}}
	newHosts(t, reg, map[uint32]string{1: "DP-9"})
	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{
		{ID: 80, AppID: "steam", Focused: true},
	}})
	reg.runningMenu = newRunningAppMenuHost(reg)
	reg.runningMenu.request = func(wayland.AuxRequest) {}

	bar := reg.bars[1]
	tile := findAction(bar.right[0].node, "running-app:steam")
	if tile == nil {
		t.Fatal("steam tile missing")
	}
	tile.Bounds = ui.Rect{X: 1600, Y: 4, W: 24, H: 24}
	if !bar.onAction("running-app:steam", buttonRight) {
		t.Fatal("right click ignored")
	}
	spec := reg.runningMenu.spec()
	want := trayMenuUnderBar(tile.Bounds)
	if spec.Anchor != want.anchor || spec.MarginLeft != want.marginLeft || spec.MarginTop != want.marginTop {
		t.Fatalf("placement = anchor %d left %d top %d, want under the tile (anchor %d left %d top %d)",
			spec.Anchor, spec.MarginLeft, spec.MarginTop, want.anchor, want.marginLeft, want.marginTop)
	}
	right := uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorRight)
	if spec.Anchor == right {
		t.Fatal("menu still glued to the output's right edge")
	}
}

func TestRunningAppSideMenuOpensInwardAndClamps(t *testing.T) {
	const outW, outH = 1536, 864
	cfg := config.Default()
	cfg.Bar.Edge = "right"
	cfg.Bar.Left, cfg.Bar.Center = nil, nil
	cfg.Bar.Right = []config.Item{{ID: "running-apps"}}
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	reg.runningIndex = []runningAppEntry{{ID: "steam"}}
	newHosts(t, reg, map[uint32]string{1: "DP-9"})
	bar := reg.bars[1]
	bar.setOutputSize(outW, outH)
	if err := bar.Configure(cfg.Bar.SurfaceExtent(), outH, 120); err != nil {
		t.Fatal(err)
	}
	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{{ID: 80, AppID: "steam", Focused: true}}})
	reg.runningMenu = newRunningAppMenuHost(reg)
	reg.runningMenu.request = func(wayland.AuxRequest) {}
	tile := findAction(bar.right[0].node, "running-app:steam")
	if tile == nil {
		t.Fatal("steam tile missing")
	}
	tile.Bounds = ui.Rect{X: 12, Y: 850, W: 24, H: 20}
	if !bar.onAction("running-app:steam", buttonRight) {
		t.Fatal("right click ignored")
	}
	w, h := reg.runningMenu.size()
	barW, _ := bar.configuredSize()
	wantX := max(0, min(outW-w, outW-barW+tile.Bounds.X-w))
	wantY := max(0, min(outH-h, tile.Bounds.Y))
	spec := reg.runningMenu.spec()
	if int(spec.MarginLeft) != wantX || int(spec.MarginTop) != wantY {
		t.Fatalf("side app menu origin = (%d,%d), want (%d,%d)", spec.MarginLeft, spec.MarginTop, wantX, wantY)
	}
}

func TestRunningAppsMenuTakesExclusiveKeyboard(t *testing.T) {
	t.Parallel()
	reg, bar := steamMenuReg(t)
	if !bar.onAction("running-app:steam", buttonRight) {
		t.Fatal("right click ignored")
	}
	if got := reg.runningMenu.spec().Keyboard; got != keyboardExclusive {
		t.Fatalf("keyboard = %d, want Exclusive so Escape reaches the overlay", got)
	}
}

func TestRunningAppsMenuEscapeCloses(t *testing.T) {
	t.Parallel()
	reg, bar := steamMenuReg(t)
	bar.onAction("running-app:steam", buttonRight)
	if !reg.runningMenu.handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEsc}) {
		t.Fatal("Escape ignored")
	}
	if reg.runningMenu.open_ {
		t.Fatal("Escape left the menu open")
	}
}

func TestRunningAppsMenuChoosesOnPressRelease(t *testing.T) {
	t.Parallel()
	reg, bar := steamMenuReg(t)
	var spawned [][]string
	reg.runArgv = func(argv []string) error {
		spawned = append(spawned, append([]string(nil), argv...))
		return nil
	}
	bar.onAction("running-app:steam", buttonRight)
	host := reg.runningMenu
	w, h := host.size()
	if err := host.configure(w, h, 120); err != nil {
		t.Fatal(err)
	}
	row := findAction(host.root, "running-app-action:Store")
	if row == nil {
		t.Fatal("Store desktop action missing from menu tree")
	}
	x, y := float64(row.Bounds.X+1), float64(row.Bounds.Y+1)
	if host.handle(wayland.Event{Kind: wayland.EventPointerRelease, X: x, Y: y}) {
		t.Fatal("release without press chose a row")
	}
	if len(spawned) != 0 {
		t.Fatal("release without press spawned")
	}
	host.handle(wayland.Event{Kind: wayland.EventPointerPress, X: x, Y: y})
	if !host.handle(wayland.Event{Kind: wayland.EventPointerRelease, X: x, Y: y}) {
		t.Fatal("press+release ignored")
	}
	if len(spawned) != 1 {
		t.Fatalf("spawns = %d, want 1", len(spawned))
	}
}

func TestRunningAppsMenuClickOutsideCloses(t *testing.T) {
	t.Parallel()
	reg, bar := steamMenuReg(t)
	reg.runningMenu = newRunningAppMenuHost(reg)
	var reqs []wayland.AuxRequest
	reg.runningMenu.request = func(req wayland.AuxRequest) { reqs = append(reqs, req) }
	bar.onAction("running-app:steam", buttonRight)
	var shield *wayland.AuxSpec
	for _, req := range reqs {
		if req.Open != nil && req.Open.ID == runningAppMenuShieldID {
			shield = req.Open
		}
	}
	if shield == nil {
		t.Fatal("no click-outside shield")
	}
	if shield.Keyboard != keyboardNone {
		t.Fatalf("shield keyboard = %d, want None", shield.Keyboard)
	}
	if shield.Callbacks.Handle == nil {
		t.Fatal("shield has no handle")
	}
	if !shield.Callbacks.Handle(wayland.Event{Kind: wayland.EventPointerPress}) {
		t.Fatal("shield press ignored")
	}
	if reg.runningMenu.open_ {
		t.Fatal("click outside left the menu open")
	}
}

func TestRunningAppsMenuRowsAreCapsules(t *testing.T) {
	t.Parallel()
	reg, bar := steamMenuReg(t)
	bar.onAction("running-app:steam", buttonRight)
	host := reg.runningMenu
	row := findAction(host.root, "running-app-window:80")
	if row == nil {
		t.Fatal("no window row")
	}
	if row.Kind != ui.KindCapsule || row.Radius != host.style.Shapes.Small {
		t.Fatalf("row = kind %d radius %d, want KindCapsule radius %d", row.Kind, row.Radius, host.style.Shapes.Small)
	}
	if row.Fill != ui.FillSoft {
		t.Fatalf("initial MRU fill = %v, want keyboard selection FillSoft", row.Fill)
	}
	if host.root == nil || host.root.Gap <= 0 {
		t.Fatal("menu column should use theme spacing between rows")
	}
}

func TestRunningAppsMenuHighlightsOnMotion(t *testing.T) {
	t.Parallel()
	reg, bar := steamMenuReg(t)
	bar.onAction("running-app:steam", buttonRight)
	host := reg.runningMenu
	w, h := host.size()
	if err := host.configure(w, h, 120); err != nil {
		t.Fatal(err)
	}
	second := findAction(host.root, "running-app-action:Store")
	if second == nil {
		t.Fatal("desktop action row missing")
	}
	x, y := float64(second.Bounds.X+1), float64(second.Bounds.Y+1)
	if !host.handle(wayland.Event{Kind: wayland.EventPointerMotion, X: x, Y: y}) {
		t.Fatal("motion ignored")
	}
	got := findAction(host.root, "running-app-action:Store")
	window := findAction(host.root, "running-app-window:80")
	if got.Fill != ui.FillSoft || window.Fill != ui.FillNone {
		t.Fatalf("fills = action %v window %v, want hover on action only", got.Fill, window.Fill)
	}
}

func TestRunningAppsMenuSeparatesCloseAll(t *testing.T) {
	t.Parallel()
	reg, bar := steamMenuReg(t)
	bar.onAction("running-app:steam", buttonRight)
	root := reg.runningMenu.root
	var sawSep bool
	for _, c := range root.Children {
		if c != nil && c.Kind == ui.KindSeparator {
			sawSep = true
		}
	}
	if !sawSep {
		t.Fatal("no separator before app close")
	}
	close := findAction(root, "running-app-force-quit")
	if close == nil || len(close.Children) != 1 || close.Children[0].Tone != ui.ToneError {
		t.Fatalf("app close action = %+v, want ToneError", close)
	}
}

func TestRunningAppsMenuMovesExactMRUWindowWithoutChangingFocus(t *testing.T) {
	t.Parallel()
	reg, bar := runningAppsWorkspaceMenuRegistry(t)
	var sent []any
	reg.niriSend = func(action any) error { sent = append(sent, action); return nil }
	if !bar.onAction("running-app:steam", buttonRight) {
		t.Fatal("right click ignored")
	}
	index := findWorkspaceMoveRow(reg.runningMenu.rows, 20)
	if index < 0 {
		t.Fatalf("menu rows = %+v, missing workspace 20", reg.runningMenu.rows)
	}
	reg.runningMenu.choose(index)
	if len(sent) != 1 {
		t.Fatalf("Niri actions = %v, want one window move", sent)
	}
	want := niri.MoveWindowToWorkspace{WindowID: 80, WorkspaceID: 20, Focus: false}
	if got, ok := sent[0].(niri.MoveWindowToWorkspace); !ok || got != want {
		t.Fatalf("move action = %#v, want %#v", sent[0], want)
	}
}

func TestRunningAppsMenuRejectsStaleWorkspaceAndWindow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		update niri.Snapshot
		wsID   uint64
	}{
		{
			name: "workspace removed",
			update: niri.Snapshot{
				FocusedOutput: "DP-1",
				Workspaces: []niri.Workspace{
					{ID: 10, Index: 1, Name: "Current", Output: "DP-1"},
					{ID: 30, Index: 3, Output: "DP-2"},
				},
				Windows: []niri.Window{{ID: 80, AppID: "steam", WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 30}},
			},
			wsID: 20,
		},
		{
			name: "MRU window removed",
			update: niri.Snapshot{
				FocusedOutput: "DP-1",
				Workspaces: []niri.Workspace{
					{ID: 10, Index: 1, Name: "Current", Output: "DP-1"},
					{ID: 20, Index: 2, Name: "Build", Output: "DP-1"},
					{ID: 30, Index: 3, Output: "DP-2"},
				},
				Windows: []niri.Window{{ID: 81, AppID: "steam", WorkspaceID: 20, HasWorkspace: true, FocusTimestamp: 20}},
			},
			wsID: 30,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg, bar := runningAppsWorkspaceMenuRegistry(t)
			var sent []any
			reg.niriSend = func(action any) error { sent = append(sent, action); return nil }
			bar.onAction("running-app:steam", buttonRight)
			index := findWorkspaceMoveRow(reg.runningMenu.rows, tc.wsID)
			if index < 0 {
				t.Fatalf("menu rows = %+v, missing workspace %d", reg.runningMenu.rows, tc.wsID)
			}
			staleRows := append([]runningAppMenuRow(nil), reg.runningMenu.rows...)
			reg.UpdateNiri(tc.update)
			// Keep the old projection to exercise the action-time target checks.
			reg.runningMenu.rows = staleRows
			reg.runningMenu.choose(index)
			if len(sent) != 0 {
				t.Fatalf("stale move sent Niri actions: %v", sent)
			}
		})
	}
}

func runningAppsWorkspaceMenuRegistry(t *testing.T) (*Registry, *Bar) {
	t.Helper()
	cfg := config.Default()
	cfg.Bar.Left, cfg.Bar.Center = nil, nil
	cfg.Bar.Right = []config.Item{{ID: "running-apps"}}
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	reg.runningIndex = []runningAppEntry{{ID: "steam"}}
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	reg.UpdateNiri(niri.Snapshot{
		FocusedOutput: "DP-1",
		Workspaces: []niri.Workspace{
			{ID: 10, Index: 1, Name: "Current", Output: "DP-1"},
			{ID: 20, Index: 2, Name: "Build", Output: "DP-1"},
			{ID: 30, Index: 3, Output: "DP-2"},
		},
		Windows: []niri.Window{
			{ID: 80, Title: "Terminal", AppID: "steam", WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 30},
			{ID: 81, Title: "Terminal", AppID: "steam", WorkspaceID: 20, HasWorkspace: true, FocusTimestamp: 20},
		},
	})
	reg.runningMenu = newRunningAppMenuHost(reg)
	reg.runningMenu.request = func(wayland.AuxRequest) {}
	return reg, reg.bars[1]
}

func TestEdgeReloadClosesRunningAppsMenu(t *testing.T) {
	reg, bar := runningAppsWorkspaceMenuRegistry(t)
	bar.setOutputSize(1536, 864)
	if !bar.onAction("running-app:steam", buttonRight) {
		t.Fatal("right click did not open the running-app menu")
	}
	if !reg.runningMenu.open_ {
		t.Fatal("running-app menu did not open")
	}

	candidate := config.Default()
	candidate.Bar.Edge = "left"
	prepared, err := reg.PrepareConfig(candidate, []wayland.HostIdentity{{Global: 1, Connector: "DP-1"}})
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	callbacks := prepared.Hosts[1]
	callbacks.OutputSize(1536, 864)
	if err := callbacks.Configure(candidate.Bar.SurfaceExtent(), 864, 120); err != nil {
		t.Fatalf("candidate Configure: %v", err)
	}
	prepared.Commit()

	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.runningMenu.open_ || reg.roots.owns(runningAppsMenuRoot(1)) {
		t.Fatal("edge reload left the running-app menu on its old bar anchor")
	}
}

func findWorkspaceMoveRow(rows []runningAppMenuRow, workspaceID uint64) int {
	for i, row := range rows {
		if row.MoveWorkspaceID == workspaceID {
			return i
		}
	}
	return -1
}

func firstCapsule(n *ui.Node) *ui.Node {
	for _, c := range capsules(n) {
		return c
	}
	return nil
}

func capsules(n *ui.Node) []*ui.Node {
	if n == nil {
		return nil
	}
	var out []*ui.Node
	for _, c := range n.Children {
		if c != nil && c.Kind == ui.KindCapsule {
			out = append(out, c)
		}
	}
	return out
}

func steamMenuReg(t *testing.T) (*Registry, *Bar) {
	t.Helper()
	cfg := config.Default()
	cfg.Bar.Left, cfg.Bar.Center = nil, nil
	cfg.Bar.Right = []config.Item{{ID: "running-apps"}}
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	reg.runningIndex = []runningAppEntry{{
		ID: "steam",
		Actions: []runningAppAction{
			{ID: "Store", Name: "Store", Exec: "steam steam://store"},
		},
	}}
	newHosts(t, reg, map[uint32]string{1: "DP-9"})
	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{
		{ID: 80, AppID: "steam", Focused: true},
	}})
	reg.runningMenu = newRunningAppMenuHost(reg)
	reg.runningMenu.request = func(wayland.AuxRequest) {}
	return reg, reg.bars[1]
}

func labels(rows []runningAppMenuRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Label
	}
	return out
}
