package render

import (
	"image"
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Fixed particle budgets (design D4). The depth split is roughly 55% far,
// 30% mid and 15% near, fixed by seed.
const (
	weatherRainCount      = 170
	weatherStormRainCount = 210
	weatherSnowCount      = 110
	weatherHeavySnowCount = 190
)

func weatherDepth(seed uint64, i int) int {
	r := weatherUnit(seed, uint64(i), 31)
	switch {
	case r < .55:
		return 0
	case r < .85:
		return 1
	}
	return 2
}

// Per-depth rain: length as a fraction of height, physical width, alpha, and
// whole laps per effect loop (so the loop seam is invisible).
var weatherRainLayers = [3]struct {
	length, width, alpha float64
	laps                 int
}{{.04, 1, .32, 5}, {.07, 1.4, .42, 7}, {.13, 3.2, .14, 10}}

// weatherRainSpan is the rows a streak covers at fall position v in [0, 1).
// The fall runs from wholly above the card to wholly below it, so a streak
// never wraps while any of it is visible.
func weatherRainSpan(v float64, h int, length float64) (top, head float64) {
	head = v*(float64(h)+length+2) - 1
	return head - length, head
}

func paintWeatherRain(c *Canvas, box ui.Rect, mask *image.Alpha, spec ui.EffectSpec, phase float64, layers ...int) {
	count := weatherRainCount
	if spec.Variant == ui.WeatherThunderstorm {
		count = weatherStormRainCount
	}
	d := clampEffect(spec.Daylight, 0, 1)
	col := LerpColor(Color{R: 200, G: 210, B: 235, A: 255}, Color{R: 230, G: 236, B: 248, A: 255}, d)
	const wind = .18
	intensity := effectIntensity(spec.Intensity)
	for _, layer := range layers {
		L := weatherRainLayers[layer]
		length := L.length * float64(box.H)
		alpha := weatherAlpha(255, L.alpha*intensity/.7) // .7 is the catalogue's daytime intensity
		loop := weatherLoop(phase, spec.Speed, L.laps)
		for i := 0; i < count; i++ {
			if weatherDepth(spec.Seed, i) != layer {
				continue
			}
			v := positiveMod(weatherUnit(spec.Seed, uint64(i), 32)+loop, 1)
			_, y := weatherRainSpan(v, box.H, length)
			x := positiveMod(weatherUnit(spec.Seed, uint64(i), 33)+wind*y/float64(box.H), 1) * float64(box.W)
			drawWeatherStroke(c, box, mask, x, y, x-wind*length, y-length, L.width, col, alpha)
		}
	}
}

var weatherSnowLayers = [3]struct {
	radius, alpha, soft float64
	laps                int
}{{1.4, .55, .45, 1}, {2.4, .8, .45, 1}, {5.5, .45, .15, 2}}

// weatherFlakeRadius is one layer's flake radius; flakes grow with the card.
func weatherFlakeRadius(h, layer int, heavy bool) float64 {
	grow := 1.0
	if heavy {
		grow = 1.2
	}
	return weatherSnowLayers[layer].radius * grow * math.Max(float64(h)/120, .75)
}

// weatherSnowY is a flake's centre row at fall position v in [0, 1): from
// wholly above the card to wholly below it.
func weatherSnowY(v float64, h int, r float64) float64 {
	return v*(float64(h)+4*r) - 2*r
}

func paintWeatherSnow(c *Canvas, box ui.Rect, mask *image.Alpha, spec ui.EffectSpec, phase float64, heavy bool, layers ...int) {
	count := weatherSnowCount
	if heavy {
		count = weatherHeavySnowCount
	}
	intensity := effectIntensity(spec.Intensity)
	sway := weatherLoop(phase, spec.Speed, 2)
	for _, layer := range layers {
		L := weatherSnowLayers[layer]
		r := weatherFlakeRadius(box.H, layer, heavy)
		edge := int(math.Ceil(r*2 + 2))
		// The radius keys the sprite: two radii can round to one edge.
		flake := weatherSpriteFor(weatherSpriteKey{kind: "flake", seed: math.Float64bits(r) + uint64(layer), w: edge, h: edge}, func() *weatherSprite {
			return bakeWeatherSprite(edge, edge, func(u, v float64) Color {
				dist := math.Hypot(u-.5, v-.5) * float64(edge) / r
				a := 1.0
				if dist > L.soft {
					a = clampEffect(1-(dist-L.soft)/(1-L.soft), 0, 1) * .85
				}
				return Color{R: 255, G: 255, B: 255, A: weatherAlpha(255, a)}
			})
		})
		loop := weatherLoop(phase, spec.Speed, L.laps)
		for i := 0; i < count; i++ {
			if weatherDepth(spec.Seed, i) != layer {
				continue
			}
			v := positiveMod(weatherUnit(spec.Seed, uint64(i), 34)+loop, 1)
			drift := math.Sin(2*math.Pi*(sway+weatherUnit(spec.Seed, uint64(i), 35))) * .02
			x := positiveMod(weatherUnit(spec.Seed, uint64(i), 36)+drift, 1) * float64(box.W)
			y := weatherSnowY(v, box.H, r)
			blitWeatherSprite(c, box, mask, flake, int(x)-edge/2, int(y)-edge/2, L.alpha*intensity/.7)
		}
	}
}
