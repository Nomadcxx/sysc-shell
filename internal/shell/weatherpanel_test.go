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

	for _, want := range []string{"Feels like", "9.5°C", "Wind", "10.4 km/h NE", "Humidity", "62%", "UV index", "0.0", "Precip chance", "10%", "Sunrise", "06:12", "Sunset", "18:44", "Elevation", "64 m", "Timezone", "Australia/Sydney"} {
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
		reading.Daily = append(reading.Daily, services.Day{
			Date: fmt.Sprintf("2026-09-%02d", 15+i), Code: code,
			High: 20 + float64(i), Low: 8 + float64(i),
			Sunrise: fmt.Sprintf("2026-09-%02dT%s", 15+i, hours[i]),
			Sunset:  fmt.Sprintf("2026-09-%02dT18:30", 15+i),
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

	if !hasLine(texts, "Today") {
		t.Fatalf("forecast texts %q do not mark today", texts)
	}
	for _, want := range []string{"Wed", "Thu", "9° / 21°C", "Clear", "Rain", "Snow", "Thunderstorm"} {
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
	if !hasLine(texts, "Today") || !hasLine(texts, "Wed") {
		t.Fatalf("a two-day body rendered %q, want today and one more day", texts)
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
