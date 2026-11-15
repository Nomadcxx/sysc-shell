package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOutputOverrideFollowsSharedEdits(t *testing.T) {
	cfg, err := Parse([]byte(`{"outputs":[{"connector":"DP-1","bar":{"edge":"left","items":{"left":["workspace"]}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Bar.Height = 60
	cfg.Bar.FontSize = 19
	cfg.Bar.Center = []Item{{ID: "clock"}}
	got := cfg.ForConnector("DP-1")
	if got.Height != 60 || got.FontSize != 19 || len(got.Center) != 1 || got.Center[0].ID != "clock" {
		t.Fatalf("inherited values stopped following shared: %+v", got)
	}
	if got.Edge != "left" || len(got.Left) != 1 || got.Left[0].ID != "workspace" {
		t.Fatalf("explicit values changed: %+v", got)
	}
}

func TestOutputOverrideWritesExplicitEqualValues(t *testing.T) {
	cfg, err := Parse([]byte(`{"bar":{"enabled":false},"outputs":[{"connector":"DP-1","bar":{"enabled":false,"height":48,"font":{"size":15}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "config.json")
	if err := Write(p, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Outputs []struct{ Bar map[string]json.RawMessage }
	}
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"enabled", "height", "font"} {
		if _, ok := saved.Outputs[0].Bar[key]; !ok {
			t.Fatalf("lost explicit %s: %s", key, b)
		}
	}
	roundTrip, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip.Bar.Enabled = true
	roundTrip.Bar.Height = 60
	roundTrip.Bar.FontSize = 19
	got := roundTrip.ForConnector("DP-1")
	if got.Enabled || got.Height != 48 || got.FontSize != 15 {
		t.Fatalf("explicit pins lost: %+v", got)
	}
}

func TestOutputOverrideExplicitHeightBypassesAutomaticDensity(t *testing.T) {
	cfg, err := Parse([]byte(`{"outputs":[{"connector":"DP-1","bar":{"height":48}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.BarFor("DP-1", 1536, 864).Height; got != 48 {
		t.Fatalf("explicit height = %d, want 48", got)
	}
}

func TestOutputOverrideSetResetKeepsOtherFields(t *testing.T) {
	cfg, err := Parse([]byte(`{"outputs":[{"connector":"DP-1","bar":{"style":"islands","reserve":0,"items":{"left":[]}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OutputBarOverridden("DP-1", "height") {
		t.Fatal("inherited height is explicit")
	}
	bar := cfg.ForConnector("DP-1")
	bar.Enabled = false
	if err := cfg.SetOutputBar("DP-1", "enabled", bar); err != nil {
		t.Fatal(err)
	}
	bar.Height = cfg.Bar.Height
	if err := cfg.SetOutputBar("DP-1", "height", bar); err != nil {
		t.Fatal(err)
	}
	if !cfg.OutputBarOverridden("DP-1", "height") {
		t.Fatal("equal-base height was not pinned")
	}
	if cfg.OutputBarOverridden("DP-1", "font.size") {
		t.Fatal("one-field edit pinned font size")
	}
	clone := cfg.Clone()
	clone.Outputs[0].Set["font.size"] = true
	if cfg.OutputBarOverridden("DP-1", "font.size") {
		t.Fatal("clone aliases presence map")
	}
	cfg.ResetOutputBar("DP-1", "enabled", "height", "gap", "font.size")
	cfg.Bar.Height = 60
	got := cfg.ForConnector("DP-1")
	if !got.Enabled || got.Height != 60 || got.Style != "islands" || got.Reserve == nil || *got.Reserve != 0 || got.Left == nil || len(got.Left) != 0 {
		t.Fatalf("reset lost unrelated policy: %+v", got)
	}
	p := filepath.Join(t.TempDir(), "config.json")
	if err := Write(p, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.OutputBarOverridden("DP-1", "height") || !loaded.OutputBarOverridden("DP-1", "items.left") {
		t.Fatal("reset/lane ownership did not survive write")
	}
}

func TestOutputOverrideGapEditPreservesExplicitThickness(t *testing.T) {
	cfg, err := Parse([]byte(`{"outputs":[{"connector":"DP-1","bar":{"height":64}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	bar := cfg.ForConnector("DP-1")
	bar.Gap = 8
	if err := cfg.SetOutputBar("DP-1", "gap", bar); err != nil {
		t.Fatal(err)
	}
	if got := cfg.ForConnector("DP-1").Height; got != 64 {
		t.Fatalf("gap edit changed explicit thickness to %d", got)
	}
}

func TestOutputOverrideSetValidatesWithoutMutation(t *testing.T) {
	cfg := Default()
	bar := cfg.Bar
	bar.Edge = "diagonal"
	if err := cfg.SetOutputBar("DP-1", "edge", bar); err == nil {
		t.Fatal("invalid edge accepted")
	}
	if err := cfg.SetOutputBar("DP-1", "unknown", cfg.Bar); err == nil {
		t.Fatal("unknown key accepted")
	}
	if err := cfg.SetOutputBar("", "enabled", cfg.Bar); err == nil {
		t.Fatal("empty connector accepted")
	}
	if err := cfg.SetOutputBar("DP-1", "reserve", cfg.Bar); err == nil {
		t.Fatal("nil reserve accepted as an explicit value")
	}
	if len(cfg.Outputs) != 0 {
		t.Fatal("failed edit mutated outputs")
	}
}

func TestOutputOverrideEveryFieldRoundTrips(t *testing.T) {
	cfg, err := Parse([]byte(`{"outputs":[{"connector":"DP-1","bar":{
		"enabled":false,"edge":"left","height":48,"gap":4,"reserve":0,
		"padding":6,"spacing":4,"style":"frosted","shape":"attached",
		"frost-opacity":65,"pill-opacity":70,"font":{"family":"Inter Variable","size":15},
		"items":{"left":[],"center":[],"right":[]}
	}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "config.json")
	if err := Write(p, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Outputs[0].Set, loaded.Outputs[0].Set) {
		t.Fatalf("ownership changed: got %+v, want %+v", loaded.Outputs[0].Set, cfg.Outputs[0].Set)
	}
	if !reflect.DeepEqual(cfg.ForConnector("DP-1"), loaded.ForConnector("DP-1")) {
		t.Fatal("explicit output policy changed after write/reload")
	}
}
