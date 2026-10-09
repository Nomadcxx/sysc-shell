package shell

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
)

// artWallpaperEngine is a fake engine with sysc-terminal installed and a
// catalog, so art tests read capabilities from the service like production
// does rather than patching them onto each snapshot.
type artWallpaperEngine struct{ stubWallpaperEngine }

func (artWallpaperEngine) Capabilities() wallpaper.Capabilities {
	return wallpaper.Capabilities{
		GSlapper: true, Terminal: true, Statics: []string{"awww"},
		Catalog: wallpaper.Catalog{
			Effects: []string{"fire", "rain"},
			Themes:  []string{"nord", "dracula"},
		},
	}
}
func (e artWallpaperEngine) RefreshTerminalCatalog() wallpaper.Capabilities { return e.Capabilities() }

// artRegistry is a registry with a bar on output 7 and a wallpaper service on
// connectors (DP-1 and DP-3 when none are named) backed by engine. No panel is
// open yet.
func artRegistry(t *testing.T, engine wallpaper.Engine, connectors ...string) (*Registry, *wallpaper.Service) {
	t.Helper()
	if len(connectors) == 0 {
		connectors = []string{"DP-1", "DP-3"}
	}
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	svc := wallpaper.NewService(wallpaper.ServiceConfig{
		Engine:     engine,
		Settings:   wallpaper.Settings{Scale: "fill", Loop: true, FPS: 30, Hidden: wallpaper.HiddenNone},
		Connectors: connectors,
	})
	t.Cleanup(svc.Close)
	reg.mu.Lock()
	reg.wallpaperSvc = svc
	reg.mu.Unlock()
	return reg, svc
}

func openArtPanel(t *testing.T, reg *Registry) *PanelHost {
	t.Helper()
	if err := reg.OpenPanel(PanelTerminalArt, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	open := reqs[1].Open
	if err := open.Callbacks.Configure(int(open.Width), int(open.Height), 120); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelTerminalArt]
	if h == nil {
		t.Fatal("no terminal art panel host")
	}
	return h
}

func TestTerminalArtBarItemTogglesItsPanel(t *testing.T) {
	got := buildWidgets([]config.Item{{ID: "terminal-art"}}, 8, standardMetrics())
	if len(got) != 1 || got[0].node == nil || got[0].node.Action != panelTerminalArtAction {
		t.Fatalf("terminal-art builds %+v, want one widget acting %q", got, panelTerminalArtAction)
	}
	if got[0].tooltip != "Terminal Art" {
		t.Errorf("tooltip = %q", got[0].tooltip)
	}
	if _, err := config.Parse([]byte(`{"bar":{"items":{"right":[{"id":"terminal-art"}]}}}`)); err != nil {
		t.Fatalf("a configured terminal-art item must load: %v", err)
	}

	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	bar := &Bar{}
	r.bindBarPanelActionsLocked(1, bar)
	if !bar.onAction(panelTerminalArtAction, buttonLeft) {
		t.Fatal("left click was not handled")
	}
	drainAux(t, r, 2)
	if _, ok := r.panelHosts[PanelTerminalArt]; !ok {
		t.Fatal("left click did not open PanelTerminalArt")
	}
	if _, ok := r.panelHosts[PanelWallpaper]; ok {
		t.Fatal("the terminal art item must not open the wallpaper panel")
	}
	if !bar.onAction(panelTerminalArtAction, buttonRight) {
		t.Fatal("right click was not handled")
	}
	if _, ok := r.panelHosts[PanelTerminalArt]; ok {
		t.Fatal("a second click must close the panel")
	}
}

func TestTerminalArtPanelOpensIndependently(t *testing.T) {
	if got := PanelTerminalArt.String(); got != "terminal-art" {
		t.Fatalf("String() = %q", got)
	}
	if id, err := parsePanelName("terminal-art"); err != nil || id != PanelTerminalArt {
		t.Fatalf("parsePanelName = %v, %v", id, err)
	}
	if id, ok := panelIDFromAux(panelSurfaceID(PanelTerminalArt)); !ok || id != PanelTerminalArt {
		t.Fatalf("panelIDFromAux = %v, %v", id, ok)
	}

	reg, _ := artRegistry(t, artWallpaperEngine{})
	trig := Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}
	open := func(id PanelID) {
		t.Helper()
		if err := reg.OpenPanel(id, 7, trig); err != nil {
			t.Fatal(err)
		}
		drainAuxQueue(reg)
	}
	state := func() (wall, art bool) {
		reg.mu.Lock()
		defer reg.mu.Unlock()
		return reg.panelHosts[PanelWallpaper] != nil, reg.panelHosts[PanelTerminalArt] != nil
	}

	open(PanelWallpaper)
	open(PanelTerminalArt)
	if wall, art := state(); !wall || !art {
		t.Fatalf("after opening both: wallpaper=%v art=%v", wall, art)
	}
	reg.mu.Lock()
	reg.closePanelLocked(PanelTerminalArt)
	reg.mu.Unlock()
	if wall, art := state(); !wall || art {
		t.Fatalf("closing art: wallpaper=%v art=%v, want wallpaper only", wall, art)
	}
	open(PanelTerminalArt)
	reg.mu.Lock()
	reg.closePanelLocked(PanelWallpaper)
	reg.mu.Unlock()
	if wall, art := state(); wall || !art {
		t.Fatalf("closing wallpaper: wallpaper=%v art=%v, want art only", wall, art)
	}
}

// artTexts lists every text in the tree.
func artTexts(n *ui.Node) []string {
	var out []string
	walkNodes(n, func(n *ui.Node) {
		if n.Kind == ui.KindText && n.Text != "" {
			out = append(out, n.Text)
		}
	})
	return out
}

func TestTerminalArtTreeNeverSaysWallpaper(t *testing.T) {
	reg, _ := artRegistry(t, artWallpaperEngine{})
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	for _, text := range artTexts(h.root) {
		if strings.Contains(text, "Wallpaper") {
			t.Errorf("the Terminal Art panel says %q", text)
		}
	}
	title := findNode(h.root, func(n *ui.Node) bool { return n.Kind == ui.KindText && n.Text == "Terminal Art" })
	if title == nil || title.TextRole != theme.RoleTitle {
		t.Fatalf("title = %+v, want Terminal Art as RoleTitle", title)
	}
}

func TestTerminalArtStatusReportsUnassignedOutput(t *testing.T) {
	h := &PanelHost{
		wallpaperOutput: "DP-1",
		wallpaperSnap: wallpaper.Snapshot{
			Connectors:  []string{"DP-1"},
			Assignments: map[string]wallpaper.Assignment{},
		},
	}
	if texts := artTexts(artPlayingRow(h, "DP-1", "DP-1")); !slices.Equal(texts, []string{"DP-1", "Nothing assigned"}) {
		t.Fatalf("row texts = %v, want the connector and Nothing assigned", texts)
	}
}

func TestTerminalArtShowsApplyErrorAfterRollback(t *testing.T) {
	h := &PanelHost{
		wallpaperOutput: "DP-1",
		wallpaperSnap: wallpaper.Snapshot{
			Caps:        wallpaper.Capabilities{Terminal: true},
			Connectors:  []string{"DP-1"},
			Assignments: map[string]wallpaper.Assignment{"DP-1": {Kind: wallpaper.KindImage, Path: "/tmp/still.png"}},
			Runtime:     map[string]wallpaper.Runtime{"DP-1": {State: wallpaper.StateStatic, Err: "sysc-terminal failed"}},
		},
	}
	for _, banner := range artBanners(h) {
		if banner.Text == "DP-1: sysc-terminal failed" {
			return
		}
	}
	t.Fatal("Terminal Art did not show the failed effect apply after restoring the still")
}

func TestTerminalArtCardsFromCatalog(t *testing.T) {
	reg, _ := artRegistry(t, artWallpaperEngine{})
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	var cards []string
	collectActions(h.root, "art-apply:", &cards)
	if !slices.Equal(cards, []string{"art-apply:fire", "art-apply:rain"}) {
		t.Fatalf("cards = %v, want every catalog effect", cards)
	}
	for _, action := range cards {
		card := findAction(h.root, action)
		if card.Name != strings.TrimPrefix(action, "art-apply:") || !card.Focusable || card.State&ui.StateDisabled != 0 {
			t.Errorf("card %s = name %q focusable %v state %v", action, card.Name, card.Focusable, card.State)
		}
	}
	if texts := artTexts(h.root); !slices.Contains(texts, "Effects") || !slices.Contains(texts, "2") {
		t.Errorf("effects header missing its count; texts = %v", texts)
	}
}

func TestTerminalArtEffectsHeaderFitsTallFontMetrics(t *testing.T) {
	metrics := standardMetrics()
	h := &PanelHost{
		theme: Theme{Metrics: metrics},
		wallpaperSnap: wallpaper.Snapshot{
			Caps: wallpaper.Capabilities{Catalog: wallpaper.Catalog{Themes: []string{"nord", "dracula"}}},
		},
	}
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 8, 24 }
	if err := ui.Layout(artEffectsHeader(h, 2), ui.Rect{W: 608, H: metrics.StandardControl}, measure); err != nil {
		t.Fatalf("effects header with 24 px font metrics: %v", err)
	}
}

type catalogRefreshArtEngine struct {
	stubWallpaperEngine
	initial   wallpaper.Capabilities
	refreshed wallpaper.Capabilities
	refreshes int
}

func (e *catalogRefreshArtEngine) Capabilities() wallpaper.Capabilities { return e.initial }
func (e *catalogRefreshArtEngine) RefreshTerminalCatalog() wallpaper.Capabilities {
	e.refreshes++
	return e.refreshed
}

func TestTerminalArtRefreshesCatalogOnOpen(t *testing.T) {
	initial := artWallpaperEngine{}.Capabilities()
	updated := initial
	updated.Catalog.Effects = append(slices.Clone(updated.Catalog.Effects), "sonar")
	engine := &catalogRefreshArtEngine{initial: initial, refreshed: updated}
	reg, svc := artRegistry(t, engine)
	go reg.relayWallpaper(svc)
	h := openArtPanel(t, reg)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		found := findAction(h.root, "art-apply:sonar") != nil
		reg.mu.Unlock()
		if found {
			if engine.refreshes != 1 {
				t.Fatalf("catalog refreshes = %d, want 1", engine.refreshes)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("new terminal effect did not appear after opening the panel")
}

func TestTerminalArtUnavailableExplains(t *testing.T) {
	reg, _ := artRegistry(t, stubWallpaperEngine{})
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	found := false
	for _, text := range artTexts(h.root) {
		if strings.Contains(text, "sysc-terminal") && strings.Contains(text, "/usr/local/bin") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no install banner; texts = %v", artTexts(h.root))
	}
	if findAction(h.root, artPaletteMenu) != nil {
		t.Error("the palette menu must be hidden without sysc-terminal")
	}
}

func TestTerminalArtOutputSelectCollapsesOnOneOutput(t *testing.T) {
	reg, _ := artRegistry(t, artWallpaperEngine{}, "eDP-1")
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	var outputs []string
	collectActions(h.root, "art-output:", &outputs)
	texts := artTexts(h.root)
	reg.mu.Unlock()
	if len(outputs) != 0 || !slices.Contains(texts, "eDP-1") {
		t.Fatalf("one output: select %v, texts %v; want a caption naming eDP-1", outputs, texts)
	}

	reg, _ = artRegistry(t, artWallpaperEngine{})
	h = openArtPanel(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	outputs = nil
	collectActions(h.root, "art-output:", &outputs)
	if !slices.Equal(outputs, []string{"art-output:all", "art-output:DP-1", "art-output:DP-3"}) {
		t.Fatalf("two outputs: select %v", outputs)
	}
}

// awaitArt waits for connector's assignment to satisfy ok.
func awaitArt(t *testing.T, svc *wallpaper.Service, connector string, ok func(wallpaper.Assignment) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok(svc.Snapshot().Assignments[connector]) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s assignment = %+v", connector, svc.Snapshot().Assignments[connector])
}

func runningEffect(effect, palette string) func(wallpaper.Assignment) bool {
	return func(a wallpaper.Assignment) bool {
		return a.Kind == wallpaper.KindEffect && a.Effect == effect && a.Theme == palette && a.Path == ""
	}
}

// artPickPalette opens the palette menu and picks name with the keyboard,
// through the same activation and menu routing a user's keys take.
func artPickPalette(t *testing.T, reg *Registry, h *PanelHost, name string) {
	t.Helper()
	menu := findAction(h.root, artPaletteMenu)
	if menu == nil {
		t.Fatal("no palette menu in the tree")
	}
	h.setFocus(menu)
	if !h.activate(reg) || !h.menus[artPaletteMenu].Opened() {
		t.Fatal("activating the palette menu did not open it")
	}
	m := h.menus[artPaletteMenu]
	for m.options[m.cursor] != name {
		before := m.cursor
		h.keyPress(reg, keyDown)
		if m.cursor == before {
			t.Fatalf("palette %q is not in the menu %v", name, m.options)
		}
	}
	h.keyPress(reg, keyEnter)
	if m.Opened() {
		t.Fatal("Enter did not close the palette menu")
	}
}

// artAct runs action through the panel's own dispatch, as a click does.
func artAct(t *testing.T, reg *Registry, h *PanelHost, action string) {
	t.Helper()
	n := findAction(h.root, action)
	if n == nil {
		t.Fatalf("no %s in the tree", action)
	}
	if !h.artAction(reg, n) {
		t.Fatalf("%s was not handled", action)
	}
}

func TestTerminalArtCardAppliesToSelectedOutput(t *testing.T) {
	reg, svc := artRegistry(t, artWallpaperEngine{})
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	artAct(t, reg, h, "art-output:DP-1")
	artAct(t, reg, h, "art-apply:fire")
	reg.mu.Unlock()
	awaitArt(t, svc, "DP-1", runningEffect("fire", "nord"))
	if _, ok := svc.Snapshot().Assignments["DP-3"]; ok {
		t.Fatal("DP-3 was not selected and must stay untouched")
	}
}

func TestTerminalArtAppliesPerOutput(t *testing.T) {
	reg, svc := artRegistry(t, artWallpaperEngine{})
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	artAct(t, reg, h, "art-output:DP-1")
	artAct(t, reg, h, "art-apply:fire")
	artAct(t, reg, h, "art-output:DP-3")
	artAct(t, reg, h, "art-apply:rain")
	reg.mu.Unlock()
	awaitArt(t, svc, "DP-1", runningEffect("fire", "nord"))
	awaitArt(t, svc, "DP-3", runningEffect("rain", "nord"))
}

func TestTerminalArtPaletteFollowsRunningEffect(t *testing.T) {
	reg, svc := artRegistry(t, artWallpaperEngine{})
	svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: "DP-1", Kind: wallpaper.KindEffect, Effect: "fire", Theme: "dracula"})
	awaitArt(t, svc, "DP-1", runningEffect("fire", "dracula"))
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	artAct(t, reg, h, "art-output:DP-1")
	menu := findAction(h.root, artPaletteMenu)
	if got := artPalette(h); got != "dracula" || menu == nil || menu.Text != "dracula" {
		t.Fatalf("palette = %q, menu %+v; want the running dracula, not the catalog's first theme", got, menu)
	}
}

func TestTerminalArtPaletteChangeReappliesRunningEffect(t *testing.T) {
	reg, svc := artRegistry(t, artWallpaperEngine{})
	svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: "DP-1", Kind: wallpaper.KindEffect, Effect: "fire", Theme: "nord"})
	awaitArt(t, svc, "DP-1", runningEffect("fire", "nord"))
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	// DP-3 runs nothing: a palette pick there only sets the next click's palette.
	artAct(t, reg, h, "art-output:DP-3")
	artPickPalette(t, reg, h, "dracula")
	artAct(t, reg, h, "art-output:DP-1")
	artPickPalette(t, reg, h, "dracula")
	reg.mu.Unlock()
	awaitArt(t, svc, "DP-1", runningEffect("fire", "dracula"))
	// Commands run in order, so a DP-3 apply would have landed by now.
	if a, ok := svc.Snapshot().Assignments["DP-3"]; ok {
		t.Fatalf("DP-3 = %+v; a palette change must not start an effect", a)
	}
}

func TestTerminalArtRestoreDisabledWithoutStill(t *testing.T) {
	reg, svc := artRegistry(t, artWallpaperEngine{})
	svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: "DP-1", Kind: wallpaper.KindEffect, Effect: "fire", Theme: "nord"})
	awaitArt(t, svc, "DP-1", runningEffect("fire", "nord"))
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	artAct(t, reg, h, "art-output:DP-1")
	if findAction(h.root, "art-pause:DP-1") == nil {
		t.Error("a running effect offers Pause")
	}
	restore := findAction(h.root, "art-restore:DP-1")
	if restore == nil || restore.State&ui.StateDisabled == 0 || restore.Tooltip != "No previous still recorded" {
		t.Fatalf("Restore still = %+v, want disabled with a reason", restore)
	}
	artAct(t, reg, h, "art-output:DP-3")
	if findAction(h.root, "art-restore:DP-3") != nil || findAction(h.root, "art-pause:DP-3") != nil {
		t.Error("an output showing nothing offers no effect controls")
	}
}

func TestTerminalArtRestoredWithoutStillReportsBlankOutput(t *testing.T) {
	h := &PanelHost{
		wallpaperOutput: "DP-1",
		wallpaperSnap: wallpaper.Snapshot{
			Connectors: []string{"DP-1"},
			Assignments: map[string]wallpaper.Assignment{
				"DP-1": {Kind: wallpaper.KindEffect, Effect: "fire", Theme: "nord"},
			},
			Runtime: map[string]wallpaper.Runtime{"DP-1": {State: wallpaper.StateStatic}},
		},
	}
	if got := artIdleText(h, "DP-1"); got != "No wallpaper displayed" {
		t.Fatalf("restored effect status = %q, want a blank-output message", got)
	}
}

func TestTerminalArtFailedEffectCanRestoreWithoutPause(t *testing.T) {
	reg, _ := artRegistry(t, artWallpaperEngine{}, "DP-1")
	h := openArtPanel(t, reg)
	h.wallpaperOutput = "DP-1"
	h.wallpaperSnap.Assignments = map[string]wallpaper.Assignment{
		"DP-1": {Kind: wallpaper.KindEffect, Effect: "fire", Theme: "nord", PreviewPath: "/w/still.png"},
	}
	h.wallpaperSnap.Runtime = map[string]wallpaper.Runtime{
		"DP-1": {State: wallpaper.StateError, Err: "terminal failed"},
	}

	row := artPlayingRow(h, "DP-1", "DP-1")
	if findAction(row, "art-pause:DP-1") != nil || findAction(row, "art-resume:DP-1") != nil {
		t.Fatal("a failed effect offers playback controls")
	}
	restore := findAction(row, "art-restore:DP-1")
	if restore == nil || restore.State&ui.StateDisabled != 0 {
		t.Fatalf("Restore still = %+v, want an enabled recovery action", restore)
	}
}

// Each output's controls act on that output alone: restoring DP-1 leaves DP-3
// playing, and DP-3, with no still recorded, cannot be restored at all.
func TestTerminalArtRestoreActsOnItsOwnOutput(t *testing.T) {
	reg, svc := artRegistry(t, artWallpaperEngine{})
	svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: "DP-1", Kind: wallpaper.KindImage, Path: "/w/still.png"})
	awaitArt(t, svc, "DP-1", func(a wallpaper.Assignment) bool { return a.Kind == wallpaper.KindImage })
	svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: "DP-1", Kind: wallpaper.KindEffect, Effect: "fire", Theme: "nord"})
	awaitArt(t, svc, "DP-1", func(a wallpaper.Assignment) bool {
		return a.Kind == wallpaper.KindEffect && a.PreviewPath == "/w/still.png"
	})
	svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: "DP-3", Kind: wallpaper.KindEffect, Effect: "rain", Theme: "nord"})
	awaitArt(t, svc, "DP-3", runningEffect("rain", "nord"))

	h := openArtPanel(t, reg)
	reg.mu.Lock()
	if other := findAction(h.root, "art-restore:DP-3"); other == nil || other.State&ui.StateDisabled == 0 {
		reg.mu.Unlock()
		t.Fatalf("DP-3 Restore = %+v, want disabled without a still", other)
	}
	restore := findAction(h.root, "art-restore:DP-1")
	if restore == nil || restore.State&ui.StateDisabled != 0 {
		reg.mu.Unlock()
		t.Fatalf("DP-1 Restore = %+v, want enabled", restore)
	}
	if !h.artAction(reg, restore) {
		reg.mu.Unlock()
		t.Fatal("DP-1 Restore was not handled")
	}
	reg.mu.Unlock()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && svc.Snapshot().Runtime["DP-1"].State != wallpaper.StateStatic {
		time.Sleep(5 * time.Millisecond)
	}
	snap := svc.Snapshot()
	if snap.Runtime["DP-1"].State != wallpaper.StateStatic || snap.Runtime["DP-3"].State != wallpaper.StatePlaying {
		t.Fatalf("runtimes after DP-1 Restore: DP-1=%+v DP-3=%+v", snap.Runtime["DP-1"], snap.Runtime["DP-3"])
	}
}

func TestTerminalArtStalePlaybackActionRefreshesSnapshot(t *testing.T) {
	reg, svc := artRegistry(t, artWallpaperEngine{}, "DP-1")
	svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: "DP-1", Kind: wallpaper.KindImage, Path: "/w/still.png"})
	awaitArt(t, svc, "DP-1", func(a wallpaper.Assignment) bool { return a.Kind == wallpaper.KindImage })
	h := openArtPanel(t, reg)
	stale := svc.Snapshot()
	stale.Assignments["DP-1"] = wallpaper.Assignment{
		Kind: wallpaper.KindEffect, Effect: "fire", Theme: "nord", PreviewPath: "/w/old.png",
	}
	stale.Runtime["DP-1"] = wallpaper.Runtime{State: wallpaper.StatePlaying}

	reg.mu.Lock()
	h.wallpaperSnap = stale
	if !h.artAction(reg, &ui.Node{Action: "art-pause:DP-1"}) {
		reg.mu.Unlock()
		t.Fatal("stale Pause was not handled")
	}
	if got := h.wallpaperSnap.Assignments["DP-1"].Kind; got != wallpaper.KindImage {
		reg.mu.Unlock()
		t.Fatalf("Pause used stale assignment kind %v", got)
	}
	h.wallpaperSnap = stale
	if !h.artAction(reg, &ui.Node{Action: "art-restore:DP-1"}) {
		reg.mu.Unlock()
		t.Fatal("stale Restore was not handled")
	}
	if got := h.wallpaperSnap.Assignments["DP-1"].Kind; got != wallpaper.KindImage {
		reg.mu.Unlock()
		t.Fatalf("Restore used stale assignment kind %v", got)
	}
	reg.mu.Unlock()
	if got := svc.Snapshot().Runtime["DP-1"].State; got != wallpaper.StateStatic {
		t.Fatalf("stale playback action changed the still's state to %v", got)
	}
}

type sevenArtEngine struct{ stubWallpaperEngine }

func (sevenArtEngine) Capabilities() wallpaper.Capabilities {
	return wallpaper.Capabilities{
		Terminal: true,
		Catalog: wallpaper.Catalog{
			Effects: []string{"a", "b", "c", "d", "e", "f", "g"},
			Themes:  []string{"nord"},
		},
	}
}
func (e sevenArtEngine) RefreshTerminalCatalog() wallpaper.Capabilities { return e.Capabilities() }

func TestTerminalArtArrowKeysWalkThreeColumns(t *testing.T) {
	reg, svc := artRegistry(t, sevenArtEngine{})
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	for _, step := range []struct {
		key  uint32
		want int
	}{
		{keyDown, 3}, {keyRight, 4}, {keyDown, 6}, {keyUp, 3}, {keyLeft, 2}, {keyUp, 0}, {keyLeft, 0},
	} {
		if !h.keyPress(reg, step.key) {
			reg.mu.Unlock()
			t.Fatalf("key %d was not handled", step.key)
		}
		if h.wallpaperSel != step.want {
			reg.mu.Unlock()
			t.Fatalf("after key %d: selection %d, want %d", step.key, h.wallpaperSel, step.want)
		}
	}
	h.keyPress(reg, keyRight)
	h.keyPress(reg, keyDown) // e is the middle card on the next row.
	if !h.keyPress(reg, keyEnter) {
		reg.mu.Unlock()
		t.Fatal("Enter on the grid was not handled")
	}
	reg.mu.Unlock()
	awaitArt(t, svc, "DP-1", runningEffect("e", "nord"))
}

func TestTerminalArtArrowKeysRequireCardFocus(t *testing.T) {
	reg, _ := artRegistry(t, sevenArtEngine{})
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h.wallpaperSel = 3
	h.focusByName("Close")
	handled := h.artKeyPress(reg, keyLeft)
	if handled || h.wallpaperSel != 3 {
		t.Fatalf("arrow without card focus handled=%v selection=%d", handled, h.wallpaperSel)
	}
	if h.artKeyPress(reg, keyEnter) {
		t.Fatal("Enter without card focus was consumed by the effect grid")
	}
}

func TestTerminalArtFocusedCardOwnsGridNavigation(t *testing.T) {
	reg, _ := artRegistry(t, sevenArtEngine{})
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h.focusByName("d")
	if !h.artKeyPress(reg, keyRight) {
		t.Fatal("right arrow was not handled by a focused card")
	}
	if h.wallpaperSel != 4 || h.focused() == nil || h.focused().Action != "art-apply:e" {
		t.Fatalf("selection=%d focused=%+v, want e at index 4", h.wallpaperSel, h.focused())
	}
}

func TestTerminalArtCardActivationSynchronizesSelectionAndFocus(t *testing.T) {
	reg, svc := artRegistry(t, artWallpaperEngine{})
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	h.wallpaperSel = 0
	card := findAction(h.root, "art-apply:rain")
	x, y := float64(card.Bounds.X+card.Bounds.W/2), float64(card.Bounds.Y+card.Bounds.H/2)
	reg.mu.Unlock()
	handle := h.handle(reg)
	if !handle(wayland.Event{Kind: wayland.EventPointerPress, Button: buttonLeft, X: x, Y: y}) ||
		!handle(wayland.Event{Kind: wayland.EventPointerRelease, Button: buttonLeft, X: x, Y: y}) {
		t.Fatal("click on the rain card was not handled")
	}
	reg.mu.Lock()
	if h.wallpaperSel != 1 || h.focused() == nil || h.focused().Action != "art-apply:rain" {
		reg.mu.Unlock()
		t.Fatalf("after card activation selection=%d focused=%+v, want rain", h.wallpaperSel, h.focused())
	}
	reg.mu.Unlock()
	awaitArt(t, svc, "DP-1", runningEffect("rain", "nord"))
}

func TestWallpaperPanelHasNoTerminalArt(t *testing.T) {
	reg, _ := artRegistry(t, artWallpaperEngine{})
	if err := reg.OpenPanel(PanelWallpaper, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	drainAuxQueue(reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelWallpaper]
	if n := findNode(h.root, func(n *ui.Node) bool { return n.Role == "tablist" }); n != nil {
		t.Errorf("tab strip still present: %+v", n)
	}
	for _, text := range artTexts(h.root) {
		if strings.Contains(text, "Terminal Art") || strings.Contains(text, "sysc-terminal") {
			t.Errorf("the wallpaper panel says %q", text)
		}
	}
	for _, label := range wallpaperEngineLabels(h.root) {
		if label == wallpaper.EngineTerminal {
			t.Error("the wallpaper engine readout names sysc-terminal")
		}
	}
	for _, e := range wallpaperMedia(h) {
		if e.Kind == wallpaper.KindEffect {
			t.Errorf("the grid lists effect %s", e.Name)
		}
	}
	var actions []string
	collectActions(h.root, "wallpaper-tab:", &actions)
	collectActions(h.root, "wallpaper-menu:effect-theme", &actions)
	if len(actions) > 0 {
		t.Errorf("art controls remain: %v", actions)
	}
}

// wallpaperWithEffect opens the wallpaper panel with fire/nord running on
// DP-1 and DP-1 selected.
func wallpaperWithEffect(t *testing.T) (*Registry, *PanelHost) {
	t.Helper()
	reg, svc := artRegistry(t, artWallpaperEngine{})
	svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: "DP-1", Kind: wallpaper.KindEffect, Effect: "fire", Theme: "nord"})
	awaitArt(t, svc, "DP-1", runningEffect("fire", "nord"))
	if err := reg.OpenPanel(PanelWallpaper, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	drainAuxQueue(reg)
	reg.mu.Lock()
	h := reg.panelHosts[PanelWallpaper]
	if !h.wallpaperAction(reg, findAction(h.root, "wallpaper-output:DP-1")) {
		reg.mu.Unlock()
		t.Fatal("select DP-1")
	}
	reg.mu.Unlock()
	return reg, h
}

func TestWallpaperStripNamesRunningEffect(t *testing.T) {
	reg, h := wallpaperWithEffect(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	strip := h.root.Children[1]
	if !slices.ContainsFunc(artTexts(strip), func(s string) bool { return strings.Contains(s, "DP-1 \u00b7 Terminal Art: fire (nord)") }) {
		t.Errorf("strip texts = %v", artTexts(strip))
	}
	if findAction(strip, "wallpaper-open-art") == nil {
		t.Error("an effect output links to the Terminal Art panel")
	}
	if findAction(strip, "wallpaper-pause") != nil {
		t.Error("effect playback is controlled on the Terminal Art panel, not here")
	}
}

func TestWallpaperOpenArtSwitchesPanels(t *testing.T) {
	reg, h := wallpaperWithEffect(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if !h.wallpaperAction(reg, findAction(h.root, "wallpaper-open-art")) {
		t.Fatal("Open Terminal Art was not handled")
	}
	art := reg.panelHosts[PanelTerminalArt]
	if reg.panelHosts[PanelWallpaper] != nil || art == nil {
		t.Fatalf("wallpaper open=%v art open=%v; want only art", reg.panelHosts[PanelWallpaper] != nil, art != nil)
	}
	if art.wallpaperOutput != "DP-1" {
		t.Fatalf("art output = %q, want the wallpaper panel's DP-1", art.wallpaperOutput)
	}
}

func TestWallpaperAllSummaryCountsEffects(t *testing.T) {
	snap := wallpaper.Snapshot{
		Connectors: []string{"DP-1", "DP-3"},
		Assignments: map[string]wallpaper.Assignment{
			"DP-1": {Kind: wallpaper.KindImage, Path: "/w/a.png"},
			"DP-3": {Kind: wallpaper.KindEffect, Effect: "fire"},
		},
	}
	if got := wallpaperSummary(snap, wallpaper.AllOutputs); got != "2 outputs \u00b7 0 video \u00b7 1 image \u00b7 1 effect" {
		t.Fatalf("summary = %q; an effect is not an image", got)
	}
}

func openArtSettings(t *testing.T, reg *Registry) *PanelHost {
	t.Helper()
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelSettings]
	h.section = "Terminal Art"
	reg.rebuildPanel(h)
	return h
}

func TestTerminalArtSectionIsReachable(t *testing.T) {
	i := slices.Index(settingsSections, "Terminal Art")
	if i < 1 || settingsSections[i-1] != "Wallpaper" {
		t.Fatalf("sections = %v, want Terminal Art right after Wallpaper", settingsSections)
	}
	if settingsSectionIcons["Terminal Art"] != "terminal" {
		t.Errorf("icon = %q, want terminal", settingsSectionIcons["Terminal Art"])
	}
	reg, _ := artRegistry(t, artWallpaperEngine{})
	h := openArtSettings(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if findAction(h.root, "art-open") == nil {
		t.Fatalf("Terminal Art settings built no Open button: %q", artTexts(h.root))
	}
}

func TestTerminalArtSettingsShowsEngineAndDefault(t *testing.T) {
	reg, _ := artRegistry(t, sevenArtEngine{})
	h := openArtSettings(t, reg)
	reg.mu.Lock()
	texts := strings.Join(artTexts(h.root), "\n")
	combo := findAction(h.root, "art-menu:default")
	reg.mu.Unlock()
	if !strings.Contains(texts, "sysc-Go") || !strings.Contains(texts, "7 effects") {
		t.Errorf("status = %q, want sysc-Go and 7 effects", texts)
	}
	if combo == nil || !strings.Contains(combo.Name, "nord") {
		t.Fatalf("unset palette combo = %+v, want the first catalog theme", combo)
	}

	reg, _ = artRegistry(t, artWallpaperEngine{})
	reg.mu.Lock()
	reg.cfg.TerminalArt.Palette = "dracula"
	reg.mu.Unlock()
	h = openArtSettings(t, reg)
	reg.mu.Lock()
	combo = findAction(h.root, "art-menu:default")
	reg.mu.Unlock()
	if combo == nil || !strings.Contains(combo.Name, "dracula") {
		t.Fatalf("configured palette combo = %+v, want dracula", combo)
	}

	reg, _ = artRegistry(t, stubWallpaperEngine{})
	h = openArtSettings(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if !slices.Contains(artTexts(h.root), "sysc-terminal is not installed. Install it to /usr/local/bin") ||
		findAction(h.root, "art-menu:default") != nil {
		t.Fatalf("without sysc-terminal: %q", artTexts(h.root))
	}
}

func TestTerminalArtSettingsPicksTheDefault(t *testing.T) {
	reg, _ := artRegistry(t, artWallpaperEngine{})
	h := openArtSettings(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if !h.artAction(reg, &ui.Node{Action: "art-default:dracula"}) || h.draft.TerminalArt.Palette != "dracula" {
		t.Fatalf("draft palette = %q, want dracula", h.draft.TerminalArt.Palette)
	}
}

func TestTerminalArtPanelStartsOnDefaultPalette(t *testing.T) {
	reg, _ := artRegistry(t, artWallpaperEngine{})
	reg.mu.Lock()
	reg.cfg.TerminalArt.Palette = "dracula"
	reg.mu.Unlock()
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if got := artPalette(h); got != "dracula" {
		t.Fatalf("palette = %q, want the configured dracula", got)
	}
}

func TestTerminalArtSettingsOpensThePanel(t *testing.T) {
	reg, _ := artRegistry(t, artWallpaperEngine{})
	h := openArtSettings(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if !h.artAction(reg, &ui.Node{Action: "art-open"}) {
		t.Fatal("Open Terminal Art was not handled")
	}
	art := reg.panelHosts[PanelTerminalArt]
	if reg.panelHosts[PanelSettings] != nil || art == nil || art.output != h.output {
		t.Fatalf("settings open=%v art=%v; want the panel on output %d", reg.panelHosts[PanelSettings] != nil, art != nil, h.output)
	}
}

// Restore from Terminal Art is about effects: on All outputs it must leave a
// video on another output playing, and an output back on its still is no
// longer running anything the panel can pause.
func TestTerminalArtRestoreTouchesOnlyEffects(t *testing.T) {
	reg, svc := artRegistry(t, artWallpaperEngine{})
	svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: "DP-1", Kind: wallpaper.KindImage, Path: "/w/a.png"})
	awaitArt(t, svc, "DP-1", func(a wallpaper.Assignment) bool { return a.Kind == wallpaper.KindImage })
	svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: "DP-1", Kind: wallpaper.KindEffect, Effect: "fire", Theme: "nord"})
	svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: "DP-3", Kind: wallpaper.KindVideo, Path: "/w/b.mp4"})
	awaitArt(t, svc, "DP-1", runningEffect("fire", "nord"))
	awaitArt(t, svc, "DP-3", func(a wallpaper.Assignment) bool { return a.Path == "/w/b.mp4" })
	if svc.Snapshot().Runtime["DP-3"].State == wallpaper.StateStatic {
		t.Fatal("the video never started, so the check below proves nothing")
	}
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	artAct(t, reg, h, "art-restore:DP-1")
	reg.mu.Unlock()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && svc.Snapshot().Runtime["DP-1"].State != wallpaper.StateStatic {
		time.Sleep(5 * time.Millisecond)
	}
	snap := svc.Snapshot()
	if snap.Runtime["DP-1"].State != wallpaper.StateStatic {
		t.Fatalf("DP-1 runtime = %+v, want its still", snap.Runtime["DP-1"])
	}
	if snap.Runtime["DP-3"].State == wallpaper.StateStatic {
		t.Fatal("restoring effects also stopped the video on DP-3")
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()
	h.wallpaperSnap = snap
	reg.rebuildPanel(h)
	if got := artTexts(artPlayingRow(h, "DP-1", "DP-1")); !slices.Equal(got, []string{"DP-1", "Showing a wallpaper"}) {
		t.Fatalf("restored output status = %q, want the still currently shown", got)
	}
	if findAction(h.root, "art-pause:DP-1") != nil || len(artRunningOn(h, "fire")) > 0 {
		t.Fatalf("a restored output still reads as running: %q", artTexts(h.root))
	}
}

// longArtEngine carries the installed catalog's long effect names, which
// short fixture names hid from the status row width.
type longArtEngine struct{ stubWallpaperEngine }

func (longArtEngine) Capabilities() wallpaper.Capabilities {
	return wallpaper.Capabilities{
		GSlapper: true, Terminal: true, Statics: []string{"awww"},
		Catalog: wallpaper.Catalog{
			Effects: []string{"justice-cross"},
			Themes:  []string{"catppuccin-mocha"},
		},
	}
}
func (e longArtEngine) RefreshTerminalCatalog() wallpaper.Capabilities { return e.Capabilities() }

// An effect playing on every output must not make the panel unopenable: the
// status and its playback controls once overflowed the 608 interior, the
// first configure failed, and every reopen closed the surface again.
func TestTerminalArtReopensWhileEffectsPlayOnEveryOutput(t *testing.T) {
	reg, svc := artRegistry(t, longArtEngine{})
	for _, connector := range []string{"DP-1", "DP-3"} {
		svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: connector, Kind: wallpaper.KindEffect, Effect: "justice-cross", Theme: "catppuccin-mocha"})
		awaitArt(t, svc, connector, runningEffect("justice-cross", "catppuccin-mocha"))
	}
	for range 2 {
		h := openArtPanel(t, reg)
		reg.mu.Lock()
		if findAction(h.root, "art-pause:DP-1") == nil || findAction(h.root, "art-pause:DP-3") == nil {
			t.Error("each playing output offers its own Pause")
		}
		reg.closePanelLocked(PanelTerminalArt)
		reg.mu.Unlock()
		drainAuxQueue(reg)
	}
}

func TestTerminalArtPauseActsOnItsOwnOutput(t *testing.T) {
	reg, svc := artRegistry(t, artWallpaperEngine{})
	for _, connector := range []string{"DP-1", "DP-3"} {
		svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: connector, Kind: wallpaper.KindEffect, Effect: "fire", Theme: "nord"})
		awaitArt(t, svc, connector, runningEffect("fire", "nord"))
	}
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	artAct(t, reg, h, "art-pause:DP-1")
	reg.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && svc.Snapshot().Runtime["DP-1"].State != wallpaper.StatePaused {
		time.Sleep(5 * time.Millisecond)
	}
	snap := svc.Snapshot()
	if snap.Runtime["DP-1"].State != wallpaper.StatePaused || snap.Runtime["DP-3"].State != wallpaper.StatePlaying {
		t.Fatalf("after DP-1 Pause: DP-1=%v DP-3=%v, want paused and playing", snap.Runtime["DP-1"].State, snap.Runtime["DP-3"].State)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h.wallpaperSnap = snap
	reg.rebuildPanel(h)
	if findAction(h.root, "art-resume:DP-1") == nil || findAction(h.root, "art-pause:DP-3") == nil {
		t.Fatal("each row should offer the control its own state supports")
	}
}

// The palette list is an overlay: opening it must not push the grid down,
// which the inline list it replaced did.
func TestTerminalArtPaletteMenuDoesNotMoveTheGrid(t *testing.T) {
	reg, _ := artRegistry(t, artWallpaperEngine{})
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	before := findAction(h.root, "art-apply:fire").Bounds
	h.setFocus(findAction(h.root, artPaletteMenu))
	if !h.activate(reg) || !h.menus[artPaletteMenu].Opened() {
		t.Fatal("the palette menu did not open")
	}
	if after := findAction(h.root, "art-apply:fire").Bounds; after != before {
		t.Fatalf("fire card moved from %+v to %+v when the palette opened", before, after)
	}
}

// longNamesArtEngine is a catalog of long names on outputs with long
// connectors: what the panel's fixed widths have to absorb.
type longNamesArtEngine struct{ stubWallpaperEngine }

func (longNamesArtEngine) Capabilities() wallpaper.Capabilities {
	effects := []string{"an-effect-name-far-longer-than-any-card-is-wide"}
	for i := range 30 {
		effects = append(effects, fmt.Sprintf("effect-%02d", i))
	}
	return wallpaper.Capabilities{
		GSlapper: true, Terminal: true, Statics: []string{"awww"},
		Catalog: wallpaper.Catalog{Effects: effects, Themes: []string{"a-palette-with-an-unreasonably-long-name"}},
	}
}
func (e longNamesArtEngine) RefreshTerminalCatalog() wallpaper.Capabilities { return e.Capabilities() }

// A fit error closes the panel, so every state the data can put it in must
// lay out and paint at the desktop's 1.0 and the laptop's 1.25.
func TestTerminalArtLongNamesNeverFailLayout(t *testing.T) {
	connectors := []string{"HDMI-A-1", "DP-1", "eDP-1"}
	for _, scale := range []int{120, 150} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			reg, svc := artRegistry(t, longNamesArtEngine{}, connectors...)
			long := longNamesArtEngine{}.Capabilities().Catalog
			for i, connector := range connectors {
				svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: connector, Kind: wallpaper.KindEffect, Effect: long.Effects[i%2], Theme: long.Themes[0]})
				awaitArt(t, svc, connector, runningEffect(long.Effects[i%2], long.Themes[0]))
			}
			if err := reg.OpenPanel(PanelTerminalArt, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1536, OutH: 864}); err != nil {
				t.Fatal(err)
			}
			panel := drainAux(t, reg, 2)[1].Open
			paintPluginStorePNG(t, panel, scale, filepath.Join(t.TempDir(), "art.png"))
			reg.mu.Lock()
			h := reg.panelHosts[PanelTerminalArt]
			h.setFocus(findAction(h.root, artPaletteMenu))
			h.activate(reg)
			reg.mu.Unlock()
			paintPluginStorePNG(t, panel, scale, filepath.Join(t.TempDir(), "art-menu.png"))
		})
	}
}

// Walking the grid past the bottom of its viewport scrolls the focused card
// into view rather than leaving focus on a card the user cannot see.
func TestTerminalArtArrowKeysKeepFocusInView(t *testing.T) {
	reg, _ := artRegistry(t, longNamesArtEngine{}, "DP-1")
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	grid := keyedScroll(h.root, artGridKey)
	for range 10 {
		if !h.keyPress(reg, keyDown) {
			t.Fatal("down arrow was not handled by the grid")
		}
	}
	grid = keyedScroll(h.root, artGridKey)
	card := h.focused()
	if grid.ScrollOffset == 0 || card == nil ||
		card.Bounds.Y < grid.Bounds.Y || card.Bounds.Y+card.Bounds.H > grid.Bounds.Y+grid.Bounds.H {
		t.Fatalf("focused %+v outside grid %+v at offset %d", card.Bounds, grid.Bounds, grid.ScrollOffset)
	}
}
