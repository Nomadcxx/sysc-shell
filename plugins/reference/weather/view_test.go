package weather

import (
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
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
	// The bar's glyph sits on the one control, so that control carries the
	// condition as its accessible name: a reader still hears "Cloudy" rather
	// than the route's own label.
	root := BarTree(snap, Options{ShowIcon: true})
	if len(root.Children) != 1 {
		t.Fatalf("bar root has %d children, want the one control", len(root.Children))
	}
	btn := root.Children[0]
	if btn.Icon == "" || btn.Name != "Cloudy" {
		t.Fatalf("bar control = %+v, want the glyph and an accessible Cloudy", btn)
	}
	panel := PanelTree(snap, Options{ShowIcon: true})
	if icon := findKind(panel, v1.KindIcon); icon == nil || icon.Name != "Cloudy" {
		t.Fatalf("panel icon = %+v, want accessible Cloudy", icon)
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
	repl := CurrentPatch(snap, Options{ShowTemperature: true, ShowUnit: true}, v1.ViewPanel)
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
		if x.Icon == name && (x.Kind == v1.KindIcon || x.Kind == v1.KindButton) {
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

// The bar carries a glyph and a temperature. It used to carry a button reading
// "Weather" beside them, which named what the reader was already looking at and
// spent bar width doing it -- width the bar has to drop widgets to find.
func TestBarTreeCarriesNoRedundantLabel(t *testing.T) {
	t.Parallel()
	snap := freshSnap(t)
	root := BarTree(snap, Options{ShowTemperature: true, ShowUnit: true, ShowIcon: true})
	if body := flatten(root); strings.Contains(body, "Weather") {
		t.Fatalf("bar reads %q; the glyph and the temperature say it already", body)
	}
}

// The whole element opens the panel, not a label beside it. The plugin routes
// any input on the node called "open", and the shell delivers a primary press
// and a secondary release, so either button reaches it.
func TestBarTreeCarriesTheOpenControl(t *testing.T) {
	t.Parallel()
	root := BarTree(freshSnap(t), Options{ShowTemperature: true, ShowIcon: true})
	if root.Kind != v1.KindRow {
		t.Fatalf("bar root is %q; the host lays a bar out as a row", root.Kind)
	}
	if len(root.Children) != 1 {
		t.Fatalf("bar root has %d children, want the one open control", len(root.Children))
	}
	btn := root.Children[0]
	if btn.Kind != v1.KindButton || btn.ID != "open" {
		t.Fatalf("bar control = %v/%q; only a control carries an action route", btn.Kind, btn.ID)
	}
	var activate, pointer bool
	for _, e := range btn.Events {
		switch e {
		case v1.EventActivate:
			activate = true
		case v1.EventPointer:
			pointer = true
		}
	}
	if !activate || !pointer {
		t.Fatalf("bar control events = %v, want activate and pointer so both buttons open the panel", btn.Events)
	}
	if err := v1.Validate(root, v1.ViewBar); err != nil {
		t.Fatal(err)
	}
}

// TestBarTreeConvertsForTheBarHost runs the host's own gate. Validate alone
// passed a button root that Convert then refused, and the shell showed "!"
// instead of the widget, so the conversion is part of the contract.
func TestBarTreeConvertsForTheBarHost(t *testing.T) {
	t.Parallel()
	snaps := map[string]Snapshot{
		"observed": freshSnap(t),
		"loading":  {},
		"disabled": {Disabled: true},
		"failed":   {FailedSince: time.Now()},
	}
	for name, snap := range snaps {
		root := BarTree(snap, Options{ShowTemperature: true, ShowUnit: true, ShowIcon: true})
		if _, err := plugin.Convert(root, v1.ViewBar); err != nil {
			t.Fatalf("%s bar tree: %v", name, err)
		}
	}
}

// The bar's reading is patched through the control itself, because the bar no
// longer holds a separate keyed node for it.
func TestCurrentPatchUpdatesTheBarControl(t *testing.T) {
	t.Parallel()
	repl := CurrentPatch(freshSnap(t), Options{ShowTemperature: true, ShowUnit: true, ShowIcon: true}, v1.ViewBar)
	if len(repl) != 1 {
		t.Fatalf("bar patch = %+v, want one replacement", repl)
	}
	if repl[0].Key != barCurrentKey {
		t.Fatalf("bar patch key = %q, want %q", repl[0].Key, barCurrentKey)
	}
	if repl[0].Node == nil || !strings.Contains(repl[0].Node.Text, "18") {
		t.Fatalf("bar patch node = %+v, want the new reading", repl[0].Node)
	}
	if repl[0].Node.ID != "open" {
		t.Fatalf("bar patch replaced the control with %+v; it must stay the open route", repl[0].Node)
	}
}
