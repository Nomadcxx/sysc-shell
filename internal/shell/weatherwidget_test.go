package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestAnObservedReadingRendersIconAndTemperature(t *testing.T) {
	t.Parallel()
	reading := services.Reading{
		Observed: true, Temperature: 18.4, Unit: services.UnitCelsius,
		Code: 0, FetchedAt: time.Now(),
	}

	text, tone := weatherText(config.Item{ID: "weather"}, reading)
	if tone != ui.ToneNormal {
		t.Fatalf("tone = %v, want normal for an observed reading", tone)
	}
	if !strings.Contains(text, "18") {
		t.Fatalf("text %q carries no temperature", text)
	}
}

// A reading that never arrived is an error, not a stale value.
func TestAnUnobservedReadingRendersTheErrorTone(t *testing.T) {
	t.Parallel()
	reading := services.Reading{FailedSince: time.Now().Add(-time.Minute)}

	text, tone := weatherText(config.Item{ID: "weather"}, reading)
	if tone != ui.ToneError {
		t.Fatalf("tone = %v, want error for a reading that never arrived", tone)
	}
	if text == "" {
		t.Fatal("an error reading rendered nothing")
	}
}

// A stale reading is still a reading: it keeps the normal tone and shows its
// age, because an aged value is information and a blank widget is not.
func TestAStaleReadingKeepsItsValueAndTone(t *testing.T) {
	t.Parallel()
	reading := services.Reading{
		Observed: true, Temperature: 18.4, Unit: services.UnitCelsius, Code: 0,
		FetchedAt:   time.Now().Add(-90 * time.Minute),
		FailedSince: time.Now().Add(-30 * time.Minute),
	}

	text, tone := weatherText(config.Item{ID: "weather"}, reading)
	if tone != ui.ToneNormal {
		t.Fatalf("tone = %v, want normal; a stale value is still a value", tone)
	}
	if !strings.Contains(text, "18") {
		t.Fatalf("a stale reading lost its temperature: %q", text)
	}
	if !strings.Contains(text, "1h") {
		t.Fatalf("a stale reading %q does not show its age", text)
	}
}

// Before the first fetch there is nothing to report and nothing has failed.
func TestAReadingBeforeTheFirstFetchRendersThePlaceholder(t *testing.T) {
	t.Parallel()
	text, tone := weatherText(config.Item{ID: "weather"}, services.Reading{})
	if tone != ui.ToneNormal {
		t.Fatalf("tone = %v, want normal before the first fetch", tone)
	}
	if text != noWorkspace {
		t.Fatalf("text = %q, want the placeholder", text)
	}
}

func TestTheUnitSuffixFollowsTheReading(t *testing.T) {
	t.Parallel()
	celsius := services.Reading{Observed: true, Temperature: 18, Unit: services.UnitCelsius}
	fahrenheit := services.Reading{Observed: true, Temperature: 65, Unit: services.UnitFahrenheit}

	if text, _ := weatherText(config.Item{ID: "weather"}, celsius); !strings.Contains(text, "°C") {
		t.Fatalf("celsius reading rendered %q", text)
	}
	if text, _ := weatherText(config.Item{ID: "weather"}, fahrenheit); !strings.Contains(text, "°F") {
		t.Fatalf("fahrenheit reading rendered %q", text)
	}
}

func TestShowConditionAppendsTheConditionWord(t *testing.T) {
	t.Parallel()
	reading := services.Reading{Observed: true, Temperature: 18, Unit: services.UnitCelsius, Code: 95}

	plain, _ := weatherText(config.Item{ID: "weather"}, reading)
	withWord, _ := weatherText(config.Item{ID: "weather", ShowCondition: true}, reading)

	if len(withWord) <= len(plain) {
		t.Fatalf("show-condition rendered %q, no longer than %q", withWord, plain)
	}
	if !strings.Contains(strings.ToLower(withWord), "thunder") {
		t.Fatalf("condition text %q does not name the condition", withWord)
	}
}

// The live gate requires a reload of coordinates, unit and interval without a
// restart. Re-acquiring leases carries the interval; nothing else reaches the
// request, so without this the shell fetches its original city forever.
//
// Nothing in Tasks 1 to 6 would catch that: the suite can be green while the
// gate item is false, which is the hole 3A's inverted Configure/apply tests
// left and 3B's aggregate history ring left after it.
func TestAnAcceptedReloadPicksUpNewCoordinatesWithoutRestarting(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(weatherConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	before := reg.Weather().Starts()

	candidate := weatherConfig()
	candidate.Weather.Latitude, candidate.Weather.Longitude = 51.5, -0.13
	candidate.Weather.Unit = "fahrenheit"
	prepared, err := reg.PrepareConfig(candidate, identities(map[uint32]string{1: "DP-9"}))
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	prepared.Commit()

	if got := reg.Weather().Starts(); got != before {
		t.Fatalf("starts = %d, want the unchanged %d; the service restarted", got, before)
	}
	if !reg.Weather().Running() {
		t.Fatal("a reload stopped a service that is still leased")
	}
	url := reg.Weather().RequestURL()
	for _, want := range []string{"latitude=51.5", "temperature_unit=fahrenheit"} {
		if !strings.Contains(url, want) {
			t.Fatalf("after reload the request is %q, which does not carry %q", url, want)
		}
	}
}

// A rejected reload must not move the request either.
func TestARejectedReloadLeavesTheRequestUnchanged(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(weatherConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})
	before := reg.Weather().RequestURL()

	broken := weatherConfig()
	broken.Weather.Latitude = 51.5
	broken.Bar.Height, broken.Bar.Gap = 4, 4
	if _, err := reg.PrepareConfig(broken, identities(map[uint32]string{1: "DP-9"})); err == nil {
		t.Fatal("an unbuildable candidate was prepared")
	}

	if after := reg.Weather().RequestURL(); after != before {
		t.Fatalf("a rejected reload moved the request from %q to %q", before, after)
	}
}

func observedWeather() services.Reading {
	return services.Reading{
		Observed: true, Temperature: 18.4, Unit: services.UnitCelsius,
		Code: 0, FetchedAt: time.Now(),
		Humidity: ptrF(62), WindSpeed: ptrF(10.4), WindDirection: ptrF(45),
		Daily: []services.Day{
			{Date: "2026-09-15", Code: 0, High: 22, Low: 6, Sunrise: "2026-09-15T06:12", Sunset: "2026-09-15T18:44"},
			{Date: "2026-09-16", Code: 61, High: 19, Low: 11, Sunrise: "2026-09-16T06:14", Sunset: "2026-09-16T18:42"},
		},
	}
}

func ptrF(v float64) *float64 { return &v }
func ptrB(v bool) *bool       { return &v }

func TestTheWeatherWidgetBuildsAnIconRowWithThePanelAction(t *testing.T) {
	t.Parallel()
	w := buildWeatherWidget(config.Item{ID: "weather"}, DefaultTheme().Metrics)
	row := w.node
	if row.Kind != ui.KindRow {
		t.Fatalf("kind = %v, want a row", row.Kind)
	}
	if row.Action != panelWeatherAction {
		t.Fatalf("action = %q, want %q", row.Action, panelWeatherAction)
	}
	if len(row.Children) != 2 || row.Children[0].Kind != ui.KindIcon || row.Children[1].Kind != ui.KindText {
		t.Fatalf("children = %+v, want an icon beside a text node", row.Children)
	}
}

func TestTheWeatherWidgetRefreshPaintsTheGlyphAndTemperature(t *testing.T) {
	t.Parallel()
	w := buildWeatherWidget(config.Item{ID: "weather"}, DefaultTheme().Metrics)
	icon, text := w.node.Children[0], w.node.Children[1]
	view := barView{Weather: observedWeather()}
	if !w.refresh(view) {
		t.Fatal("the first refresh reported no change")
	}
	if icon.Icon != "clear-day" {
		t.Fatalf("icon = %q, want clear-day", icon.Icon)
	}
	if text.Text != "18°C" {
		t.Fatalf("text = %q, want 18°C", text.Text)
	}

	night := observedWeather()
	night.IsDay = ptrB(false)
	if !w.refresh(barView{Weather: night}) {
		t.Fatal("a changed reading reported no change")
	}
	if icon.Icon != "clear-night" {
		t.Fatalf("icon = %q, want clear-night", icon.Icon)
	}
}

func TestTheWeatherWidgetRefreshIsStableForAnUnchangedReading(t *testing.T) {
	t.Parallel()
	w := buildWeatherWidget(config.Item{ID: "weather"}, DefaultTheme().Metrics)
	view := barView{Weather: observedWeather()}
	w.refresh(view)
	if w.refresh(view) {
		t.Fatal("an unchanged reading reported a change")
	}
}

func collectTooltipLines(n *ui.Node) []string {
	if n == nil {
		return nil
	}
	var out []string
	if n.Text != "" {
		out = append(out, n.Text)
	}
	for _, child := range n.Children {
		out = append(out, collectTooltipLines(child)...)
	}
	return out
}

func tooltipHasLine(lines []string, substr string) bool {
	for _, line := range lines {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

func TestTheWeatherWidgetTooltipCarriesFacts(t *testing.T) {
	t.Parallel()
	w := buildWeatherWidget(config.Item{ID: "weather"}, DefaultTheme().Metrics)
	if !w.refresh(barView{Weather: observedWeather()}) {
		t.Fatal("the first refresh reported no change")
	}
	lines := collectTooltipLines(w.tooltipTree())
	if lines == nil {
		t.Fatal("the observed reading built no tooltip tree")
	}
	for _, want := range []string{"Clear", "6", "22", "10.4", "NE", "Updated"} {
		if !tooltipHasLine(lines, want) {
			t.Fatalf("tooltip lines %q are missing %q", lines, want)
		}
	}
	for _, line := range lines {
		if line == "Weather" {
			t.Fatal("the tooltip still says the literal word Weather")
		}
	}
}

func TestTheWeatherWidgetTooltipShowsTheAgeWhenStale(t *testing.T) {
	t.Parallel()
	w := buildWeatherWidget(config.Item{ID: "weather"}, DefaultTheme().Metrics)
	reading := observedWeather()
	reading.FetchedAt = time.Now().Add(-12 * time.Minute)
	reading.FailedSince = time.Now()
	w.refresh(barView{Weather: reading})
	lines := collectTooltipLines(w.tooltipTree())
	if !tooltipHasLine(lines, "12m") {
		t.Fatalf("tooltip lines %q do not carry the age", lines)
	}
}

func TestTheWeatherWidgetTooltipStates(t *testing.T) {
	t.Parallel()
	w := buildWeatherWidget(config.Item{ID: "weather"}, DefaultTheme().Metrics)
	w.refresh(barView{})
	if w.tooltipTree() != nil {
		t.Fatalf("the placeholder built a tooltip: %+v", w.tooltipTree())
	}

	failed := services.Reading{FailedSince: time.Now()}
	w.refresh(barView{Weather: failed})
	lines := collectTooltipLines(w.tooltipTree())
	if !tooltipHasLine(lines, "weather unavailable") {
		t.Fatalf("tooltip lines %q do not carry the failure", lines)
	}
}
