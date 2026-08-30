package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	          "items": {"left": ["workspace"], "center": [{"id": "clock"}],
	                    "right": [{"id": "window-title", "max-width": 200}]}},
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
		{"unknown item", `{"bar":{"items":{"left":["no-such-widget"]}}}`, "bar.items.left[0]"},
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

func TestDefaultVocabularyShipsBothClocksAndBothNiriWidgets(t *testing.T) {
	t.Parallel()
	cfg := Default()

	if got := len(cfg.Bar.Left); got != 2 {
		t.Fatalf("left items = %d, want workspace and window-title", got)
	}
	if cfg.Bar.Left[0].ID != "workspace" {
		t.Fatalf("left[0] = %q, want workspace", cfg.Bar.Left[0].ID)
	}
	if cfg.Bar.Left[1].ID != "window-title" || cfg.Bar.Left[1].MaxWidth <= 0 {
		t.Fatalf("left[1] = %+v, want window-title with a positive max width", cfg.Bar.Left[1])
	}
	if len(cfg.Bar.Center) != 1 || cfg.Bar.Center[0].ID != "clock" {
		t.Fatalf("center = %+v, want one clock", cfg.Bar.Center)
	}
	if len(cfg.Bar.Right) != 1 || cfg.Bar.Right[0].ID != "clock" {
		t.Fatalf("right = %+v, want one clock", cfg.Bar.Right)
	}
	// The two default clocks must differ, or the defaults do not demonstrate
	// a date.
	if cfg.Bar.Center[0].Format == cfg.Bar.Right[0].Format {
		t.Fatal("the two default clocks share a format; one should show the date")
	}
	for _, item := range append(append([]Item{}, cfg.Bar.Center...), cfg.Bar.Right...) {
		if item.Boundary <= 0 {
			t.Fatalf("default clock %+v has no tick boundary", item)
		}
	}
}

func TestTheFixtureVocabularyIsGone(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"shell-name", "meter", "toggle"} {
		body := []byte(`{"bar":{"items":{"left":["` + id + `"]}}}`)
		if _, err := Parse(body); err == nil {
			t.Fatalf("the retired fixture id %q was accepted", id)
		}
	}
}

func TestAnItemIsEitherAStringOrAnObject(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"bar":{"items":{
		"left":["workspace"],
		"center":[{"id":"clock","format":"15:04:05"}],
		"right":[{"id":"window-title","max-width":120}]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.Bar.Left[0].ID != "workspace" {
		t.Fatalf("bare string item = %+v, want workspace", cfg.Bar.Left[0])
	}
	if got := cfg.Bar.Center[0]; got.Format != "15:04:05" || got.Boundary != time.Second {
		t.Fatalf("seconds clock = %+v, want a one second boundary", got)
	}
	if got := cfg.Bar.Right[0]; got.MaxWidth != 120 {
		t.Fatalf("window-title = %+v, want max width 120", got)
	}
}

func TestAClockWithoutSecondsTicksOncePerMinute(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"bar":{"items":{"center":[{"id":"clock","format":"Mon 2 Jan"}]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := cfg.Bar.Center[0].Boundary; got != time.Minute {
		t.Fatalf("boundary = %v, want one minute", got)
	}
}

// "HH:MM" is not a Go layout; Format renders it literally and forever
// unchanged. Catching that is the whole point of validating the layout.
func TestATimeInvariantFormatIsRejected(t *testing.T) {
	t.Parallel()
	for _, layout := range []string{"HH:MM", "hh:mm:ss", "the time"} {
		body := []byte(`{"bar":{"items":{"center":[{"id":"clock","format":"` + layout + `"}]}}}`)
		if _, err := Parse(body); err == nil {
			t.Fatalf("the time-invariant layout %q was accepted", layout)
		}
	}
}

// A coarse but legitimate layout must not be mistaken for a typo.
func TestACoarseFormatIsAccepted(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"bar":{"items":{"center":[{"id":"clock","format":"January 2006"}]}}}`))
	if err != nil {
		t.Fatalf("a month-and-year layout was rejected: %v", err)
	}
	if got := cfg.Bar.Center[0].Boundary; got != time.Minute {
		t.Fatalf("boundary = %v, want one minute", got)
	}
}

func TestAnOptionOnTheWrongItemIsRejected(t *testing.T) {
	t.Parallel()
	cases := []string{
		`{"bar":{"items":{"left":[{"id":"workspace","format":"15:04"}]}}}`,
		`{"bar":{"items":{"left":[{"id":"clock","max-width":100}]}}}`,
	}
	for _, body := range cases {
		if _, err := Parse([]byte(body)); err == nil {
			t.Fatalf("an option on the wrong item was accepted: %s", body)
		}
	}
}

func TestANonPositiveMaxWidthIsRejected(t *testing.T) {
	t.Parallel()
	body := []byte(`{"bar":{"items":{"left":[{"id":"window-title","max-width":0}]}}}`)
	if _, err := Parse(body); err == nil {
		t.Fatal("a zero max width was accepted")
	}
}

func TestAnUnknownItemStillNamesItsFieldPath(t *testing.T) {
	t.Parallel()
	_, err := Parse([]byte(`{"bar":{"items":{"right":["workspace","nope"]}}}`))
	if err == nil {
		t.Fatal("an unknown item was accepted")
	}
	if !strings.Contains(err.Error(), "bar.items.right[1]") {
		t.Fatalf("error %q does not name the failing field path", err)
	}
}
