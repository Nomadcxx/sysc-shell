package weather

import (
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
	owm "github.com/Nomadcxx/sysc-shell/weather"
)

func sampleForecast(t *testing.T) owm.Forecast {
	t.Helper()
	fc, err := owm.Decode([]byte(forecastBody))
	if err != nil {
		t.Fatal(err)
	}
	return fc
}

func freshSnap(t *testing.T) Snapshot {
	t.Helper()
	return Snapshot{Observed: true, FetchedAt: time.Now(), Forecast: sampleForecast(t)}
}

func TestBarTreeShowsConfiguredFields(t *testing.T) {
	t.Parallel()
	snap := freshSnap(t)
	opt := Options{ShowTemperature: true, ShowUnit: true, ShowIcon: true, ShowCondition: true}
	root := BarTree(snap, opt)
	if err := v1.Validate(root, v1.ViewBar); err != nil {
		t.Fatal(err)
	}
	body := flatten(root)
	if !strings.Contains(body, "18") || !strings.Contains(body, "°C") {
		t.Fatalf("bar %q missing temperature", body)
	}
	if !strings.Contains(body, render.IconName(3)) && !hasIcon(root, render.IconName(3)) {
		t.Fatalf("bar missing icon name for code 3")
	}
	if !strings.Contains(body, "Cloudy") {
		t.Fatalf("bar %q missing condition", body)
	}

	plain := flatten(BarTree(snap, Options{ShowTemperature: true}))
	if strings.Contains(plain, "°C") || strings.Contains(plain, "Cloudy") || hasIcon(BarTree(snap, Options{ShowTemperature: true}), render.IconName(3)) {
		t.Fatalf("unrequested fields still rendered: %q", plain)
	}
}

func TestBarTreeLoadingDisabledAndFailed(t *testing.T) {
	t.Parallel()
	loading := flatten(BarTree(Snapshot{}, Options{ShowTemperature: true}))
	if loading != "Weather" && !strings.Contains(loading, "Weather") {
		t.Fatalf("loading bar = %q", loading)
	}
	disabled := BarTree(Snapshot{Disabled: true}, Options{})
	if err := v1.Validate(disabled, v1.ViewBar); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flatten(disabled), "off") {
		t.Fatalf("disabled bar = %q", flatten(disabled))
	}
	failed := BarTree(Snapshot{FailedSince: time.Now()}, Options{})
	if err := v1.Validate(failed, v1.ViewBar); err != nil {
		t.Fatal(err)
	}
	if tone(failed) != v1.ToneError {
		t.Fatalf("failed bar tone = %q", tone(failed))
	}
}

func TestBarTreeStaleAppendsAge(t *testing.T) {
	t.Parallel()
	snap := freshSnap(t)
	snap.FailedSince = snap.FetchedAt.Add(-5 * time.Minute)
	snap.FetchedAt = time.Now().Add(-5 * time.Minute)
	root := BarTree(snap, Options{ShowTemperature: true, ShowUnit: true})
	if !strings.Contains(flatten(root), "5m") && !strings.Contains(flatten(root), "now") {
		t.Fatalf("stale bar = %q, want an age", flatten(root))
	}
}

func TestTooltipCurrentAndForecastModes(t *testing.T) {
	t.Parallel()
	snap := freshSnap(t)
	current := TooltipTree(snap, Options{TooltipMode: "current"})
	if err := v1.Validate(current, v1.ViewTooltip); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flatten(current), "Cloudy") {
		t.Fatalf("current tooltip = %q", flatten(current))
	}
	forecast := TooltipTree(snap, Options{TooltipMode: "forecast"})
	if err := v1.Validate(forecast, v1.ViewTooltip); err != nil {
		t.Fatal(err)
	}
	body := flatten(forecast)
	if strings.Count(body, "°") < 7 {
		t.Fatalf("forecast tooltip %q missing seven days", body)
	}
}

func TestPanelTreeShowsCurrentCardAndSevenDays(t *testing.T) {
	t.Parallel()
	snap := freshSnap(t)
	root := PanelTree(snap, Options{})
	if err := v1.Validate(root, v1.ViewPanel); err != nil {
		t.Fatal(err)
	}
	body := flatten(root)
	if !strings.Contains(body, "Cloudy") {
		t.Fatalf("panel missing current condition: %q", body)
	}
	if !strings.Contains(body, "22") || !strings.Contains(body, "12") {
		t.Fatalf("panel missing high/low: %q", body)
	}
	if !strings.Contains(body, "06:12") || !strings.Contains(body, "18:44") {
		t.Fatalf("panel missing sunrise/sunset: %q", body)
	}
	if got := countKeysPrefix(root, "day:"); got != 7 {
		t.Fatalf("forecast days = %d, want 7", got)
	}
}

func TestTreesCarryAccessibleConditionText(t *testing.T) {
	t.Parallel()
	snap := freshSnap(t)
	root := BarTree(snap, Options{ShowIcon: true})
	icon := findKind(root, v1.KindIcon)
	if icon == nil || icon.Name != "Cloudy" {
		t.Fatalf("icon = %+v, want accessible Cloudy", icon)
	}
}

func TestBarTreeAcceptsALiteralAccent(t *testing.T) {
	t.Parallel()
	root := BarTree(freshSnap(t), Options{ShowTemperature: true, Accent: "#ff8800"})
	if err := v1.Validate(root, v1.ViewBar); err != nil {
		t.Fatal(err)
	}
}

func TestCurrentPatchUpdatesTemperatureAndAge(t *testing.T) {
	t.Parallel()
	snap := freshSnap(t)
	repl := CurrentPatch(snap, Options{ShowTemperature: true, ShowUnit: true})
	if len(repl) == 0 {
		t.Fatal("no replacements")
	}
	found := false
	for _, r := range repl {
		if r.Key == "temp" && r.Node != nil && strings.Contains(r.Node.Text, "18") {
			found = true
		}
	}
	if !found {
		t.Fatalf("patch = %+v, want keyed temp", repl)
	}
}

func TestParseOptionsReadsBarFieldsAndTooltipMode(t *testing.T) {
	t.Parallel()
	opt := ParseOptions(map[string]any{
		"bar_temperature": false,
		"bar_unit":        false,
		"bar_icon":        false,
		"bar_condition":   true,
		"tooltip_mode":    "forecast",
		"accent":          "#aabbcc",
	})
	if opt.ShowTemperature || opt.ShowUnit || opt.ShowIcon || !opt.ShowCondition {
		t.Fatalf("%+v", opt)
	}
	if opt.TooltipMode != "forecast" || opt.Accent != "#aabbcc" {
		t.Fatalf("%+v", opt)
	}
}

func flatten(n *v1.Node) string {
	if n == nil {
		return ""
	}
	parts := []string{n.Text}
	for _, c := range n.Children {
		parts = append(parts, flatten(c))
	}
	return strings.Join(parts, " ")
}

func hasIcon(n *v1.Node, name string) bool {
	found := false
	walk(n, func(x *v1.Node) {
		if x.Kind == v1.KindIcon && x.Icon == name {
			found = true
		}
	})
	return found
}

func findKind(n *v1.Node, k v1.NodeKind) *v1.Node {
	var found *v1.Node
	walk(n, func(x *v1.Node) {
		if found == nil && x.Kind == k {
			found = x
		}
	})
	return found
}

func tone(n *v1.Node) v1.Tone {
	var t v1.Tone
	walk(n, func(x *v1.Node) {
		if x.Tone != "" {
			t = x.Tone
		}
	})
	return t
}

func countKeysPrefix(n *v1.Node, prefix string) int {
	ncount := 0
	walk(n, func(x *v1.Node) {
		if strings.HasPrefix(x.Key, prefix) {
			ncount++
		}
	})
	return ncount
}

func walk(n *v1.Node, fn func(*v1.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, c := range n.Children {
		walk(c, fn)
	}
}
