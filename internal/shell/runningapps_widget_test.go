package shell

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestRunningAppsCapsule(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{{ID: "running-apps"}}, 8)
	if len(widgets) != 1 {
		t.Fatalf("built %d widgets, want 1", len(widgets))
	}
	w := widgets[0]
	view := barView{Running: []runningAppSlot{
		{Key: "firefox", Focused: true},
		{Key: "brave"},
	}}
	if w.refresh == nil {
		t.Fatal("running-apps should refresh a tree")
	}
	if !w.refresh(view) {
		t.Fatal("the first refresh reported no change")
	}
	if w.node.Kind != ui.KindCapsule {
		t.Fatalf("root kind = %d, want KindCapsule", w.node.Kind)
	}
	row := w.inner
	if row == nil || row.Kind != ui.KindRow || len(row.Children) != 2 {
		t.Fatalf("inner = %+v, want a row of two tiles", row)
	}
	if row.Gap != 4 {
		t.Fatalf("gap = %d, want 4", row.Gap)
	}
	firefox, brave := row.Children[0], row.Children[1]
	if firefox.Width != 24 || firefox.Height != 24 || firefox.Fill != ui.FillNone || firefox.Padding != runningAppIconPad {
		t.Fatalf("focused tile = %+v, want 24x24 FillNone pad %d", firefox, runningAppIconPad)
	}
	if brave.Width != 24 || brave.Height != 24 || brave.Fill != ui.FillNone {
		t.Fatalf("idle tile = %+v, want 24x24 FillNone", brave)
	}
	if firefox.Action != "running-app:firefox" || brave.Action != "running-app:brave" {
		t.Fatalf("actions = %q %q", firefox.Action, brave.Action)
	}
	if letter := tileLetter(firefox); letter != "F" {
		t.Fatalf("firefox letter = %q, want F", letter)
	}
	if w.refresh(view) {
		t.Fatal("an unchanged view rebuilt the tiles")
	}
	if !w.refresh(barView{}) {
		t.Fatal("clearing slots reported no change")
	}
	if len(w.node.Children) != 0 {
		t.Fatalf("empty capsule children = %d, want none", len(w.node.Children))
	}
}

func tileLetter(n *ui.Node) string {
	if n == nil || len(n.Children) != 1 || n.Children[0] == nil {
		return ""
	}
	return n.Children[0].Text
}

func TestRunningAppsIconSizeFollowsTheOutputScale(t *testing.T) {
	if got := runningAppIconPixelSize(int(ui.ScaleUnit)); got != runningAppIconSize {
		t.Fatalf("scale 1 size = %d, want %d", got, runningAppIconSize)
	}
	if got := runningAppIconPixelSize(240); got != 2*runningAppIconSize {
		t.Fatalf("scale 2 size = %d, want %d", got, 2*runningAppIconSize)
	}
	if got := runningAppIconPixelSize(0); got != runningAppIconSize {
		t.Fatalf("unconfigured scale = %d, want the logical size", got)
	}
}

func TestRunningAppsIconFollowsConfigureScale(t *testing.T) {
	t.Parallel()
	small, large, worker := runningAppFirefoxIcons(t, runningAppIconSize, 2*runningAppIconSize)
	cfg := config.Default()
	cfg.Bar.Left, cfg.Bar.Center = nil, nil
	cfg.Bar.Right = []config.Item{{ID: "running-apps"}}
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	reg.trayIcons = worker
	reg.runningIndex = []runningAppEntry{{ID: "firefox", Icon: "firefox"}}
	cb, err := reg.NewHost(1, "DP-9")
	if err != nil {
		t.Fatal(err)
	}
	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{{ID: 1, AppID: "firefox"}}})

	tile := runningAppFirstTile(t, reg.bars[1])
	if tile == nil || len(tile.Children) != 1 || tile.Children[0].Image != small {
		t.Fatalf("before configure tile = %+v, want the logical-size raster", tile)
	}

	if err := cb.Configure(800, BarHeight, 240); err != nil {
		t.Fatal(err)
	}
	tile = runningAppFirstTile(t, reg.bars[1])
	if tile == nil || len(tile.Children) != 1 || tile.Children[0].Image != large {
		t.Fatalf("after scale-2 configure tile = %+v, want the physical-size raster", tile)
	}
}

func TestRunningAppsIconUsesACachedRaster(t *testing.T) {
	t.Parallel()
	img, worker := runningAppFirefoxIcon(t)
	cfg := config.Default()
	cfg.Bar.Left, cfg.Bar.Center = nil, nil
	cfg.Bar.Right = []config.Item{{ID: "running-apps"}}
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	reg.trayIcons = worker
	reg.runningIndex = []runningAppEntry{{ID: "firefox", Icon: "firefox"}}
	newHosts(t, reg, map[uint32]string{1: "DP-9"})
	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{{ID: 1, AppID: "firefox"}}})

	tile := runningAppFirstTile(t, reg.bars[1])
	if tile == nil || len(tile.Children) != 1 || tile.Children[0].Kind != ui.KindImage || tile.Children[0].Image != img {
		t.Fatalf("tile = %+v, want KindImage cache hit", tile)
	}
}

func TestRunningAppsIconArrivesAfterPaint(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeLauncherPNG(t, filepath.Join(root, "firefox.png"))
	cfg := config.Default()
	cfg.Bar.Left, cfg.Bar.Center = nil, nil
	cfg.Bar.Right = []config.Item{{ID: "running-apps"}}
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	worker := icons.NewWorker(icons.NewResolver("hicolor", []string{root}), reg.applyTrayIcon)
	go func() { _ = worker.Run(ctx) }()
	reg.trayIcons = worker
	reg.runningIndex = []runningAppEntry{{ID: "firefox", Icon: "firefox"}}
	newHosts(t, reg, map[uint32]string{1: "DP-9"})
	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{{ID: 1, AppID: "firefox"}}})
	bar := reg.bars[1]
	tileContent := func() (string, ui.Kind, *ui.Image) {
		bar.mu.Lock()
		defer bar.mu.Unlock()
		tile := runningAppFirstTile(t, bar)
		if tile == nil || len(tile.Children) != 1 || tile.Children[0] == nil {
			return "", 0, nil
		}
		return tileLetter(tile), tile.Children[0].Kind, tile.Children[0].Image
	}
	if letter, _, _ := tileContent(); letter != "F" {
		t.Fatalf("before decode letter = %q, want F", letter)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, kind, image := tileContent()
		if kind == ui.KindImage && image != nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("tile stayed a letter after the raster landed")
}

func runningAppFirefoxIcon(t *testing.T) (*ui.Image, *icons.Worker) {
	t.Helper()
	small, _, worker := runningAppFirefoxIcons(t, runningAppIconSize)
	return small, worker
}

func runningAppFirefoxIcons(t *testing.T, sizes ...int) (small, large *ui.Image, worker *icons.Worker) {
	t.Helper()
	root := t.TempDir()
	writeLauncherPNG(t, filepath.Join(root, "firefox.png"))
	worker = icons.NewWorker(icons.NewResolver("hicolor", []string{root}), nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = worker.Run(ctx) }()
	imgs := make([]*ui.Image, len(sizes))
	for i, size := range sizes {
		key := icons.Square("firefox", size)
		if _, _, err := worker.Request(key); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if img, ok := worker.Lookup(key); ok && img != nil {
				imgs[i] = img
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if imgs[i] == nil {
			t.Fatalf("icon worker did not cache firefox at %d", size)
		}
	}
	small = imgs[0]
	if len(imgs) > 1 {
		large = imgs[1]
	}
	return small, large, worker
}

func runningAppFirstTile(t *testing.T, bar *Bar) *ui.Node {
	t.Helper()
	if bar == nil {
		t.Fatal("missing bar")
	}
	secs := bar.sections()
	if len(secs) < 3 || len(secs[2]) == 0 || secs[2][0] == nil {
		t.Fatalf("right section = %+v", secs)
	}
	row := secs[2][0]
	if len(row.Children) == 1 && row.Children[0] != nil && row.Children[0].Kind == ui.KindRow {
		row = row.Children[0]
	}
	if len(row.Children) == 0 {
		return nil
	}
	return row.Children[0]
}
