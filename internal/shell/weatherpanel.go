package shell

import (
	"fmt"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// weatherTree builds PanelWeather from one dominant hero and a compact
// four-day strip. Every read here is cached; this runs under Registry.mu and
// on the Wayland owner, where a fetch would stall every bar on the machine.
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
	bodyH := max(panelH-2*m.PanelPadding-weatherHeaderHeight(m)-theme.MarginL, 0)
	panelW := panelTargetSize(PanelWeather).W
	if h.place.Panel.W > 0 {
		panelW = h.place.Panel.W
	}
	bodyW := max(panelW-2*m.PanelPadding, 0)
	forecastH := weatherForecastHeight(m)
	heroH := max(bodyH-theme.MarginL-forecastH, 0)
	hero := weatherHero(reading, location, m)
	hero.Height = heroH

	children := []*ui.Node{
		weatherHeader(m),
		{Kind: ui.KindColumn, Gap: theme.MarginL, Height: bodyH, Children: []*ui.Node{
			hero,
			weatherForecastStrip(reading, m, bodyW, forecastH),
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

// weatherHero is the standalone hero. The sized form is shared with the
// Control Centre Today card so both surfaces keep the same information budget.
func weatherHero(reading services.Reading, location string, m theme.Metrics) *ui.Node {
	panelW := panelTargetSize(PanelWeather).W
	contentW := max(panelW-2*m.PanelPadding-2*m.CardPadding, 0)
	return weatherHeroCard(reading, location, m, contentW, 0, weatherHeroEffectKey)
}

func weatherHeroCard(reading services.Reading, location string, m theme.Metrics, contentW, height int, effectKey string) *ui.Node {
	if !reading.Observed {
		text, tone := "-", ui.ToneNormal
		if !reading.FailedSince.IsZero() {
			text, tone = "weather unavailable", ui.ToneError
		}
		card := &ui.Node{
			Kind: ui.KindCapsule, Padding: m.CardPadding,
			Height: height,
			Fill:   ui.FillContainerHigh, Shape: ui.ShapeCard,
			Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginM, Children: []*ui.Node{
				{Kind: ui.KindText, Text: text, Tone: tone},
			}}},
		}
		return card
	}

	isDay := reading.IsDay == nil || *reading.IsDay
	heroTone := ui.ToneAccent
	if !isDay {
		heroTone = ui.ToneNormal
	}
	headline := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginL, Height: m.IconHero, Children: []*ui.Node{
		{Kind: ui.KindIcon, Icon: render.WeatherIconName(reading.Code, isDay), IconSize: m.IconHero, Tone: heroTone},
		{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: fmt.Sprintf("%.0f%s", reading.Temperature, unitSuffix(reading.Unit)), TextRole: theme.RoleHeadline, Tabular: true},
			{Kind: ui.KindText, Text: render.WeatherCondition(reading.Code), TextRole: theme.RoleLabel},
		}},
	}}
	lines := []*ui.Node{
		{Kind: ui.KindText, TextRole: theme.RoleCaption, Text: location},
		headline,
		{Kind: ui.KindText, TextRole: theme.RoleLabel, Tabular: true, Tone: ui.ToneAccent, Text: weatherDayRange(reading)},
		weatherEssentials(reading, m, contentW),
		{Kind: ui.KindText, TextRole: theme.RoleCaption, Tabular: true, Text: weatherFreshness(reading)},
	}
	card := &ui.Node{
		Kind: ui.KindCapsule, Padding: m.CardPadding,
		Height: height,
		Fill:   ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginM, Children: lines}},
	}
	return weatherCardWithEffect(card, reading, effectKey)
}

func weatherEssentials(reading services.Reading, m theme.Metrics, contentW int) *ui.Node {
	cellW := max((contentW-2*theme.MarginS)/3, 0)
	return &ui.Node{Kind: ui.KindRow, Height: max(m.StandardControl, 2*m.IconSmall+theme.MarginXXS), Gap: theme.MarginS, Children: []*ui.Node{
		weatherEssentialCell(m, cellW, "Feels like", weatherOptional(reading.Apparent, func(v float64) string {
			return fmt.Sprintf("%.1f%s", v, unitSuffix(reading.Unit))
		})),
		weatherEssentialCell(m, cellW, "Wind", weatherWind(reading)),
		weatherEssentialCell(m, cellW, "Humidity", weatherOptional(reading.Humidity, func(v float64) string {
			return fmt.Sprintf("%.0f%%", v)
		})),
	}}
}

func weatherEssentialCell(m theme.Metrics, width int, label, value string) *ui.Node {
	return &ui.Node{Kind: ui.KindColumn, Width: width, Gap: theme.MarginXXS, Children: []*ui.Node{
		{Kind: ui.KindText, Text: label, TextRole: theme.RoleCaption},
		{Kind: ui.KindText, Text: value, TextRole: theme.RoleBody, Tabular: true},
	}}
}

func weatherFreshness(reading services.Reading) string {
	if reading.FetchedAt.IsZero() {
		return absent
	}
	if reading.Stale() {
		return "Stale · Age " + humaniseAge(time.Since(reading.FetchedAt))
	}
	return "Updated " + reading.FetchedAt.Format("15:04")
}

func weatherForecastHeight(m theme.Metrics) int {
	return m.IconLarge + m.StandardControl + 2*m.CardPadding + 2*theme.MarginS
}

func weatherForecastStrip(reading services.Reading, m theme.Metrics, width, height int) *ui.Node {
	const forecastSlots = 4
	slotW := max((width-(forecastSlots-1)*theme.MarginM)/forecastSlots, 0)
	strip := &ui.Node{Kind: ui.KindRow, Height: height, Gap: theme.MarginM}
	for i := 0; i < forecastSlots; i++ {
		var day *services.Day
		if i+1 < len(reading.Daily) {
			day = &reading.Daily[i+1]
		}
		strip.Children = append(strip.Children, weatherForecastSlot(m, slotW, height, day, reading.Unit))
	}
	return strip
}

func weatherForecastSlot(m theme.Metrics, width, height int, day *services.Day, unit services.Unit) *ui.Node {
	label, icon, temperature := absent, "cloud", absent
	if day != nil {
		label = day.Date
		if parsed, err := time.Parse("2006-01-02", day.Date); err == nil {
			label = parsed.Format("Mon")
		}
		icon = render.WeatherIconName(day.Code, true)
		temperature = fmt.Sprintf("%.0f%s / %.0f%s", day.High, unitSuffix(unit), day.Low, unitSuffix(unit))
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Width: width, Height: height,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: label, TextRole: theme.RoleLabel},
			{Kind: ui.KindIcon, Icon: icon, IconSize: m.IconLarge, Tone: ui.ToneAccent},
			{Kind: ui.KindText, Text: temperature, TextRole: theme.RoleCaption, Tabular: true},
		}}},
	}
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

// weatherOptional renders a present figure through format and an absent one
// as the dash, so a missing field keeps its row's space.
func weatherOptional(value *float64, format func(float64) string) string {
	if value == nil {
		return absent
	}
	return format(*value)
}
