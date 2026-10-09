package shell

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
)

// TestAssetWallpaperPanel shows the wallpaper picker over a library of
// generated images in a fictional ~/Pictures/Wallpapers, so no real wallpaper or
// folder appears. The previews go through the shell's own generator and decoder.
func TestAssetWallpaperPanel(t *testing.T) {
	assetsDir(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	lib := filepath.Join(home, "Pictures", "Wallpapers")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	hues := []color.NRGBA{{R: 230, G: 90, B: 70, A: 255}, {R: 60, G: 150, B: 220, A: 255}, {R: 90, G: 190, B: 120, A: 255},
		{R: 170, G: 90, B: 200, A: 255}, {R: 240, G: 180, B: 60, A: 255}, {R: 70, G: 200, B: 190, A: 255}}
	for i, hue := range hues {
		data, err := os.ReadFile(assetGradientFile(t, 1280, 720, hue, color.NRGBA{R: 15, G: 20, B: 45, A: 255}))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(lib, fmt.Sprintf("scene-%02d.png", i+1)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	library := wallpaper.Scan([]string{lib})

	thumbs := wallpaper.NewThumbnailer(wallpaper.CacheDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go thumbs.Run(ctx)
	thumbs.Enqueue(library.All(), lib)
	deadline := time.Now().Add(10 * time.Second)
	for thumbs.Counts().Done < len(hues) {
		if time.Now().After(deadline) {
			t.Fatalf("previews not generated: %+v", thumbs.Counts())
		}
		time.Sleep(20 * time.Millisecond)
	}

	reg := newPanelRegistry(t)
	keepInvalidationsDrained(t, reg)
	if err := reg.OpenPanel(PanelWallpaper, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: assetOutW, OutH: assetOutH}); err != nil {
		t.Fatal(err)
	}
	panel := drainAux(t, reg, 2)[1].Open
	h := reg.panelHosts[PanelWallpaper]
	settleHostAnimation(reg, h)
	reg.mu.Lock()
	assetBase(t, reg)
	h.wallpaperDir = lib
	h.wallpaperSnap = wallpaper.Snapshot{
		Library: library, Connectors: []string{"eDP-1"}, Caps: wallpaper.Capabilities{GSlapper: true},
		Assignments: map[string]wallpaper.Assignment{"eDP-1": {Kind: wallpaper.KindImage, Path: filepath.Join(lib, "scene-02.png")}},
	}
	reg.mu.Unlock()

	// The first paint asks the decoder for each preview; the second shows them.
	paintAssetPanel(t, reg, PanelWallpaper, panel, "wallpaper", "default")
	time.Sleep(500 * time.Millisecond)
	paintAssetPanel(t, reg, PanelWallpaper, panel, "wallpaper", "default")
}
