package shell

import (
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
)

// openWallpaperPanel brings the picker up on a 1920x1080 top-bar output with a
// service backed by a fake engine, so nothing here execs or touches a socket.
func openWallpaperPanel(t *testing.T, roots []string) (*Registry, *wallpaper.Service, []wayland.AuxRequest) {
	t.Helper()
	return wallpaperPanel(t, roots, true)
}

// wallpaperPanel opens the picker. With relay false the snapshot relay never
// starts, so a caller can prove something about a wallpaper state change
// without an asynchronous rebuild racing its assertion.
func wallpaperPanel(t *testing.T, roots []string, relay bool) (*Registry, *wallpaper.Service, []wayland.AuxRequest) {
	t.Helper()
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
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
	if relay {
		go reg.relayWallpaper(svc)
	}

	if err := reg.OpenPanel(PanelWallpaper, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	// The compositor configures the surface at the size the spec asked for,
	// which carries the joints beside the body.
	open := reqs[1].Open
	if err := open.Callbacks.Configure(int(open.Width), int(open.Height), 120); err != nil {
		t.Fatal(err)
	}
	return reg, svc, reqs
}

type stubWallpaperEngine struct{}

func (stubWallpaperEngine) AdvanceGeneration(string, uint64)                        {}
func (stubWallpaperEngine) Apply(wallpaper.Job, wallpaper.Settings) (string, error) { return "", nil }
func (stubWallpaperEngine) Restore(string, string) error                            { return nil }
func (stubWallpaperEngine) SetPaused(string, bool) error                            { return nil }
func (stubWallpaperEngine) Capabilities() wallpaper.Capabilities {
	return wallpaper.Capabilities{GSlapper: true, Statics: []string{"awww"}}
}
func (e stubWallpaperEngine) RefreshTerminalCatalog() wallpaper.Capabilities { return e.Capabilities() }

type wallpaperRestoreProbe struct {
	stubWallpaperEngine
	restored chan string
}

func (e *wallpaperRestoreProbe) Restore(connector, _ string) error {
	e.restored <- connector
	return nil
}

func (wallpaperRestoreProbe) Capabilities() wallpaper.Capabilities {
	return wallpaper.Capabilities{
		Terminal: true,
		Catalog: wallpaper.Catalog{
			Effects: []wallpaper.EffectInfo{{ID: "fire"}},
			Themes:  []string{"nord"},
		},
	}
}
func (e wallpaperRestoreProbe) RefreshTerminalCatalog() wallpaper.Capabilities {
	return e.Capabilities()
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
	// Attached, it tucks 1 px under the opaque bar and keeps no gap.
	reg.mu.Lock()
	pad := reg.cfg.Panels.Padding
	reg.mu.Unlock()
	anchor := 40 - 1
	wantH := 1080 - anchor - pad
	if int(panel.Height) != wantH {
		t.Fatalf("height = %d, want the clamped %d", panel.Height, wantH)
	}
	if int(panel.Width) != 980+2*12 {
		t.Fatalf("width = %d, want 980 plus a 12 px joint each side", panel.Width)
	}
}

func TestWallpaperPanelIsAnAttachedExclusiveOverlay(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openWallpaperPanel(t, nil)
	h := wallpaperHost(t, reg)

	reg.mu.Lock()
	attached := h.place.Attached()
	reg.mu.Unlock()
	if !attached {
		t.Error("the picker floats; it attaches to the bar like every panel but the floating three")
	}
	if reqs[1].Open.Keyboard != keyboardExclusive {
		t.Errorf("keyboard = %d, want exclusive", reqs[1].Open.Keyboard)
	}
	if reqs[0].Open == nil || reqs[0].Open.ID != panelShieldSurfaceID(7) {
		t.Errorf("first request = %+v, want the dismiss shield", reqs[0].Open)
	}
}

func TestWallpaperTreeShape(t *testing.T) {
	t.Parallel()

	// A seeded library: an empty one deliberately shows an explanation in
	// place of the grid, which is a different shape.
	reg, _, _ := openWallpaperPanel(t, []string{seedWallpaperRoot(t)})
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
	// Until a thumbnail decodes, the tile keeps the same box and shows the
	// kind glyph rather than leaving a hole in the grid (D6).
	thumb := wallpaperTileThumb(tile)
	switch thumb.Kind {
	case ui.KindImage:
		if g := wallpaperGridOf(h); thumb.ImageW != g.thumbW || thumb.ImageH != g.thumbH {
			t.Fatalf("raster box = %dx%d, want %dx%d", thumb.ImageW, thumb.ImageH, g.thumbW, g.thumbH)
		}
	case ui.KindStack, ui.KindRow:
		// The placeholder holds the tile's box so a late preview cannot reflow
		// the grid. It carries a glyph only where the embedded icon subset has
		// one, which for media it does not.
		if g := wallpaperGridOf(h); thumb.Width != g.thumbW || thumb.Height != g.thumbH {
			t.Fatalf("placeholder box = %dx%d, want %dx%d", thumb.Width, thumb.Height, g.thumbW, g.thumbH)
		}
		for _, c := range thumb.Children {
			if c.Kind == ui.KindIcon && !render.ValidMaterialIcon(c.Icon) {
				t.Fatalf("placeholder glyph %q is not in the subset", c.Icon)
			}
		}
	default:
		t.Fatalf("thumb kind = %v, want an image or its placeholder", thumb.Kind)
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
	first := wallpaperMedia(h)[0].Path
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
	first := wallpaperMedia(h)[0].Path
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
	png := wallpaper.Entry{Path: "/w/a.png"}
	matched, total := wallpaperMatchCount(h, png)
	if matched != 2 || total != 2 {
		t.Fatalf("match count = %d/%d, want 2/2", matched, total)
	}
	snap.Assignments["DP-3"] = wallpaper.Assignment{Kind: wallpaper.KindVideo, Path: "/w/b.mp4"}
	if matched, total = wallpaperMatchCount(h, png); matched != 1 || total != 2 {
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
	first := wallpaperMedia(h)[0].Path
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

func TestWallpaperRestoreLeavesEffectsToTerminalArt(t *testing.T) {
	engine := &wallpaperRestoreProbe{restored: make(chan string, 1)}
	reg, svc := artRegistry(t, engine, "DP-1")
	svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: "DP-1", Kind: wallpaper.KindEffect, Effect: "fire", Theme: "nord"})
	awaitArt(t, svc, "DP-1", func(a wallpaper.Assignment) bool { return a.Kind == wallpaper.KindEffect })

	if err := reg.OpenPanel(PanelWallpaper, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	drainAuxQueue(reg)
	reg.mu.Lock()
	h := reg.panelHosts[PanelWallpaper]
	h.wallpaperOutput = "DP-1"
	reg.rebuildPanel(h)
	restore := findAction(h.root, "wallpaper-restore")
	if restore == nil || restore.State&ui.StateDisabled == 0 || !strings.Contains(restore.Tooltip, "Terminal Art") {
		reg.mu.Unlock()
		t.Fatalf("Restore = %+v; Terminal Art effects must route to their own panel", restore)
	}
	h.wallpaperRestore(reg) // a stale action must not blank an effect without a prior still
	reg.mu.Unlock()

	select {
	case connector := <-engine.restored:
		t.Fatalf("Wallpaper panel restored effect on %s", connector)
	case <-time.After(100 * time.Millisecond):
	}
	if got := svc.Snapshot().Runtime["DP-1"].State; got != wallpaper.StatePlaying {
		t.Fatalf("effect runtime = %v, want it still playing", got)
	}
}

func TestWallpaperPlaybackStateRequiresPlayableVideo(t *testing.T) {
	for _, tc := range []struct {
		name      string
		states    []wallpaper.State
		wantPause bool
		want      bool
	}{
		{name: "playing", states: []wallpaper.State{wallpaper.StatePlaying}, want: true},
		{name: "paused", states: []wallpaper.State{wallpaper.StatePaused}, wantPause: true, want: true},
		{name: "restored still", states: []wallpaper.State{wallpaper.StateStatic}},
		{name: "failed", states: []wallpaper.State{wallpaper.StateError}},
		{name: "mixed playing and paused", states: []wallpaper.State{wallpaper.StatePlaying, wallpaper.StatePaused}, want: true},
		{name: "mixed paused and restored", states: []wallpaper.State{wallpaper.StatePaused, wallpaper.StateStatic}, wantPause: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			connectors := make([]string, len(tc.states))
			snap := wallpaper.Snapshot{
				Assignments: make(map[string]wallpaper.Assignment, len(tc.states)),
				Runtime:     make(map[string]wallpaper.Runtime, len(tc.states)),
			}
			for i, state := range tc.states {
				connector := fmt.Sprintf("DP-%d", i+1)
				connectors[i] = connector
				snap.Assignments[connector] = wallpaper.Assignment{Kind: wallpaper.KindVideo, Path: "/w/video.mp4"}
				snap.Runtime[connector] = wallpaper.Runtime{State: state}
			}
			snap.Connectors = connectors
			h := &PanelHost{wallpaperSnap: snap, wallpaperOutput: wallpaper.AllOutputs}
			paused, ok := wallpaperPlaybackState(h)
			if paused != tc.wantPause || ok != tc.want {
				t.Fatalf("wallpaperPlaybackState = (%v, %v), want (%v, %v)", paused, ok, tc.wantPause, tc.want)
			}
		})
	}
}

func TestWallpaperPauseRefreshesStaleSnapshot(t *testing.T) {
	reg, svc, _ := wallpaperPanel(t, nil, false)
	svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: "DP-1", Kind: wallpaper.KindImage, Path: "/w/still.png"})
	awaitArt(t, svc, "DP-1", func(a wallpaper.Assignment) bool { return a.Kind == wallpaper.KindImage })
	reg.mu.Lock()
	h := reg.panelHosts[PanelWallpaper]
	h.wallpaperOutput = "DP-1"
	h.wallpaperSnap = wallpaper.Snapshot{
		Connectors: []string{"DP-1"},
		Assignments: map[string]wallpaper.Assignment{
			"DP-1": {Kind: wallpaper.KindVideo, Path: "/w/old.mp4"},
		},
		Runtime: map[string]wallpaper.Runtime{"DP-1": {State: wallpaper.StatePlaying}},
	}
	h.wallpaperSetPaused(reg, true)
	if got := h.wallpaperSnap.Assignments["DP-1"].Kind; got != wallpaper.KindImage {
		reg.mu.Unlock()
		t.Fatalf("Pause used stale assignment kind %v", got)
	}
	if findAction(h.root, "wallpaper-pause") != nil || findAction(h.root, "wallpaper-resume") != nil {
		reg.mu.Unlock()
		t.Fatal("the stale playback control remained after refreshing to a still")
	}
	reg.mu.Unlock()
	if got := svc.Snapshot().Runtime["DP-1"].State; got != wallpaper.StateStatic {
		t.Fatalf("stale pause changed still state to %v", got)
	}
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
	first := wallpaperMedia(h)[0].Path
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

func TestWallpaperBarItemIsKnownButNotDefault(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	for _, zone := range [][]config.Item{cfg.Bar.Left, cfg.Bar.Center, cfg.Bar.Right} {
		for _, item := range zone {
			if item.ID == "wallpaper" {
				t.Fatal("the default bar layout must not change; the glyph is opt-in")
			}
		}
	}
	if _, err := config.Parse([]byte(`{"bar":{"items":{"right":[{"id":"wallpaper"}]}}}`)); err != nil {
		t.Fatalf("a configured wallpaper item must load: %v", err)
	}
}

func TestWallpaperHotplugReplaysAndKeepsAssignments(t *testing.T) {
	t.Parallel()

	root := seedWallpaperRoot(t)
	reg, svc, _ := openWallpaperPanel(t, []string{root})
	h := wallpaperHost(t, reg)

	reg.mu.Lock()
	h.wallpaperOutput = "DP-3"
	h.wallpaperSel = 0
	first := wallpaperMedia(h)[0].Path
	h.wallpaperKeyPress(reg, keyEnter)
	reg.mu.Unlock()
	awaitAssignment(t, svc, "DP-3", first)

	// The output goes away: the assignment survives, the runtime does not.
	reg.wallpaperOutputGone("DP-3")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap := svc.Snapshot()
		if !slices.Contains(snap.Connectors, "DP-3") {
			if snap.Assignments["DP-3"].Path != first {
				t.Fatalf("disconnect dropped the assignment: %q", snap.Assignments["DP-3"].Path)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if slices.Contains(svc.Snapshot().Connectors, "DP-3") {
		t.Fatal("disconnect never reached the service")
	}

	// It comes back: the saved assignment is replayed.
	reg.wallpaperOutputConnected("DP-3")
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if slices.Contains(svc.Snapshot().Connectors, "DP-3") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("reconnect never reached the service")
}

func TestWallpaperUnknownOutputStaysUntouched(t *testing.T) {
	t.Parallel()

	root := seedWallpaperRoot(t)
	reg, svc, _ := openWallpaperPanel(t, []string{root})

	reg.wallpaperOutputConnected("HDMI-A-1")
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, ok := svc.Snapshot().Assignments["HDMI-A-1"]; ok {
			t.Fatal("a newly seen output must stay untouched until the user assigns (D20)")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// findAction returns the first node carrying action.
func findAction(n *ui.Node, action string) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Action == action {
		return n
	}
	for _, c := range n.Children {
		if got := findAction(c, action); got != nil {
			return got
		}
	}
	return nil
}

func collectActions(n *ui.Node, prefix string, out *[]string) {
	if n == nil {
		return
	}
	if strings.HasPrefix(n.Action, prefix) {
		*out = append(*out, n.Action)
	}
	for _, c := range n.Children {
		collectActions(c, prefix, out)
	}
}

func TestWallpaperChromeHasEveryControl(t *testing.T) {
	t.Parallel()

	root := seedWallpaperRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "deep.png"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	reg, _, _ := openWallpaperPanel(t, []string{root})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	for _, action := range []string{"wallpaper-close", "wallpaper-restore"} {
		if findAction(h.root, action) == nil {
			t.Errorf("chrome is missing %s", action)
		}
	}

	var outputs []string
	collectActions(h.root, "wallpaper-output:", &outputs)
	if len(outputs) != 3 {
		t.Errorf("output select = %v, want All plus each connector", outputs)
	}

	var filters []string
	collectActions(h.root, "wallpaper-filter:", &filters)
	if len(filters) != 3 {
		t.Errorf("kind filter = %v, want All/Images/Videos", filters)
	}

	// Child directories are the folder dropdown's options.
	if got := len(wallpaperDirs(h)); got != 1 {
		t.Errorf("folder dropdown holds %d entries, want the one child directory", got)
	}
	if findAction(h.root, "wallpaper-menu:folder") == nil {
		t.Error("the picker must offer a folder dropdown")
	}

	// Up only exists once the picker has descended out of a root.
	if findAction(h.root, "wallpaper-up") != nil {
		t.Error("Up must not show at a library root")
	}
	h.wallpaperDir = filepath.Join(root, "nested")
	reg.rebuildPanel(h)
	if findAction(h.root, "wallpaper-up") == nil {
		t.Error("Up must show once nested")
	}
}

func TestWallpaperChromeActionsDrivePanelState(t *testing.T) {
	t.Parallel()

	root := seedWallpaperRoot(t)
	reg, _, _ := openWallpaperPanel(t, []string{root})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	if !h.wallpaperAction(reg, findAction(h.root, "wallpaper-output:DP-1")) {
		t.Fatal("the output select did not handle its own action")
	}
	if h.wallpaperOutput != "DP-1" {
		t.Errorf("output = %q, want DP-1", h.wallpaperOutput)
	}

	videos := findAction(h.root, fmt.Sprintf("wallpaper-filter:%d", wallpaper.FilterVideos))
	if videos == nil || !h.wallpaperAction(reg, videos) {
		t.Fatal("the kind filter did not handle its own action")
	}
	if h.wallpaperFilter != wallpaper.FilterVideos {
		t.Errorf("filter = %v, want videos", h.wallpaperFilter)
	}
	for _, e := range wallpaperMedia(h) {
		if e.Kind != wallpaper.KindVideo {
			t.Fatalf("the videos filter still shows %s", e.Name)
		}
	}
}

func TestWallpaperTitleOffersRefresh(t *testing.T) {
	if findAction(wallpaperHeader(&PanelHost{id: PanelWallpaper}, 900), "wallpaper-refresh") == nil {
		t.Fatal("wallpaper title has no refresh action")
	}
}

func TestWallpaperRefreshActionRescansLibrary(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "before.png"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	reg := newPanelRegistry(t)
	svc := wallpaper.NewService(wallpaper.ServiceConfig{
		Engine:     stubWallpaperEngine{},
		Connectors: []string{"DP-1"},
		Roots:      []string{root},
	})
	t.Cleanup(svc.Close)
	reg.mu.Lock()
	reg.wallpaperSvc = svc
	h := &PanelHost{id: PanelWallpaper}
	if err := os.WriteFile(filepath.Join(root, "after.png"), []byte("x"), 0o644); err != nil {
		reg.mu.Unlock()
		t.Fatalf("add wallpaper: %v", err)
	}
	if !h.wallpaperAction(reg, &ui.Node{Action: "wallpaper-refresh"}) {
		reg.mu.Unlock()
		t.Fatal("refresh action was not handled")
	}
	reg.mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snap := svc.Snapshot()
		if slices.ContainsFunc(snap.Library.View(root, wallpaper.FilterAll, ""), func(e wallpaper.Entry) bool {
			return e.Name == "after.png"
		}) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("refresh did not rescan the library")
}

func TestWallpaperSelectionFallsBackWhenOutputDisconnects(t *testing.T) {
	snap := wallpaper.Snapshot{Connectors: []string{"DP-1"}}
	if got := wallpaperOutputSelection(snap, "DP-3"); got != wallpaper.AllOutputs {
		t.Fatalf("stale output selection = %q, want %q", got, wallpaper.AllOutputs)
	}
}

func TestWallpaperRejectsStaleOutputAction(t *testing.T) {
	reg, svc, _ := openWallpaperPanel(t, nil)
	h := wallpaperHost(t, reg)
	svc.Enqueue(wallpaper.Command{Op: wallpaper.OpDisconnect, Token: "DP-3"})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && slices.Contains(svc.Snapshot().Connectors, "DP-3") {
		time.Sleep(5 * time.Millisecond)
	}
	if slices.Contains(svc.Snapshot().Connectors, "DP-3") {
		t.Fatal("test output did not disconnect")
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()
	if !h.wallpaperAction(reg, &ui.Node{Action: "wallpaper-output:DP-3"}) {
		t.Fatal("stale output action was not consumed")
	}
	if h.wallpaperOutput != wallpaper.AllOutputs {
		t.Fatalf("stale output action selected %q, want all", h.wallpaperOutput)
	}
}

func TestWallpaperVideoTileIsInertWithoutGSlapper(t *testing.T) {
	t.Parallel()

	root := seedWallpaperRoot(t)
	reg, _, _ := openWallpaperPanel(t, []string{root})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	snap := h.wallpaperSnap
	snap.Caps = wallpaper.Capabilities{GSlapper: false, Statics: []string{"awww"}}
	h.wallpaperSnap = snap
	reg.rebuildPanel(h)

	var video wallpaper.Entry
	for _, e := range wallpaperMedia(h) {
		if e.Kind == wallpaper.KindVideo {
			video = e
		}
	}
	if video.Path == "" {
		t.Fatal("fixture has no video")
	}
	if wallpaperCanApply(h, video) {
		t.Error("a video tile must be inert without gslapper")
	}
	tile := findAction(h.root, "wallpaper-tile")
	if tile == nil {
		t.Fatal("no tiles")
	}
	// The engine strip is what tells the user why: gSlapper has no pill, the
	// installed fallback still does.
	labels := wallpaperEngineLabels(h.root)
	if slices.Contains(labels, "gSlapper") {
		t.Errorf("engine pills %v name gSlapper, which is not installed", labels)
	}
	if !slices.Contains(labels, "awww") {
		t.Errorf("engine pills %v omit the installed awww", labels)
	}
}

// wallpaperEngineLabels reads the engine strip's pills back out of the tree.
// wallpaperEngineLabels lists the engine readouts: text with Role "status".
func wallpaperEngineLabels(root *ui.Node) []string {
	var out []string
	walkNodes(root, func(n *ui.Node) {
		if n.Role == "status" && n.Kind == ui.KindText && n.Text != "" {
			out = append(out, n.Text)
		}
	})
	return out
}

// Every installed engine gets a pill, in the order they are reached.
// The footer names the one engine painting the selected outputs. It used to
// be a row of pills for every installed engine, which read as a choice.
func TestWallpaperFooterNamesOneEngine(t *testing.T) {
	t.Parallel()

	root := seedWallpaperRoot(t)
	reg, _, _ := openWallpaperPanel(t, []string{root})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	footer := func() *ui.Node { return h.root.Children[len(h.root.Children)-1] }
	h.wallpaperSnap.Caps = wallpaper.Capabilities{GSlapper: true, Statics: []string{"awww", "swaybg"}}
	reg.rebuildPanel(h)
	if got := wallpaperEngineLabels(footer()); !slices.Equal(got, []string{"gSlapper"}) {
		t.Errorf("footer engine = %v, want only the engine that would paint", got)
	}
	if findAction(footer(), "wallpaper-menu:palette") == nil {
		t.Error("the shell theme combo belongs in the footer")
	}

	h.wallpaperSnap.Caps = wallpaper.Capabilities{}
	reg.rebuildPanel(h)
	engine := findNode(footer(), func(n *ui.Node) bool { return n.Role == "status" })
	if engine == nil || engine.Text != "no wallpaper engine installed" || engine.Tone != ui.ToneError {
		t.Errorf("no engine: footer engine = %+v", engine)
	}
}

func TestWallpaperWarnsWhenAForeignSurfaceOwnsTheOutput(t *testing.T) {
	t.Parallel()

	root := seedWallpaperRoot(t)
	reg, _, _ := openWallpaperPanel(t, []string{root})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	h.wallpaperOutput = "DP-1"
	snap := h.wallpaperSnap
	snap.Covered = map[string]string{"DP-1": "quickshell"}
	h.wallpaperSnap = snap
	reg.rebuildPanel(h)

	var warned bool
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindText && strings.Contains(n.Text, "quickshell") && strings.Contains(n.Text, "not be visible") {
			warned = true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(h.root)
	if !warned {
		t.Fatal("an output already painted by another wallpaper must say so; gslapper reports playing either way")
	}

	// An output nobody else owns says nothing.
	h.wallpaperOutput = "DP-3"
	reg.rebuildPanel(h)
	warned = false
	walk(h.root)
	if warned {
		t.Error("an uncovered output must not warn")
	}
}

func TestWallpaperReportsPreviewGeneration(t *testing.T) {
	t.Parallel()

	root := seedWallpaperRoot(t)
	reg, _, _ := openWallpaperPanel(t, []string{root})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	hasText := func(want string) bool {
		found := false
		var walk func(*ui.Node)
		walk = func(n *ui.Node) {
			if n == nil {
				return
			}
			if n.Kind == ui.KindText && strings.Contains(n.Text, want) {
				found = true
			}
			for _, c := range n.Children {
				walk(c)
			}
		}
		walk(h.root)
		return found
	}

	snap := h.wallpaperSnap
	snap.ThumbsDone, snap.ThumbsTotal = 12, 645
	h.wallpaperSnap = snap
	reg.rebuildPanel(h)
	bar := findNode(h.root, func(n *ui.Node) bool { return n.Role == "progressbar" })
	if bar == nil || bar.Name != "Generating previews" || bar.Value != 12 || bar.Max != 645 {
		t.Errorf("progress bar = %+v, want 12 of 645", bar)
	}
	if !hasText("12/645") || !hasText("\u2591") {
		t.Error("a library still generating previews must draw its block bar and count")
	}

	// The open folder's own progress leads; the library's follows.
	snap.ThumbsFolder, snap.ThumbsFolderDone, snap.ThumbsFolderTotal = h.wallpaperDir, 3, 5
	h.wallpaperSnap = snap
	reg.rebuildPanel(h)
	if !hasText("3/5") || !hasText("library 12/645") {
		t.Error("the open folder's progress must lead, with the library's after it")
	}
	if bar := findNode(h.root, func(n *ui.Node) bool { return n.Role == "progressbar" }); bar == nil || bar.Value != 3 || bar.Max != 5 {
		t.Errorf("progress bar = %+v, want the folder's 3 of 5", bar)
	}

	// Finished generation says nothing at all.
	snap.ThumbsDone = 645
	h.wallpaperSnap = snap
	reg.rebuildPanel(h)
	if findNode(h.root, func(n *ui.Node) bool { return n.Role == "progressbar" }) != nil {
		t.Error("a finished library must not keep reporting progress")
	}

	// An apply in flight is visible.
	snap.Runtime = map[string]wallpaper.Runtime{"DP-1": {State: wallpaper.StateStarting}}
	h.wallpaperOutput = "DP-1"
	h.wallpaperSnap = snap
	reg.rebuildPanel(h)
	if !hasText("Applying wallpaper") {
		t.Error("an apply in flight must be visible")
	}
}

// An empty grid reads as a broken picker unless it says why and offers the
// one action that gets out of it.
func TestWallpaperEmptyStatesOfferAnAction(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, _, _ := openWallpaperPanel(t, []string{root})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	state := func() (string, string) {
		t.Helper()
		n := wallpaperEmptyState(reg, h)
		text, action := "", ""
		walkNodes(n, func(c *ui.Node) {
			if c.Kind == ui.KindText && text == "" {
				text = c.Text
			}
			if c.Action != "" {
				action = c.Action
			}
		})
		return text, action
	}

	h.search = ui.NewField("zzzz")
	if text, action := state(); !strings.Contains(text, `match "zzzz"`) || action != "wallpaper-clear-search" {
		t.Errorf("search: %q / %q", text, action)
	}
	if !h.wallpaperAction(reg, &ui.Node{Action: "wallpaper-clear-search"}) || wallpaperSearch(h) != "" {
		t.Error("Clear search did not clear the field")
	}

	h.wallpaperFilter = wallpaper.FilterVideos
	if text, action := state(); !strings.Contains(text, "No videos") || action != "wallpaper-filter:0" {
		t.Errorf("filtered: %q / %q", text, action)
	}
	h.wallpaperFilter = wallpaper.FilterAll

	h.wallpaperDir = filepath.Join(root, "empty")
	if text, action := state(); !strings.Contains(text, "No images or videos in empty") || action != "wallpaper-up" {
		t.Errorf("empty folder: %q / %q", text, action)
	}

	h.wallpaperSnap.Library = wallpaper.Scan([]string{filepath.Join(root, "empty")})
	h.wallpaperDir = filepath.Join(root, "empty")
	if _, action := state(); action != "wallpaper-library-settings" {
		t.Errorf("empty root: action %q, want Library settings", action)
	}

	h.wallpaperSnap.Library = nil
	if text, action := state(); !strings.Contains(text, "Indexing") || action != "" ||
		findNode(wallpaperEmptyState(reg, h), func(n *ui.Node) bool { return n.Kind == ui.KindSpinner }) == nil {
		t.Errorf("indexing: %q / %q, want a spinner and no action", text, action)
	}
}

func TestWallpaperLibrarySettingsOpensSettings(t *testing.T) {
	reg, _, _ := openWallpaperPanel(t, []string{t.TempDir()})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if !h.wallpaperAction(reg, &ui.Node{Action: "wallpaper-library-settings"}) {
		t.Fatal("Library settings was not handled")
	}
	settings := reg.panelHosts[PanelSettings]
	if reg.panelHosts[PanelWallpaper] != nil || settings == nil || settings.section != "Wallpaper" {
		t.Fatalf("wallpaper open=%v settings=%+v; want Settings at Wallpaper", reg.panelHosts[PanelWallpaper] != nil, settings)
	}
}

func TestWallpaperManyDirectoriesDoNotOverflowTheChrome(t *testing.T) {
	t.Parallel()

	// A real library has dozens of subdirectories. Laying those out as chips in
	// one row failed layout outright and closed the panel:
	//   ui: child 8 of kind 3 does not fit in 948x40
	root := t.TempDir()
	for i := range 40 {
		if err := os.MkdirAll(filepath.Join(root, fmt.Sprintf("collection-%02d", i)), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "a.png"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	reg, _, _ := openWallpaperPanel(t, []string{root})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	// The panel must lay out at its real size without erroring.
	if err := ui.LayoutColumn(h.root, ui.Rect{W: h.place.Panel.W, H: h.place.Panel.H}, h.measureText()); err != nil {
		t.Fatalf("layout failed with %d directories: %v", 40, err)
	}
	if got := len(wallpaperMedia(h)); got != 1 {
		t.Fatalf("grid holds %d tiles, want only the image", got)
	}
	if got := len(wallpaperDirs(h)); got != 40 {
		t.Fatalf("folder dropdown holds %d entries, want 40", got)
	}
	// The open list is capped, so a large library scrolls inside its own box
	// rather than pushing the grid off the panel.
	h.wallpaperMenu = "folder"
	list := wallpaperOptionList(h, wallpaperFolderOptions(h))
	if list == nil {
		t.Fatal("no folder option list")
	}
	if list.ItemCount < 40 {
		t.Fatalf("list offers %d options, want every folder", list.ItemCount)
	}
	if list.Height > wallpaperOptionMaxRow*wallpaperOptionRowH {
		t.Fatalf("list is %dpx tall, want at most %d",
			list.Height, wallpaperOptionMaxRow*wallpaperOptionRowH)
	}
}

func TestWallpaperFolderDropdownReachesEveryRoot(t *testing.T) {
	t.Parallel()

	// A second library root -- the video directory -- is only reachable if it
	// is offered somewhere. It used to have a strip of its own; it is now an
	// option in the folder dropdown alongside the current folder's children.
	one := seedWallpaperRoot(t)
	two := seedWallpaperRoot(t)
	reg, _, _ := openWallpaperPanel(t, []string{one, two})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	opts := wallpaperFolderOptions(h)
	for _, root := range []string{one, two} {
		if !slices.ContainsFunc(opts, func(o wallpaperOption) bool {
			return o.action == "wallpaper-dir:"+root
		}) {
			t.Errorf("root %s is not reachable from the folder dropdown", root)
		}
	}
}

func TestWallpaperOnlyNamesIconsTheSubsetCarries(t *testing.T) {
	t.Parallel()

	// An icon the embedded subset does not hold fails the whole surface at
	// render time and closes the panel. Live testing caught "folder" that way;
	// this catches the next one here instead.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"a.png", "clip.mp4"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	reg, _, _ := openWallpaperPanel(t, []string{root})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	var bad []string
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindIcon && n.Icon != "" && !render.ValidMaterialIcon(n.Icon) {
			bad = append(bad, n.Icon)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(h.root)
	// The grid's rows are built on demand, so check the tiles too.
	for _, entry := range wallpaperMedia(h) {
		walk(wallpaperTile(reg, h, wallpaperGridOf(h), entry, 0))
	}
	if len(bad) > 0 {
		t.Fatalf("icons not in the embedded subset: %v (have %v)", bad, render.MaterialIconNames())
	}
}

// The picker is 1100 tall by design and a laptop panel is not. The grid takes
// whatever the chrome leaves, so on a short output every row above it has to be
// paid for out of the panel that was actually granted -- otherwise the grid runs
// past the bottom edge and eats the item count. Caught live on a 1536x864
// output, where the count row never appeared.
func TestWallpaperColumnFitsAShortPanel(t *testing.T) {
	t.Parallel()

	root := seedWallpaperRoot(t)
	reg, _, _ := openWallpaperPanel(t, []string{root})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	// A 1536x864 logical output leaves roughly this much after the bar.
	const short = 802
	h.place.Panel.H = short
	reg.rebuildPanel(h)

	measure := h.measureText()
	box := ui.Rect{W: h.place.Panel.W, H: short}
	if err := ui.LayoutColumn(h.root, box, measure); err != nil {
		t.Fatalf("layout: %v", err)
	}
	last := h.root.Children[len(h.root.Children)-1]
	if bottom := last.Bounds.Y + last.Bounds.H; bottom > short {
		t.Errorf("last row ends at %d, past the %d-tall panel", bottom, short)
	}
}

// The grid's tiles keep visible gaps on both axes, hold their thumbnail inside
// the tile padding, and use the width beside the scrollbar. The rows used to
// touch because the tile stretched to the whole row pitch, and the 210 px
// image sat in a 210 px tile with 4 px padding (sysc-1066).
func TestWallpaperTileGeometry(t *testing.T) {
	for _, width := range []int{980, 760} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			t.Parallel()
			reg, _, _ := openWallpaperPanel(t, []string{seedWallpaperRoot(t)})
			h := wallpaperHost(t, reg)
			reg.mu.Lock()
			defer reg.mu.Unlock()
			h.place.Panel.W = width
			reg.rebuildPanel(h)
			if err := ui.LayoutColumn(h.root, ui.Rect{W: width, H: h.place.Panel.H}, h.measureText()); err != nil {
				t.Fatalf("layout: %v", err)
			}
			list := wallpaperListNode(t, h)
			if len(list.Children) < 2 {
				t.Fatalf("laid out %d rows, want the seeded two", len(list.Children))
			}
			first, second := list.Children[0].Children, list.Children[1].Children
			if len(first) != wallpaperColumns {
				t.Fatalf("first row holds %d tiles, want %d", len(first), wallpaperColumns)
			}
			if gap := second[0].Bounds.Y - (first[0].Bounds.Y + first[0].Bounds.H); gap < wallpaperRowGap {
				t.Errorf("rows are %d px apart, want at least %d", gap, wallpaperRowGap)
			}
			for i := 1; i < len(first); i++ {
				if gap := first[i].Bounds.X - (first[i-1].Bounds.X + first[i-1].Bounds.W); gap != wallpaperColGap {
					t.Errorf("tiles %d and %d are %d px apart, want %d", i-1, i, gap, wallpaperColGap)
				}
			}
			for i, tile := range first {
				inner := tile.Bounds
				inner.X += tile.Padding
				inner.Y += tile.Padding
				inner.W -= 2 * tile.Padding
				inner.H -= 2 * tile.Padding
				thumb := wallpaperTileThumb(tile).Bounds
				if thumb.X < inner.X || thumb.Y < inner.Y ||
					thumb.X+thumb.W > inner.X+inner.W || thumb.Y+thumb.H > inner.Y+inner.H {
					t.Errorf("tile %d thumbnail %+v leaves the padded tile %+v", i, thumb, inner)
				}
			}
			content := list.Bounds.X + list.Bounds.W - list.Padding
			right := first[len(first)-1].Bounds.X + first[len(first)-1].Bounds.W
			if right > content-wallpaperScrollStrip {
				t.Errorf("last tile ends at %d, inside the scrollbar strip (list ends %d)", right, content)
			}
			if slack := content - wallpaperScrollStrip - right; slack >= wallpaperColumns {
				t.Errorf("grid leaves %d px unused beside the scrollbar", slack)
			}
		})
	}
}

// The All summary counts outputs, and a single-output machine is the common
// laptop case rather than an edge one.
func TestWallpaperSummaryCountsOneOutput(t *testing.T) {
	t.Parallel()

	snap := wallpaper.Snapshot{
		Connectors:  []string{"eDP-1"},
		Assignments: map[string]wallpaper.Assignment{"eDP-1": {Kind: wallpaper.KindVideo, Path: "/w/a.mp4"}},
	}
	if got := wallpaperSummary(snap, wallpaper.AllOutputs); !strings.HasPrefix(got, "1 output ") {
		t.Errorf("summary = %q, want it to start with \"1 output \"", got)
	}
	snap.Connectors = []string{"DP-1", "DP-3"}
	snap.Assignments["DP-1"] = wallpaper.Assignment{Kind: wallpaper.KindImage, Path: "/w/b.png"}
	snap.Assignments["DP-3"] = wallpaper.Assignment{Kind: wallpaper.KindImage, Path: "/w/c.png"}
	if got := wallpaperSummary(snap, wallpaper.AllOutputs); !strings.HasPrefix(got, "2 outputs ") {
		t.Errorf("summary = %q, want it to start with \"2 outputs \"", got)
	}
}

// The wheel used to go to whichever scrollable was built first, which in this
// panel was the folder band above the grid. No amount of clicking in the grid
// could move it, because the pointer was never consulted.
func TestScrollGoesToTheRegionUnderThePointer(t *testing.T) {
	t.Parallel()

	first := &ui.Node{Kind: ui.KindVirtualList, Bounds: ui.Rect{X: 0, Y: 0, W: 100, H: 50}}
	second := &ui.Node{Kind: ui.KindVirtualList, Bounds: ui.Rect{X: 0, Y: 50, W: 100, H: 200}}
	root := &ui.Node{
		Kind:     ui.KindColumn,
		Bounds:   ui.Rect{X: 0, Y: 0, W: 100, H: 300},
		Children: []*ui.Node{first, second},
	}

	if got := scrollAt(root, 10, 120); got != second {
		t.Error("the wheel over the lower list must scroll the lower list")
	}
	if got := scrollAt(root, 10, 10); got != first {
		t.Error("the wheel over the upper list must scroll the upper list")
	}
	// Off both, the first is still the answer: keyboard scrolling has no
	// pointer to consult.
	if got := scrollAt(root, 10, 275); got != first {
		t.Error("with the pointer over neither list, the first is the fallback")
	}
}

// The engine row listed every installed binary with nothing to say which was
// doing the work.
func TestWallpaperEngineCaptionFollowsTheOutput(t *testing.T) {
	t.Parallel()

	root := seedWallpaperRoot(t)
	reg, _, _ := openWallpaperPanel(t, []string{root})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	h.wallpaperOutput = "DP-1"
	h.wallpaperSnap.Connectors = []string{"DP-1"}
	h.wallpaperSnap.Caps = wallpaper.Capabilities{GSlapper: true, Statics: []string{"awww", "swaybg"}}

	// Nothing painted yet: the caption still names the engine that would take
	// it, so it is never blank on a fresh session.
	h.wallpaperSnap.Runtime = map[string]wallpaper.Runtime{}
	if got := wallpaperActiveEngine(h); got != wallpaper.EngineGSlapper {
		t.Errorf("with nothing assigned, active engine = %q, want the default", got)
	}

	// Once something has painted, the recorded engine wins over the default.
	h.wallpaperSnap.Runtime = map[string]wallpaper.Runtime{"DP-1": {Engine: "awww"}}
	reg.rebuildPanel(h)
	if got := wallpaperEngineLabels(h.root); !slices.Equal(got, []string{"awww"}) {
		t.Errorf("engine caption = %v, want the engine that painted it", got)
	}
}

// Choosing a scheme has to survive the next wallpaper apply, or it looks like
// the choice was never made.
func TestPinnedPaletteSurvivesAWallpaperApply(t *testing.T) {
	t.Parallel()

	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)

	reg.setPalette("gruvbox")
	reg.mu.Lock()
	source, seed := reg.cfg.ThemeGen.Source, reg.cfg.ThemeGen.Seed
	reg.mu.Unlock()
	if source != "palette" || seed != "gruvbox" {
		t.Fatalf("palette = %s/%s, want palette/gruvbox", source, seed)
	}

	reg.setWallpaperSeed("wallpaper", "/tmp/some-image.png")

	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.cfg.ThemeGen.Source != "palette" || reg.cfg.ThemeGen.Seed != "gruvbox" {
		t.Errorf("a wallpaper apply un-pinned the scheme: %s/%s",
			reg.cfg.ThemeGen.Source, reg.cfg.ThemeGen.Seed)
	}
}

// Every named scheme has to be reachable, or the palettes added in f73f25f
// stay unreachable without hand-editing the config.
func TestWallpaperPaletteDropdownOffersEveryScheme(t *testing.T) {
	t.Parallel()

	root := seedWallpaperRoot(t)
	reg, _, _ := openWallpaperPanel(t, []string{root})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	opts := wallpaperPaletteOptions(h)
	if opts[0].label != wallpaperAutoPalette {
		t.Errorf("first option = %q, want Auto", opts[0].label)
	}
	for _, name := range theme.PaletteNames() {
		if !slices.ContainsFunc(opts, func(o wallpaperOption) bool {
			return o.action == "wallpaper-palette:"+name
		}) {
			t.Errorf("scheme %q is not offered by the picker", name)
		}
	}
}

func TestThumbArrivalDoesNotRebuildTheTree(t *testing.T) {
	t.Parallel()
	// The virtual list's Item builder looks the raster up at layout time
	// (wallpaperThumbFor), so a decoded thumbnail needs the surface repainted,
	// not the tree rebuilt. On a 980x1100 picker a rebuild plus a full repaint
	// is roughly 40 ms of blit per thumbnail, paid once per file in a library
	// of hundreds.
	root := seedWallpaperRoot(t)
	// No relay: a snapshot update rebuilds the tree asynchronously, and a
	// stray rebuild would masquerade as the failure this test hunts.
	reg, _, _ := wallpaperPanel(t, []string{root}, false)
	h := wallpaperHost(t, reg)

	reg.mu.Lock()
	before := h.root
	reg.mu.Unlock()

	// applyWallpaperThumb takes reg.mu itself, so nothing may hold it across
	// this call.
	reg.applyWallpaperThumb(icons.Key{}, &ui.Image{Width: 2, Height: 2, Stride: 8, Pix: make([]byte, 16)})

	reg.mu.Lock()
	after := h.root
	reg.mu.Unlock()
	if after != before {
		t.Error("the tree was rebuilt for a raster arrival")
	}
}

// A thumbnail that decodes after its tile was laid out has to reach that
// tile. Painting reuses the laid-out tree, and the virtual list only asks for
// rasters while laying out, so a repaint alone left the tile blank until a
// scroll or a snapshot happened to lay the panel out again (sysc-1065).
func TestDecodedThumbReachesItsLaidOutTile(t *testing.T) {
	// Not parallel: the preview cache follows XDG_CACHE_HOME.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := seedWallpaperRoot(t)
	still := wallpaper.CachedStillPath(filepath.Join(root, "a.png"))
	if still == "" {
		t.Fatal("no preview path for the seeded still")
	}
	// No relay, so no snapshot rebuild can lay the panel out behind the test.
	reg, svc, _ := wallpaperPanel(t, []string{root}, false)
	h := wallpaperHost(t, reg)
	deadline := time.Now().Add(5 * time.Second)
	for svc.Snapshot().Library == nil {
		if time.Now().After(deadline) {
			t.Fatal("the library never indexed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Lay the grid out while the preview does not exist yet, so the tile is
	// built blank and nothing has asked for a decode.
	reg.mu.Lock()
	h.wallpaperSnap = svc.Snapshot()
	h.wallpaperDir = root
	reg.rebuildPanel(h)
	before := h.root
	if n := laidOutWallpaperRasters(h.root); n != 0 {
		reg.mu.Unlock()
		t.Fatalf("%d rasters before any preview existed", n)
	}
	worker := reg.wallpaperThumbsLocked()
	reg.mu.Unlock()

	// The preview lands and decodes. The worker publishes through
	// applyWallpaperThumb, which is the path under test.
	if err := os.MkdirAll(filepath.Dir(still), 0o700); err != nil {
		t.Fatal(err)
	}
	writeWallpaperJPEG(t, still)
	if _, _, err := worker.Request(wallpaperThumbKey(still, wallpaperGridOf(h))); err != nil {
		t.Fatal(err)
	}

	for {
		reg.mu.Lock()
		painted := laidOutWallpaperRasters(h.root)
		rebuilt := h.root != before
		reg.mu.Unlock()
		if rebuilt {
			t.Fatal("the tree was rebuilt; a raster arrival should only lay it out again")
		}
		if painted > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the decoded preview never reached its laid-out tile")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// laidOutWallpaperRasters counts tiles in the laid-out grid that hold a
// raster. It reads the list's current children, which layout produced, not
// Item, which would build fresh rows and hide the defect.
func laidOutWallpaperRasters(n *ui.Node) int {
	if n == nil {
		return 0
	}
	count := 0
	if n.Kind == ui.KindImage && n.Image != nil {
		count++
	}
	for _, c := range n.Children {
		count += laidOutWallpaperRasters(c)
	}
	return count
}

func writeWallpaperJPEG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, wallpaper.ThumbWidth, wallpaper.ThumbHeight))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 0x30, 0x70, 0xc0, 0xff
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWallpaperOurNamespaceIncludesTerminal(t *testing.T) {
	if !wallpaperOurNamespace("sysc-terminal") {
		t.Fatal("sysc-terminal on Background must be ours")
	}
	if !wallpaperOurNamespace("slapper") {
		t.Fatal("slapper must stay ours")
	}
	if wallpaperOurNamespace("org.gnome.Shell") {
		t.Fatal("a foreign Background namespace was claimed")
	}
}

// The panel reads top to bottom as the scan path: the header band, the
// controls card (where an apply lands, then how to find something), any
// banners, the path rule, the grid, and the footer band.
func TestWallpaperRowOrderIsTheScanPath(t *testing.T) {
	t.Parallel()

	reg, _, _ := openWallpaperPanel(t, []string{seedWallpaperRoot(t)})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	all := h.root.Children
	if len(all) < 5 {
		t.Fatalf("%d rows, want header, controls, rule, grid, footer", len(all))
	}
	header, controls := all[0], all[1]
	rule, grid, footer := all[len(all)-3], all[len(all)-2], all[len(all)-1]
	for _, action := range []string{"wallpaper-refresh", "wallpaper-close"} {
		if findAction(header, action) == nil {
			t.Errorf("header lacks %s", action)
		}
	}
	if findNode(header, func(n *ui.Node) bool { return n.Text == "WALLPAPER" && n.Tone == ui.ToneAccent }) == nil {
		t.Error("header lacks the SYSC rail")
	}
	for _, action := range []string{"wallpaper-output:all", "wallpaper-restore", "wallpaper-filter:0", "wallpaper-menu:folder"} {
		if findAction(controls, action) == nil {
			t.Errorf("controls card lacks %s", action)
		}
	}
	if findNode(controls, func(n *ui.Node) bool { return n.Kind == ui.KindTextField }) == nil {
		t.Error("controls card lacks the search field")
	}
	if !strings.HasPrefix(rule.Name, "Folder ") {
		t.Errorf("row before the grid is %q, want the path rule", rule.Name)
	}
	if grid.Kind != ui.KindVirtualList {
		t.Errorf("row before the footer is %v, want the grid", grid.Kind)
	}
	if findAction(footer, "wallpaper-menu:palette") == nil {
		t.Error("the footer lacks the shell theme")
	}
}

func TestWallpaperChromeHasNoCaptionLabels(t *testing.T) {
	t.Parallel()

	reg, _, _ := openWallpaperPanel(t, []string{seedWallpaperRoot(t)})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	walkNodes(h.root, func(n *ui.Node) {
		switch n.Text {
		case "SEARCH", "OUTPUT", "SHOW", "FOLDER", "THEME", "ENGINE":
			t.Errorf("caption label %q is still in the chrome", n.Text)
		}
	})
}

// The laptop's panel is about 820 tall. The sectioned chrome goes compact
// there and has to leave at least three and a half rows of tiles (owner
// decision, 2026-10-09; it was four before the chrome gained its sections).
func TestWallpaperGridRowsOnLaptop(t *testing.T) {
	t.Parallel()

	reg, _, _ := openWallpaperPanel(t, []string{seedWallpaperRoot(t)})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h.place.Panel.W, h.place.Panel.H = 980, 820
	reg.rebuildPanel(h)
	if pitch := wallpaperGridOf(h).pitch; 2*wallpaperListNode(t, h).Height < 7*pitch {
		t.Fatalf("grid is %d tall, want at least three and a half %d rows", wallpaperListNode(t, h).Height, pitch)
	}
	if err := ui.LayoutColumn(h.root, ui.Rect{W: 980, H: 820}, h.measureText()); err != nil {
		t.Fatalf("layout: %v", err)
	}
	last := h.root.Children[len(h.root.Children)-1]
	if bottom := last.Bounds.Y + last.Bounds.H; bottom > 820 {
		t.Fatalf("footer ends at %d, past the panel", bottom)
	}
}

func TestWallpaperOutputSelectCollapsesOnOneOutput(t *testing.T) {
	reg, _ := artRegistry(t, stubWallpaperEngine{}, "eDP-1")
	if err := reg.OpenPanel(PanelWallpaper, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	drainAuxQueue(reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelWallpaper]
	var outputs []string
	collectActions(h.root, "wallpaper-output:", &outputs)
	controls := h.root.Children[1]
	if len(outputs) != 0 || findNode(controls, func(n *ui.Node) bool { return n.Text == "eDP-1" }) == nil {
		t.Fatalf("one output: select %v; want a caption naming eDP-1 in the controls card", outputs)
	}
}

// A held background never starts the wallpaper service, so the picker opens to
// a nil snapshot. It used to render that as an endless "Indexing wallpaper
// library…" spinner with nothing explaining the hold (#114).
func TestWallpaperPickerExplainsAHeldBackground(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	reg.mu.Lock()
	reg.backgroundHeld = true
	reg.backgroundError = "the lock session service is not reporting"
	reg.mu.Unlock()
	if err := reg.OpenPanel(PanelWallpaper, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	open := reqs[1].Open
	if err := open.Callbacks.Configure(int(open.Width), int(open.Height), 120); err != nil {
		t.Fatal(err)
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelWallpaper]
	if h == nil {
		t.Fatal("picker did not open")
	}
	var said []string
	walkNodes(h.root, func(n *ui.Node) {
		if n.Kind == ui.KindText && n.Text != "" {
			said = append(said, n.Text)
		}
	})
	if !strings.Contains(strings.Join(said, "\n"), reg.backgroundError) {
		t.Fatalf("picker never says why the background is held:\n%s", strings.Join(said, "\n"))
	}
	if n := findNodeKey(h.root, "wallpaper-indexing"); n != nil {
		t.Fatal("picker still spins while the background is held")
	}
}

// Choosing a folder in the picker moves preview generation to it, so the grid
// on screen fills first.
func TestWallpaperOpeningAFolderPrioritisesItsPreviews(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(root, "a.png"), filepath.Join(sub, "x.png")} {
		writeWallpaperJPEG(t, p)
	}
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	svc := wallpaper.NewService(wallpaper.ServiceConfig{
		Engine:     stubWallpaperEngine{},
		Settings:   wallpaper.Settings{Scale: "fill", Loop: true, FPS: 30, Hidden: wallpaper.HiddenNone},
		Connectors: []string{"DP-1"},
		Roots:      []string{root},
		CacheDir:   t.TempDir(),
	})
	t.Cleanup(svc.Close)
	reg.mu.Lock()
	reg.wallpaperSvc = svc
	reg.mu.Unlock()
	if err := reg.OpenPanel(PanelWallpaper, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	drainAux(t, reg, 2)
	h := wallpaperHost(t, reg)

	reg.mu.Lock()
	h.wallpaperAction(reg, &ui.Node{Action: "wallpaper-dir:" + sub})
	reg.mu.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	for svc.Snapshot().ThumbsFolder != sub {
		if time.Now().After(deadline) {
			t.Fatalf("generation never moved to %s; folder is %q", sub, svc.Snapshot().ThumbsFolder)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A tile says what its preview is doing: decoding while the preview is
// missing, and "no preview" once the generator has recorded a failure, instead
// of an empty box either way (sysc-1069).
func TestWallpaperTileStates(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := seedWallpaperRoot(t)
	reg, _, _ := wallpaperPanel(t, []string{root}, false)
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	g := wallpaperGridOf(h)
	tileText := func(n *ui.Node) string {
		var b strings.Builder
		walkNodes(n, func(c *ui.Node) {
			if c.Kind == ui.KindText {
				b.WriteString(c.Text)
				b.WriteString("|")
			}
		})
		return b.String()
	}
	pending := wallpaper.Entry{Name: "a.png", Path: filepath.Join(root, "a.png"), Kind: wallpaper.KindImage}
	if got := tileText(wallpaperTile(reg, h, g, pending, 1)); !strings.Contains(got, "decoding") {
		t.Errorf("a tile with no preview reads %q, want it decoding", got)
	}

	broken := wallpaper.Entry{Name: "b.png", Path: filepath.Join(root, "b.png"), Kind: wallpaper.KindImage}
	marker := strings.TrimSuffix(wallpaper.CachedStillPath(broken.Path), ".jpg") + ".fail"
	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := tileText(wallpaperTile(reg, h, g, broken, 1)); !strings.Contains(got, "no preview") || strings.Contains(got, "decoding") {
		t.Errorf("a recorded failure reads %q, want no preview", got)
	}

	// The keyboard selection carries the accent corner brackets.
	h.wallpaperSel = 1
	if got := tileText(wallpaperTile(reg, h, g, pending, 1)); !strings.Contains(got, "\u250c") {
		t.Errorf("the selected tile reads %q, want corner brackets", got)
	}

	// An applied tile names the output it is on.
	h.wallpaperOutput = "DP-1"
	h.wallpaperSnap.Assignments = map[string]wallpaper.Assignment{"DP-1": {Path: pending.Path, Kind: wallpaper.KindImage}}
	if got := tileText(wallpaperTile(reg, h, g, pending, 0)); !strings.Contains(got, "[\u2713] DP-1") {
		t.Errorf("the applied tile reads %q, want [\u2713] DP-1", got)
	}
}

// wallpaperTileThumb is a tile's thumbnail box: the first child of its content
// column, whether or not the selection overlay wraps that column in a stack.
func wallpaperTileThumb(tile *ui.Node) *ui.Node {
	content := tile.Children[0]
	if content.Kind == ui.KindStack {
		content = content.Children[0]
	}
	return content.Children[0]
}
