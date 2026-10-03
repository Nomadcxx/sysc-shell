package shell

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
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
			Effects: []wallpaper.EffectInfo{{ID: "fire"}, {ID: "rain"}, {ID: "fire-text", Text: true}},
			Themes:  []string{"nord", "dracula"},
		},
	}
}

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

func TestTerminalArtCardsFromCatalog(t *testing.T) {
	reg, _ := artRegistry(t, artWallpaperEngine{})
	h := openArtPanel(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	var cards []string
	collectActions(h.root, "art-apply:", &cards)
	if !slices.Equal(cards, []string{"art-apply:fire", "art-apply:rain"}) {
		t.Fatalf("cards = %v, want fire and rain; text effects are hidden", cards)
	}
	for _, action := range cards {
		card := findAction(h.root, action)
		if card.Name != strings.TrimPrefix(action, "art-apply:") || !card.Focusable || card.State&ui.StateDisabled != 0 {
			t.Errorf("card %s = name %q focusable %v state %v", action, card.Name, card.Focusable, card.State)
		}
	}
	if findByName(h.root, "fire-text") != nil {
		t.Error("a text effect is listed")
	}
	if !slices.Contains(artTexts(h.root), "2 effects") {
		t.Errorf("footer missing; texts = %v", artTexts(h.root))
	}
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
	if findAction(h.root, "art-menu:palette") != nil {
		t.Error("the palette combo must be hidden without sysc-terminal")
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
	combo := findAction(h.root, "art-menu:palette")
	if got := artPalette(h); got != "dracula" || combo == nil || !strings.Contains(combo.Name, "dracula") {
		t.Fatalf("palette = %q, combo %+v; want the running dracula, not the catalog's first theme", got, combo)
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
	artAct(t, reg, h, "art-menu:palette")
	artAct(t, reg, h, "art-palette:dracula")
	artAct(t, reg, h, "art-output:DP-1")
	artAct(t, reg, h, "art-menu:palette")
	artAct(t, reg, h, "art-palette:dracula")
	reg.mu.Unlock()
	awaitArt(t, svc, "DP-1", runningEffect("fire", "dracula"))
	// Commands run in order, so a DP-3 apply would have landed by now.
	if a, ok := svc.Snapshot().Assignments["DP-3"]; ok {
		t.Fatalf("DP-3 = %+v; a palette change must not start an effect", a)
	}
}
