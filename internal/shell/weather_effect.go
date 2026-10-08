package shell

import (
	"math"
	"time"

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
		Daylight:  weatherDaylight(reading, time.Now()),
		Seed:      weatherEffectSeed,
		Intensity: intensity,
		Speed:     speed,
	}
	if err := spec.Validate(); err != nil {
		return ui.EffectSpec{}, false
	}
	return spec, true
}

// weatherTwilight is the half-width of the dawn and dusk blend around
// sunrise and sunset.
const weatherTwilight = 20 * time.Minute

// weatherDaylight is how far into day the sky is at now: 0 at night, 1 in
// full day, smoothly between across sunrise and sunset +/- weatherTwilight.
// Sunrise and sunset are local wall times in the reading's timezone. Any
// missing piece falls back to the reading's is_day flag, as before.
func weatherDaylight(reading services.Reading, now time.Time) float64 {
	fallback := 1.0
	if reading.IsDay != nil && !*reading.IsDay {
		fallback = 0
	}
	if reading.Timezone == nil {
		return fallback
	}
	loc, err := time.LoadLocation(*reading.Timezone)
	if err != nil {
		return fallback
	}
	local := now.In(loc)
	date := local.Format("2006-01-02")
	for _, d := range reading.Daily {
		if d.Date != date {
			continue
		}
		rise, errRise := time.ParseInLocation("2006-01-02T15:04", d.Sunrise, loc)
		set, errSet := time.ParseInLocation("2006-01-02T15:04", d.Sunset, loc)
		if errRise != nil || errSet != nil || !set.After(rise) {
			return fallback
		}
		up := smoothAround(local, rise)
		down := smoothAround(local, set)
		return math.Min(up, 1-down)
	}
	return fallback
}

// smoothAround is 0 well before edge, 1 well after it, and a smoothstep
// across edge +/- weatherTwilight.
func smoothAround(t, edge time.Time) float64 {
	span := float64(2 * weatherTwilight)
	x := (float64(t.Sub(edge)) + float64(weatherTwilight)) / span
	x = math.Max(0, math.Min(1, x))
	return x * x * (3 - 2*x)
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
// capsule while layering the effect, scrim, and existing foreground in its
// content box. The current card grammar has one foreground child; malformed
// cards are left untouched rather than losing content.
func weatherCardWithEffect(card *ui.Node, reading services.Reading, key string, bias float64) *ui.Node {
	if card == nil || len(card.Children) != 1 || card.Children[0] == nil {
		return card
	}
	effect := weatherEffectNode(reading, key, bias)
	if effect == nil {
		return card
	}
	effect.Shape = card.Shape
	card.Children = []*ui.Node{{
		Kind: ui.KindStack,
		Children: []*ui.Node{
			effect,
			{Kind: ui.KindCapsule, Fill: ui.FillScrim, Shape: card.Shape},
			card.Children[0],
		},
	}}
	return card
}
