package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
	for _, item := range []string{
		`{"id": "clock", "plugin": "org.sysc.timer"}`,
		`{"id": "clock", "entry": "bar"}`,
		`{"id": "clock", "instance": "t1"}`,
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
	if !strings.Contains(err.Error(), "t1") {
		t.Fatalf("err = %v, want it to name the instance", err)
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
