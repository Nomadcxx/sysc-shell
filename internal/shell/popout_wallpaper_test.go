package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
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
	return wallpaper.Capabilities{GSlapper: true, Statics: []string{"awww"}}
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
	thumb := tile.Children[0].Children[0]
	switch thumb.Kind {
	case ui.KindImage:
		if thumb.ImageW != wallpaperTileWidth || thumb.ImageH != wallpaperThumbH {
			t.Fatalf("raster box = %dx%d, want %dx%d", thumb.ImageW, thumb.ImageH, wallpaperTileWidth, wallpaperThumbH)
		}
	case ui.KindRow:
		// The placeholder holds the tile's box so a late preview cannot reflow
		// the grid. It carries a glyph only where the embedded icon subset has
		// one, which for media it does not.
		if thumb.Width != wallpaperTileWidth || thumb.Height != wallpaperThumbH {
			t.Fatalf("placeholder box = %dx%d, want %dx%d", thumb.Width, thumb.Height, wallpaperTileWidth, wallpaperThumbH)
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

	// Child directories live in their own compact band above the grid.
	if got := len(wallpaperDirs(h)); got != 1 {
		t.Errorf("folder band holds %d entries, want the one child directory", got)
	}
	if wallpaperDirBand(h) == nil {
		t.Error("a directory with children must offer a folder band")
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
func wallpaperEngineLabels(root *ui.Node) []string {
	var out []string
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindCapsule && n.Fill == ui.FillSoft &&
			len(n.Children) == 1 && n.Children[0].Kind == ui.KindText {
			out = append(out, n.Children[0].Text)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

// Every installed engine gets a pill, in the order they are reached.
func TestWallpaperEngineStripNamesWhatIsInstalled(t *testing.T) {
	t.Parallel()

	root := seedWallpaperRoot(t)
	reg, _, _ := openWallpaperPanel(t, []string{root})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	snap := h.wallpaperSnap
	snap.Caps = wallpaper.Capabilities{GSlapper: true, Statics: []string{"awww", "swaybg"}}
	h.wallpaperSnap = snap
	reg.rebuildPanel(h)

	want := []string{"gSlapper", "awww", "swaybg"}
	if got := wallpaperEngineLabels(h.root); !slices.Equal(got, want) {
		t.Errorf("engine pills = %v, want %v", got, want)
	}

	// With nothing installed the strip says so rather than vanishing.
	snap.Caps = wallpaper.Capabilities{}
	h.wallpaperSnap = snap
	reg.rebuildPanel(h)
	if got := wallpaperEngineLabels(h.root); len(got) != 0 {
		t.Errorf("engine pills = %v, want none", got)
	}
	var said bool
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindText && strings.Contains(n.Text, "no wallpaper engine installed") {
			said = true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(h.root)
	if !said {
		t.Error("a machine with no engine must be told so")
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
	if !hasText("Generating previews") || !hasText("12 / 645") {
		t.Error("a library still generating previews must say so")
	}

	// Finished generation says nothing at all.
	snap.ThumbsDone = 645
	h.wallpaperSnap = snap
	reg.rebuildPanel(h)
	if hasText("Generating previews") {
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

func TestWallpaperEmptyStatesExplainThemselves(t *testing.T) {
	t.Parallel()

	empty := t.TempDir()
	reg, _, _ := openWallpaperPanel(t, []string{empty})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	text := wallpaperEmptyState(h).Text
	if !strings.Contains(text, "No supported wallpapers") {
		t.Errorf("empty directory says %q", text)
	}

	h.search = ui.NewField("zzzz")
	if text := wallpaperEmptyState(h).Text; !strings.Contains(text, "match your search") {
		t.Errorf("a search with no hits says %q", text)
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
		t.Fatalf("folder band holds %d entries, want 40", got)
	}
	// The band is capped, so folders cannot crowd the wallpapers out.
	band := wallpaperDirBand(h)
	if band == nil {
		t.Fatal("no folder band")
	}
	if band.Height > wallpaperDirMaxRows*wallpaperDirRowHeight {
		t.Fatalf("band is %dpx tall, want at most %d", band.Height, wallpaperDirMaxRows*wallpaperDirRowHeight)
	}
}

func TestWallpaperFolderStripAppearsOnlyForMultipleRoots(t *testing.T) {
	t.Parallel()

	one := seedWallpaperRoot(t)
	reg, _, _ := openWallpaperPanel(t, []string{one})
	h := wallpaperHost(t, reg)
	reg.mu.Lock()
	if strip := wallpaperFolderStrip(h); strip != nil {
		t.Error("a single root is not a choice worth a control")
	}
	reg.mu.Unlock()

	two := seedWallpaperRoot(t)
	reg2, _, _ := openWallpaperPanel(t, []string{one, two})
	h2 := wallpaperHost(t, reg2)
	reg2.mu.Lock()
	defer reg2.mu.Unlock()
	strip := wallpaperFolderStrip(h2)
	if strip == nil || len(strip.Children) != 2 {
		t.Fatalf("two roots must give a two-chip strip, got %v", strip)
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
		walk(wallpaperTile(reg, h, entry, 0))
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
