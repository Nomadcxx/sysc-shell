package shell

import (
	"fmt"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// weatherTree builds PanelWeather: the header over a two-column body, the
// reference's arrangement. The hero and details cards share the left column;
// the day list scrolls on the right. Every read here is a cached one; this
// runs under Registry.mu and on the Wayland owner, where a fetch would stall
// every bar on the machine.
func weatherTree(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	reading := services.Reading{}
	location := ""
	if r != nil {
		reading = r.reading
		location = weatherLocation(r.cfg.Weather, reading)
	}

	panelH := h.place.Panel.H
	if panelH <= 0 {
		panelH = panelTargetSize(PanelWeather).H
	}
	if h.weatherView == "" {
		h.weatherView = "daily"
	}
	bodyH := max(panelH-2*m.PanelPadding-weatherHeaderHeight(m)-theme.MarginL, 0)
	bodyW := panelTargetSize(PanelWeather).W - 2*m.PanelPadding
	leftW := max((bodyW-theme.MarginL)*2/5, 0)
	rightW := max(bodyW-theme.MarginL-leftW, 0)

	children := []*ui.Node{
		weatherHeader(m),
		{Kind: ui.KindRow, Gap: theme.MarginL, Height: bodyH, Children: []*ui.Node{
			{Kind: ui.KindColumn, Width: leftW, Gap: theme.MarginL, Children: []*ui.Node{
				weatherHero(reading, location, m),
				weatherDetails(reading, m),
			}},
			{Kind: ui.KindColumn, Width: rightW, Children: []*ui.Node{
				weatherViewTabs(h, m),
				{Kind: ui.KindScroll, Height: max(bodyH-m.StandardControl-theme.MarginM, 0), Children: []*ui.Node{
					weatherForecastList(reading, h, m),
				}},
			}},
		}},
	}
	if h.errLabel != "" {
		children = append(children, &ui.Node{
			Kind: ui.KindText, Text: h.errLabel, Tone: ui.ToneError,
			TextRole: theme.RoleCaption,
		})
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginL, Padding: m.PanelPadding, Children: children}
}

// weatherLocation names the place the reading describes: the configured
// label wins, then the geocoded city name, then the coordinates an
// unconfigured block falls back to.
func weatherLocation(w config.Weather, reading services.Reading) string {
	if w.Location != "" {
		return w.Location
	}
	if reading.Location != "" {
		return reading.Location
	}
	if w.Configured {
		if w.City != "" {
			return w.City
		}
		return fmt.Sprintf("%.2f°, %.2f°", w.Latitude, w.Longitude)
	}
	return absent
}

// weatherHeaderHeight is the header capsule's height, named so the body can
// size against it without guessing.
func weatherHeaderHeight(m theme.Metrics) int {
	return m.StandardControl + 2*m.CardPadding
}

func weatherHeader(m theme.Metrics) *ui.Node {
	well := m.StandardControl
	title := &ui.Node{
		Kind: ui.KindText, Text: "Weather",
		TextRole: theme.RoleTitle, Name: "Weather", Role: "heading",
	}
	close := &ui.Node{
		Kind: ui.KindButton, Action: "weather-close", Name: "Close", Role: "button",
		Focusable: true, Width: m.CompactControl, Height: m.CompactControl, Shape: ui.ShapeCircle,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "close", IconSize: m.IconNormal}},
	}
	top := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginL, Height: well, PinEnd: true, Children: []*ui.Node{title, close}}
	return &ui.Node{
		Kind: ui.KindCapsule, Padding: m.CardPadding, Height: weatherHeaderHeight(m),
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{top},
	}
}

// weatherDayRange is today's low/high line, or the dash before a body.
func weatherDayRange(reading services.Reading) string {
	if len(reading.Daily) == 0 {
		return absent
	}
	today := reading.Daily[0]
	suffix := unitSuffix(reading.Unit)
	return fmt.Sprintf("Low %.0f%s  High %.0f%s", today.Low, suffix, today.High, suffix)
}

// weatherHero is the headline card: the condition glyph beside the
// temperature, then the day's range, the place, and how fresh the reading is.
func weatherHero(reading services.Reading, location string, m theme.Metrics) *ui.Node {
	if !reading.Observed {
		text, tone := "-", ui.ToneNormal
		if !reading.FailedSince.IsZero() {
			text, tone = "weather unavailable", ui.ToneError
		}
		return &ui.Node{
			Kind: ui.KindCapsule, Padding: m.CardPadding,
			Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
			Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginM, Children: []*ui.Node{
				{Kind: ui.KindText, Text: text, Tone: tone},
			}}},
		}
	}

	isDay := reading.IsDay == nil || *reading.IsDay
	heroTone := ui.ToneAccent
	if !isDay {
		heroTone = ui.ToneNormal
	}
	headline := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginL, Height: m.StandardControl, Children: []*ui.Node{
		{Kind: ui.KindIcon, Icon: render.WeatherIconName(reading.Code, isDay), IconSize: m.IconHero, Tone: heroTone},
		{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: fmt.Sprintf("%.0f%s", reading.Temperature, unitSuffix(reading.Unit)), TextRole: theme.RoleTitle, Tabular: true},
			{Kind: ui.KindText, Text: render.WeatherCondition(reading.Code), TextRole: theme.RoleLabel},
		}},
	}}
	lines := []*ui.Node{headline}
	if len(reading.Daily) > 0 {
		lines = append(lines, &ui.Node{Kind: ui.KindText, TextRole: theme.RoleLabel, Tabular: true, Tone: ui.ToneAccent,
			Text: weatherDayRange(reading)})
	}
	lines = append(lines, &ui.Node{Kind: ui.KindText, TextRole: theme.RoleCaption, Text: location})
	if !reading.FetchedAt.IsZero() {
		lines = append(lines, &ui.Node{Kind: ui.KindText, TextRole: theme.RoleCaption, Tabular: true,
			Text: "Updated " + reading.FetchedAt.Format("15:04")})
	}
	if reading.Stale() {
		lines = append(lines, &ui.Node{Kind: ui.KindText, TextRole: theme.RoleCaption,
			Text: "Age " + humaniseAge(time.Since(reading.FetchedAt))})
	}
	card := &ui.Node{
		Kind: ui.KindCapsule, Padding: m.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginM, Children: lines}},
	}
	return weatherCardWithEffect(card, reading, weatherHeroEffectKey)
}

// weatherDetails is the labelled figure card under the hero: the reference's
// row set and order first, then the shell's extra figures, each value pinned
// right and painted in the accent tone the reference uses.
func weatherDetails(reading services.Reading, m theme.Metrics) *ui.Node {
	var today services.Day
	if len(reading.Daily) > 0 {
		today = reading.Daily[0]
	}
	rows := []*ui.Node{
		weatherRow(m, "thermometer", "Temperature max", weatherDayTemp(reading, today.High)),
		weatherRow(m, "thermometer", "Temperature min", weatherDayTemp(reading, today.Low)),
		weatherRow(m, "wind", "Wind", weatherWind(reading)),
		weatherRow(m, "sunrise", "Sunrise", weatherClock(today.Sunrise)),
		weatherRow(m, "sunset", "Sunset", weatherClock(today.Sunset)),
		weatherRow(m, "elevation", "Elevation", weatherOptional(reading.Elevation, func(v float64) string {
			return fmt.Sprintf("%.0f m", v)
		})),
		weatherRow(m, "clear-day", "UV index", weatherOptional(reading.UVIndex, func(v float64) string {
			return fmt.Sprintf("%.1f", v)
		})),
		weatherRow(m, "schedule", "Timezone", weatherTimezone(reading)),
		weatherRow(m, "thermometer", "Feels like", weatherOptional(reading.Apparent, func(v float64) string {
			return fmt.Sprintf("%.1f%s", v, unitSuffix(reading.Unit))
		})),
		weatherRow(m, "humidity", "Humidity", weatherOptional(reading.Humidity, func(v float64) string {
			return fmt.Sprintf("%.0f%%", v)
		})),
		weatherRow(m, "humidity", "Precip chance", weatherOptional(today.PrecipitationProbability, func(v float64) string {
			return fmt.Sprintf("%.0f%%", v)
		})),
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Padding: m.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: rows}},
	}
}

// weatherRowHeight keeps a detail row compact: the row is a label, not a
// control, so it sizes to its glyph rather than to a control rung.
func weatherRowHeight(m theme.Metrics) int {
	return m.IconSmall + 2*theme.MarginS
}

func weatherRow(m theme.Metrics, icon, label, value string) *ui.Node {
	leading := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{
		{Kind: ui.KindIcon, Icon: icon, IconSize: m.IconSmall},
		{Kind: ui.KindText, Text: label, TextRole: theme.RoleLabel},
	}}
	return &ui.Node{Kind: ui.KindRow, Height: weatherRowHeight(m), Gap: theme.MarginM, PinEnd: true, Children: []*ui.Node{
		leading,
		{Kind: ui.KindText, Text: value, TextRole: theme.RoleBody, Tabular: true, Tone: ui.ToneAccent},
	}}
}

// weatherDayTemp renders a today figure, or the dash before a body.
func weatherDayTemp(reading services.Reading, value float64) string {
	if len(reading.Daily) == 0 {
		return absent
	}
	return fmt.Sprintf("%.0f%s", value, unitSuffix(reading.Unit))
}

// weatherIsToday reports whether a daily date is the machine's current day;
// the forecast starts at the location's midnight, which is the machine's
// whenever the owner is in the configured place.
func weatherIsToday(date string) bool {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return false
	}
	today := time.Now()
	return parsed.Year() == today.Year() && parsed.YearDay() == today.YearDay()
}

// weatherViewTabs is the Daily/Hourly segmented control, the component the
// network panel's tabs use.
func weatherViewTabs(h *PanelHost, m theme.Metrics) *ui.Node {
	view := h.weatherView
	return &ui.Node{
		Kind: ui.KindSegmented, Key: "weather-view", Gap: theme.MarginXXS, Height: m.StandardControl,
		Children: []*ui.Node{
			weatherSegment(m, "weather-view:daily", "Daily", view != "hourly"),
			weatherSegment(m, "weather-view:hourly", "Hourly", view == "hourly"),
		},
	}
}

func weatherSegment(m theme.Metrics, action, label string, selected bool) *ui.Node {
	n := &ui.Node{
		Kind: ui.KindButton, Action: action, Name: label, Role: "tab",
		Focusable: true, Height: m.CompactControl,
		Children: []*ui.Node{{Kind: ui.KindText, Text: label}},
	}
	if selected {
		n.State |= ui.StateSelected
	}
	return n
}

// weatherForecastList renders whichever view the segmented control selected.
func weatherForecastList(reading services.Reading, h *PanelHost, m theme.Metrics) *ui.Node {
	if h.weatherView == "hourly" {
		return weatherHourlyList(reading, m)
	}
	return weatherForecast(reading, m)
}

// weatherHourlyList is the hourly view: the hours the wire carried, each with
// its glyph, clock label, temperature and condition summary, in the day
// list's row grammar.
func weatherHourlyList(reading services.Reading, m theme.Metrics) *ui.Node {
	if len(reading.Hourly) == 0 {
		return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "No hourly forecast yet", TextRole: theme.RoleLabel},
		}}
	}
	// The reference fixes the hourly view at seven rows; the wire may carry a
	// full 168-hour window, but the panel must stay a compact forecast surface.
	const weatherHourlyRowLimit = 7
	rowCount := min(len(reading.Hourly), weatherHourlyRowLimit)
	rows := make([]*ui.Node, 0, rowCount)
	for _, hour := range reading.Hourly[:rowCount] {
		rows = append(rows, weatherHourRow(m, hour, reading.Unit))
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Padding: m.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: rows}},
	}
}

func weatherHourRow(m theme.Metrics, hour services.Hour, unit services.Unit) *ui.Node {
	isDay := hour.IsDay == nil || *hour.IsDay
	hourTone := ui.ToneAccent
	if !isDay {
		hourTone = ui.ToneNormal
	}
	summary := render.WeatherCondition(hour.Code)
	if hour.PrecipProbability != nil {
		summary = fmt.Sprintf("%s, %.0f%%", summary, *hour.PrecipProbability)
	}
	return &ui.Node{Kind: ui.KindRow, Height: m.CompactControl, Gap: theme.MarginM, Children: []*ui.Node{
		{Kind: ui.KindIcon, Icon: render.WeatherIconName(hour.Code, isDay), IconSize: m.IconSmall, Tone: hourTone},
		{Kind: ui.KindText, Text: weatherClock(hour.Time), TextRole: theme.RoleLabel, Tabular: true},
		{Kind: ui.KindText, Text: fmt.Sprintf("%.0f%s", hour.Temperature, unitSuffix(unit)), TextRole: theme.RoleBody, Tabular: true},
		{Kind: ui.KindText, Text: summary, TextRole: theme.RoleCaption, PinEnd: true},
	}}
}

// weatherForecast is the day list on the right: today excluded — the hero
// owns today, as the reference renders it — then the days the body carried,
// each with its glyph, high/low range and condition word.
func weatherForecast(reading services.Reading, m theme.Metrics) *ui.Node {
	if len(reading.Daily) == 0 {
		return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "No forecast yet", TextRole: theme.RoleLabel},
		}}
	}
	rows := make([]*ui.Node, 0, len(reading.Daily))
	for _, day := range reading.Daily {
		if weatherIsToday(day.Date) {
			continue
		}
		rows = append(rows, weatherDayRow(m, day, reading.Unit))
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Padding: m.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: rows}},
	}
}

func weatherDayRow(m theme.Metrics, day services.Day, unit services.Unit) *ui.Node {
	label := day.Date
	if parsed, err := time.Parse("2006-01-02", day.Date); err == nil {
		label = parsed.Format("Mon")
	}
	return &ui.Node{Kind: ui.KindRow, Height: m.CompactControl, Gap: theme.MarginM, Children: []*ui.Node{
		{Kind: ui.KindIcon, Icon: render.WeatherIconName(day.Code, true), IconSize: m.IconSmall, Tone: ui.ToneAccent},
		{Kind: ui.KindText, Text: label, TextRole: theme.RoleLabel},
		{Kind: ui.KindText, Text: fmt.Sprintf("%.0f%s / %.0f%s", day.High, unitSuffix(unit), day.Low, unitSuffix(unit)), TextRole: theme.RoleBody, Tabular: true},
		{Kind: ui.KindText, Text: render.WeatherCondition(day.Code), TextRole: theme.RoleCaption, PinEnd: true},
	}}
}

func weatherWind(reading services.Reading) string {
	if reading.WindSpeed == nil {
		return absent
	}
	wind := fmt.Sprintf("%.1f km/h", *reading.WindSpeed)
	if reading.WindDirection != nil {
		wind += " " + windCompass(*reading.WindDirection)
	}
	return wind
}

func weatherTimezone(reading services.Reading) string {
	if reading.Timezone == nil {
		return absent
	}
	if reading.TimezoneAbbreviation == nil {
		return *reading.Timezone
	}
	return *reading.Timezone + " (" + *reading.TimezoneAbbreviation + ")"
}

// weatherOptional renders a present figure through format and an absent one
// as the dash, so a missing field keeps its row's space.
func weatherOptional(value *float64, format func(float64) string) string {
	if value == nil {
		return absent
	}
	return format(*value)
}

// weatherClock trims an ISO instant to its local clock time.
func weatherClock(iso string) string {
	if i := strings.LastIndex(iso, "T"); i >= 0 && i+1 < len(iso) {
		return iso[i+1:]
	}
	if iso == "" {
		return absent
	}
	return iso
}
