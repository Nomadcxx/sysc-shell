package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultBarMatchesDMSContentBand(t *testing.T) {
	t.Parallel()
	bar := Default().Bar
	if bar.Padding != 6 || bar.Spacing != 4 {
		t.Fatalf("default padding/spacing = %d/%d, want 6/4", bar.Padding, bar.Spacing)
	}
}

func TestParseAcceptsAFullDocument(t *testing.T) {
	t.Parallel()
	const doc = `{
	  "bar": {"enabled": true, "edge": "top", "height": 48, "gap": 4,
	          "padding": 8, "spacing": 6,
	          "font": {"family": "Inter", "size": 14},
	          "items": {"left": ["shell-name"], "center": ["workspace"],
	                    "right": ["meter", "toggle"]}},
	  "theme": {"background": "#101418", "foreground": "#e8ecf0",
	            "accent": "#0080ff", "muted": "#303438", "error": "#ff4040",
	            "radius": 12},
	  "outputs": [{"connector": "DP-1", "bar": {"height": 44}}]
	}`
	cfg, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Bar.Height != 48 || cfg.Bar.Gap != 4 {
		t.Fatalf("bar = %+v", cfg.Bar)
	}
	if cfg.Bar.FontFamily != "Inter" {
		t.Fatalf("font family = %q, want Inter", cfg.Bar.FontFamily)
	}
	if got := cfg.ForConnector("DP-1").Height; got != 44 {
		t.Fatalf("DP-1 height = %d, want the override 44", got)
	}
	if got := cfg.ForConnector("DP-3").Height; got != 48 {
		t.Fatalf("DP-3 height = %d, want the base 48", got)
	}
	if got := cfg.ForConnector("DP-1").Gap; got != 4 {
		t.Fatalf("DP-1 gap = %d, want the base 4 to survive a partial override", got)
	}
}

func TestParseIsMissingFieldTolerant(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"bar": {"height": 60}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Bar.Height != 60 {
		t.Fatalf("height = %d, want 60", cfg.Bar.Height)
	}
	// An absent field must inherit the default, not overwrite it with zero.
	if cfg.Bar.Gap != Default().Bar.Gap {
		t.Fatalf("gap = %d, want the default %d", cfg.Bar.Gap, Default().Bar.Gap)
	}
	if cfg.Bar.FontSize != Default().Bar.FontSize {
		t.Fatalf("font size = %d, want the default", cfg.Bar.FontSize)
	}
}

func TestThemeReportsBackgroundOpacity(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		background string
		want       bool
	}{
		{"#101418", true},
		{"#101418ff", true},
		{"#101418FF", true},
		{"#101418fe", false},
		{"", false},
	} {
		if got := (Theme{Background: tc.background}).BackgroundOpaque(); got != tc.want {
			t.Errorf("BackgroundOpaque(%q) = %v, want %v", tc.background, got, tc.want)
		}
	}
}

func TestValidationReportsTheFieldPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{"height below the gap", `{"bar":{"height":7,"gap":4}}`, "bar.height"},
		{"negative gap", `{"bar":{"gap":-1}}`, "bar.gap"},
		{"unsupported edge", `{"bar":{"edge":"bottom"}}`, "bar.edge"},
		{"unknown edge", `{"bar":{"edge":"sideways"}}`, "bar.edge"},
		{"negative padding", `{"bar":{"padding":-3}}`, "bar.padding"},
		{"negative spacing", `{"bar":{"spacing":-3}}`, "bar.spacing"},
		{"zero font size", `{"bar":{"font":{"size":0}}}`, "bar.font.size"},
		{"bad colour", `{"theme":{"accent":"0080ff"}}`, "theme.accent"},
		{"negative radius", `{"theme":{"radius":-2}}`, "theme.radius"},
		{"unknown item", `{"bar":{"items":{"left":["clock"]}}}`, "bar.items.left[0]"},
		{"empty connector", `{"outputs":[{"connector":""}]}`, "outputs[0].connector"},
		{"duplicate connector",
			`{"outputs":[{"connector":"DP-1"},{"connector":"DP-1"}]}`, "outputs[1].connector"},
		{"override leaves no body",
			`{"outputs":[{"connector":"DP-1","bar":{"height":6}}]}`, "outputs[0].bar.height"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(c.doc))
			if err == nil {
				t.Fatalf("Parse(%s) succeeded, want a validation error", c.doc)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not name %q", err, c.want)
			}
		})
	}
}

func TestMalformedJSONIsRejected(t *testing.T) {
	t.Parallel()
	if _, err := Parse([]byte(`{"bar":`)); err == nil {
		t.Fatal("malformed JSON was accepted")
	}
}

func TestUnknownConfigurationFieldsAreRejected(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		doc  string
		want string
	}{
		{"top level", `{"bars":{}}`, "bars"},
		{"nested", `{"bar":{"heigth":56}}`, "heigth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(tc.doc))
			if err == nil {
				t.Fatalf("Parse(%s) accepted an unknown field", tc.doc)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name %q", err, tc.want)
			}
		})
	}
}

func TestLoadTreatsAMissingFileAsDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Bar.Height != Default().Bar.Height {
		t.Fatal("a missing file did not fall back to the defaults")
	}
}

func TestLoadReadsAFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"bar":{"height":52}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Bar.Height != 52 {
		t.Fatalf("height = %d, want 52", cfg.Bar.Height)
	}
}

func TestARejectedCandidateLeavesNoPartialState(t *testing.T) {
	t.Parallel()
	// The bar is valid but the second output is not; nothing may be returned.
	const doc = `{"bar":{"height":48},"outputs":[
	  {"connector":"DP-1"},
	  {"connector":"DP-3","bar":{"gap":-9}}]}`
	cfg, err := Parse([]byte(doc))
	if err == nil {
		t.Fatal("Parse accepted a document with an invalid override")
	}
	if len(cfg.Outputs) != 0 || cfg.Bar.Height != 0 {
		t.Fatalf("a rejected candidate returned partial state: %+v", cfg)
	}
}
