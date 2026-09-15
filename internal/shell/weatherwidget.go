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

// panelWeatherAction is the bar click that toggles the weather panel.
const panelWeatherAction = "panel:weather"

// weatherText renders the temperature text and the tone it should paint in.
//
// The three states are deliberately distinct. Nothing fetched yet renders the
// placeholder in the normal tone, because nothing has gone wrong. A fetch that
// has never succeeded renders an error. A reading whose fetch later began
// failing keeps its value and its normal tone, and shows its age: an aged
// temperature is information, a blank widget is not.
func weatherText(item config.Item, reading services.Reading) (string, ui.Tone) {
	if !reading.Observed {
		if reading.FailedSince.IsZero() {
			return noWorkspace, ui.ToneNormal
		}
		return "weather unavailable", ui.ToneError
	}
	text := fmt.Sprintf("%.0f%s", reading.Temperature, unitSuffix(reading.Unit))
	if item.ShowCondition {
		text += " " + render.WeatherCondition(reading.Code)
	}
	if reading.Stale() {
		text += " (" + humaniseAge(time.Since(reading.FetchedAt)) + ")"
	}
	return text, ui.ToneNormal
}

func unitSuffix(u services.Unit) string {
	if u == services.UnitFahrenheit {
		return "°F"
	}
	return "°C"
}

// buildWeatherWidget renders the weather item: the condition glyph beside the
// temperature, the weather panel as its action, and a structured tooltip that
// carries the reading's facts rather than the item's name.
func buildWeatherWidget(item config.Item, m theme.Metrics) textWidget {
	icon := &ui.Node{Kind: ui.KindIcon, Icon: "cloud", IconSize: m.IconNormal}
	text := &ui.Node{Kind: ui.KindText, Tabular: true, MaxWidth: item.MaxWidth}
	row := &ui.Node{
		Kind: ui.KindRow, Gap: theme.MarginS,
		Action: panelWeatherAction, Name: "Weather", Role: "button",
		Children: []*ui.Node{icon, text},
	}
	tip := new(*ui.Node)
	w := textWidget{node: row, tip: tip}
	var prev weatherRenderKey
	w.refresh = func(v barView) bool {
		return refreshWeatherWidget(icon, text, tip, item, v.Weather, &prev)
	}
	return w
}

// weatherRenderKey is the comparable part of a reading the widget renders.
// Daily and the optional pointers are not comparable, so refresh compares the
// fields the widget paints and rebuilds only when one moved.
type weatherRenderKey struct {
	observed      bool
	code          int
	temperature   float64
	unit          services.Unit
	showCondition bool
	fetchedAt     time.Time
	failedSince   time.Time
	isDaySet      bool
	isDay         bool
	todaySet      bool
	todayLow      float64
	todayHigh     float64
	windSet       bool
	wind          float64
	windDirSet    bool
	windDir       float64
	humiditySet   bool
	humidity      float64
}

func refreshWeatherWidget(icon, text *ui.Node, tip **ui.Node, item config.Item, reading services.Reading, prev *weatherRenderKey) bool {
	key := weatherRenderKey{
		observed:      reading.Observed,
		code:          reading.Code,
		temperature:   reading.Temperature,
		unit:          reading.Unit,
		showCondition: item.ShowCondition,
		fetchedAt:     reading.FetchedAt,
		failedSince:   reading.FailedSince,
	}
	if reading.IsDay != nil {
		key.isDaySet, key.isDay = true, *reading.IsDay
	}
	if len(reading.Daily) > 0 {
		key.todaySet, key.todayLow, key.todayHigh = true, reading.Daily[0].Low, reading.Daily[0].High
	}
	if reading.WindSpeed != nil {
		key.windSet, key.wind = true, *reading.WindSpeed
	}
	if reading.WindDirection != nil {
		key.windDirSet, key.windDir = true, *reading.WindDirection
	}
	if reading.Humidity != nil {
		key.humiditySet, key.humidity = true, *reading.Humidity
	}
	if *prev == key {
		return false
	}
	*prev = key

	isDay := reading.IsDay == nil || *reading.IsDay
	if !reading.Observed {
		icon.Icon = "cloud"
	} else {
		icon.Icon = render.WeatherIconName(reading.Code, isDay)
	}
	text.Text, text.Tone = weatherText(item, reading)
	*tip = weatherTooltipTree(reading)
	return true
}

// weatherTooltipTree is the hover surface for the bar item: the condition,
// today's range, the wind, and how old the reading is. A placeholder carries
// no facts and builds no tree; a failed fetch names the failure.
func weatherTooltipTree(reading services.Reading) *ui.Node {
	if !reading.Observed {
		if reading.FailedSince.IsZero() {
			return nil
		}
		return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "weather unavailable", Tone: ui.ToneError},
		}}
	}
	lines := []*ui.Node{
		{Kind: ui.KindText, Text: render.WeatherCondition(reading.Code)},
	}
	if len(reading.Daily) > 0 {
		lines = append(lines, &ui.Node{Kind: ui.KindText, Tabular: true, Text: weatherDayRange(reading)})
	}
	if reading.WindSpeed != nil {
		wind := fmt.Sprintf("Wind %.1f km/h", *reading.WindSpeed)
		if reading.WindDirection != nil {
			wind += " " + windCompass(*reading.WindDirection)
		}
		lines = append(lines, &ui.Node{Kind: ui.KindText, Tabular: true, Text: wind})
	}
	if reading.Humidity != nil {
		lines = append(lines, &ui.Node{Kind: ui.KindText, Tabular: true, Text: fmt.Sprintf("Humidity %.0f%%", *reading.Humidity)})
	}
	if !reading.FetchedAt.IsZero() {
		lines = append(lines, &ui.Node{Kind: ui.KindText, Tabular: true, Text: "Updated " + reading.FetchedAt.Format("15:04")})
	}
	if reading.Stale() {
		lines = append(lines, &ui.Node{Kind: ui.KindText, Text: "Age " + humaniseAge(time.Since(reading.FetchedAt))})
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: lines}
}

// windCompass renders a bearing as one of the eight named directions.
func windCompass(degrees float64) string {
	names := [...]string{"N", "NE", "E", "SE", "S", "SW", "W", "NW"}
	return names[int((degrees+22.5)/45)%8]
}

// humaniseAge renders an age at one significant unit. A bar has no room for
// "1h32m14s", and the reader only needs to know roughly how old this is.
func humaniseAge(age time.Duration) string {
	switch {
	case age >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(age.Hours())/24)
	case age >= time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	case age >= time.Minute:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	return "now"
}
