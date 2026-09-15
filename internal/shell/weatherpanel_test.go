package shell

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestPanelWeatherIdentity(t *testing.T) {
	t.Parallel()
	if got := PanelWeather.String(); got != "weather" {
		t.Fatalf("String() = %q, want weather", got)
	}
	if got := panelSurfaceID(PanelWeather); got != "panel:weather" {
		t.Fatalf("surface id = %q, want panel:weather", got)
	}
	if got := panelTargetSize(PanelWeather); got != (ui.Rect{W: 460, H: 560}) {
		t.Fatalf("target size = %+v, want 460x560", got)
	}
}

func TestPanelWeatherAnswersItsIPCName(t *testing.T) {
	t.Parallel()
	id, err := parsePanelName("weather")
	if err != nil || id != PanelWeather {
		t.Fatalf("parsePanelName(weather) = %v, %v", id, err)
	}
	id, ok := panelIDFromAux("panel:weather")
	if !ok || id != PanelWeather {
		t.Fatalf("panelIDFromAux(panel:weather) = %v, %v", id, ok)
	}
}

func TestPanelWeatherDispatchesTheWeatherTree(t *testing.T) {
	t.Parallel()
	r := &Registry{cfg: config.Default(), reading: observedWeather()}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}

	if !hasLine(collectTooltipLines(r.panelTree(h)), "Clear") {
		t.Fatal("PanelWeather still dispatches the placeholder tree")
	}
}

func TestWeatherBarActionTogglesTheStandalonePanel(t *testing.T) {
	r := newPanelRegistry(t)
	bar := &Bar{conn: "DP-1"}
	r.bars = map[uint32]*Bar{7: bar}
	r.bindBarPanelActionsLocked(7, bar)

	if !bar.onAction(panelWeatherAction, buttonLeft) {
		t.Fatal("left-click did not handle the weather action")
	}
	_ = drainAux(t, r, 2)
	r.mu.Lock()
	h := r.panelHosts[PanelWeather]
	r.mu.Unlock()
	if h == nil {
		t.Fatal("left-click did not create PanelWeather")
	}

	if !bar.onAction(panelWeatherAction, buttonLeft) {
		t.Fatal("the second left-click was not handled")
	}
	_ = drainAux(t, r, 2)
	r.mu.Lock()
	h = r.panelHosts[PanelWeather]
	r.mu.Unlock()
	if h != nil {
		t.Fatal("the second left-click did not close PanelWeather")
	}
}

func treeIcons(n *ui.Node) []string {
	if n == nil {
		return nil
	}
	var out []string
	if n.Kind == ui.KindIcon && n.Icon != "" {
		out = append(out, n.Icon)
	}
	for _, child := range n.Children {
		out = append(out, treeIcons(child)...)
	}
	return out
}

func treeActions(n *ui.Node) []string {
	if n == nil {
		return nil
	}
	var out []string
	if n.Action != "" {
		out = append(out, n.Action)
	}
	for _, child := range n.Children {
		out = append(out, treeActions(child)...)
	}
	return out
}

func hasLine(lines []string, substr string) bool {
	for _, line := range lines {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

func hasValue(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func TestWeatherPanelHeroCarriesTheReading(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Weather.Location = "Brisbane"
	r := &Registry{cfg: cfg, reading: observedWeather()}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}

	tree := weatherTree(r, h)
	texts := collectTooltipLines(tree)
	icons := treeIcons(tree)
	actions := treeActions(tree)

	for _, want := range []string{"18°C", "Clear", "Brisbane", "Low 6°", "High 22°", "Updated"} {
		if !hasLine(texts, want) {
			t.Fatalf("hero texts %q are missing %q", texts, want)
		}
	}
	if !hasValue(icons, "clear-day") {
		t.Fatalf("hero icons %q are missing clear-day", icons)
	}
	if !hasValue(actions, "weather-close") {
		t.Fatalf("actions %q are missing the close button", actions)
	}
}

func TestWeatherPanelDetailsCarryTheRows(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Weather.Location = "Brisbane"
	r := &Registry{cfg: cfg, reading: observedWeather()}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}

	tree := weatherTree(r, h)
	texts := collectTooltipLines(tree)
	icons := treeIcons(tree)

	for _, want := range []string{"Temperature min", "6°C", "Temperature max", "22°C", "Feels like", "9.5°C", "Wind", "10.4 km/h NE", "Humidity", "62%", "UV index", "0.0", "Precip chance", "10%", "Sunrise", "06:12", "Sunset", "18:44", "Elevation", "64 m", "Timezone", "Australia/Sydney"} {
		if !hasLine(texts, want) {
			t.Fatalf("detail texts %q are missing %q", texts, want)
		}
	}
	for _, want := range []string{"thermometer", "wind", "humidity", "sunrise", "sunset", "elevation"} {
		if !hasValue(icons, want) {
			t.Fatalf("detail icons %q are missing %q", icons, want)
		}
	}
}

func TestWeatherPanelDetailsPutMaximumBeforeMinimum(t *testing.T) {
	t.Parallel()
	r := &Registry{cfg: config.Default(), reading: observedWeather()}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}
	texts := collectTooltipLines(weatherTree(r, h))
	minIndex, maxIndex := -1, -1
	for i, text := range texts {
		if text == "Temperature min" {
			minIndex = i
		}
		if text == "Temperature max" {
			maxIndex = i
		}
	}
	if maxIndex < 0 || minIndex < 0 || maxIndex > minIndex {
		t.Fatalf("detail order = %q, want Temperature max before Temperature min", texts)
	}
}

func TestWeatherRangesCarryTheReadingUnit(t *testing.T) {
	t.Parallel()
	reading := services.Reading{
		Unit:  services.UnitFahrenheit,
		Daily: []services.Day{{Low: 10, High: 20}},
	}
	if got := weatherDayRange(reading); got != "Low 10°F  High 20°F" {
		t.Fatalf("day range = %q, want Fahrenheit on both values", got)
	}
	forecast := ccForecastDay(DefaultTheme().Metrics, 200, &reading.Daily[0], reading.Unit)
	if !hasLine(collectTooltipLines(forecast), "20°F / 10°F") {
		t.Fatalf("forecast range = %q, want Fahrenheit on both values", collectTooltipLines(forecast))
	}
}

func TestWeatherPanelRendersDashesForAbsentFields(t *testing.T) {
	t.Parallel()
	r := &Registry{reading: services.Reading{Observed: true, Temperature: 18, Unit: services.UnitCelsius, Code: 3, FetchedAt: time.Now()}}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}

	texts := collectTooltipLines(weatherTree(r, h))
	dashes := 0
	for _, line := range texts {
		if line == absent {
			dashes++
		}
	}
	if dashes < 7 {
		t.Fatalf("an absent-field reading rendered %d dashes: %q", dashes, texts)
	}
}

func TestWeatherPanelRendersTheErrorState(t *testing.T) {
	t.Parallel()
	r := &Registry{reading: services.Reading{FailedSince: time.Now()}}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}

	if !hasLine(collectTooltipLines(weatherTree(r, h)), "weather unavailable") {
		t.Fatal("a never-observed reading rendered no error state")
	}
}

func TestWeatherPanelStaleReadingShowsItsAge(t *testing.T) {
	t.Parallel()
	reading := observedWeather()
	reading.FetchedAt = time.Now().Add(-90 * time.Minute)
	reading.FailedSince = time.Now().Add(-30 * time.Minute)
	r := &Registry{reading: reading}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}

	if !hasLine(collectTooltipLines(weatherTree(r, h)), "1h") {
		t.Fatal("a stale reading does not show its age on the panel")
	}
}

func weekReading() services.Reading {
	reading := observedWeather()
	codes := []int{0, 61, 71, 95, 3, 2, 45}
	hours := []string{"06:12", "06:14", "06:16", "06:18", "06:20", "06:22", "06:24"}
	reading.Daily = nil
	for i, code := range codes {
		date := time.Now().AddDate(0, 0, i).Format("2006-01-02")
		reading.Daily = append(reading.Daily, services.Day{
			Date: date, Code: code,
			High: 20 + float64(i), Low: 8 + float64(i),
			Sunrise: date + "T" + hours[i],
			Sunset:  date + "T18:30",
		})
	}
	return reading
}

func TestWeatherPanelForecastCarriesTheWeek(t *testing.T) {
	t.Parallel()
	r := &Registry{reading: weekReading()}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}

	tree := weatherTree(r, h)
	texts := collectTooltipLines(tree)
	icons := treeIcons(tree)

	tomorrow := time.Now().AddDate(0, 0, 1).Format("Mon")
	afterTomorrow := time.Now().AddDate(0, 0, 2).Format("Mon")
	if hasLine(texts, "Today") {
		t.Fatalf("forecast texts %q still mark today", texts)
	}
	for _, want := range []string{tomorrow, afterTomorrow, "21°C / 9°C", "Clear", "Rain", "Snow", "Thunderstorm"} {
		if !hasLine(texts, want) {
			t.Fatalf("forecast texts %q are missing %q", texts, want)
		}
	}
	scrolls := 0
	var walk func(n *ui.Node)
	walk = func(n *ui.Node) {
		if n.Kind == ui.KindScroll {
			scrolls++
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(tree)
	if scrolls != 1 {
		t.Fatalf("the forecast list is not in a scroll: %d scrolls", scrolls)
	}
	_ = icons
}

func TestWeatherPanelForecastRendersWhatExists(t *testing.T) {
	t.Parallel()
	r := &Registry{reading: observedWeather()}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}

	texts := collectTooltipLines(weatherTree(r, h))
	tomorrow := time.Now().AddDate(0, 0, 1).Format("Mon")
	if !hasLine(texts, tomorrow) {
		t.Fatalf("a two-day body rendered %q, want tomorrow's row", texts)
	}
}

func TestWeatherPanelForecastWithoutDataIsAPlaceholder(t *testing.T) {
	t.Parallel()
	r := &Registry{reading: services.Reading{Observed: true, Temperature: 18, Unit: services.UnitCelsius, Code: 3, FetchedAt: time.Now()}}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}

	if !hasLine(collectTooltipLines(weatherTree(r, h)), "No forecast yet") {
		t.Fatal("an empty forecast rendered no placeholder")
	}
}

func hourlyReading() services.Reading {
	reading := observedWeather()
	reading.Hourly = []services.Hour{
		{Time: "2026-09-15T14:00", Code: 0, Temperature: 14.2, IsDay: ptrB(true), PrecipProbability: ptrF(10)},
		{Time: "2026-09-15T15:00", Code: 2, Temperature: 13.8, IsDay: ptrB(true), PrecipProbability: ptrF(20)},
		{Time: "2026-09-15T16:00", Code: 61, Temperature: 12.5, IsDay: ptrB(false), PrecipProbability: ptrF(80)},
	}
	return reading
}

func TestTheWeatherPanelSeedsTheDailyView(t *testing.T) {
	t.Parallel()
	r := &Registry{reading: observedWeather()}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}
	tree := weatherTree(r, h)
	if h.weatherView != "daily" {
		t.Fatalf("seeded view = %q, want daily", h.weatherView)
	}
	if hasLine(collectTooltipLines(tree), "Today") {
		t.Fatal("the daily view still marks today, which the hero owns")
	}
}

func TestTheWeatherPanelHourlyViewCarriesTheHours(t *testing.T) {
	t.Parallel()
	r := &Registry{reading: hourlyReading()}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}
	h.weatherView = "hourly"
	tree := weatherTree(r, h)
	texts := collectTooltipLines(tree)
	icons := treeIcons(tree)
	for _, want := range []string{"14:00", "16:00", "14°C", "12°C"} {
		if !hasLine(texts, want) {
			t.Fatalf("hourly texts %q are missing %q", texts, want)
		}
	}
	for _, want := range []string{"clear-day", "partly-cloudy", "rain"} {
		if !hasValue(icons, want) {
			t.Fatalf("hourly icons %q are missing %q", icons, want)
		}
	}
}

func TestWeatherPanelHourlyViewCapsAtTheReferenceRowCount(t *testing.T) {
	t.Parallel()
	reading := hourlyReading()
	for i := len(reading.Hourly); i < 8; i++ {
		reading.Hourly = append(reading.Hourly, services.Hour{
			Time: fmt.Sprintf("2026-09-15T%02d:00", 14+i), Code: 0, Temperature: 10,
		})
	}
	list := weatherHourlyList(reading, DefaultTheme().Metrics)
	if len(list.Children) != 1 || len(list.Children[0].Children) != 7 {
		t.Fatalf("hourly rows = %d, want 7", len(list.Children[0].Children))
	}
}

func TestTheWeatherPanelSwitchesViewsThroughTheSegmentedAction(t *testing.T) {
	r := &Registry{reading: hourlyReading(), panelHosts: make(map[PanelID]*PanelHost)}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}
	h.root = weatherTree(r, h)
	h.focus = ui.Focusables(h.root)
	h.roving.Count = len(h.focus)
	r.panelHosts[PanelWeather] = h

	seg := findAction(h.root, "weather-view:hourly")
	if seg == nil {
		t.Fatal("the hourly segment is missing")
	}
	h.setFocus(seg)
	if !h.activate(r) {
		t.Fatal("the hourly segment was not handled")
	}
	if h.weatherView != "hourly" {
		t.Fatalf("view = %q, want hourly", h.weatherView)
	}
	if !hasLine(collectTooltipLines(h.root), "14:00") {
		t.Fatal("the rebuilt tree still shows the daily view")
	}

	back := findAction(h.root, "weather-view:daily")
	if back == nil {
		t.Fatal("the daily segment is missing after switching")
	}
	h.setFocus(back)
	if !h.activate(r) {
		t.Fatal("the daily segment was not handled")
	}
	tomorrow := time.Now().AddDate(0, 0, 1).Format("Mon")
	if !hasLine(collectTooltipLines(h.root), tomorrow) {
		t.Fatal("the rebuilt tree did not return to the daily view")
	}
}

func TestTheWeatherLocationPrefersTheLabelThenTheResolvedName(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Weather.Configured = true
	cfg.Weather.Latitude, cfg.Weather.Longitude = 27.47, 153.02

	resolved := services.Reading{Location: "Brisbane"}
	if got := weatherLocation(cfg.Weather, resolved); got != "Brisbane" {
		t.Fatalf("location = %q, want the resolved name", got)
	}

	labelled := cfg.Weather
	labelled.Location = "Home"
	if got := weatherLocation(labelled, resolved); got != "Home" {
		t.Fatalf("location = %q, want the configured label to win", got)
	}

	if got := weatherLocation(cfg.Weather, services.Reading{}); got != "27.47°, 153.02°" {
		t.Fatalf("location = %q, want the coordinates fallback", got)
	}
}

func TestTheWeatherLocationDoesNotInventCoordinatesForAnUnresolvedCity(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Weather.Configured = true
	cfg.Weather.City = "Brisbane"
	if got := weatherLocation(cfg.Weather, services.Reading{}); got != "Brisbane" {
		t.Fatalf("location = %q, want the configured city while geocoding is unresolved", got)
	}
}

func TestTheWeatherPanelTonesFollowTheReference(t *testing.T) {
	t.Parallel()
	r := &Registry{reading: hourlyReading()}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}

	day := weatherTree(r, h)
	var heroTone ui.Tone
	var found bool
	var walk func(n *ui.Node)
	walk = func(n *ui.Node) {
		if !found && n.Kind == ui.KindIcon && n.Icon == "clear-day" {
			heroTone, found = n.Tone, true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(day)
	if !found || heroTone != ui.ToneAccent {
		t.Fatalf("the day hero glyph tone = %v (found %v), want accent", heroTone, found)
	}

	night := hourlyReading()
	falseValue := false
	night.IsDay = &falseValue
	for i := range night.Hourly {
		night.Hourly[i].IsDay = &falseValue
	}
	n := &Registry{reading: night}
	h2 := &PanelHost{id: PanelWeather, theme: DefaultTheme()}
	h2.weatherView = "hourly"
	toneSeen := map[ui.Tone]bool{}
	var walk2 func(n *ui.Node)
	walk2 = func(n *ui.Node) {
		if n.Kind == ui.KindIcon && n.Tone != ui.ToneNormal {
			toneSeen[n.Tone] = true
		}
		for _, c := range n.Children {
			walk2(c)
		}
	}
	walk2(weatherTree(n, h2))
	if toneSeen[ui.ToneAccent] {
		t.Fatal("an all-night hourly view painted an accent glyph")
	}
}
