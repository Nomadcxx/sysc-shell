package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
)

// A terminal effect is thin glyphs on black; at the configured frost the
// blurred ground hides it. While one plays, a translucent bar drops its ground
// and pills to the frost floor. A bar that paints solid stays solid.
func TestEffectBehindDropsFrostToTheFloor(t *testing.T) {
	t.Parallel()
	pct := func(p int) uint8 { return uint8((p*255 + 50) / 100) }
	floor := pct(theme.OpacityMinFrost)
	solid := opacityAlpha(90, false)
	for _, tc := range []struct {
		style        string
		blur         bool
		ground, pill uint8
	}{
		{"frosted", true, floor, floor},
		{"islands", true, 0, floor},
		{"frosted", false, solid, 0xff},
		{"solid", true, solid, 0xff},
	} {
		cfg := config.Default()
		cfg.Theme.BarOpacity = 90
		bar := cfg.Bar
		bar.Style = tc.style
		base := ThemeFrom(cfg, bar)
		got := base.WithEffectBehind(true, tc.blur)
		if got.Surfaces.Bar != tc.ground || got.PillAlpha != tc.pill {
			t.Errorf("%q blur=%v: ground %#x pill %#x, want %#x %#x",
				tc.style, tc.blur, got.Surfaces.Bar, got.PillAlpha, tc.ground, tc.pill)
		}
		// The flag survives a capability change, and clearing it restores the
		// configured frost.
		if again := got.WithCompositor(tc.blur); again.Surfaces.Bar != got.Surfaces.Bar || again.PillAlpha != got.PillAlpha {
			t.Errorf("%q: WithCompositor dropped the effect flag", tc.style)
		}
		want := base.WithCompositor(tc.blur)
		if back := got.WithEffectBehind(false, tc.blur); back.Surfaces.Bar != want.Surfaces.Bar || back.PillAlpha != want.PillAlpha {
			t.Errorf("%q: clearing the effect gave %#x %#x, want the configured %#x %#x",
				tc.style, back.Surfaces.Bar, back.PillAlpha, want.Surfaces.Bar, want.PillAlpha)
		}
	}
}

// Only the output playing the effect changes, and returning it to a still
// restores the configured frost.
func TestWallpaperEffectRethemesOnlyItsOutputsBar(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.mu.Lock()
	reg.caps.Blur = true
	reg.mu.Unlock()
	dp1 := withTestBar(t, reg, 7, reg.cfg)
	dp3, leases, _, err := reg.buildBar(reg.cfg, "DP-3", reg.tokens)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { releaseAll(leases) })
	reg.setTestBar(8, dp3)
	configured := dp1.themeSnapshot().Surfaces.Bar
	floor := uint8((theme.OpacityMinFrost*255 + 50) / 100)
	if configured == floor {
		t.Fatal("the default frost already sits at the floor, so the check below proves nothing")
	}

	snap := func(state wallpaper.State) wallpaper.Snapshot {
		return wallpaper.Snapshot{
			Connectors: []string{"DP-1", "DP-3"},
			Assignments: map[string]wallpaper.Assignment{
				"DP-1": {Kind: wallpaper.KindEffect, Effect: "matrix", Theme: "nord"},
				"DP-3": {Kind: wallpaper.KindImage, Path: "/w/a.png"},
			},
			Runtime: map[string]wallpaper.Runtime{"DP-1": {State: state}, "DP-3": {State: wallpaper.StateStatic}},
		}
	}
	reg.mu.Lock()
	changed := reg.noteEffectOutputsLocked(snap(wallpaper.StatePlaying))
	reg.mu.Unlock()
	if len(changed) != 1 || changed[0] != 7 {
		t.Fatalf("rethemed outputs %v, want only DP-1's bar", changed)
	}
	if got := dp1.themeSnapshot().Surfaces.Bar; got != floor {
		t.Fatalf("DP-1 ground %#x while its effect plays, want the floor %#x", got, floor)
	}
	if got := dp3.themeSnapshot().Surfaces.Bar; got != configured {
		t.Fatalf("DP-3 ground %#x, want its configured %#x", got, configured)
	}
	// A config reload re-resolves every bar; the effect must survive it.
	cfg, tokens := reg.effectiveThemeLocked()
	if th := reg.panelThemeForState(7, cfg, tokens); th.Surfaces.Bar != floor {
		t.Fatalf("re-resolved DP-1 ground %#x, want the floor %#x", th.Surfaces.Bar, floor)
	}

	reg.mu.Lock()
	changed = reg.noteEffectOutputsLocked(snap(wallpaper.StateStatic))
	reg.mu.Unlock()
	if len(changed) != 1 || dp1.themeSnapshot().Surfaces.Bar != configured {
		t.Fatalf("after the still returned: changed %v, DP-1 ground %#x, want %#x", changed, dp1.themeSnapshot().Surfaces.Bar, configured)
	}
}
