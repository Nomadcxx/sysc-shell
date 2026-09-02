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
	  "theme": {"radius": 12},
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
	// Time and date sit together in the centre, as each reference shell does.
	if len(cfg.Bar.Center) != 2 {
		t.Fatalf("center = %+v, want a time and a date clock", cfg.Bar.Center)
	}
	for i, item := range cfg.Bar.Center {
		if item.ID != "clock" {
			t.Fatalf("center[%d] = %q, want clock", i, item.ID)
		}
	}
	// The two default clocks must differ, or the defaults do not demonstrate
	// a date.
	if cfg.Bar.Center[0].Format == cfg.Bar.Center[1].Format {
		t.Fatal("the two default clocks share a format; one should show the date")
	}
	// The right section carries status widgets rather than a second clock.
	if len(cfg.Bar.Right) == 0 {
		t.Fatal("right section is empty; the default bar should ship status widgets")
	}
	for i, item := range cfg.Bar.Right {
		if item.ID == "clock" {
			t.Fatalf("right[%d] is a clock; the date moved to the centre", i)
		}
	}
	for _, item := range cfg.Bar.Center {
		if item.Boundary <= 0 {
			t.Fatalf("default clock %+v has no tick boundary", item)
		}
	}
	for _, item := range cfg.Bar.Right {
		members := item.Items
		if len(members) == 0 {
			members = []Item{item}
		}
		for _, m := range members {
			if m.Interval <= 0 {
				t.Fatalf("default status widget %q has no sampling interval", m.ID)
			}
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

func TestMetricItemsCarryTheirSelectorsAndDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"bar":{"items":{"right":[
		{"id":"cpu","display":"meter"},
		{"id":"memory"},
		{"id":"filesystem","path":"/fixture"},
		{"id":"block","device":"nvme9n1","direction":"read"},
		{"id":"network","interface":"eth9","direction":"rx","display":"graph"}]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	items := cfg.Bar.Right
	if len(items) != 5 {
		t.Fatalf("items = %d, want 5", len(items))
	}
	if items[0].Display != "meter" {
		t.Fatalf("cpu display = %q, want meter", items[0].Display)
	}
	if items[1].Display != "text" {
		t.Fatalf("memory display = %q, want the text default", items[1].Display)
	}
	if items[2].Path != "/fixture" {
		t.Fatalf("filesystem path = %q, want /fixture", items[2].Path)
	}
	if items[3].Device != "nvme9n1" || items[3].Direction != "read" {
		t.Fatalf("block = %+v, want device nvme9n1 reading", items[3])
	}
	if items[4].Interface != "eth9" || items[4].Direction != "rx" {
		t.Fatalf("network = %+v, want eth9 receiving", items[4])
	}
	for _, item := range items {
		if item.Interval <= 0 {
			t.Fatalf("item %+v has no sampling interval", item)
		}
	}
}

// A rate has no full scale, so a meter of "3.2 MB/s" would be meaningless.
func TestAMeterOnARateSourceIsRejected(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"bar":{"items":{"right":[{"id":"block","device":"nvme9n1","display":"meter"}]}}}`,
		`{"bar":{"items":{"right":[{"id":"network","interface":"eth9","display":"meter"}]}}}`,
	} {
		_, err := Parse([]byte(body))
		if err == nil {
			t.Fatalf("a meter on a rate source was accepted: %s", body)
		}
		if !strings.Contains(err.Error(), "display") {
			t.Fatalf("error %q does not name the display field", err)
		}
	}
}

func TestAGraphIsAcceptedOnEverySource(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"bar":{"items":{"right":[{"id":"cpu","display":"graph"}]}}}`,
		`{"bar":{"items":{"right":[{"id":"network","interface":"eth9","display":"graph"}]}}}`,
	} {
		if _, err := Parse([]byte(body)); err != nil {
			t.Fatalf("a graph was rejected on %s: %v", body, err)
		}
	}
}

func TestAMissingSelectorIsRejected(t *testing.T) {
	t.Parallel()
	cases := []struct{ body, want string }{
		{`{"bar":{"items":{"right":[{"id":"filesystem"}]}}}`, "path"},
		{`{"bar":{"items":{"right":[{"id":"block"}]}}}`, "device"},
		{`{"bar":{"items":{"right":[{"id":"network"}]}}}`, "interface"},
	}
	for _, c := range cases {
		err := errFromParse(t, c.body)
		if !strings.Contains(err.Error(), c.want) {
			t.Fatalf("error %q does not name the missing %q", err, c.want)
		}
	}
}

func TestASelectorOnTheWrongItemIsRejected(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"bar":{"items":{"right":[{"id":"cpu","path":"/fixture"}]}}}`,
		`{"bar":{"items":{"right":[{"id":"memory","device":"nvme9n1"}]}}}`,
		`{"bar":{"items":{"right":[{"id":"clock","interval":"2s"}]}}}`,
	} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Fatalf("an option on the wrong item was accepted: %s", body)
		}
	}
}

func TestAnInvalidDirectionIsRejected(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"bar":{"items":{"right":[{"id":"block","device":"nvme9n1","direction":"rx"}]}}}`,
		`{"bar":{"items":{"right":[{"id":"network","interface":"eth9","direction":"read"}]}}}`,
	} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Fatalf("a direction from the wrong vocabulary was accepted: %s", body)
		}
	}
}

func TestANonPositiveIntervalIsRejected(t *testing.T) {
	t.Parallel()
	body := `{"bar":{"items":{"right":[{"id":"cpu","interval":"0s"}]}}}`
	if _, err := Parse([]byte(body)); err == nil {
		t.Fatal("a zero interval was accepted")
	}
}

// errFromParse fails the test unless parsing returns an error.
func errFromParse(t *testing.T, body string) error {
	t.Helper()
	_, err := Parse([]byte(body))
	if err == nil {
		t.Fatalf("Parse(%s) succeeded, want a validation error", body)
	}
	return err
}

// A selector of whitespace is a typo, not a subject. Accepting it would load a
// widget that renders the placeholder forever, waiting for a mount named " ".
func TestAWhitespaceSelectorIsRejected(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"bar":{"items":{"right":[{"id":"filesystem","path":"  "}]}}}`,
		`{"bar":{"items":{"right":[{"id":"block","device":"\t"}]}}}`,
		`{"bar":{"items":{"right":[{"id":"network","interface":" "}]}}}`,
	} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Fatalf("a whitespace selector was accepted: %s", body)
		}
	}
}

func TestTheWeatherBlockResolves(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{
		"weather":{"latitude":0,"longitude":0,"unit":"fahrenheit","interval":"20m"},
		"bar":{"items":{"right":[{"id":"weather","max-width":160,"show-condition":true}]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if !cfg.Weather.Configured {
		t.Fatal("a supplied weather block did not resolve as configured")
	}
	if cfg.Weather.Unit != "fahrenheit" {
		t.Fatalf("unit = %q, want fahrenheit", cfg.Weather.Unit)
	}
	if cfg.Weather.Interval != 20*time.Minute {
		t.Fatalf("interval = %v, want 20m", cfg.Weather.Interval)
	}
	item := cfg.Bar.Right[0]
	if item.ID != "weather" || item.MaxWidth != 160 || !item.ShowCondition {
		t.Fatalf("item = %+v, want a weather widget with a cap and its condition", item)
	}
}

func TestTheWeatherBlockDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{
		"weather":{"latitude":0,"longitude":0},
		"bar":{"items":{"right":[{"id":"weather"}]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Weather.Unit != "celsius" {
		t.Fatalf("unit = %q, want the celsius default", cfg.Weather.Unit)
	}
	if cfg.Weather.Interval != 15*time.Minute {
		t.Fatalf("interval = %v, want the 15m default", cfg.Weather.Interval)
	}
	if cfg.Bar.Right[0].ShowCondition {
		t.Fatal("show-condition defaulted true, want false")
	}
}

// The configuration's first cross-section check. Without it the widget would
// render an error forever because of a block the user was never told about.
func TestAWeatherWidgetWithoutCoordinatesIsRejected(t *testing.T) {
	t.Parallel()
	_, err := Parse([]byte(`{"bar":{"items":{"right":[{"id":"weather"}]}}}`))
	if err == nil {
		t.Fatal("a weather widget with no weather block was accepted")
	}
	if !strings.Contains(err.Error(), "weather.latitude") {
		t.Fatalf("error %q does not name the missing field", err)
	}
}

// A weather block with no widget is harmless, not an error: a user may be
// mid-edit, and nothing renders wrong.
func TestAWeatherBlockWithoutAWidgetIsAccepted(t *testing.T) {
	t.Parallel()
	if _, err := Parse([]byte(`{"weather":{"latitude":0,"longitude":0}}`)); err != nil {
		t.Fatalf("a weather block with no widget was rejected: %v", err)
	}
}

func TestOutOfRangeCoordinatesAreRejected(t *testing.T) {
	t.Parallel()
	cases := []struct{ body, want string }{
		{`{"weather":{"latitude":91,"longitude":0}}`, "weather.latitude"},
		{`{"weather":{"latitude":-91,"longitude":0}}`, "weather.latitude"},
		{`{"weather":{"latitude":0,"longitude":181}}`, "weather.longitude"},
		{`{"weather":{"latitude":0,"longitude":-181}}`, "weather.longitude"},
	}
	for _, c := range cases {
		err := errFromParse(t, c.body)
		if !strings.Contains(err.Error(), c.want) {
			t.Fatalf("error %q does not name %q", err, c.want)
		}
	}
}

func TestAnInvalidWeatherUnitIsRejected(t *testing.T) {
	t.Parallel()
	err := errFromParse(t, `{"weather":{"latitude":0,"longitude":0,"unit":"kelvin"}}`)
	if !strings.Contains(err.Error(), "weather.unit") {
		t.Fatalf("error %q does not name the unit field", err)
	}
}

func TestANonPositiveWeatherIntervalIsRejected(t *testing.T) {
	t.Parallel()
	err := errFromParse(t, `{"weather":{"latitude":0,"longitude":0,"interval":"0s"}}`)
	if !strings.Contains(err.Error(), "weather.interval") {
		t.Fatalf("error %q does not name the interval field", err)
	}
}

// show-condition belongs to the weather widget alone.
func TestShowConditionOnTheWrongItemIsRejected(t *testing.T) {
	t.Parallel()
	body := `{"bar":{"items":{"right":[{"id":"clock","show-condition":true}]}}}`
	if _, err := Parse([]byte(body)); err == nil {
		t.Fatal("show-condition was accepted on a clock")
	}
}

func TestABatteryItemResolvesItsOptions(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(
		`{"bar":{"items":{"right":[{"id":"battery","label":"time","warn-below":15}]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	item := cfg.Bar.Right[0]
	if item.Label != "time" || item.WarnBelow != 15 {
		t.Fatalf("item = %+v, want the time label and a 15 threshold", item)
	}
}

func TestABatteryItemDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"bar":{"items":{"right":[{"id":"battery"}]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	item := cfg.Bar.Right[0]
	if item.Label != "percent" {
		t.Fatalf("label = %q, want the percent default", item.Label)
	}
	if item.WarnBelow != 20 {
		t.Fatalf("warn-below = %d, want the default 20", item.WarnBelow)
	}
	// Without a positive interval the lease would be rejected and the bar
	// would fail to build.
	if item.Interval <= 0 {
		t.Fatalf("interval = %v, want a positive default", item.Interval)
	}
}

func TestABatteryIntervalIsAccepted(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(
		`{"bar":{"items":{"right":[{"id":"battery","interval":"45s"}]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := cfg.Bar.Right[0].Interval; got != 45*time.Second {
		t.Fatalf("interval = %v, want 45s", got)
	}
}

func TestAnInvalidBatteryLabelIsRejected(t *testing.T) {
	t.Parallel()
	err := errFromParse(t, `{"bar":{"items":{"right":[{"id":"battery","label":"volts"}]}}}`)
	if !strings.Contains(err.Error(), "label") {
		t.Fatalf("error %q does not name the label field", err)
	}
}

func TestAnOutOfRangeWarnBelowIsRejected(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"bar":{"items":{"right":[{"id":"battery","warn-below":0}]}}}`,
		`{"bar":{"items":{"right":[{"id":"battery","warn-below":100}]}}}`,
	} {
		err := errFromParse(t, body)
		if !strings.Contains(err.Error(), "warn-below") {
			t.Fatalf("error %q does not name the threshold field", err)
		}
	}
}

func TestBatteryOptionsOnTheWrongItemAreRejected(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"bar":{"items":{"right":[{"id":"clock","label":"percent"}]}}}`,
		`{"bar":{"items":{"right":[{"id":"cpu","warn-below":20}]}}}`,
	} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Fatalf("a battery option was accepted on another item: %s", body)
		}
	}
}

func TestDefaultPanelAndSessionValues(t *testing.T) {
	c := Default()
	if c.ThemeGen.Source != "wallpaper" || c.ThemeGen.Scheme != "scheme-tonal-spot" || c.ThemeGen.Mode != "dark" {
		t.Fatalf("theme defaults wrong: %+v", c.ThemeGen)
	}
	if c.Panels.Gap != 0 || c.Panels.Padding != 8 {
		t.Fatalf("panels defaults wrong: %+v", c.Panels)
	}
	if c.Accessibility.ReducedMotion || c.Accessibility.HighContrast {
		t.Fatalf("accessibility must default off")
	}
	if c.Session.Locker != "" {
		t.Fatalf("locker must default empty")
	}
}

func TestThemeSourceValidation(t *testing.T) {
	for _, bad := range []string{"gradient", "auto", ""} {
		body := []byte(`{"theme-gen":{"source":"` + bad + `"}}`)
		if _, err := Parse(body); err == nil {
			t.Fatalf("source %q must be rejected", bad)
		} else if !strings.Contains(err.Error(), "theme-gen.source") {
			t.Fatalf("error %q must name the field path", err)
		}
	}
	for _, ok := range []string{"wallpaper", "hex", "stock"} {
		seed := "#3050a0"
		if ok == "stock" {
			seed = "Blue"
		}
		body := []byte(`{"theme-gen":{"source":"` + ok + `","seed":"` + seed + `"}}`)
		if _, err := Parse(body); err != nil {
			t.Fatalf("source %q must be accepted: %v", ok, err)
		}
	}
	if _, err := Parse([]byte(`{"theme-gen":{"source":"stock","seed":"mauve"}}`)); err == nil {
		t.Fatal("unknown stock name must fail")
	}
}

func TestHexSeedValidation(t *testing.T) {
	if _, err := Parse([]byte(`{"theme-gen":{"source":"hex","seed":"blue"}}`)); err == nil {
		t.Fatal("hex source with non-hex seed must fail")
	}
	if _, err := Parse([]byte(`{"theme-gen":{"source":"hex","seed":"#3050a0"}}`)); err != nil {
		t.Fatalf("hex seed must pass: %v", err)
	}
}

func TestRetiredThemeColourFieldsAreRejected(t *testing.T) {
	for _, field := range []string{"background", "foreground", "accent", "muted", "error"} {
		body := []byte(`{"theme":{"` + field + `":"#101418"}}`)
		if _, err := Parse(body); err == nil {
			t.Fatalf("retired theme.%s must be rejected, not ignored", field)
		}
	}
}
