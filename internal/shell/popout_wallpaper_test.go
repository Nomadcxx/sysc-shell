package shell

import (
	"testing"

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
