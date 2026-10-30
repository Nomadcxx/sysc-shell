package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// pluginConfig is a configuration exercising every plugin field: an enabled
// plugin, a placement for it, plugin-scoped settings, and settings for the one
// placement instance.
const pluginConfig = `{
  "bar": {"items": {"right": [
    "clock",
    {"id": "plugin", "plugin": "org.sysc.timer", "entry": "bar", "instance": "timer-1"}
  ]}},
  "plugins": {
    "enabled": ["org.sysc.timer"],
    "settings": {"org.sysc.timer": {"sound": true, "warn_at": 30}},
    "instances": {"timer-1": {"label": "Tea"}}
  }
}`

func TestParseReadsPluginPlacementsAndSettings(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(pluginConfig))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got := cfg.Plugins.Enabled; !reflect.DeepEqual(got, []string{"org.sysc.timer"}) {
		t.Errorf("enabled = %v", got)
	}
	if len(cfg.Bar.Right) != 2 {
		t.Fatalf("right = %d items, want 2", len(cfg.Bar.Right))
	}
	placement := cfg.Bar.Right[1]
	if placement.ID != "plugin" || placement.Plugin != "org.sysc.timer" ||
		placement.Entry != "bar" || placement.Instance != "timer-1" {
		t.Errorf("placement = %+v", placement)
	}
	if got := cfg.Plugins.Settings["org.sysc.timer"]["sound"]; got != true {
		t.Errorf("plugin setting sound = %v, want true", got)
	}
	if got := cfg.Plugins.Instances["timer-1"]["label"]; got != "Tea" {
		t.Errorf("instance setting label = %v, want Tea", got)
	}
}

func TestParseMigratesLegacyWeatherPluginToNativeItem(t *testing.T) {
	t.Parallel()

	const input = `{
  "bar": {"items": {"right": [
    {"id": "plugin", "plugin": "org.sysc.weather", "entry": "bar", "instance": "weather-1"},
    {"id": "plugin", "plugin": "org.sysc.timer", "entry": "bar", "instance": "timer-1"}
  ]}},
  "outputs": [{"connector": "DP-1", "bar": {"items": {"center": [
    {"id": "group", "items": [
      {"id": "plugin", "plugin": "org.sysc.weather", "entry": "bar", "instance": "weather-1"},
      "clock"
    ]}
  ]}}}],
  "plugins": {
    "enabled": ["org.sysc.weather", "org.sysc.timer"],
    "settings": {"org.sysc.weather": {
      "latitude": -33.87,
      "longitude": 151.21,
      "unit": "fahrenheit",
      "interval": "20m",
      "bar_condition": true
    }},
    "instances": {"weather-1": {"label": "legacy weather"}}
  }
}`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	wantRight := []Item{
		{ID: "weather", ShowCondition: true},
		{ID: "plugin", Plugin: "org.sysc.timer", Entry: "bar", Instance: "timer-1"},
	}
	if !reflect.DeepEqual(cfg.Bar.Right, wantRight) {
		t.Errorf("bar.right = %+v, want %+v", cfg.Bar.Right, wantRight)
	}
	if len(cfg.Outputs) != 1 || len(cfg.Outputs[0].Bar.Center) != 1 {
		t.Fatalf("outputs = %+v", cfg.Outputs)
	}
	group := cfg.Outputs[0].Bar.Center[0]
	if group.ID != "group" || len(group.Items) != 2 ||
		!reflect.DeepEqual(group.Items[0], Item{ID: "weather", ShowCondition: true}) {
		t.Errorf("output group = %+v, want native weather and clock", group)
	}
	if !cfg.Weather.Configured || cfg.Weather.Latitude != -33.87 || cfg.Weather.Longitude != 151.21 ||
		cfg.Weather.Unit != "fahrenheit" || cfg.Weather.Interval != 20*time.Minute {
		t.Errorf("weather = %+v, want legacy location and preferences", cfg.Weather)
	}
	if !reflect.DeepEqual(cfg.Plugins.Enabled, []string{"org.sysc.timer"}) {
		t.Errorf("plugins.enabled = %v, want only timer", cfg.Plugins.Enabled)
	}
	if got := cfg.Plugins.Settings["org.sysc.weather"]["bar_condition"]; got != true {
		t.Errorf("legacy plugin settings were not preserved: bar_condition = %v", got)
	}
	if got := cfg.Plugins.Instances["weather-1"]["label"]; got != "legacy weather" {
		t.Errorf("legacy instance settings were not preserved: label = %v", got)
	}
}

func TestParseKeepsNativeWeatherLocationOverLegacyPluginSettings(t *testing.T) {
	t.Parallel()

	const input = `{
  "weather": {"latitude": 27.47, "longitude": 153.02, "unit": "celsius"},
  "bar": {"items": {"right": [
    {"id": "plugin", "plugin": "org.sysc.weather", "entry": "bar", "instance": "weather-1"}
  ]}},
  "plugins": {"settings": {"org.sysc.weather": {
    "latitude": 51.5,
    "longitude": -0.13,
    "unit": "fahrenheit"
  }}}
}`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Weather.Latitude != 27.47 || cfg.Weather.Longitude != 153.02 || cfg.Weather.Unit != "celsius" {
		t.Errorf("weather = %+v, want the existing native configuration", cfg.Weather)
	}
	if !reflect.DeepEqual(cfg.Bar.Right, []Item{{ID: "weather"}}) {
		t.Errorf("bar.right = %+v, want native weather", cfg.Bar.Right)
	}
}

func TestParseCopiesLegacyWeatherPreferencesWhenNativeLocationExists(t *testing.T) {
	t.Parallel()

	const input = `{
  "weather": {"latitude": 27.47, "longitude": 153.02},
  "bar": {"items": {"right": [
    {"id": "plugin", "plugin": "org.sysc.weather", "entry": "bar", "instance": "weather-1"}
  ]}},
  "plugins": {"settings": {"org.sysc.weather": {
    "unit": "fahrenheit",
    "interval": "20m"
  }}}
}`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Weather.Latitude != 27.47 || cfg.Weather.Longitude != 153.02 || cfg.Weather.Unit != "fahrenheit" ||
		cfg.Weather.Interval != 20*time.Minute {
		t.Errorf("weather = %+v, want native place and legacy preferences", cfg.Weather)
	}
}

func TestParseDropsLegacyWeatherPlacementWithoutAUsableLocation(t *testing.T) {
	t.Parallel()

	const input = `{
  "bar": {"items": {"right": [
    {"id": "plugin", "plugin": "org.sysc.weather", "entry": "bar", "instance": "weather-1"},
    "clock"
  ]}},
  "outputs": [{"connector": "DP-1", "bar": {"items": {"left": [
    {"id": "group", "items": [
      {"id": "plugin", "plugin": "org.sysc.weather", "entry": "bar", "instance": "weather-2"}
    ]}
  ]}}}],
  "plugins": {
    "enabled": ["org.sysc.weather", "org.sysc.timer"],
    "settings": {"org.sysc.weather": {"unit": "fahrenheit"}}
  }
}`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(cfg.Bar.Right, []Item{{ID: "clock", Format: "15:04", Boundary: time.Minute}}) {
		t.Errorf("bar.right = %+v, want only the retained clock", cfg.Bar.Right)
	}
	if len(cfg.Outputs) != 1 || len(cfg.Outputs[0].Bar.Left) != 0 {
		t.Errorf("output bar.left = %+v, want the empty legacy group removed", cfg.Outputs)
	}
	if !reflect.DeepEqual(cfg.Outputs[0].Bar.Right, []Item{{ID: "clock", Format: "15:04", Boundary: time.Minute}}) {
		t.Errorf("inherited output bar.right = %+v, want only one clock", cfg.Outputs[0].Bar.Right)
	}
	if !reflect.DeepEqual(cfg.Plugins.Enabled, []string{"org.sysc.timer"}) {
		t.Errorf("plugins.enabled = %v, want only timer", cfg.Plugins.Enabled)
	}
	if got := cfg.Plugins.Settings["org.sysc.weather"]["unit"]; got != "fahrenheit" {
		t.Errorf("legacy plugin settings were not preserved: unit = %v", got)
	}
}

func TestPluginConfigRoundTripsThroughAWrite(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(pluginConfig))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, cfg); err != nil {
		t.Fatalf("Write: %v", err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(cfg.Plugins, back.Plugins) {
		t.Errorf("plugins = %+v, want %+v", back.Plugins, cfg.Plugins)
	}
	if !reflect.DeepEqual(cfg.Bar.Right, back.Bar.Right) {
		t.Errorf("right = %+v, want %+v", back.Bar.Right, cfg.Bar.Right)
	}
}

func TestAPlacementForAPluginThatIsNotInstalledSurvives(t *testing.T) {
	t.Parallel()

	// Configuration cannot know what is installed, and a plugin that is
	// temporarily absent must not cost the user their bar layout. The
	// placement is preserved so the host can show a placeholder in its slot.
	const missing = `{
  "bar": {"items": {"right": [{"id": "plugin", "plugin": "org.example.absent", "entry": "bar", "instance": "gone-1"}]}},
  "plugins": {"enabled": []}
}`
	cfg, err := Parse([]byte(missing))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Bar.Right) != 1 || cfg.Bar.Right[0].Plugin != "org.example.absent" {
		t.Fatalf("right = %+v", cfg.Bar.Right)
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, cfg); err != nil {
		t.Fatalf("Write: %v", err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(cfg.Bar.Right, back.Bar.Right) {
		t.Errorf("right = %+v, want it preserved", back.Bar.Right)
	}
}

func TestParseRejectsAnIncompletePlacement(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		item string
	}{
		{"no plugin", `{"id": "plugin", "entry": "bar", "instance": "t1"}`},
		{"no entry", `{"id": "plugin", "plugin": "org.sysc.timer", "instance": "t1"}`},
		{"no instance", `{"id": "plugin", "plugin": "org.sysc.timer", "entry": "bar"}`},
		{"bad plugin id", `{"id": "plugin", "plugin": "timer", "entry": "bar", "instance": "t1"}`},
		{"bad entry id", `{"id": "plugin", "plugin": "org.sysc.timer", "entry": "Bar", "instance": "t1"}`},
		{"bad instance id", `{"id": "plugin", "plugin": "org.sysc.timer", "entry": "bar", "instance": "../t"}`},
		{"bare string form", `"plugin"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(`{"bar": {"items": {"right": [` + c.item + `]}}}`))
			if err == nil {
				t.Fatalf("Parse accepted a placement with %s", c.name)
			}
		})
	}
}

func TestParseRejectsPlacementFieldsOnABuiltInItem(t *testing.T) {
	t.Parallel()

	// A misplaced field is an error rather than a silently ignored one, so a
	// user sees that their edit did nothing.
	//
	// "instance" was a third case here until sub-project B gave every widget a
	// stable identity. It is now accepted on any item, including a group, and
	// TestInstanceIsAcceptedOnABuiltInItem covers that. "plugin" and "entry"
	// stay placement-only, which is what this test still holds.
	for _, item := range []string{
		`{"id": "clock", "plugin": "org.sysc.timer"}`,
		`{"id": "clock", "entry": "bar"}`,
	} {
		_, err := Parse([]byte(`{"bar": {"items": {"right": [` + item + `]}}}`))
		if err == nil {
			t.Errorf("Parse accepted %s", item)
		}
	}
}

func TestParseRejectsBuiltInOptionsOnAPlacement(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`{"bar": {"items": {"right": [
	  {"id": "plugin", "plugin": "org.sysc.timer", "entry": "bar", "instance": "t1", "format": "15:04"}
	]}}}`))
	if err == nil {
		t.Fatal("Parse accepted a clock option on a plugin placement")
	}
}

func TestParseRejectsADuplicateInstanceID(t *testing.T) {
	t.Parallel()

	// An instance id namespaces one placement's settings. Two placements
	// sharing one would silently share values, which is the opposite of what
	// the id exists for.
	_, err := Parse([]byte(`{"bar": {"items": {
	  "left":  [{"id": "plugin", "plugin": "org.sysc.timer", "entry": "bar", "instance": "t1"}],
	  "right": [{"id": "plugin", "plugin": "org.sysc.timer", "entry": "bar", "instance": "t1"}]
	}}}`))
	if err == nil {
		t.Fatal("Parse accepted a duplicate instance id")
	}
	if !strings.Contains(err.Error(), "instance") || strings.Contains(err.Error(), "t1") {
		t.Fatalf("err = %v, want a safe instance diagnostic", err)
	}
}

func TestTwoPlacementsOfOneWidgetKeepSeparateSettings(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(`{"bar": {"items": {"right": [
	  {"id": "plugin", "plugin": "org.sysc.timer", "entry": "bar", "instance": "tea"},
	  {"id": "plugin", "plugin": "org.sysc.timer", "entry": "bar", "instance": "eggs"}
	]}}, "plugins": {"instances": {"tea": {"label": "Tea"}, "eggs": {"label": "Eggs"}}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := cfg.Plugins.Instances["tea"]["label"]; got != "Tea" {
		t.Errorf("tea label = %v", got)
	}
	if got := cfg.Plugins.Instances["eggs"]["label"]; got != "Eggs" {
		t.Errorf("eggs label = %v", got)
	}
}

func TestParseRejectsMalformedPluginSection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		json string
	}{
		{"bad enabled id", `{"plugins": {"enabled": ["timer"]}}`},
		{"duplicate enabled id", `{"plugins": {"enabled": ["org.sysc.timer", "org.sysc.timer"]}}`},
		{"bad settings plugin id", `{"plugins": {"settings": {"timer": {"a": 1}}}}`},
		{"bad settings key", `{"plugins": {"settings": {"org.sysc.timer": {"Has Space": 1}}}}`},
		{"bad instance id", `{"plugins": {"instances": {"../x": {"a": 1}}}}`},
		{"bad instance setting key", `{"plugins": {"instances": {"t1": {"Has Space": 1}}}}`},
		{"unknown field", `{"plugins": {"catalog": "https://example.invalid"}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse([]byte(c.json)); err == nil {
				t.Fatalf("Parse accepted %s", c.name)
			}
		})
	}
}

func TestDefaultConfigHasNoPlugins(t *testing.T) {
	t.Parallel()

	d := Default()
	if len(d.Plugins.Enabled) != 0 || len(d.Plugins.Settings) != 0 || len(d.Plugins.Instances) != 0 {
		t.Fatalf("default plugins = %+v, want empty", d.Plugins)
	}

	// An empty plugin section must not appear in a written document, so a
	// configuration that uses no plugins stays as short as it was.
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, d); err != nil {
		t.Fatalf("Write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "plugins") {
		t.Fatalf("default document mentions plugins:\n%s", data)
	}
}

func TestPluginSourcesRoundTrip(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"plugins": {"sources": [
		{"name": "sysc", "enabled": false},
		{"name": "mine", "url": "https://github.com/me/my-plugins"},
		{"name": "dev", "url": "file:///home/me/catalog", "enabled": false}
	]}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got := cfg.Plugins.EffectiveSources()
	want := []PluginSource{
		{Name: "sysc", URL: BuiltinPluginSource.URL, Enabled: false},
		{Name: "mine", URL: "https://github.com/me/my-plugins", Enabled: true},
		{Name: "dev", URL: "file:///home/me/catalog", Enabled: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EffectiveSources = %+v", got)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil || !reflect.DeepEqual(again.Plugins.EffectiveSources(), want) {
		t.Fatalf("after write: %+v, %v", again.Plugins.EffectiveSources(), err)
	}
}

func TestDefaultConfigHasTheBuiltinSourceEnabled(t *testing.T) {
	t.Parallel()
	got := Default().Plugins.EffectiveSources()
	if len(got) != 1 || got[0] != BuiltinPluginSource || !got[0].Enabled {
		t.Fatalf("EffectiveSources = %+v", got)
	}
}

func TestParseRejectsBadPluginSources(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"bad name":        `[{"name": "My Source", "url": "https://e.com/r"}]`,
		"duplicate":       `[{"name": "a", "url": "https://e.com/r"}, {"name": "a", "url": "https://e.com/s"}]`,
		"http":            `[{"name": "a", "url": "http://e.com/r"}]`,
		"relative file":   `[{"name": "a", "url": "file://relative/path"}]`,
		"missing url":     `[{"name": "a"}]`,
		"repointed sysc":  `[{"name": "sysc", "url": "https://e.com/fork"}]`,
		"ssh unsupported": `[{"name": "a", "url": "ssh://git@e.com/r"}]`,
	}
	for name, sources := range cases {
		if _, err := Parse([]byte(`{"plugins": {"sources": ` + sources + `}}`)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
