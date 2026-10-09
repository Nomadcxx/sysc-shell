package render

import (
	"image"
	"math"
	"strconv"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// weatherThemePull mixes every sky colour toward the panel's container so a
// realistic sky still sits inside the themed card (owner decision 2026-10-08).
const weatherThemePull = .15

var (
	weatherWarm = weatherHex("#f2a66b")
	weatherRose = weatherHex("#c7798c")
)

// weatherSkyTable is the design's D6 table: {day, night} x {top, 60%, bottom}.
var weatherSkyTable = map[weatherKind][2][3]string{
	weatherClear:        {{"#2a63cf", "#7fb6f2", "#cfe6ff"}, {"#050925", "#121c4a", "#2a3772"}},
	weatherPartlyCloudy: {{"#3a6fc9", "#8dbbea", "#d6e8fa"}, {"#070b26", "#151f4a", "#2b3568"}},
	weatherCloudy:       {{"#5f7290", "#93a5bd", "#c4cfdc"}, {"#0d111d", "#1d2333", "#30384c"}},
	weatherFog:          {{"#8c97a6", "#b3bcc6", "#d3d8dd"}, {"#1b2029", "#2c323d", "#434a56"}},
	weatherRain:         {{"#36445a", "#5e6c82", "#8794a6"}, {"#090d17", "#151b29", "#262e40"}},
	weatherSnow:         {{"#7d93b0", "#aebed2", "#e0e8f1"}, {"#141b2e", "#26314a", "#3d4a66"}},
	weatherHeavySnow:    {{"#6f8099", "#9aa9bc", "#cfd8e2"}, {"#121828", "#222b40", "#36425a"}},
	weatherThunderstorm: {{"#1c2230", "#363e52", "#59627a"}, {"#06080e", "#121622", "#232a3b"}},
}

// weatherHex parses an opaque #rrggbb literal from the tables above.
func weatherHex(s string) Color {
	if len(s) != 7 || s[0] != '#' {
		return Color{}
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return Color{}
	}
	return Color{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}
}

func weatherPull(c Color, style Style) Color {
	if style.Container.A == 0 {
		return c
	}
	return LerpColor(c, style.Container, weatherThemePull)
}

// weatherBand is the twilight weight: 1 at Daylight 0.5, 0 at night and day.
func weatherBand(daylight float64) float64 {
	return math.Max(0, 1-math.Abs(2*clampEffect(daylight, 0, 1)-1))
}

// weatherGlow is how strongly a condition shows the dawn and dusk colours.
func weatherGlow(kind weatherKind) float64 {
	switch kind {
	case weatherClear, weatherPartlyCloudy:
		return .8
	case weatherCloudy, weatherFog, weatherSnow:
		return .4
	}
	return .25
}

func weatherSkyStops(kind weatherKind, daylight float64, style Style) [3]Color {
	row := weatherSkyTable[kind]
	t := weatherSmoothstep(0, 1, daylight)
	glow := weatherBand(daylight) * weatherGlow(kind)
	var out [3]Color
	for i := range out {
		out[i] = LerpColor(weatherHex(row[1][i]), weatherHex(row[0][i]), t)
	}
	out[2] = LerpColor(out[2], weatherWarm, glow)
	out[1] = LerpColor(out[1], weatherRose, glow*.45)
	for i := range out {
		out[i] = weatherPull(out[i], style)
	}
	return out
}

// paintWeatherSky fills the box with the three-stop gradient, one colour per
// row. It reads no phase: the sky is static, and the forms carry the motion.
func paintWeatherSky(c *Canvas, box ui.Rect, mask *image.Alpha, stops [3]Color) {
	x0, y0, x1, y1 := c.clip(box)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	for y := y0; y < y1; y++ {
		v := (float64(y-box.Y) + .5) / float64(box.H)
		var col Color
		if v < .6 {
			col = LerpColor(stops[0], stops[1], v/.6)
		} else {
			col = LerpColor(stops[1], stops[2], (v-.6)/.4)
		}
		blendWeatherRow(c, box, mask, y, x0, x1, col)
	}
}

const weatherStarCount = 70

// paintWeatherStars draws twinkling points on clear and partly cloudy nights.
// They fade out by Daylight 0.6.
func paintWeatherStars(c *Canvas, box ui.Rect, mask *image.Alpha, spec ui.EffectSpec, phase float64) {
	if spec.Variant != ui.WeatherClear && spec.Variant != ui.WeatherPartlyCloudy || spec.Daylight >= .6 {
		return
	}
	white := Color{R: 255, G: 255, B: 255, A: 255}
	fade := math.Pow(1-spec.Daylight/.6, 1.5)
	twinkleLoop := weatherLoop(phase, spec.Speed, 2)
	for i := uint64(0); i < weatherStarCount; i++ {
		x := int(weatherUnit(spec.Seed, i, 11) * float64(box.W))
		y := int(weatherUnit(spec.Seed, i, 12) * float64(box.H) * .7)
		r := weatherUnit(spec.Seed, i, 13)
		twinkle := .45 + .55*math.Pow(math.Sin(2*math.Pi*(twinkleLoop+weatherUnit(spec.Seed, i, 14))), 2)
		alpha := weatherAlpha(255, (.25+r*.6)*twinkle*fade)
		localWeatherPixel(c, box, mask, x, y, white, alpha, 1)
		if r > .9 {
			localWeatherPixel(c, box, mask, x+1, y, white, alpha/2, 1)
		}
	}
}

// weatherLoop is phase advanced k whole cycles per loop, so the effect's loop
// seam is invisible. Speed 0 parks everything at zero.
func weatherLoop(phase, speed float64, k int) float64 {
	if speed <= 0 || math.IsNaN(phase) || math.IsInf(phase, 0) {
		return 0
	}
	return positiveMod(positiveMod(phase, 1)*float64(max(k, 1)), 1)
}
