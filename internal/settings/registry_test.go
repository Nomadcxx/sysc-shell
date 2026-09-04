package settings

import (
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"strconv"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

func TestRegistryCoversAllSections(t *testing.T) {
	t.Parallel()
	r := Default()
	sections := []string{"Bar", "Widgets", "Appearance", "Panels", "Session", "Accessibility"}
	for _, s := range sections {
		if len(r.Section(s)) == 0 {
			t.Fatalf("section %s empty", s)
		}
	}
}

func TestEntryGetSetRoundTrip(t *testing.T) {
	t.Parallel()
	r := Default()
	e := r.ByPath("bar.height")
	if e == nil || e.Kind != KindInt {
		t.Fatal("bar.height must be KindInt")
	}
	cfg := config.Default()
	if err := e.Set(&cfg, "48"); err != nil {
		t.Fatal(err)
	}
	if got := e.Get(cfg); got != "48" {
		t.Fatalf("got %q, want 48", got)
	}
}

func TestSetRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	e := Default().ByPath("bar.edge")
	if e == nil {
		t.Fatal("missing bar.edge")
	}
	if err := e.Set(&cfg, "diagonal"); err == nil {
		t.Fatal("enum must reject")
	}
	e2 := Default().ByPath("bar.height")
	if err := e2.Set(&cfg, "not-a-number"); err == nil {
		t.Fatal("int must reject")
	}
}

func TestSearchMatchesLabels(t *testing.T) {
	t.Parallel()
	// Motion is three settings now: the composition axis, its speed, and the
	// accessibility switch. The search has to reach each of them, so this
	// asserts membership rather than which one sorts first.
	want := map[string]bool{
		"appearance.motion":            false,
		"appearance.motion-speed":      false,
		"accessibility.reduced-motion": false,
	}
	for _, e := range Default().Search("motion") {
		if _, ok := want[e.Path]; ok {
			want[e.Path] = true
		}
	}
	for path, found := range want {
		if !found {
			t.Errorf("search motion did not reach %s", path)
		}
	}
}

func TestRegistryWidgetsFollowConfiguredBar(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Bar.Left = []config.Item{{ID: "window-title", MaxWidth: 200}}
	cfg.Bar.Center = nil
	cfg.Bar.Right = nil
	r := DefaultFor(cfg)
	if r.ByPath("widgets.window-title.max-width") == nil {
		t.Fatal("title option missing for configured bar")
	}
	if r.ByPath("widgets.clock.format") != nil {
		t.Fatal("clock option present though clock is not on the bar")
	}
}

func TestRegistryExposesBarItemLists(t *testing.T) {
	t.Parallel()
	r := Default()
	e := r.ByPath("bar.items.left")
	if e == nil || e.Kind != KindString {
		t.Fatal("bar.items.left must be a string entry")
	}
	cfg := config.Default()
	if got := e.Get(cfg); got != "workspace,window-title" {
		t.Fatalf("left items = %q", got)
	}
	if err := e.Set(&cfg, "window-title,workspace"); err != nil {
		t.Fatal(err)
	}
	if got := e.Get(cfg); got != "window-title,workspace" {
		t.Fatalf("after set = %q", got)
	}
	if cfg.Bar.Left[0].ID != "window-title" || cfg.Bar.Left[0].MaxWidth <= 0 {
		t.Fatalf("reused title lost max width: %+v", cfg.Bar.Left[0])
	}
}

// TestAppearanceAxesRoundTrip covers every D3 axis through the registry: it is
// discoverable, it reports the configured value, and setting it lands back on
// the composition. An axis that resolves into the theme but cannot be reached
// from settings is not exposed, however well it paints.
func TestAppearanceAxesRoundTrip(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path string
		set  string
		get  func(config.Config) string
	}{
		{"appearance.density", "comfortable", func(c config.Config) string { return string(c.Theme.Density) }},
		{"appearance.font-family", "Iosevka", func(c config.Config) string { return c.Theme.FontFamily }},
		{"appearance.mono-font-family", "Iosevka Term", func(c config.Config) string { return c.Theme.MonoFontFamily }},
		{"appearance.font-scale", "125", func(c config.Config) string { return strconv.Itoa(c.Theme.FontScale) }},
		{"appearance.font-weight", "500", func(c config.Config) string { return strconv.Itoa(c.Theme.FontWeight) }},
		{"appearance.radius", "20", func(c config.Config) string { return strconv.Itoa(c.Theme.Radius) }},
		{"appearance.motion", "expressive", func(c config.Config) string { return string(c.Theme.Motion) }},
		{"appearance.motion-speed", "200", func(c config.Config) string { return strconv.Itoa(c.Theme.MotionSpeed) }},
		{"appearance.bar-opacity", "90", func(c config.Config) string { return strconv.Itoa(c.Theme.BarOpacity) }},
		{"appearance.panel-opacity", "85", func(c config.Config) string { return strconv.Itoa(c.Theme.PanelOpacity) }},
		{"appearance.overlay-opacity", "80", func(c config.Config) string { return strconv.Itoa(c.Theme.OverlayOpacity) }},
		{"appearance.elevation", "standard", func(c config.Config) string { return string(c.Theme.Elevation) }},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			e := Default().ByPath(tc.path)
			if e.Path == "" {
				t.Fatalf("%s is not registered", tc.path)
			}
			if e.Section != "Appearance" {
				t.Errorf("section = %q, want Appearance", e.Section)
			}
			cfg := config.Default()
			if err := e.Set(&cfg, tc.set); err != nil {
				t.Fatalf("set: %v", err)
			}
			if got := tc.get(cfg); got != tc.set {
				t.Errorf("config holds %q, want %q", got, tc.set)
			}
			if got := e.Get(cfg); got != tc.set {
				t.Errorf("Get = %q, want %q", got, tc.set)
			}
		})
	}
}

// TestAppearanceIntegerAxesAreBounded keeps the percent and weight fields on
// the integer control with the bounds the theme package defines, rather than a
// second copy of the range that can drift from it.
func TestAppearanceIntegerAxesAreBounded(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path     string
		min, max int
	}{
		{"appearance.font-scale", theme.FontScaleMin, theme.FontScaleMax},
		{"appearance.font-weight", theme.FontWeightMin, theme.FontWeightMax},
		{"appearance.radius", theme.RadiusMin, theme.RadiusMax},
		{"appearance.motion-speed", theme.SpeedMin, theme.SpeedMax},
		{"appearance.bar-opacity", theme.OpacityMin, theme.OpacityMax},
	} {
		e := Default().ByPath(tc.path)
		if e.Min != tc.min || e.Max != tc.max {
			t.Errorf("%s bounds = %d..%d, want %d..%d", tc.path, e.Min, e.Max, tc.min, tc.max)
		}
		cfg := config.Default()
		if err := e.Set(&cfg, strconv.Itoa(tc.max+1)); err == nil {
			t.Errorf("%s accepted %d, above its maximum", tc.path, tc.max+1)
		}
	}
}

// TestAppearancePresetRebasesTheAxes checks a preset reseeds the axes still
// sitting on the old preset's value while keeping one the user changed. That
// is the whole point of the rebase helper: switching preset must not silently
// discard a deliberate choice.
func TestAppearancePresetRebasesTheAxes(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	if err := Default().ByPath("appearance.density").Set(&cfg, "comfortable"); err != nil {
		t.Fatal(err)
	}
	before := cfg.Theme.Radius
	if err := Default().ByPath("appearance.preset").Set(&cfg, "compact"); err != nil {
		t.Fatal(err)
	}
	if cfg.Theme.Preset != theme.PresetCompact {
		t.Errorf("preset = %q, want compact", cfg.Theme.Preset)
	}
	if cfg.Theme.Density != "comfortable" {
		t.Errorf("density = %q; a deliberate choice was discarded by the preset", cfg.Theme.Density)
	}
	if cfg.Theme.Radius == before {
		t.Errorf("radius stayed %d; an axis still on the old preset was not reseeded", before)
	}
}
