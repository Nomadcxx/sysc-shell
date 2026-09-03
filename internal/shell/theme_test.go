package shell

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
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
		{"zero control height", func(th *Theme) { th.ControlHeight = 0 }},
		{"zero compact height", func(th *Theme) { th.CompactHeight = 0 }},
		{"zero icon size", func(th *Theme) { th.IconSize = 0 }},
		{"negative button padding", func(th *Theme) { th.ButtonPadding = -1 }},
		{"negative card radius", func(th *Theme) { th.CardRadius = -1 }},
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
	// The container is darker than the token this test first carried: the
	// resolver repairs a foreground that cannot reach its floor, and this
	// test asserts an identity mapping, so its palette has to pass on its own.
	tok := paletteWith(func(t *theme.Tokens) {
		t.Surface, t.SurfaceContainer = "#111318", "#181a1d"
		t.SurfaceContainerHigh, t.SurfaceContainerHighest = "#25272b", "#303238"
		t.OnSurface = "#e2e2e6"
		t.Primary, t.OnPrimary = "#a8c7fa", "#0a1f3d"
		t.PrimaryContainer, t.OnPrimaryContainer = "#0a4a5c", "#d6e3ff"
		t.OnSurfaceVariant, t.Outline = "#c3c6cf", "#8d9199"
		t.OutlineVariant, t.Error, t.OnError = "#45484f", "#ffb4ab", "#310001"
	})
	th := ThemeFromTokens(tok, 12)
	if th.Background != parseColor(tok.Surface, Color{}) || th.Foreground != parseColor(tok.OnSurface, Color{}) ||
		th.Accent != parseColor(tok.Primary, Color{}) || th.Muted != parseColor(tok.OnSurfaceVariant, Color{}) ||
		th.Error != parseColor(tok.Error, Color{}) || th.Radius != 12 {
		t.Fatalf("mapping wrong: %+v", th)
	}
	// The capsule palette. Muted stays OnSurfaceVariant, which is the meter
	// track, so a capsule must not borrow it.
	if th.Capsule != parseColor(tok.SurfaceContainerHigh, Color{}) {
		t.Errorf("Capsule = %+v, want SurfaceContainerHigh", th.Capsule)
	}
	// The bar and the panels share one Surface, so a capsule and a card are
	// the same fill. They must not drift back onto separate levels.
	if th.Capsule != th.SurfaceContainerHigh {
		t.Errorf("Capsule = %+v, want the card fill %+v", th.Capsule, th.SurfaceContainerHigh)
	}
	if th.SurfaceContainerHigh != parseColor(tok.SurfaceContainerHigh, Color{}) {
		t.Errorf("SurfaceContainerHigh = %+v, want generated token", th.SurfaceContainerHigh)
	}
	if th.SurfaceContainerHighest != parseColor(tok.SurfaceContainerHighest, Color{}) {
		t.Errorf("SurfaceContainerHighest = %+v, want generated token", th.SurfaceContainerHighest)
	}
	if th.OutlineVariant != parseColor(tok.OutlineVariant, Color{}) {
		t.Errorf("OutlineVariant = %+v, want generated token", th.OutlineVariant)
	}
	if th.OnError != parseColor(tok.OnError, Color{}) {
		t.Errorf("OnError = %+v, want generated token", th.OnError)
	}
	if th.Container != parseColor(tok.PrimaryContainer, Color{}) {
		t.Errorf("Container = %+v, want PrimaryContainer", th.Container)
	}
	if th.OnAccent != parseColor(tok.OnPrimary, Color{}) {
		t.Errorf("OnAccent = %+v, want OnPrimary", th.OnAccent)
	}
	if th.OnContainer != parseColor(tok.OnPrimaryContainer, Color{}) {
		t.Errorf("OnContainer = %+v, want OnPrimaryContainer", th.OnContainer)
	}
	if th.Capsule == th.Muted {
		t.Error("capsule fill must not be the meter track colour")
	}
}

func TestDefaultThemeCarriesCapsulePadding(t *testing.T) {
	t.Parallel()
	if got := DefaultTheme().CapsulePadding; got != 8 {
		t.Fatalf("CapsulePadding = %d, want 8", got)
	}
}

func TestDefaultThemeCarriesChromeMetrics(t *testing.T) {
	t.Parallel()
	th := DefaultTheme()
	if th.ControlHeight != 40 || th.CompactHeight != 32 || th.ButtonPadding != 12 {
		t.Fatalf("control metrics = %d/%d padding %d, want 40/32 padding 12",
			th.ControlHeight, th.CompactHeight, th.ButtonPadding)
	}
	if th.IconSize != 20 || th.ProfileIconSize != 18 || th.OSDIconSize != 24 {
		t.Fatalf("icon metrics = %d/%d/%d, want 20/18/24",
			th.IconSize, th.ProfileIconSize, th.OSDIconSize)
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

// contrast is the WCAG ratio between two opaque colours.
func contrast(a, b Color) float64 {
	lum := func(c Color) float64 {
		ch := func(v uint8) float64 {
			f := float64(v) / 255
			if f <= 0.03928 {
				return f / 12.92
			}
			return math.Pow((f+0.055)/1.055, 2.4)
		}
		return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
	}
	hi, lo := lum(a), lum(b)
	if hi < lo {
		hi, lo = lo, hi
	}
	return (hi + 0.05) / (lo + 0.05)
}

// The capsule palette was measured against a live reference bar. These bounds
// stop a future palette edit from silently returning the bar to the state where
// pills were present but invisible.
func TestDefaultPaletteKeepsCapsulesAndPillsVisible(t *testing.T) {
	t.Parallel()
	th := DefaultTheme()

	if got := contrast(th.Background, th.Capsule); got < 1.45 {
		t.Errorf("capsule/bar contrast = %.3f:1, want at least 1.45 so cards read as pills", got)
	}
	if got := contrast(th.Surface, th.SurfaceContainerHigh); got < 1.45 {
		t.Errorf("card/panel contrast = %.3f:1, want at least 1.45", got)
	}
	if got := contrast(th.OnSurface, th.SurfaceContainerHigh); got < 4.5 {
		t.Errorf("text/card contrast = %.2f:1, want at least 4.5", got)
	}
	if got := contrast(th.Outline, th.Surface); got < 3.0 {
		t.Errorf("outline/panel contrast = %.2f:1, want at least 3.0", got)
	}
	// An unfocused workspace pill has to be a surface, not a tint of the bar.
	if got := contrast(th.Background, th.Container); got < 2.5 {
		t.Errorf("pill/bar contrast = %.2f:1, want at least 2.5 (reference is 3.5)", got)
	}
	if got := contrast(th.Background, th.Accent); got < 3.0 {
		t.Errorf("focused pill/bar contrast = %.2f:1, want at least 3.0", got)
	}
	// Numerals must stay legible on the fill their capsule supplies.
	if got := contrast(th.Accent, th.OnAccent); got < 3.0 {
		t.Errorf("numeral on the focused pill = %.2f:1, want at least 3.0", got)
	}
	if got := contrast(th.Container, th.OnContainer); got < 3.0 {
		t.Errorf("numeral on an unfocused pill = %.2f:1, want at least 3.0", got)
	}
}

// Every surface the shell paints follows the generated palette. ThemeFrom
// resolves against theme.Fallback and never reads the generated tokens, so a
// surface built through it paints the built-in colours whatever the user chose.
func TestSurfaceThemeFollowsTheGeneratedPalette(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)

	r.mu.Lock()
	r.tokens = paletteWith(func(t *theme.Tokens) {
		t.Surface, t.SurfaceContainer = "#101010", "#202020"
		t.OnSurface, t.OnSurfaceVariant = "#f0f0f0", "#a0a0a0"
		t.Primary, t.OnPrimary = "#ff00ff", "#000000"
		t.PrimaryContainer, t.OnPrimaryContainer = "#800080", "#ffffff"
		t.Outline, t.Error, t.OnError = "#303030", "#ff0000", "#ffffff"
	})
	got := r.surfaceTheme()
	r.mu.Unlock()

	want := parseColor("#ff00ff", Color{})
	if got.Accent != want {
		t.Fatalf("accent = %+v, want the generated primary %+v", got.Accent, want)
	}
	if fallback := ThemeFrom(cfg, cfg.Bar); got.Accent == fallback.Accent {
		t.Fatal("the surface theme resolved to the fallback palette")
	}
}

// A surface that resolves its own theme through ThemeFrom is painting the
// fallback whatever the user chose. The toast stack, the tray menu and the
// tray drawer each did, so notifications and the tray ignored the theme.
func TestNoShellSurfaceResolvesTheFallbackPalette(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	// theme.go declares ThemeFrom; bar.go's New is the documented registry-free
	// constructor, a bare Bar with no generated palette in reach, and has no
	// caller outside tests. Every other file paints a real surface.
	allowed := map[string]bool{"theme.go": true, "bar.go": true}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || allowed[name] {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(src, []byte("ThemeFrom(")) {
			t.Fatalf("%s resolves a theme through ThemeFrom, which ignores the generated tokens; use Registry.surfaceTheme", name)
		}
	}
}

func TestLerpColorsTravelsThePaletteButNotTheGeometry(t *testing.T) {
	t.Parallel()
	from := DefaultTheme()
	to := ThemeFromTokens(lightPalette(), 12)

	if got := from.LerpColors(to, 0); got.Surface != from.Surface {
		t.Errorf("progress 0 surface = %+v, want the outgoing %+v", got.Surface, from.Surface)
	}
	if got := from.LerpColors(to, 1); got.Surface != to.Surface {
		t.Errorf("progress 1 surface = %+v, want the incoming %+v", got.Surface, to.Surface)
	}

	mid := from.LerpColors(to, 0.5)
	if mid.Surface == from.Surface || mid.Surface == to.Surface {
		t.Errorf("midpoint surface = %+v, want a blend of %+v and %+v", mid.Surface, from.Surface, to.Surface)
	}
	// Derived names must follow the roles they alias rather than blend twice.
	if mid.Background != mid.Surface || mid.Capsule != mid.SurfaceContainerHigh {
		t.Error("legacy aliases drifted from the roles they derive from during the blend")
	}
	// Geometry arrives whole: a control that resized mid-fade would relayout
	// on every frame.
	if mid.ControlHeight != to.ControlHeight || mid.Radius != to.Radius {
		t.Errorf("geometry interpolated: height %d radius %d, want %d and %d",
			mid.ControlHeight, mid.Radius, to.ControlHeight, to.Radius)
	}
}

// paletteWith returns the compiled fallback with specific roles replaced. The
// resolver rejects an incomplete palette, so a test names the two or three
// roles it cares about rather than hand-writing all of them.
func paletteWith(set func(*theme.Tokens)) theme.Tokens {
	tok := theme.Fallback
	set(&tok)
	return tok
}

// lightPalette is a complete light-mode palette. It is generated output
// rather than a handful of greys: the resolver repairs a foreground that
// cannot reach its floor, so a palette mixing light surfaces with the dark
// fallback's untouched rungs would be repaired into something neither light
// nor dark.
func lightPalette() theme.Tokens {
	return paletteWith(func(t *theme.Tokens) {
		t.Primary = "#904b40"
		t.OnPrimary = "#ffffff"
		t.PrimaryContainer = "#ffdad4"
		t.OnPrimaryContainer = "#3a0905"
		t.Secondary = "#775651"
		t.OnSecondary = "#ffffff"
		t.SecondaryContainer = "#ffdad4"
		t.OnSecondaryContainer = "#2c1512"
		t.Tertiary = "#705c2e"
		t.OnTertiary = "#ffffff"
		t.TertiaryContainer = "#fbdfa6"
		t.OnTertiaryContainer = "#251a00"
		t.Error = "#ba1a1a"
		t.OnError = "#ffffff"
		t.ErrorContainer = "#ffdad6"
		t.OnErrorContainer = "#410002"
		t.Surface = "#fff8f6"
		t.OnSurface = "#231918"
		t.SurfaceVariant = "#f5ddda"
		t.OnSurfaceVariant = "#534341"
		t.SurfaceDim = "#e8d6d3"
		t.SurfaceBright = "#fff8f6"
		t.SurfaceContainerLowest = "#ffffff"
		t.SurfaceContainerLow = "#fff0ee"
		t.SurfaceContainer = "#fceae7"
		t.SurfaceContainerHigh = "#f7e4e1"
		t.SurfaceContainerHighest = "#f1dfdc"
		t.Background = "#fff8f6"
		t.OnBackground = "#231918"
		t.Outline = "#857370"
		t.OutlineVariant = "#d8c2be"
		t.InverseSurface = "#392e2c"
		t.InverseOnSurface = "#ffedea"
		t.InversePrimary = "#ffb4a8"
		t.Shadow = "#000000"
		t.Scrim = "#000000"
		t.SurfaceTint = "#904b40"
		t.PrimaryFixed = "#ffdad4"
		t.PrimaryFixedDim = "#ffb4a8"
		t.OnPrimaryFixed = "#3a0905"
		t.OnPrimaryFixedVariant = "#73342a"
		t.SecondaryFixed = "#ffdad4"
		t.SecondaryFixedDim = "#e7bdb6"
		t.OnSecondaryFixed = "#2c1512"
		t.OnSecondaryFixedVariant = "#5d3f3b"
		t.TertiaryFixed = "#fbdfa6"
		t.TertiaryFixedDim = "#dec48c"
		t.OnTertiaryFixed = "#251a00"
		t.OnTertiaryFixedVariant = "#564419"
	})
}

func TestResolveThemeValidatesEveryGroup(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	got, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	if got.Metrics.BarHeight != 48 || got.Metrics.StandardControl != 40 {
		t.Errorf("metrics = %+v, want the standard row", got.Metrics)
	}
	if got.Shapes.Medium != 12 || got.Shapes.Card != 12 {
		t.Errorf("shapes = %+v, want the 12 px base", got.Shapes)
	}
	if got.Type.Spec(theme.RoleBody).Size != 14 || got.Type.Spec(theme.RoleTitle).Weight != 600 {
		t.Errorf("type = %+v, want the standard ramp", got.Type)
	}
	if got.Surfaces != (Surfaces{Bar: 0xff, Panel: 0xff, Overlay: 0xff}) {
		t.Errorf("surfaces = %+v, want opaque", got.Surfaces)
	}
	if got.Motion.Durations.Medium != 180*time.Millisecond {
		t.Errorf("motion medium = %v, want 180ms", got.Motion.Durations.Medium)
	}
	if got.Motion.Spatial != theme.CurveOutCubic {
		t.Errorf("curve = %q, want out-cubic", got.Motion.Spatial)
	}
	if got.Palette.Surface == (Color{}) || got.Palette.Scrim == (Color{}) {
		t.Error("the palette did not parse")
	}
}

func TestResolveThemeRejectsAnIncompletePalette(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	tok := theme.Fallback
	tok.SurfaceContainerHigh = ""
	if _, err := ResolveTheme(cfg, cfg.Bar, tok); err == nil {
		t.Fatal("ResolveTheme accepted an incomplete palette")
	}
}

func TestResolveThemeAppliesCompositionAndBarOverride(t *testing.T) {
	t.Parallel()
	doc := `{"theme":{"preset":"compact"},"bar":{"height":52}}`
	cfg, err := config.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	// The compact row supplies the control heights and icons.
	if got.Metrics.StandardControl != 36 || got.Metrics.IconNormal != 18 {
		t.Errorf("metrics = %+v, want the compact row", got.Metrics)
	}
	// The explicit bar height wins over the row it came from.
	if got.Metrics.BarHeight != 52 || got.BarHeight != 52 {
		t.Errorf("bar height = %d, want the explicit 52", got.BarHeight)
	}
	if got.Shapes.Medium != 8 {
		t.Errorf("radius = %d, want compact's 8", got.Shapes.Medium)
	}
	if got.Motion.Durations.Medium != 144*time.Millisecond {
		t.Errorf("medium = %v, want compact's 144ms", got.Motion.Durations.Medium)
	}
}

func TestResolveThemeAppliesAccessibilityAfterBarOverrides(t *testing.T) {
	t.Parallel()
	doc := `{"theme":{"preset":"expressive"},"bar":{"height":52},"accessibility":{"high-contrast":true}}`
	cfg, err := config.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveTheme(cfg, cfg.Bar, theme.FallbackHighContrast)
	if err != nil {
		t.Fatal(err)
	}
	// Expressive asks for 95 percent panels; high contrast overrides that
	// after every other axis has been applied.
	if got.Surfaces != (Surfaces{Bar: 0xff, Panel: 0xff, Overlay: 0xff}) {
		t.Errorf("surfaces = %+v, want high contrast to force opaque", got.Surfaces)
	}
	if !got.Outlined {
		t.Error("high contrast did not enable the structural outline")
	}
	// The bar override still stands.
	if got.BarHeight != 52 {
		t.Errorf("bar height = %d, want the explicit 52", got.BarHeight)
	}
}

func TestResolveThemeRepairsAFailingForeground(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	tok := theme.Fallback
	tok.OnSurface = tok.Surface // unreadable
	got, err := ResolveTheme(cfg, cfg.Bar, tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.Palette.OnSurface == got.Palette.Surface {
		t.Error("the resolver painted text in its own background colour")
	}
}

func TestResolveThemeCarriesReducedMotion(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	got, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Motion.Reduced {
		t.Error("reduced motion did not reach the resolved theme")
	}
}

// TestNoShellFileOutsideThemeAssemblesAStyle keeps the resolver the only place
// a palette or a composition is put together. A second assembly site is how
// two surfaces end up a shade apart, which is the defect this tranche exists
// to remove.
func TestNoShellFileOutsideThemeAssemblesAStyle(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") || name == "theme.go" {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"render.Style{", "Palette{"} {
			if strings.Contains(string(body), banned) {
				t.Errorf("%s assembles %s; resolve it in theme.go instead", name, banned)
			}
		}
	}
}
