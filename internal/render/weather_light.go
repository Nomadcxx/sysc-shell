package render

import (
	"image"
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// weatherCelestialX places the sun or moon: SceneBias -1 puts it at a fifth of
// the width, +1 at four fifths, so it clears the hero's text column.
func weatherCelestialX(box ui.Rect, bias float64) int {
	return int(float64(box.W) * (.2 + .6*(clampEffect(bias, -1, 1)+1)/2))
}

// paintWeatherCelestial crossfades the moon into the sun through twilight.
// The sun sinks toward the horizon and warms as Daylight falls toward 0.5.
func paintWeatherCelestial(c *Canvas, box ui.Rect, mask *image.Alpha, spec ui.EffectSpec, phase float64) {
	d := clampEffect(spec.Daylight, 0, 1)
	r := float64(min(box.W, box.H))
	cx := weatherCelestialX(box, spec.SceneBias)
	if moonA := 1 - weatherSmoothstep(.35, .60, d); moonA > 0 {
		paintWeatherMoon(c, box, mask, cx, int(float64(box.H)*.3), r, moonA)
	}
	if sunA := weatherSmoothstep(.40, .65, d); sunA > 0 {
		cy := int(float64(box.H) * (.66 + (.30-.66)*weatherSmoothstep(.45, 1, d)))
		// Quantised like the cloud palette: the warmth keys the sprite, and a
		// continuous value would rebake the bloom every time Daylight moved.
		warm := math.Round((1-weatherSmoothstep(.5, .9, d))*10) / 10
		paintWeatherSun(c, box, mask, cx, cy, r, sunA, warm, spec, phase)
	}
}

func paintWeatherSun(c *Canvas, box ui.Rect, mask *image.Alpha, cx, cy int, r, alpha, warm float64, spec ui.EffectSpec, phase float64) {
	core := LerpColor(weatherHex("#fff4d6"), weatherHex("#ffb270"), warm)
	edge := int(math.Round(r * 1.3))
	bloom := weatherSpriteFor(weatherSpriteKey{kind: "bloom", a: core, w: edge, h: edge}, func() *weatherSprite {
		return bakeWeatherSprite(edge, edge, func(u, v float64) Color {
			dist := math.Hypot(u-.5, v-.5) * 2 // 0 centre, 1 edge
			a := .9*math.Exp(-dist*dist*60) + .35*math.Exp(-dist*dist*9) + .22*math.Exp(-dist*dist*2.2)
			col := core
			col.A = weatherAlpha(255, a)
			return col
		})
	})
	blitWeatherSprite(c, box, mask, bloom, cx-edge/2, cy-edge/2, alpha)
	rays := weatherSpriteFor(weatherSpriteKey{kind: "rays", a: core, w: edge, h: edge}, func() *weatherSprite {
		return bakeWeatherSprite(edge, edge, func(u, v float64) Color {
			dx, dy := u-.5, v-.5
			dist := math.Hypot(dx, dy) * 2
			spoke := math.Pow(math.Abs(math.Cos(6*math.Atan2(dy, dx)+.3)), 24)
			col := core
			col.A = weatherAlpha(255, .22*spoke*math.Max(0, 1-dist))
			return col
		})
	})
	breathe := .6 + .4*math.Pow(math.Sin(2*math.Pi*weatherLoop(phase, spec.Speed, 1)), 2)
	blitWeatherSprite(c, box, mask, rays, cx-edge/2, cy-edge/2, alpha*breathe)
}

func paintWeatherMoon(c *Canvas, box ui.Rect, mask *image.Alpha, cx, cy int, r, alpha float64) {
	edge := int(math.Round(r * .9))
	moon := weatherSpriteFor(weatherSpriteKey{kind: "moon", w: edge, h: edge}, func() *weatherSprite {
		disc := .085 / .9 // disc radius as a fraction of the sprite
		return bakeWeatherSprite(edge, edge, func(u, v float64) Color {
			dist := math.Hypot(u-.5, v-.5)
			if dist <= disc {
				shade := LerpColor(weatherHex("#fbfbff"), weatherHex("#cfd6ee"), dist/disc)
				if weatherFBM(u*40, v*40, 5, 3) > .62 {
					shade = LerpColor(shade, weatherHex("#969fc3"), .35)
				}
				return shade
			}
			glow := weatherHex("#c8d6ff")
			glow.A = weatherAlpha(255, .32*math.Exp(-(dist-disc)*9))
			return glow
		})
	})
	blitWeatherSprite(c, box, mask, moon, cx-edge/2, cy-edge/2, alpha)
}

// weatherFlash is the storm's double pulse, once per effect loop.
func weatherFlash(spec ui.EffectSpec, phase float64) float64 {
	if spec.Variant != ui.WeatherThunderstorm {
		return 0
	}
	p := weatherLoop(phase, spec.Speed, 1)
	pulse := func(centre, width float64) float64 { return math.Max(0, 1-math.Abs(p-centre)/width) }
	return math.Max(pulse(.47, .010), .8*pulse(.495, .016))
}

// paintWeatherLightning washes the sky and draws a seeded, jagged bolt
// from the cloud base while the flash is lit.
func paintWeatherLightning(c *Canvas, box ui.Rect, mask *image.Alpha, spec ui.EffectSpec, phase float64) {
	f := weatherFlash(spec, phase)
	if f <= 0 {
		return
	}
	x0, y0, x1, y1 := c.clip(box)
	wash := Color{R: 190, G: 205, B: 255, A: weatherAlpha(255, f*.28)}
	for y := y0; y < y1; y++ {
		blendWeatherRow(c, box, mask, y, x0, x1, wash)
	}
	top, length := float64(box.H)*.18, float64(box.H)*.75
	x := weatherUnit(spec.Seed, 1, 41)*.6 + .2
	pts := [][2]float64{{x, 0}}
	for i := 1; i <= 9; i++ {
		x += (weatherUnit(spec.Seed, uint64(i), 42) - .5) * .09
		pts = append(pts, [2]float64{x, float64(i) / 9})
	}
	bolt := Color{R: 215, G: 225, B: 255, A: 255}
	// Glow, halo, then core: each pass is wider and fainter than the next.
	for _, w := range [][2]float64{{10, .10}, {4, .30}, {1.4, 1}} {
		for i := 1; i < len(pts); i++ {
			drawWeatherStroke(c, box, mask,
				pts[i-1][0]*float64(box.W), top+pts[i-1][1]*length,
				pts[i][0]*float64(box.W), top+pts[i][1]*length,
				w[0], bolt, weatherAlpha(255, w[1]*f))
		}
	}
}
