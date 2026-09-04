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
		{ID: 80, AppID: "steam", Focused: true},
		{ID: 81, AppID: "steam"},
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
	want := []string{"Store", "Library", "Friends", "Close all"}
	if len(got) != len(want) {
		t.Fatalf("menu = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %q, want %q", i, got[i], want[i])
		}
	}

	host.choose(0)
	if len(spawned) != 1 {
		t.Fatalf("spawns = %d, want 1", len(spawned))
	}
	if len(spawned[0]) < 6 || spawned[0][0] != "niri" || spawned[0][3] != "spawn" {
		t.Fatalf("spawn argv = %v, want niri msg action spawn -- …", spawned[0])
	}

	bar.onAction("running-app:steam", buttonRight)
	reg.runningMenu.choose(len(reg.runningMenu.rows) - 1)
	if len(sent) < 3 {
		t.Fatalf("after Close all, sends = %d, want FocusWindow plus two CloseWindow", len(sent))
	}
	_, ok0 := sent[len(sent)-2].(niri.CloseWindow)
	_, ok1 := sent[len(sent)-1].(niri.CloseWindow)
	if !ok0 || !ok1 {
		t.Fatalf("Close all sent %+v %+v, want CloseWindow pair", sent[len(sent)-2], sent[len(sent)-1])
	}

	bar.onAction("running-app:steam", buttonRight)
	reg.UpdateNiri(niri.Snapshot{})
	if reg.runningMenu != nil && reg.runningMenu.open_ {
		t.Fatal("menu stayed open after the slot disappeared")
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
	row := firstCapsule(host.root)
	if row == nil {
		t.Fatal("no capsule row")
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
	root := reg.runningMenu.root
	if root == nil || root.Gap != 0 {
		t.Fatal("menu column should stack with no inter-row gap")
	}
	row := firstCapsule(root)
	if row == nil {
		t.Fatal("no capsule row")
	}
	if row.Kind != ui.KindCapsule || row.Radius != runningAppMenuRadius {
		t.Fatalf("row = kind %d radius %d, want KindCapsule radius %d", row.Kind, row.Radius, runningAppMenuRadius)
	}
	if row.Fill != ui.FillNone {
		t.Fatalf("idle fill = %v, want FillNone until hover or a key", row.Fill)
	}
	w, _ := reg.runningMenu.size()
	if w >= trayMenuWidth {
		t.Fatalf("width = %d, want less than the 280px tray constant", w)
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
	second := capsules(host.root)[1]
	x, y := float64(second.Bounds.X+1), float64(second.Bounds.Y+1)
	if !host.handle(wayland.Event{Kind: wayland.EventPointerMotion, X: x, Y: y}) {
		t.Fatal("motion ignored")
	}
	got := capsules(host.root)
	if got[0].Fill != ui.FillNone || got[1].Fill != ui.FillSoft {
		t.Fatalf("fills = %v %v, want idle then FillSoft under the pointer", got[0].Fill, got[1].Fill)
	}
}

func TestRunningAppsMenuSeparatesCloseAll(t *testing.T) {
	t.Parallel()
	reg, bar := steamMenuReg(t)
	bar.onAction("running-app:steam", buttonRight)
	root := reg.runningMenu.root
	var sawSep bool
	var closeTone ui.Tone
	for _, c := range root.Children {
		if c != nil && c.Kind == ui.KindRow {
			for _, inner := range c.Children {
				if inner != nil && inner.Kind == ui.KindSeparator {
					sawSep = true
				}
			}
		}
		if c != nil && c.Kind == ui.KindCapsule && len(c.Children) == 1 && c.Children[0].Text == "Close all" {
			closeTone = c.Children[0].Tone
		}
	}
	if !sawSep {
		t.Fatal("no separator before Close all")
	}
	if closeTone != ui.ToneError {
		t.Fatalf("Close all tone = %v, want ToneError", closeTone)
	}
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
