package shell

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestDefaultThemeGeometryMatchesTheBaseline(t *testing.T) {
	t.Parallel()
	surface, body, gap := DefaultTheme().Geometry()
	if gap != 4 {
		t.Fatalf("gap = %d, want 4", gap)
	}
	if body != 40 {
		t.Fatalf("body = %d, want 40, which is height 48 minus twice the gap", body)
	}
	// The surface height is also the exclusive zone, so Niri windows begin 44
	// logical pixels from the screen edge.
	if surface != 44 {
		t.Fatalf("surface = %d, want 44, which is gap plus body", surface)
	}
}

func TestDefaultThemeMatchesDMSContentBand(t *testing.T) {
	t.Parallel()
	th := DefaultTheme()
	if th.BarPadding != 6 {
		t.Fatalf("bar padding = %d, want 6 for a 28px item band inside the 40px body", th.BarPadding)
	}
	if th.Spacing != 4 {
		t.Fatalf("item spacing = %d, want the DMS reference value 4", th.Spacing)
	}
}

func TestNominalHeightIsATokenNotASurfaceDimension(t *testing.T) {
	t.Parallel()
	th := DefaultTheme()
	surface, _, _ := th.Geometry()
	if surface == th.BarHeight {
		t.Fatal("the surface equals the nominal height; 48 must not reach Wayland as a dimension")
	}
}

func TestThemeGeometryScalesWithTokens(t *testing.T) {
	t.Parallel()
	th := DefaultTheme()
	th.BarHeight, th.BarGap = 60, 6
	surface, body, gap := th.Geometry()
	if gap != 6 || body != 48 || surface != 54 {
		t.Fatalf("geometry = surface %d body %d gap %d, want 54/48/6", surface, body, gap)
	}
}

func TestThemeValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*Theme)
	}{
		{"height leaves no body", func(th *Theme) { th.BarHeight = 8 }},
		{"negative gap", func(th *Theme) { th.BarGap = -1 }},
		{"zero text size", func(th *Theme) { th.TextSize = 0 }},
		{"negative radius", func(th *Theme) { th.Radius = -2 }},
		{"negative padding", func(th *Theme) { th.BarPadding = -1 }},
		{"negative spacing", func(th *Theme) { th.Spacing = -1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			th := DefaultTheme()
			c.mutate(&th)
			if err := th.Valid(); err == nil {
				t.Fatalf("Valid() accepted %s", c.name)
			}
		})
	}

	if err := DefaultTheme().Valid(); err != nil {
		t.Fatalf("the default theme is invalid: %v", err)
	}
}

func TestTokensResolveToBarTheme(t *testing.T) {
	tok := theme.Tokens{
		Surface: "#111318", OnSurface: "#e2e2e6", Primary: "#a8c7fa",
		OnSurfaceVariant: "#c3c6cf", Error: "#ffb4ab",
	}
	th := ThemeFromTokens(tok, 12)
	if th.Background != parseColor(tok.Surface, Color{}) || th.Foreground != parseColor(tok.OnSurface, Color{}) ||
		th.Accent != parseColor(tok.Primary, Color{}) || th.Muted != parseColor(tok.OnSurfaceVariant, Color{}) ||
		th.Error != parseColor(tok.Error, Color{}) || th.Radius != 12 {
		t.Fatalf("mapping wrong: %+v", th)
	}
}

func TestRegistryGeneratesThemeAtStartup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	stub := "#!/bin/sh\ncat > colors.json <<'EOF'\n" +
		`{"dark":{"surface":"#111318","on_surface":"#e2e2e6","primary":"#a8c7fa"},"light":{"surface":"#faf9fd","on_surface":"#1a1c1e","primary":"#3b5ba9"}}` +
		"\nEOF\n"
	if err := os.WriteFile(filepath.Join(dir, "matugen"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.ThemeGen.Source = "wallpaper"
	cfg.ThemeGen.Seed = "/tmp/wall.jpg"
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	if reg.Tokens() == (theme.Tokens{}) {
		t.Fatal("registry must hold generated tokens after construction")
	}
}

func TestAThemeOnlyReloadDoesNotRestartMetricsOrWeather(t *testing.T) {
	cfg := weatherConfig()
	cfg.Bar.Right = []config.Item{{ID: "cpu", Display: "text", Interval: 2 * time.Second}}
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	metricsStarts := reg.Metrics().Starts()
	weatherStarts := reg.Weather().Starts()
	if metricsStarts == 0 || weatherStarts == 0 {
		t.Fatalf("expected leased metrics and weather, starts %d/%d", metricsStarts, weatherStarts)
	}

	candidate := cfg
	candidate.ThemeGen.Mode = "light"
	prepared, err := reg.PrepareConfig(candidate, identities(map[uint32]string{1: "DP-9"}))
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	prepared.Commit()

	if got := reg.Metrics().Starts(); got != metricsStarts {
		t.Fatalf("metrics starts = %d after theme reload, want %d", got, metricsStarts)
	}
	if got := reg.Weather().Starts(); got != weatherStarts {
		t.Fatalf("weather starts = %d after theme reload, want %d", got, weatherStarts)
	}
	if !reg.Metrics().Running() || !reg.Weather().Running() {
		t.Fatal("theme reload dropped a metrics or weather lease")
	}
}
