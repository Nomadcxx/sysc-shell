package shell

import (
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
	if hasValue(treeActions(tree), "weather-view:daily") || hasValue(treeActions(tree), "weather-view:hourly") {
		t.Fatal("weather surface still exposes the retired Daily/Hourly tabs")
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

func TestWeatherPanelHeroCarriesOnlyCurrentWeatherInformation(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Weather.Location = "Brisbane"
	r := &Registry{cfg: cfg, reading: observedWeather()}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}

	tree := weatherTree(r, h)
	texts := collectTooltipLines(tree)
	for _, want := range []string{"18°C", "Clear", "Brisbane", "Low 6°", "High 22°", "Feels like", "9.5°C", "Wind", "10.4 km/h NE", "Humidity", "62%", "Updated"} {
		if !hasLine(texts, want) {
			t.Fatalf("weather texts %q are missing %q", texts, want)
		}
	}
	for _, forbidden := range []string{"Temperature max", "Temperature min", "UV index", "Timezone", "Sunrise", "Sunset", "Precip chance", "Elevation"} {
		if hasLine(texts, forbidden) {
			t.Fatalf("weather texts %q still contain retired field %q", texts, forbidden)
		}
	}
	if !hasValue(treeIcons(tree), "clear-day") {
		t.Fatalf("weather icons %q are missing clear-day", treeIcons(tree))
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
	r := &Registry{reading: observedWeather()}
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

	night := observedWeather()
	falseValue := false
	night.IsDay = &falseValue
	n := &Registry{reading: night}
	h2 := &PanelHost{id: PanelWeather, theme: DefaultTheme()}
	var nightTone ui.Tone
	var nightFound bool
	var walk2 func(n *ui.Node)
	walk2 = func(n *ui.Node) {
		if !nightFound && n.Kind == ui.KindIcon && n.Icon == "clear-night" {
			nightTone, nightFound = n.Tone, true
		}
		for _, c := range n.Children {
			walk2(c)
		}
	}
	walk2(weatherTree(n, h2))
	if !nightFound || nightTone != ui.ToneNormal {
		t.Fatalf("night hero tone = %v (found %v), want normal", nightTone, nightFound)
	}
}
