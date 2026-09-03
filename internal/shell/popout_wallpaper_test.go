package shell

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
)

// openWallpaperPanel brings the picker up on a 1920x1080 top-bar output with a
// service backed by a fake engine, so nothing here execs or touches a socket.
func openWallpaperPanel(t *testing.T, roots []string) (*Registry, *wallpaper.Service, []wayland.AuxRequest) {
	t.Helper()
	reg := newPanelRegistry(t)
	svc := wallpaper.NewService(wallpaper.ServiceConfig{
		Engine:     stubWallpaperEngine{},
		Settings:   wallpaper.Settings{Scale: "fill", Loop: true, FPS: 30, Hidden: wallpaper.HiddenNone},
		Connectors: []string{"DP-1", "DP-3"},
		Roots:      roots,
	})
	t.Cleanup(svc.Close)

	reg.mu.Lock()
	reg.wallpaperSvc = svc
	reg.mu.Unlock()
	go reg.relayWallpaper(svc)

	if err := reg.OpenPanel(PanelWallpaper, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	size := panelTargetSize(PanelWallpaper)
	if err := reqs[1].Open.Callbacks.Configure(size.W, size.H, 120); err != nil {
		t.Fatal(err)
	}
	return reg, svc, reqs
}

type stubWallpaperEngine struct{}

func (stubWallpaperEngine) Apply(wallpaper.Job, wallpaper.Settings) (string, error) { return "", nil }
func (stubWallpaperEngine) Restore(string, string) error                            { return nil }
func (stubWallpaperEngine) SetPaused(string, bool) error                            { return nil }
func (stubWallpaperEngine) Capabilities() wallpaper.Capabilities {
	return wallpaper.Capabilities{GSlapper: true, Static: "awww"}
}

func wallpaperHost(t *testing.T, reg *Registry) *PanelHost {
	t.Helper()
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelWallpaper]
	if h == nil {
		t.Fatal("no wallpaper panel host")
	}
	return h
}

func TestParsePanelNameWallpaper(t *testing.T) {
	t.Parallel()

	id, err := parsePanelName("wallpaper")
	if err != nil || id != PanelWallpaper {
		t.Fatalf("parsePanelName(wallpaper) = %v, %v", id, err)
	}
	if got := PanelWallpaper.String(); got != "wallpaper" {
		t.Fatalf("String() = %q", got)
	}
}

func TestWallpaperPanelGeometry(t *testing.T) {
	t.Parallel()

	if got := panelTargetSize(PanelWallpaper); got.W != 980 || got.H != 1100 {
		t.Fatalf("target size = %dx%d, want 980x1100", got.W, got.H)
	}

	reg, _, reqs := openWallpaperPanel(t, nil)
	panel := reqs[1].Open

	// 1100 does not fit under a 40px bar on a 1080 output, so the M4 clamp
	// shrinks it rather than letting it run off the screen (D2).
	reg.mu.Lock()
	gap, pad := reg.cfg.Panels.Gap, reg.cfg.Panels.Padding
	reg.mu.Unlock()
	anchor := 40 + gap
	wantH := 1080 - anchor - pad
	if int(panel.Height) != wantH {
		t.Fatalf("height = %d, want the clamped %d", panel.Height, wantH)
	}
	if int(panel.Width) != 980 {
		t.Fatalf("width = %d, want 980", panel.Width)
	}
}

func TestWallpaperPanelIsAFloatingExclusiveOverlay(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openWallpaperPanel(t, nil)
	h := wallpaperHost(t, reg)

	reg.mu.Lock()
	centred := h.place.CenterY
	reg.mu.Unlock()
	if !centred {
		t.Error("the picker floats like the launcher rather than hugging the bar")
	}
	if reqs[1].Open.Keyboard != keyboardExclusive {
		t.Errorf("keyboard = %d, want exclusive", reqs[1].Open.Keyboard)
	}
	if reqs[0].Open == nil || reqs[0].Open.ID != shieldSurfaceID(PanelWallpaper) {
		t.Errorf("first request = %+v, want the dismiss shield", reqs[0].Open)
	}
}

func TestWallpaperTreeShape(t *testing.T) {
	t.Parallel()

	reg, _, _ := openWallpaperPanel(t, nil)
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	var field, list *ui.Node
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		switch n.Kind {
		case ui.KindTextField:
			if field == nil {
				field = n
			}
		case ui.KindVirtualList:
			if list == nil {
				list = n
			}
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(h.root)

	if field == nil {
		t.Error("the picker has no search field")
	}
	if list == nil {
		t.Fatal("the grid must be a virtual list, not a page of buttons (D8)")
	}
	if list.ItemHeight <= 0 {
		t.Errorf("virtual list ItemHeight = %d, want a row height", list.ItemHeight)
	}
}

// seedWallpaperRoot writes four images and one video into a temp root.
func seedWallpaperRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"a.png", "b.png", "c.png", "d.png", "clip.mp4"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	return root
}

func wallpaperListNode(t *testing.T, h *PanelHost) *ui.Node {
	t.Helper()
	var found *ui.Node
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil || found != nil {
			return
		}
		if n.Kind == ui.KindVirtualList {
			found = n
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(h.root)
	if found == nil {
		t.Fatal("no virtual list")
	}
	return found
}

func TestWallpaperGridPacksFourTilesPerRow(t *testing.T) {
	t.Parallel()

	reg, _, _ := openWallpaperPanel(t, []string{seedWallpaperRoot(t)})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	list := wallpaperListNode(t, h)
	// Five media files over four columns is two rows.
	if list.ItemCount != 2 {
		t.Fatalf("ItemCount = %d, want ceil(5/4) = 2", list.ItemCount)
	}
	row := list.Item(0)
	if row == nil || len(row.Children) != 4 {
		t.Fatalf("first row has %d tiles, want 4", len(row.Children))
	}
	tile := row.Children[0]
	if tile.Kind != ui.KindCapsule {
		t.Fatalf("tile kind = %v, want a capsule so the radius comes from the theme", tile.Kind)
	}
	thumb := tile.Children[0].Children[0]
	if thumb.Kind != ui.KindImage || thumb.ImageW != wallpaperTileWidth || thumb.ImageH != wallpaperThumbH {
		t.Fatalf("thumb = %+v, want a %dx%d raster box", thumb, wallpaperTileWidth, wallpaperThumbH)
	}
}

func TestWallpaperArrowsMoveWithinAndBetweenRows(t *testing.T) {
	t.Parallel()

	reg, _, _ := openWallpaperPanel(t, []string{seedWallpaperRoot(t)})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	h.wallpaperSel = 0
	h.wallpaperKeyPress(reg, keyRight)
	if h.wallpaperSel != 1 {
		t.Fatalf("right = %d, want 1", h.wallpaperSel)
	}
	h.wallpaperKeyPress(reg, keyDown)
	if h.wallpaperSel != 5-1 && h.wallpaperSel != 1+wallpaperColumns {
		t.Fatalf("down = %d, want a row further on, clamped to the last tile", h.wallpaperSel)
	}
	h.wallpaperSel = 0
	h.wallpaperKeyPress(reg, keyLeft)
	if h.wallpaperSel != 0 {
		t.Fatalf("left at the start = %d, want a clamp to 0", h.wallpaperSel)
	}
}

func TestWallpaperEnterAppliesToTheSelectedOutput(t *testing.T) {
	t.Parallel()

	root := seedWallpaperRoot(t)
	reg, svc, _ := openWallpaperPanel(t, []string{root})
	h := wallpaperHost(t, reg)

	reg.mu.Lock()
	h.wallpaperOutput = "DP-1"
	h.wallpaperSel = 0
	first := wallpaperEntries(h)[0].Path
	h.wallpaperKeyPress(reg, keyEnter)
	reg.mu.Unlock()

	awaitAssignment(t, svc, "DP-1", first)
}

func TestWallpaperAllAppliesToEveryOutput(t *testing.T) {
	t.Parallel()

	root := seedWallpaperRoot(t)
	reg, svc, _ := openWallpaperPanel(t, []string{root})
	h := wallpaperHost(t, reg)

	reg.mu.Lock()
	h.wallpaperOutput = wallpaper.AllOutputs
	h.wallpaperSel = 0
	first := wallpaperEntries(h)[0].Path
	h.wallpaperKeyPress(reg, keyEnter)
	reg.mu.Unlock()

	awaitAssignment(t, svc, "DP-1", first)
	awaitAssignment(t, svc, "DP-3", first)
}

func awaitAssignment(t *testing.T, svc *wallpaper.Service, connector, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if svc.Snapshot().Assignments[connector].Path == path {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s never took %s (got %q)", connector, path, svc.Snapshot().Assignments[connector].Path)
}

func TestWallpaperSummaryIsReadBackFromTheSnapshot(t *testing.T) {
	t.Parallel()

	// The mixed summary and the n/m badge are counted off the snapshot, so a
	// different snapshot produces a different string rather than a stale one.
	snap := wallpaper.Snapshot{
		Connectors: []string{"DP-1", "DP-3"},
		Assignments: map[string]wallpaper.Assignment{
			"DP-1": {Kind: wallpaper.KindImage, Path: "/w/a.png"},
			"DP-3": {Kind: wallpaper.KindVideo, Path: "/w/b.mp4"},
		},
		Runtime: map[string]wallpaper.Runtime{},
	}
	if got := wallpaperSummary(snap, wallpaper.AllOutputs); got != "2 outputs · 1 video · 1 image" {
		t.Fatalf("summary = %q", got)
	}

	snap.Assignments["DP-3"] = wallpaper.Assignment{Kind: wallpaper.KindImage, Path: "/w/a.png"}
	if got := wallpaperSummary(snap, wallpaper.AllOutputs); got != "2 outputs · 0 video · 2 image" {
		t.Fatalf("summary after reassign = %q", got)
	}

	h := &PanelHost{wallpaperSnap: snap, wallpaperOutput: wallpaper.AllOutputs}
	matched, total := wallpaperMatchCount(h, "/w/a.png")
	if matched != 2 || total != 2 {
		t.Fatalf("match count = %d/%d, want 2/2", matched, total)
	}
	snap.Assignments["DP-3"] = wallpaper.Assignment{Kind: wallpaper.KindVideo, Path: "/w/b.mp4"}
	if matched, total = wallpaperMatchCount(h, "/w/a.png"); matched != 1 || total != 2 {
		t.Fatalf("match count = %d/%d, want 1/2", matched, total)
	}
}

func TestWallpaperRestoreEnqueuesRestore(t *testing.T) {
	t.Parallel()

	root := seedWallpaperRoot(t)
	reg, svc, _ := openWallpaperPanel(t, []string{root})
	h := wallpaperHost(t, reg)

	reg.mu.Lock()
	h.wallpaperOutput = "DP-1"
	h.wallpaperSel = 0
	first := wallpaperEntries(h)[0].Path
	h.wallpaperKeyPress(reg, keyEnter)
	reg.mu.Unlock()
	awaitAssignment(t, svc, "DP-1", first)

	reg.mu.Lock()
	h.wallpaperRestore(reg)
	reg.mu.Unlock()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if svc.Snapshot().Runtime["DP-1"].State == wallpaper.StateStatic {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("restore never put the output back on the static fallback")
}

func TestWallpaperApplyUpdatesTheThemeSeed(t *testing.T) {
	t.Parallel()

	root := seedWallpaperRoot(t)
	reg, svc, _ := openWallpaperPanel(t, []string{root})

	var mu sync.Mutex
	var gotSource, gotSeed string
	reg.mu.Lock()
	reg.wallpaperSvc.SetConfigHook(func(source, seed string) {
		mu.Lock()
		defer mu.Unlock()
		gotSource, gotSeed = source, seed
	})
	h := reg.panelHosts[PanelWallpaper]
	h.wallpaperOutput = "DP-1"
	h.wallpaperSel = 0
	first := wallpaperEntries(h)[0].Path
	h.wallpaperKeyPress(reg, keyEnter)
	reg.mu.Unlock()

	awaitAssignment(t, svc, "DP-1", first)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		source, seed := gotSource, gotSeed
		mu.Unlock()
		if source == "wallpaper" && seed == first {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("theme seed hook never saw the applied image (source=%q seed=%q)", gotSource, gotSeed)
}
