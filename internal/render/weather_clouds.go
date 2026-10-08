package render

import (
	"image"
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// weatherCloudTable holds the cloud palettes: family -> {day, night} -> {lit, shade}.
var weatherCloudTable = map[string][2][2]string{
	"lit":   {{"#ffffff", "#a8b6cd"}, {"#96a0c4", "#282e48"}},
	"grey":  {{"#d6dce6", "#788498"}, {"#5c647c", "#1e2230"}},
	"storm": {{"#788092", "#2e3442"}, {"#464c60", "#0e1018"}},
}

func weatherCloudFamily(kind weatherKind) string {
	switch kind {
	case weatherThunderstorm:
		return "storm"
	case weatherRain, weatherCloudy, weatherHeavySnow:
		return "grey"
	}
	return "lit"
}

// weatherCloudColours is the lit and shaded cloud colour for a condition and
// time of day. Daylight is quantised to tenths because the colours key the
// cloud sprites: a continuous value would rebake through every dusk.
func weatherCloudColours(kind weatherKind, daylight float64, style Style) (lit, shade Color) {
	d := math.Round(clampEffect(daylight, 0, 1)*10) / 10
	row := weatherCloudTable[weatherCloudFamily(kind)]
	lit = LerpColor(weatherHex(row[1][0]), weatherHex(row[0][0]), d)
	shade = LerpColor(weatherHex(row[1][1]), weatherHex(row[0][1]), d)
	band := weatherBand(d)
	lit = LerpColor(lit, weatherHex("#ffc09c"), band*.6)
	shade = LerpColor(shade, weatherHex("#966c8c"), band*.4)
	return weatherPull(lit, style), weatherPull(shade, style)
}

// weatherCloudCount is the catalogue's fixed cloud budget per variant.
func weatherCloudCount(kind weatherKind) int {
	switch kind {
	case weatherPartlyCloudy:
		return 4
	case weatherCloudy, weatherHeavySnow, weatherThunderstorm:
		return 7
	case weatherRain:
		return 6
	case weatherSnow:
		return 5
	case weatherFog:
		return 2
	}
	return 0
}

// weatherCloudSprite bakes one textured cloud: an fbm-eroded mass with a flat
// base, lit from above and shaded below.
func weatherCloudSprite(seed uint64, lit, shade Color, w, h int) *weatherSprite {
	return weatherSpriteFor(weatherSpriteKey{kind: "cloud", seed: seed, a: lit, b: shade, w: w, h: h}, func() *weatherSprite {
		return bakeWeatherSprite(w, h, 2, func(u, v float64) Color {
			nx, ny := u*2-1, v*2-1
			top, bottom := math.Max(0, -ny), math.Max(0, ny)
			base := 1 - nx*nx*1.05 - top*top*1.6 - bottom*bottom*5.5
			// Noise adds at most .625 to base*.9, so below this the density
			// is zero whatever the noise says: skip the noise.
			if base*.9+.625 <= .05 {
				return Color{}
			}
			n := weatherFBM(u*6.5, v*3.2, seed, 4)
			dens := weatherSmoothstep(.05, .42, base*.9+(n-.5)*1.25)
			light := clampEffect(.95-v*1.05+(n-.5)*.55, 0, 1)
			col := LerpColor(shade, lit, light)
			col.A = weatherAlpha(255, dens)
			return col
		})
	})
}

// paintWeatherClouds draws the far (slow, faint, high) or near (larger,
// lower, faster) layer. Motion is a slow periodic drift, not a wrap: the
// effect loop is nine seconds, and no sky crosses a card that fast.
func paintWeatherClouds(c *Canvas, box ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, kind weatherKind, phase float64, near bool, flash float64) {
	count := weatherCloudCount(kind)
	if count == 0 {
		return
	}
	lit, shade := weatherCloudColours(kind, spec.Daylight, style)
	intensity := effectIntensity(spec.Intensity)
	loop := weatherLoop(phase, spec.Speed, 1)
	for i := 0; i < count; i++ {
		depth := float64(i) / float64(max(count-1, 1))
		if (depth >= .6) != near {
			continue
		}
		scale := .55 + .7*depth
		if kind == weatherPartlyCloudy {
			scale *= .9
		}
		w := int(float64(box.W) * .62 * scale)
		h := w / 2
		seed := spec.Seed + uint64(i%4)
		sprite := weatherCloudSprite(seed, lit, shade, w, h)
		baseX := weatherUnit(spec.Seed, uint64(i), 21)*float64(box.W+w) - float64(w)*.6
		drift := math.Sin(2*math.Pi*(loop+weatherUnit(spec.Seed, uint64(i), 22))) * (.01 + .03*depth) * float64(box.W)
		y := (.02 + .40*weatherUnit(spec.Seed, uint64(i), 23) + .12*depth) * float64(box.H)
		alpha := (.55 + .45*depth) * intensity
		blitWeatherSprite(c, box, mask, sprite, int(baseX+drift), int(y), alpha)
		if flash > 0 {
			bright := weatherCloudSprite(seed, Color{R: 235, G: 240, B: 255, A: 255}, Color{R: 150, G: 160, B: 200, A: 255}, w, h)
			blitWeatherSprite(c, box, mask, bright, int(baseX+drift), int(y), alpha*flash*.55)
		}
	}
}

// paintWeatherFog sways four soft bands across the card. Each band is wider
// than the card and its drift is a sway, never a wrap, so it need not tile.
func paintWeatherFog(c *Canvas, box ui.Rect, mask *image.Alpha, spec ui.EffectSpec, phase float64) {
	col := LerpColor(weatherHex("#788092"), weatherHex("#ecf0f4"), math.Round(clampEffect(spec.Daylight, 0, 1)*10)/10)
	w, h := int(float64(box.W)*1.3), int(float64(box.H)*.55)
	loop := weatherLoop(phase, spec.Speed, 1)
	for i := 0; i < 4; i++ {
		seed := spec.Seed + 30 + uint64(i)
		band := weatherSpriteFor(weatherSpriteKey{kind: "fog", seed: seed, a: col, w: w, h: h}, func() *weatherSprite {
			// The grid is evaluated row by row, so the vertical falloff is
			// computed once per row rather than once per sample.
			lastV, falloff := -1.0, 0.0
			return bakeWeatherSprite(w, h, 4, func(u, v float64) Color {
				if v != lastV {
					lastV, falloff = v, math.Pow(math.Sin(v*math.Pi), 1.4)
				}
				n := weatherFBM(u*8, v*4, seed, 4)
				a := clampEffect((n-.32)*1.9, 0, 1) * falloff
				out := col
				out.A = weatherAlpha(255, a)
				return out
			})
		})
		drift := math.Sin(2*math.Pi*(loop+float64(i)*.25)) * .1 * float64(box.W)
		y := int(float64(box.H)*(.2+float64(i)*.2)) - h/2
		blitWeatherSprite(c, box, mask, band, int(drift)-w/8, y, .55+.1*float64(i))
	}
}
