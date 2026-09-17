package settings

import (
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
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
	if got := e.Get(cfg); got != "launcher,workspace,window-title" {
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

func TestDensitySettingDoesNotPersistAStaleDerivedBar(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		explicit   bool
		wantHeight int
	}{
		{name: "derived", wantHeight: 31},
		{name: "explicit bar override", explicit: true, wantHeight: 52},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			if tc.explicit {
				cfg.Bar.Height = 52
			}
			if err := Default().ByPath("appearance.density").Set(&cfg, string(theme.DensityDefault)); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "config.json")
			if err := config.Write(path, cfg); err != nil {
				t.Fatal(err)
			}
			back, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if back.Bar.Height != tc.wantHeight {
				t.Fatalf("reloaded bar height = %d, want %d", back.Bar.Height, tc.wantHeight)
			}
		})
	}
}

// TestEntryUsesItsOwnAccessors is the whole point of D1: an entry carries the
// code that reads and writes its field, so a new setting cannot be declared
// without one and no switch can fall through to an empty string.
func TestEntryUsesItsOwnAccessors(t *testing.T) {
	t.Parallel()
	e := Entry{
		Path: "test.flag", Label: "Flag", Section: "Bar", Kind: KindBool,
		Get: func(c config.Config) string { return strconv.FormatBool(c.Bar.Enabled) },
		Set: func(c *config.Config, v string) error {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return err
			}
			c.Bar.Enabled = b
			return nil
		},
	}
	cfg := config.Default()
	cfg.Bar.Enabled = false
	if got := e.Get(cfg); got != "false" {
		t.Fatalf("Get = %q, want false", got)
	}
	if err := e.Set(&cfg, "true"); err != nil {
		t.Fatal(err)
	}
	if !cfg.Bar.Enabled {
		t.Fatal("Set did not reach the field")
	}
}

// TestEveryConfigDomainHasAnEntry is the guard against the five-of-twelve gap
// reopening. Weather, Wallpaper, Tray, Outputs and Plugins were modelled in
// configuration and unreachable from every surface, and that happened quietly
// because nothing ever asserted otherwise.
//
// It runs against a configuration that exercises each domain rather than
// config.Default(), because D9 exposes three of them as a row per discovered
// item: a tray token, an output override and a plugin have no rows until one
// exists to carry them. Asserting against an empty default would force a
// placeholder entry per domain, which is what this guard exists to prevent.
func TestEveryConfigDomainHasAnEntry(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Tray.Hidden = []string{"steam"}
	cfg.Outputs = []config.OutputOverride{{Connector: "DP-1", Bar: cfg.Bar}}
	cfg.Plugins.Enabled = []string{"com.example.widget"}
	r := DefaultFor(cfg)

	for _, prefix := range []string{
		"bar.", "appearance.", "theme.templates.", "panels.", "session.",
		"accessibility.", "weather.", "wallpaper.", "tray.", "outputs.", "plugins.",
	} {
		found := false
		for _, section := range SectionNames() {
			for _, e := range r.Section(section) {
				if strings.HasPrefix(e.Path, prefix) {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("no settings entry reaches the %q domain", prefix)
		}
	}
}

// TestEverySectionIsOneOfTheNamedSections closes the other half of the same
// hole: the pane walks SectionNames, so an entry filed under a section the
// rail does not list is as unreachable as one that was never written.
func TestEverySectionIsOneOfTheNamedSections(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Tray.Pinned = []string{"steam"}
	cfg.Outputs = []config.OutputOverride{{Connector: "DP-1", Bar: cfg.Bar}}
	cfg.Plugins.Enabled = []string{"com.example.widget"}

	names := SectionNames()
	if len(names) != 12 {
		t.Fatalf("SectionNames = %d sections, want the twelve of the information architecture", len(names))
	}
	for _, e := range DefaultFor(cfg).entries {
		if !slices.Contains(names, e.Section) {
			t.Errorf("%s is filed under %q, which no section lists", e.Path, e.Section)
		}
	}
}

// TestEveryEnumOptionSurvivesTheLoader is the drift guard for the vocabularies
// this package restates. An option the loader refuses produces a setting that
// writes a file the shell then declines to start from, and the user sees the
// shell fail rather than the setting fail.
func TestEveryEnumOptionSurvivesTheLoader(t *testing.T) {
	t.Parallel()
	base := config.Default()
	base.Tray.Hidden = []string{"steam"}
	base.Outputs = []config.OutputOverride{{Connector: "DP-1", Bar: base.Bar}}
	base.Plugins.Enabled = []string{"com.example.widget"}

	for _, e := range DefaultFor(base).entries {
		if e.Kind != KindEnum {
			continue
		}
		for _, option := range e.Options {
			t.Run(e.Path+"="+option, func(t *testing.T) {
				cfg := base
				if err := e.Set(&cfg, option); err != nil {
					t.Fatalf("set: %v", err)
				}
				path := filepath.Join(t.TempDir(), "config.json")
				if err := config.Write(path, cfg); err != nil {
					t.Fatalf("write: %v", err)
				}
				if _, err := config.Load(path); err != nil {
					t.Fatalf("the shell refuses what this option wrote: %v", err)
				}
			})
		}
	}
}

// TestWeatherPlaceIsOneWayOrTheOther holds the loader's exclusive rule at the
// setting: coordinates and a city name cannot both reach the file, and the
// writer prefers the city, so coordinates set beside a stale city would be
// silently discarded.
func TestWeatherPlaceIsOneWayOrTheOther(t *testing.T) {
	t.Parallel()
	r := Default()
	cfg := config.Default()

	if err := r.ByPath("weather.city").Set(&cfg, "Bristol"); err != nil {
		t.Fatal(err)
	}
	if err := r.ByPath("weather.latitude").Set(&cfg, "51.45"); err != nil {
		t.Fatal(err)
	}
	if err := r.ByPath("weather.longitude").Set(&cfg, "-2.58"); err != nil {
		t.Fatal(err)
	}
	if cfg.Weather.City != "" {
		t.Errorf("city = %q; coordinates did not displace it", cfg.Weather.City)
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	back, err := config.Load(path)
	if err != nil {
		t.Fatalf("the shell refuses what the weather settings wrote: %v", err)
	}
	if back.Weather.Latitude != 51.45 || back.Weather.Longitude != -2.58 {
		t.Errorf("coordinates came back as %v,%v", back.Weather.Latitude, back.Weather.Longitude)
	}
}

// TestBoundsRejectWhatTheLoaderWouldReject keeps the numeric entries from
// writing a file that cannot be read back.
func TestBoundsRejectWhatTheLoaderWouldReject(t *testing.T) {
	t.Parallel()
	r := Default()
	for _, tc := range []struct{ path, value string }{
		{"weather.latitude", "91"},
		{"weather.longitude", "-181"},
		{"weather.interval", "0s"},
		{"weather.interval", "fortnightly"},
		{"wallpaper.fade-duration", "-1"},
		{"wallpaper.image-directory", "  "},
		{"weather.city", strings.Repeat("x", 81)},
		{"weather.location", "two\nlines"},
	} {
		cfg := config.Default()
		if err := r.ByPath(tc.path).Set(&cfg, tc.value); err == nil {
			t.Errorf("%s accepted %q", tc.path, tc.value)
		}
	}
}

// TestResetUsesThePresetForThemeAxes holds D5: "default" is not one thing.
// config.Write bases every appearance axis against the selected preset and
// everything else against Default(), so reset has to resolve an entry through
// the same rule the writer uses, or resetting an axis writes a value the
// writer then records as a deviation.
func TestResetUsesThePresetForThemeAxes(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Theme.Preset = theme.PresetCompact
	comp, ok := theme.PresetComposition(theme.PresetCompact)
	if !ok {
		t.Fatal("compact preset is missing")
	}
	cfg.Theme.Composition = comp

	r := DefaultFor(cfg)
	e := r.ByPath("appearance.radius")
	if e == nil {
		t.Fatal("appearance.radius is missing")
	}
	if !e.IsDefault(cfg) {
		t.Fatal("an axis sitting on its preset value must read as default")
	}
	if got := e.Default(cfg); got != strconv.Itoa(comp.Radius) {
		t.Fatalf("Default = %q, want the preset's %d", got, comp.Radius)
	}

	cfg.Theme.Radius = comp.Radius + 3
	if e.IsDefault(cfg) {
		t.Fatal("a changed axis must not read as default, or its row hides its reset")
	}

	acc := r.ByPath("accessibility.reduced-motion")
	if got := acc.Default(cfg); got != "false" {
		t.Fatalf("a non-theme entry defaults against config.Default(), got %q", got)
	}
}

// TestEveryEntryResolvesADefault stops a new entry shipping without one: a nil
// Default reads as always-default, so the row silently loses its reset rather
// than failing anywhere visible.
func TestEveryEntryResolvesADefault(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Tray.Hidden = []string{"steam"}
	cfg.Outputs = []config.OutputOverride{{Connector: "DP-1", Bar: cfg.Bar}}
	cfg.Plugins.Enabled = []string{"com.example.widget"}
	for _, e := range DefaultFor(cfg).entries {
		if e.Default == nil {
			t.Errorf("%s resolves no default", e.Path)
		}
	}
}

// TestPresetAxesTrackTheirPreset is the guard on the axis set itself. Every
// composition axis has to follow the preset, and an axis left out of that set
// would resolve against config.Default() instead, quietly disagreeing with the
// writer for exactly the entries D5 is about.
func TestPresetAxesTrackTheirPreset(t *testing.T) {
	t.Parallel()
	standard := config.Default()
	compact := standard
	comp, ok := theme.PresetComposition(theme.PresetCompact)
	if !ok {
		t.Fatal("compact preset is missing")
	}
	compact.Theme.Preset = theme.PresetCompact
	compact.Theme.Composition = comp

	moved := 0
	for _, path := range presetAxisPaths() {
		e := DefaultFor(standard).ByPath(path)
		if e == nil {
			t.Errorf("%s is named as a preset axis but is not registered", path)
			continue
		}
		if e.Default(standard) != e.Default(compact) {
			moved++
		}
	}
	if moved == 0 {
		t.Fatal("no named axis resolved differently under another preset")
	}
}

// TestSeedOffersStockNamesWhenSourceIsStock is half of sysc-107: with the
// source on stock the seed names one of a closed set, so it is a picker.
func TestSeedOffersStockNamesWhenSourceIsStock(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.ThemeGen.Source = "stock"
	e := DefaultFor(cfg).ByPath("appearance.seed")
	if e == nil {
		t.Fatal("appearance.seed is missing")
	}
	if e.Kind != KindEnum {
		t.Fatalf("Kind = %v, want an enum when the source is stock", e.Kind)
	}
	if len(e.Options) != len(theme.StockNames()) {
		t.Fatalf("Options = %d, want the %d stock names", len(e.Options), len(theme.StockNames()))
	}
}

// TestSourceCarriesASeedItsOwnSourceCanRead is the other half. The source and
// the seed are one choice spread over two fields: the loader reads the seed
// through the source, so changing the source alone left a seed it refused and
// the shell stopped loading its own configuration. Picking a stock theme was
// impossible for that reason, not because the picker was missing.
func TestSourceCarriesASeedItsOwnSourceCanRead(t *testing.T) {
	t.Parallel()
	for _, source := range themeSources {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			if err := Default().ByPath("appearance.source").Set(&cfg, source); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "config.json")
			if err := config.Write(path, cfg); err != nil {
				t.Fatal(err)
			}
			back, err := config.Load(path)
			if err != nil {
				t.Fatalf("selecting %q wrote a configuration the shell refuses: %v", source, err)
			}
			if back.ThemeGen.Source != source {
				t.Errorf("source came back as %q", back.ThemeGen.Source)
			}
		})
	}
}

// TestEveryEntryCarriesADescriptionAndAGroup keeps the row anatomy whole. A
// row without a caption is the legibility gap this milestone exists to close,
// and an entry with no group falls outside every heading the pane renders.
func TestEveryEntryCarriesADescriptionAndAGroup(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Tray.Hidden = []string{"steam"}
	cfg.Outputs = []config.OutputOverride{{Connector: "DP-1", Bar: cfg.Bar}}
	cfg.Plugins.Enabled = []string{"com.example.widget"}
	for _, e := range DefaultFor(cfg).entries {
		if e.Describe == "" {
			t.Errorf("%s carries no description", e.Path)
		}
		if e.Group == "" {
			t.Errorf("%s belongs to no group", e.Path)
		}
	}
}
