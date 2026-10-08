package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	weatherHeroEffectKey  = "weather:hero"
	weatherTodayEffectKey = "weather:today"
	weatherHomeEffectKey  = "weather:home"
	// A fixed seed keeps the particle field stable when a reading is refreshed.
	weatherEffectSeed uint64 = 0x534f4d455f574541
)

// weatherEffectSpec maps the existing weather icon vocabulary to the host's
// effect catalogue. The icon name includes day/night state, so the mapper
// cannot silently grow a second WMO table.
func weatherEffectSpec(reading services.Reading) (ui.EffectSpec, bool) {
	if !reading.Observed {
		return ui.EffectSpec{}, false
	}

	isDay := reading.IsDay == nil || *reading.IsDay
	icon := render.WeatherIconName(reading.Code, isDay)
	variant := ui.WeatherCloudy
	switch icon {
	case "clear-day", "clear-night":
		variant = ui.WeatherClear
	case "partly-cloudy", "partly-cloudy-night":
		variant = ui.WeatherPartlyCloudy
	case "cloud":
		variant = ui.WeatherCloudy
	case "fog":
		variant = ui.WeatherFog
	case "rain":
		variant = ui.WeatherRain
	case "snow":
		variant = ui.WeatherSnow
	case "heavy-snow":
		variant = ui.WeatherHeavySnow
	case "thunderstorm":
		variant = ui.WeatherThunderstorm
	}

	intensity, speed := 0.72, 1.0
	if !isDay {
		intensity, speed = 0.56, 0.8
	}
	spec := ui.EffectSpec{
		Program:   ui.EffectWeather,
		Variant:   variant,
		Night:     !isDay,
		Seed:      weatherEffectSeed,
		Intensity: intensity,
		Speed:     speed,
	}
	if err := spec.Validate(); err != nil {
		return ui.EffectSpec{}, false
	}
	return spec, true
}

func weatherEffectNode(reading services.Reading, key string, bias float64) *ui.Node {
	spec, ok := weatherEffectSpec(reading)
	if !ok {
		return nil
	}
	spec.SceneBias = bias
	if err := spec.Validate(); err != nil {
		return nil
	}
	return &ui.Node{Kind: ui.KindEffect, Key: key, Shape: ui.ShapeCard, Effect: spec}
}

// weatherCardWithEffect keeps card chrome and accessibility on the original
// capsule and makes the effect the card itself: the capsule's padding moves
// onto the foreground, so the scene fills the card edge to edge while the
// text keeps its inset. The current card grammar has one foreground child;
// malformed cards are left untouched rather than losing content.
func weatherCardWithEffect(card *ui.Node, reading services.Reading, key string, bias float64) *ui.Node {
	if card == nil || len(card.Children) != 1 || card.Children[0] == nil {
		return card
	}
	effect := weatherEffectNode(reading, key, bias)
	if effect == nil {
		return card
	}
	effect.Shape = card.Shape
	foreground := card.Children[0]
	foreground.Padding += card.Padding
	card.Padding = 0
	card.Children = []*ui.Node{{
		Kind: ui.KindStack,
		Children: []*ui.Node{
			effect,
			{Kind: ui.KindCapsule, Fill: weatherScrimFill(bias), Shape: card.Shape},
			foreground,
		},
	}}
	return card
}

// weatherScrimFill darkens the side the text is on. The scene sits toward
// the bias end, so the text is at the other; a centred scene has the text
// beneath it and keeps the flat scrim.
func weatherScrimFill(bias float64) ui.Fill {
	switch {
	case bias > 0:
		return ui.FillScrimFadeLeading
	case bias < 0:
		return ui.FillScrimFadeTrailing
	default:
		return ui.FillScrim
	}
}
