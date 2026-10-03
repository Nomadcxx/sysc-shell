package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
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
// DP-1 and DP-3 backed by engine. No panel is open yet.
func artRegistry(t *testing.T, engine wallpaper.Engine) (*Registry, *wallpaper.Service) {
	t.Helper()
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	svc := wallpaper.NewService(wallpaper.ServiceConfig{
		Engine:     engine,
		Settings:   wallpaper.Settings{Scale: "fill", Loop: true, FPS: 30, Hidden: wallpaper.HiddenNone},
		Connectors: []string{"DP-1", "DP-3"},
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
	drainAuxQueue(reg)
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
