package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
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
	r.mu.Lock()
	r.bars = map[uint32]*Bar{7: bar}
	r.bindBarPanelActionsLocked(7, bar)
	r.mu.Unlock()

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

func TestWeatherBarRightClickOpensTheStandalonePanel(t *testing.T) {
	r := newPanelRegistry(t)
	bar := &Bar{conn: "DP-1"}
	r.mu.Lock()
	r.bars = map[uint32]*Bar{7: bar}
	r.bindBarPanelActionsLocked(7, bar)
	r.mu.Unlock()

	if !bar.onAction(panelWeatherAction, buttonRight) {
		t.Fatal("right-click did not handle the weather action")
	}
	_ = drainAux(t, r, 2)
	r.mu.Lock()
	h := r.panelHosts[PanelWeather]
	r.mu.Unlock()
	if h == nil {
		t.Fatal("right-click did not create PanelWeather")
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

func TestWeatherPanelUsesADominantHeroAndFourDayStrip(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Weather.Location = "Brisbane"
	r := &Registry{cfg: cfg, reading: observedWeather()}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}

	tree := weatherTree(r, h)
	if len(tree.Children) != 2 || tree.Children[1].Kind != ui.KindColumn {
		t.Fatalf("weather tree = %+v, want header plus body column", tree)
	}
	body := tree.Children[1]
	if len(body.Children) != 2 {
		t.Fatalf("weather body has %d children, want hero and forecast", len(body.Children))
	}
	hero, forecast := body.Children[0], body.Children[1]
	if hero.Kind != ui.KindCapsule || hero.Height <= body.Height/2 {
		t.Fatalf("hero = %+v, want a full-width dominant card", hero)
	}
	if forecast.Kind != ui.KindRow || len(forecast.Children) != 4 {
		t.Fatalf("forecast = %+v, want four compact slots", forecast)
	}
	for i, slot := range forecast.Children {
		if slot == nil || slot.Kind != ui.KindCapsule {
			t.Fatalf("forecast slot %d = %+v, want a card", i, slot)
		}
	}
	view := findNode(tree, func(n *ui.Node) bool { return n.Kind == ui.KindSegmented && n.Key == "weather-view" })
	if view == nil || len(view.Children) != 2 {
		t.Fatalf("weather view = %+v, want a Daily/Hourly segmented control", view)
	}
	if view.Role != "tablist" || view.Name == "" {
		t.Fatalf("weather view accessibility = %q/%q, want a named tablist", view.Name, view.Role)
	}
	if !hasValue(treeActions(view), "weather-view:daily") || !hasValue(treeActions(view), "weather-view:hourly") {
		t.Fatalf("weather view actions = %q, want both forecast choices", treeActions(view))
	}
	if !view.Children[0].State.Has(ui.StateSelected) || view.Children[1].State.Has(ui.StateSelected) {
		t.Fatalf("default forecast selection = %+v, want Daily selected", view.Children)
	}
	for _, forbidden := range []string{"Temperature max", "Temperature min", "UV index", "Timezone", "Sunrise", "Sunset", "Precip chance"} {
		if hasLine(collectTooltipLines(tree), forbidden) {
			t.Fatalf("weather surface still exposes retired detail %q", forbidden)
		}
	}

	if err := ui.LayoutColumn(tree, ui.Rect{W: 460, H: 560}, func(string, ui.TextAttrs) (int, int) {
		return 8, 16
	}); err != nil {
		t.Fatalf("weather tree does not lay out at its target size: %v", err)
	}
	header := tree.Children[0]
	tabs := findNode(header, func(n *ui.Node) bool { return n.Kind == ui.KindSegmented && n.Key == "weather-view" })
	close := findNode(header, func(n *ui.Node) bool { return n.Action == "weather-close" })
	if tabs == nil || close == nil || tabs.Bounds.X+tabs.Bounds.W > close.Bounds.X ||
		tabs.Bounds.X < header.Bounds.X || tabs.Bounds.X+tabs.Bounds.W > header.Bounds.X+header.Bounds.W {
		t.Fatalf("header tab bounds = %+v, close = %+v, header = %+v; want visible non-overlapping controls", tabs, close, header.Bounds)
	}
	stack := hero.Children[0]
	if stack.Kind != ui.KindStack || len(stack.Children) < 1 || stack.Children[0].Kind != ui.KindEffect {
		t.Fatalf("hero content = %+v, want effect-first stack", stack)
	}
	if stack.Bounds.W != hero.Bounds.W-2*hero.Padding || stack.Bounds.H != hero.Bounds.H-2*hero.Padding {
		t.Fatalf("hero stack bounds = %+v, card = %+v, want stack to fill card content", stack.Bounds, hero.Bounds)
	}
	if stack.Children[0].Bounds != stack.Bounds {
		t.Fatalf("effect bounds = %+v, stack = %+v, want full hero effect target", stack.Children[0].Bounds, stack.Bounds)
	}
}

func TestWeatherPanelHourlyViewRendersTheCachedForecastInASevenRowViewport(t *testing.T) {
	t.Parallel()
	var hourly []services.Hour
	for i := 0; i < 8; i++ {
		day := i != 1
		hour := services.Hour{
			Time:        time.Date(2026, time.October, 21, 8+i, 0, 0, 0, time.UTC).Format("2006-01-02T15:04"),
			Code:        0,
			Temperature: 68 + float64(i),
			IsDay:       &day,
		}
		if i == 0 {
			rain, humidity, wind := 40.0, 62.0, 12.0
			hour.PrecipProbability, hour.Humidity, hour.WindSpeed = &rain, &humidity, &wind
		}
		hourly = append(hourly, hour)
	}
	r := &Registry{cfg: config.Default(), reading: services.Reading{
		Observed: true, Unit: services.UnitFahrenheit, Temperature: 68, Hourly: hourly,
	}}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme(), weatherView: "hourly"}

	tree := weatherTree(r, h)
	list := findNode(tree, func(n *ui.Node) bool { return n.Kind == ui.KindScroll && n.Key == "weather-hourly" })
	if list == nil {
		t.Fatal("hourly view has no scrollable list")
	}
	if len(list.Children) != len(hourly) {
		t.Fatalf("hour rows = %d, want all %d cached hours", len(list.Children), len(hourly))
	}
	first := list.Children[0]
	if first.Kind != ui.KindRow || !hasLine(collectTooltipLines(first), "08:00") ||
		!hasLine(collectTooltipLines(first), "68°F") || !hasLine(collectTooltipLines(first), "Clear · Rain 40%") {
		t.Fatalf("first hourly row = %+v, want local time, configured temperature, condition and rain chance", first)
	}
	if !strings.Contains(first.Tooltip, "Condition: Clear") || !strings.Contains(first.Tooltip, "Rain: 40%") ||
		!strings.Contains(first.Tooltip, "Humidity: 62%") || !strings.Contains(first.Tooltip, "Wind: 12.0 km/h") {
		t.Fatalf("hourly tooltip = %q, want condition, rain, humidity and wind", first.Tooltip)
	}
	dayGlyph := findNode(first, func(n *ui.Node) bool { return n.Kind == ui.KindIcon })
	nightGlyph := findNode(list.Children[1], func(n *ui.Node) bool { return n.Kind == ui.KindIcon })
	if dayGlyph == nil || dayGlyph.Tone != ui.ToneAccent {
		t.Fatalf("day glyph = %+v, want accent tone", dayGlyph)
	}
	if nightGlyph == nil || nightGlyph.Tone != ui.ToneNormal {
		t.Fatalf("night glyph = %+v, want normal tone", nightGlyph)
	}

	m := h.metrics()
	wantHeight := 7*(m.IconSmall+2*theme.MarginXXS) + 6*theme.MarginXXS
	if list.Height != wantHeight {
		t.Fatalf("hourly viewport height = %d, want seven rows at icon height plus padding and gaps", list.Height)
	}
	if err := ui.LayoutColumn(tree, panelTargetSize(PanelWeather), func(s string, _ ui.TextAttrs) (int, int) {
		return len(s) * 8, 16
	}); err != nil {
		t.Fatalf("hourly tree does not lay out at its target size: %v", err)
	}
	if list.ContentH <= list.Bounds.H {
		t.Fatalf("hourly list content = %d, viewport = %d, want later hours to scroll", list.ContentH, list.Bounds.H)
	}
}

func TestWeatherPanelHourlyViewFitsEveryThemeDensity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		density theme.Density
	}{
		{name: "mini", density: theme.DensityMini},
		{name: "compact", density: theme.DensityCompact},
		{name: "default", density: theme.DensityDefault},
		{name: "comfortable", density: theme.DensityComfortable},
		{name: "spacious", density: theme.DensitySpacious},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, ok := theme.MetricsFor(tc.density)
			if !ok {
				t.Fatalf("metrics missing for %q", tc.name)
			}
			th := DefaultTheme()
			th.Metrics = m
			r := &Registry{cfg: config.Default(), reading: observedWeather()}
			r.reading.Hourly = make([]services.Hour, 8)
			h := &PanelHost{id: PanelWeather, theme: th, weatherView: "hourly"}
			tree := weatherTree(r, h)
			if err := ui.LayoutColumn(tree, panelTargetSize(PanelWeather), h.measureText()); err != nil {
				t.Fatalf("hourly weather panel does not fit 460x560 at %s density: %v", tc.name, err)
			}
			body := tree.Children[1]
			hero := body.Children[0]
			if hero.Height*3 < body.Height {
				t.Fatalf("hourly hero height = %d at %s density, body = %d; want the hero to occupy at least a third", hero.Height, tc.name, body.Height)
			}
		})
	}
}

func TestWeatherPanelViewActionRetainsTheSelectedModeForThePanelLifetime(t *testing.T) {
	r := &Registry{cfg: config.Default(), reading: observedWeather()}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rebuildPanel(h)

	activate := func(action string) {
		t.Helper()
		for i, n := range h.focus {
			if n.Action == action {
				h.roving.Set(i)
				if !h.activate(r) {
					t.Fatalf("activation of %q was not handled", action)
				}
				return
			}
		}
		t.Fatalf("panel has no focusable action %q", action)
	}
	selected := func(action string) bool {
		n := findNode(h.root, func(n *ui.Node) bool { return n.Action == action })
		return n != nil && n.State.Has(ui.StateSelected)
	}

	activate("weather-view:hourly")
	if h.weatherView != "hourly" || !selected("weather-view:hourly") {
		t.Fatalf("after selecting hourly, mode = %q and selected = %v", h.weatherView, selected("weather-view:hourly"))
	}
	if findNode(h.root, func(n *ui.Node) bool { return n.Kind == ui.KindScroll && n.Key == "weather-hourly" }) == nil {
		t.Fatal("hourly selection did not rebuild the panel with its hourly list")
	}

	activate("weather-view:daily")
	if h.weatherView != "daily" || !selected("weather-view:daily") {
		t.Fatalf("after selecting daily, mode = %q and selected = %v", h.weatherView, selected("weather-view:daily"))
	}
	if findNode(h.root, func(n *ui.Node) bool { return n.Kind == ui.KindScroll && n.Key == "weather-hourly" }) != nil {
		t.Fatal("daily selection kept the hourly list in the panel")
	}
}

func TestWeatherPanelHourlyViewKeepsEmptyStaleAndErrorStates(t *testing.T) {
	t.Parallel()
	now := time.Now()
	stale := observedWeather()
	stale.FetchedAt = now.Add(-2 * time.Hour)
	stale.FailedSince = now.Add(-time.Hour)
	stale.Hourly = []services.Hour{{Time: "2026-10-21T11:00", Code: 3, Temperature: 18}}

	for _, tc := range []struct {
		name    string
		reading services.Reading
		want    []string
	}{
		{name: "empty", reading: services.Reading{}, want: []string{"No hourly forecast yet"}},
		{name: "failed", reading: services.Reading{FailedSince: now}, want: []string{"No hourly forecast yet", "weather unavailable"}},
		{name: "stale", reading: stale, want: []string{"11:00", "Stale · Age"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := &Registry{cfg: config.Default(), reading: tc.reading}
			h := &PanelHost{id: PanelWeather, theme: DefaultTheme(), weatherView: "hourly"}
			tree := weatherTree(r, h)
			for _, want := range tc.want {
				if !hasLine(collectTooltipLines(tree), want) {
					t.Errorf("hourly weather text %q is missing %q", collectTooltipLines(tree), want)
				}
			}
			if findNode(tree, func(n *ui.Node) bool { return n.Kind == ui.KindScroll && n.Key == "weather-hourly" }) == nil {
				t.Fatal("hourly mode lost its forecast viewport")
			}
		})
	}
}

func TestWeatherPanelHeroCarriesOnlyCurrentWeatherInformation(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Weather.Location = "Brisbane"
	reading := observedWeather()
	// Without a zone the sky reads is_day, not the wall clock against the
	// fixture's sunrise and sunset; TestWeatherDaylightFollowsSunriseAndSunset
	// covers the clock.
	reading.Timezone = nil
	r := &Registry{cfg: cfg, reading: reading}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}

	tree := weatherTree(r, h)
	texts := collectTooltipLines(tree)
	// The three-cell grid is gone; every fact it carried now rides the one
	// quiet meta line, so assert the facts rather than their old labels.
	for _, want := range []string{"18°C", "Clear", "Brisbane", "Low 6°", "High 22°", "feels 10°C", "10.4 km/h NE", "62%"} {
		if !hasLine(texts, want) {
			t.Fatalf("weather texts %q are missing %q", texts, want)
		}
	}
	for _, forbidden := range []string{"Temperature max", "Temperature min", "UV index", "Timezone", "Sunrise", "Sunset", "Precip chance", "Elevation"} {
		if hasLine(texts, forbidden) {
			t.Fatalf("weather texts %q still contain retired field %q", texts, forbidden)
		}
	}
	// The hero's mark is the effect form, not a glyph: the clear state has to
	// be legible from the effect spec the hero actually carries.
	effect := findNode(tree, func(n *ui.Node) bool { return n.Kind == ui.KindEffect })
	if effect == nil || effect.Effect.Variant != ui.WeatherClear || effect.Effect.Daylight != 1 {
		t.Fatalf("hero effect = %+v, want the clear day variant", effect)
	}
	if !hasValue(treeActions(tree), "weather-close") {
		t.Fatalf("actions %q are missing the close button", treeActions(tree))
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
	if dashes < 4 {
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

func TestWeatherPanelForecastCarriesFourFollowingDays(t *testing.T) {
	t.Parallel()
	r := &Registry{reading: weekReading()}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}

	tree := weatherTree(r, h)
	forecast := tree.Children[1].Children[1]
	if forecast.Kind != ui.KindRow || len(forecast.Children) != 4 {
		t.Fatalf("forecast = %+v, want four following days", forecast)
	}
	texts := collectTooltipLines(forecast)

	for i := 1; i <= 4; i++ {
		if !hasLine(texts, time.Now().AddDate(0, 0, i).Format("Mon")) {
			t.Fatalf("forecast texts %q are missing day %d", texts, i)
		}
	}
	for _, want := range []string{"21°C / 9°C"} {
		if !hasLine(texts, want) {
			t.Fatalf("forecast texts %q are missing %q", texts, want)
		}
	}
	for _, want := range []string{"rain", "snow", "thunderstorm", "cloud"} {
		if !hasValue(treeIcons(forecast), want) {
			t.Fatalf("forecast icons %q are missing %q", treeIcons(forecast), want)
		}
	}
}

func TestWeatherPanelForecastWithoutDataIsAPlaceholder(t *testing.T) {
	t.Parallel()
	r := &Registry{reading: services.Reading{Observed: true, Temperature: 18, Unit: services.UnitCelsius, Code: 3, FetchedAt: time.Now()}}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}

	forecast := weatherTree(r, h).Children[1].Children[1]
	if forecast.Kind != ui.KindRow || len(forecast.Children) != 4 {
		t.Fatalf("empty forecast = %+v, want four stable slots", forecast)
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

	if got := weatherLocation(cfg.Weather, services.Reading{}); got != "" {
		t.Fatalf("location = %q, want no machine coordinate fallback", got)
	}
}

func TestTheWeatherHeroOmitsAnUnresolvedCoordinateCaption(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Weather.Configured = true
	cfg.Weather.Latitude, cfg.Weather.Longitude = -37.81, 144.96
	r := &Registry{cfg: cfg, reading: observedWeather()}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}

	tree := weatherTree(r, h)
	hero := tree.Children[1].Children[0]
	stack := hero.Children[0]
	foreground := stack.Children[2]
	if foreground.Kind != ui.KindColumn || len(foreground.Children) == 0 {
		t.Fatalf("hero foreground = %+v, want content column", foreground)
	}
	// The reading group leads the hero. An unresolved coordinate must not
	// reappear as a caption above it.
	if foreground.Children[0].Kind == ui.KindText {
		t.Fatalf("first hero content = %q, want the reading group, not a caption", foreground.Children[0].Text)
	}
	for _, line := range collectTooltipLines(hero) {
		if strings.Contains(line, "-37.81") || strings.Contains(line, "144.96") {
			t.Fatalf("hero still contains machine coordinates: %q", line)
		}
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

func TestTheWeatherPanelFollowsTheDayAndNightReference(t *testing.T) {
	t.Parallel()
	// Without a zone the sky reads is_day rather than the wall clock.
	dayReading := observedWeather()
	dayReading.Timezone = nil
	r := &Registry{reading: dayReading}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}
	day := findNode(weatherTree(r, h), func(n *ui.Node) bool { return n.Kind == ui.KindEffect })
	if day == nil || day.Effect.Daylight != 1 {
		t.Fatalf("day hero effect = %+v, want the daylight form", day)
	}

	night := observedWeather()
	night.Timezone = nil
	falseValue := false
	night.IsDay = &falseValue
	n := &Registry{reading: night}
	h2 := &PanelHost{id: PanelWeather, theme: DefaultTheme()}
	got := findNode(weatherTree(n, h2), func(n *ui.Node) bool { return n.Kind == ui.KindEffect })
	if got == nil || got.Effect.Daylight != 0 {
		t.Fatalf("night hero effect = %+v, want the nocturnal form", got)
	}
	if got.Effect.Intensity >= day.Effect.Intensity {
		t.Fatalf("night intensity %v is not quieter than day %v", got.Effect.Intensity, day.Effect.Intensity)
	}
}

func TestWeatherHeroCarriesNoStaticGlyph(t *testing.T) {
	t.Parallel()
	r := &Registry{cfg: config.Default(), reading: observedWeather()}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}
	hero := findNode(weatherTree(r, h), func(n *ui.Node) bool {
		return n.Kind == ui.KindEffect && n.Key == weatherHeroEffectKey
	})
	if hero == nil {
		t.Fatal("hero effect layer missing")
	}
	card := findNode(weatherTree(r, h), func(n *ui.Node) bool {
		return n.Kind == ui.KindStack
	})
	if card == nil {
		t.Fatal("hero stack missing")
	}
	walkNodes(card, func(n *ui.Node) {
		if n.Kind == ui.KindIcon {
			t.Fatalf("hero still paints a static glyph %q beside the effect form", n.Icon)
		}
	})
}

func TestWeatherHeroComposesByAspect(t *testing.T) {
	t.Parallel()
	m := DefaultTheme().Metrics
	reading := observedWeather()

	// The standalone panel's hero is 416x346: stacked, scene centred.
	stacked := weatherHeroCard(reading, "Lisbon", m, 416, 346+2*m.CardPadding, weatherHeroEffectKey, nil, time.Now())
	effect := findNode(stacked, func(n *ui.Node) bool { return n.Kind == ui.KindEffect })
	if effect == nil {
		t.Fatal("stacked hero has no effect")
	}
	if effect.Effect.SceneBias != 0 {
		t.Fatalf("stacked scene bias = %v, want 0 (centred)", effect.Effect.SceneBias)
	}

	// The Control Centre card is 578x318: split, scene against the leading edge.
	split := weatherHeroCard(reading, "Lisbon", m, 578, 318+2*m.CardPadding, weatherTodayEffectKey, nil, time.Now())
	effect = findNode(split, func(n *ui.Node) bool { return n.Kind == ui.KindEffect })
	if effect == nil {
		t.Fatal("split hero has no effect")
	}
	if effect.Effect.SceneBias >= 0 {
		t.Fatalf("split scene bias = %v, want negative (leading edge)", effect.Effect.SceneBias)
	}
	if err := effect.Effect.Validate(); err != nil {
		t.Fatalf("split hero effect is invalid: %v", err)
	}
}
