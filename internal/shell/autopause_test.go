package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
)

func TestAutoPauseAreaExcludesTheBar(t *testing.T) {
	cases := []struct {
		name      string
		edge      string
		thickness int
		want      wallpaper.AutoArea
	}{
		{"top", "top", 36, wallpaper.AutoArea{Y: 36, W: 1000, H: 764}},
		{"bottom", "bottom", 36, wallpaper.AutoArea{W: 1000, H: 764}},
		{"left", "left", 36, wallpaper.AutoArea{X: 36, W: 964, H: 800}},
		{"right", "right", 36, wallpaper.AutoArea{W: 964, H: 800}},
		{"clamped", "top", 900, wallpaper.AutoArea{Y: 900, W: 1000, H: 0}},
	}
	for _, tc := range cases {
		if got := autoPauseArea(tc.edge, tc.thickness, 1000, 800); got != tc.want {
			t.Fatalf("%s: area = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// newAutoPauseFixture wires a registry to a wallpaper service with one
// 1920x1080 bar output and an active workspace, so the driver has an output
// to manage and the service processes whatever the driver enqueues.
func newAutoPauseFixture(t *testing.T, engine wallpaper.Engine) (*Registry, *wallpaper.Service) {
	t.Helper()
	reg := newPanelRegistry(t)
	bar := withTestBar(t, reg, 7, reg.cfg)
	bar.setOutputSize(1920, 1080)
	svc := wallpaper.NewService(wallpaper.ServiceConfig{
		Engine:     engine,
		Settings:   wallpaper.Settings{Scale: "fill", Loop: true, FPS: 30, Hidden: wallpaper.HiddenNone},
		Connectors: []string{"DP-1"},
	})
	t.Cleanup(svc.Close)

	reg.mu.Lock()
	reg.wallpaperSvc = svc
	reg.niriSnapshot = niri.Snapshot{Workspaces: []niri.Workspace{{ID: 1, Output: "DP-1", Active: true}}}
	reg.mu.Unlock()
	return reg, svc
}

// awaitAutoPause polls the service snapshot until want holds.
func awaitAutoPause(t *testing.T, svc *wallpaper.Service, want func(wallpaper.Snapshot) bool) wallpaper.Snapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		snap := svc.Snapshot()
		if want(snap) {
			return snap
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for the autopause state; PausedBy=%v FPS=%v unsupported=%v",
				snap.PausedBy, snap.FPS, snap.FPSUnsupported)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAutoPauseLockReasonFollowsTheConfig(t *testing.T) {
	reg, svc := newAutoPauseFixture(t, stubWallpaperEngine{})

	reg.SetLockActive(true)
	awaitAutoPause(t, svc, func(s wallpaper.Snapshot) bool {
		return s.PausedBy["DP-1"].Has(wallpaper.ReasonLock)
	})

	reg.SetLockActive(false)
	awaitAutoPause(t, svc, func(s wallpaper.Snapshot) bool {
		return !s.PausedBy["DP-1"].Has(wallpaper.ReasonLock)
	})

	reg.mu.Lock()
	reg.cfg.Wallpaper.PauseLocked = false
	reg.mu.Unlock()

	reg.SetLockActive(true)
	reg.mu.Lock()
	applied := reg.autoPauseApplied["DP-1"]
	reg.mu.Unlock()
	if applied.Has(wallpaper.ReasonLock) {
		t.Fatalf("pause_locked=false still applied the lock reason: %v", applied)
	}
}

// TestAutoPauseCoversAFullscreenTileWithoutAPosition is the Niri 26.04
// shape: a tiled window has a tile size and no view position, so only a tile
// the size of the whole output (a fullscreen window) counts as covering.
func TestAutoPauseCoversAFullscreenTileWithoutAPosition(t *testing.T) {
	reg, svc := newAutoPauseFixture(t, stubWallpaperEngine{})

	reg.mu.Lock()
	reg.niriSnapshot.Windows = []niri.Window{{
		WorkspaceID: 1, HasWorkspace: true,
		HasLayout: true, TileW: 1920, TileH: 1080,
	}}
	reg.mu.Unlock()
	reg.syncAutoPause()
	awaitAutoPause(t, svc, func(s wallpaper.Snapshot) bool {
		return s.PausedBy["DP-1"].Has(wallpaper.ReasonOccluded)
	})

	// A tile that only spans the working area cannot be proven to be in
	// view without a position, so the cover clears again.
	reg.mu.Lock()
	reg.niriSnapshot.Windows[0].TileW = 900
	reg.mu.Unlock()
	reg.syncAutoPause()
	awaitAutoPause(t, svc, func(s wallpaper.Snapshot) bool {
		return !s.PausedBy["DP-1"].Has(wallpaper.ReasonOccluded)
	})
}

func TestLockerPausedCallbackDrivesTheDriver(t *testing.T) {
	reg, svc := newAutoPauseFixture(t, stubWallpaperEngine{})

	reg.mu.Lock()
	m := reg.lockerLocked()
	reg.mu.Unlock()

	m.paused(true)
	awaitAutoPause(t, svc, func(s wallpaper.Snapshot) bool {
		return s.PausedBy["DP-1"].Has(wallpaper.ReasonLock)
	})

	m.paused(false)
	awaitAutoPause(t, svc, func(s wallpaper.Snapshot) bool {
		return !s.PausedBy["DP-1"].Has(wallpaper.ReasonLock)
	})
}

func TestAutoPauseLockBackgroundFollowsTheConfig(t *testing.T) {
	reg, svc := newAutoPauseFixture(t, stubWallpaperEngine{})

	reg.mu.Lock()
	reg.backgroundHeld = true
	reg.mu.Unlock()
	reg.syncAutoPause()
	awaitAutoPause(t, svc, func(s wallpaper.Snapshot) bool {
		return s.PausedBy["DP-1"].Has(wallpaper.ReasonLockBackground)
	})

	reg.mu.Lock()
	reg.backgroundHeld = false
	reg.mu.Unlock()
	reg.syncAutoPause()
	awaitAutoPause(t, svc, func(s wallpaper.Snapshot) bool {
		return !s.PausedBy["DP-1"].Has(wallpaper.ReasonLockBackground)
	})

	reg.mu.Lock()
	reg.cfg.Wallpaper.PauseLocked = false
	reg.backgroundHeld = true
	reg.mu.Unlock()
	reg.syncAutoPause()
	reg.mu.Lock()
	applied := reg.autoPauseApplied["DP-1"]
	reg.mu.Unlock()
	if applied.Has(wallpaper.ReasonLockBackground) {
		t.Fatalf("pause_locked=false still applied the lock background reason: %v", applied)
	}
}

// unsupportedFPSEngine answers every rate command with the unknown-command
// error, the way an older terminal-art build does.
type unsupportedFPSEngine struct{ stubWallpaperEngine }

func (unsupportedFPSEngine) SetFPS(string, int) error { return wallpaper.ErrFPSUnsupported }

func TestAutoPauseFallsBackToPauseWhenTheEngineRejectsTheRate(t *testing.T) {
	reg, svc := newAutoPauseFixture(t, unsupportedFPSEngine{})
	go reg.relayWallpaper(svc)

	svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: "DP-1", Kind: wallpaper.KindEffect, Effect: "fire", Theme: "nord"})
	awaitAutoPause(t, svc, func(s wallpaper.Snapshot) bool {
		return s.Assignments["DP-1"].Kind == wallpaper.KindEffect && s.Runtime["DP-1"].State == wallpaper.StatePlaying
	})

	reg.mu.Lock()
	reg.cfg.Wallpaper.OnBattery = wallpaper.OnBatteryLimit
	reg.autoPauseBattery = wallpaper.AutoBattery{Present: true, OnBattery: true, Charge: 80, ChargeValid: true}
	reg.mu.Unlock()
	reg.syncAutoPause()

	snap := awaitAutoPause(t, svc, func(s wallpaper.Snapshot) bool {
		return s.PausedBy["DP-1"].Has(wallpaper.ReasonBattery)
	})
	if !snap.FPSUnsupported["DP-1"] {
		t.Fatalf("rate rejection was not reported: %v", snap.FPSUnsupported)
	}
	if snap.FPS["DP-1"] != 10 {
		t.Fatalf("FPS = %d, want 10", snap.FPS["DP-1"])
	}
}
