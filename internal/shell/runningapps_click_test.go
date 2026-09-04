package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
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

func labels(rows []runningAppMenuRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Label
	}
	return out
}
