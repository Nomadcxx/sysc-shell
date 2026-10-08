package render

import (
	"fmt"
	"image"
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

type weatherKind uint8

const (
	weatherClear weatherKind = iota
	weatherPartlyCloudy
	weatherCloudy
	weatherFog
	weatherRain
	weatherSnow
	weatherHeavySnow
	weatherThunderstorm
)

func weatherKindFor(variant ui.EffectVariant) (weatherKind, error) {
	switch variant {
	case ui.WeatherClear:
		return weatherClear, nil
	case ui.WeatherPartlyCloudy:
		return weatherPartlyCloudy, nil
	case ui.WeatherCloudy:
		return weatherCloudy, nil
	case ui.WeatherFog:
		return weatherFog, nil
	case ui.WeatherRain:
		return weatherRain, nil
	case ui.WeatherSnow:
		return weatherSnow, nil
	case ui.WeatherHeavySnow:
		return weatherHeavySnow, nil
	case ui.WeatherThunderstorm:
		return weatherThunderstorm, nil
	default:
		return 0, fmt.Errorf("render: unsupported weather variant %d", variant)
	}
}

// paintWeatherEffect paints the Living sky (2026-10-08 design): a full-bleed
// sky for the condition and time of day, then light, clouds and weather in
// depth order. Every textured form is a cached sprite; see weather_sprite.go.
func paintWeatherEffect(c *Canvas, box ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, phase float64) error {
	kind, err := weatherKindFor(spec.Variant)
	if err != nil {
		return err
	}
	if box.W <= 0 || box.H <= 0 {
		return nil
	}
	paintWeatherSky(c, box, mask, weatherSkyStops(kind, spec.Daylight, style))
	paintWeatherStars(c, box, mask, spec, phase)
	if kind == weatherClear || kind == weatherPartlyCloudy {
		paintWeatherCelestial(c, box, mask, spec, phase)
	}
	flash := weatherFlash(spec, phase)
	paintWeatherClouds(c, box, mask, style, spec, kind, phase, false, flash)
	if kind == weatherThunderstorm {
		paintWeatherLightning(c, box, mask, spec, phase)
	}
	switch kind {
	case weatherRain, weatherThunderstorm:
		paintWeatherRain(c, box, mask, spec, phase, 0, 1)
	case weatherSnow, weatherHeavySnow:
		paintWeatherSnow(c, box, mask, spec, phase, kind == weatherHeavySnow, 0, 1)
	}
	paintWeatherClouds(c, box, mask, style, spec, kind, phase, true, flash)
	switch kind {
	case weatherFog:
		paintWeatherFog(c, box, mask, spec, phase)
	case weatherRain, weatherThunderstorm:
		paintWeatherRain(c, box, mask, spec, phase, 2)
	case weatherSnow, weatherHeavySnow:
		paintWeatherSnow(c, box, mask, spec, phase, kind == weatherHeavySnow, 2)
	}
	return nil
}

// drawWeatherStroke draws a round-capped, antialiased line. Coverage is the
// distance to the segment, so every pixel blends once: stamping overlapping
// discs along the line would compound a faint glow toward opaque.
func drawWeatherStroke(c *Canvas, box ui.Rect, mask *image.Alpha, x0, y0, x1, y1, width float64, col Color, alpha uint8) {
	if alpha == 0 || width <= 0 || math.IsNaN(x0+y0+x1+y1) || math.IsInf(x0+y0+x1+y1, 0) {
		return
	}
	radius := width / 2
	minX := max(int(math.Floor(math.Min(x0, x1)-radius-1)), 0)
	maxX := min(int(math.Ceil(math.Max(x0, x1)+radius+1)), box.W)
	minY := max(int(math.Floor(math.Min(y0, y1)-radius-1)), 0)
	maxY := min(int(math.Ceil(math.Max(y0, y1)+radius+1)), box.H)
	dx, dy := x1-x0, y1-y0
	length2 := dx*dx + dy*dy
	for y := minY; y < maxY; y++ {
		py := float64(y) + .5
		for x := minX; x < maxX; x++ {
			px := float64(x) + .5
			t := 0.0
			if length2 > 0 {
				t = clampEffect(((px-x0)*dx+(py-y0)*dy)/length2, 0, 1)
			}
			coverage := clampEffect(radius+.5-math.Hypot(px-x0-dx*t, py-y0-dy*t), 0, 1)
			if coverage > 0 {
				localWeatherPixel(c, box, mask, x, y, col, alpha, coverage)
			}
		}
	}
}

func localWeatherPixel(c *Canvas, box ui.Rect, mask *image.Alpha, x, y int, col Color, alpha uint8, coverage float64) {
	if box.W <= 0 || box.H <= 0 || x < 0 || y < 0 || x >= box.W || y >= box.H {
		return
	}
	// Clamp the local coordinate before forming the absolute coordinate. The
	// range check above keeps off-card particles from being folded onto an edge.
	x = clampInt(x, 0, box.W-1)
	y = clampInt(y, 0, box.H-1)
	absoluteX, xOK := checkedEffectAdd(box.X, x)
	absoluteY, yOK := checkedEffectAdd(box.Y, y)
	if !xOK || !yOK {
		return
	}
	blendWeatherPixel(c, box, mask, absoluteX, absoluteY,
		Color{R: col.R, G: col.G, B: col.B, A: alpha}, weatherCoverage(coverage))
}

// blendWeatherRow fills one horizontal span with a single colour. Every bound
// blendWeatherPixel checks is invariant across a row, so the sky hoists them
// out of the inner loop and keeps only the mask lookup and the blend. The
// effect loop never settles, so this span runs on every frame a weather
// surface is open; per-pixel revalidation is what put it past the frame cap.
func blendWeatherRow(c *Canvas, box ui.Rect, mask *image.Alpha, y, x0, x1 int, col Color) {
	if c == nil || mask == nil || col.A == 0 ||
		box.W <= 0 || box.H <= 0 || box.W > maxEffectDimension || box.H > maxEffectDimension ||
		box.W > maxEffectPixels/box.H {
		return
	}
	boxRight, ok := checkedEffectAdd(box.X, box.W)
	if !ok {
		return
	}
	boxBottom, ok := checkedEffectAdd(box.Y, box.H)
	if !ok {
		return
	}
	if y < box.Y || y >= boxBottom || y < 0 || y >= c.Height {
		return
	}
	lo, hi := max(max(x0, box.X), 0), min(min(x1, boxRight), c.Width)
	if c.restrict.W > 0 && c.restrict.H > 0 {
		restrictRight, rightOK := checkedEffectAdd(c.restrict.X, c.restrict.W)
		restrictBottom, bottomOK := checkedEffectAdd(c.restrict.Y, c.restrict.H)
		if !rightOK || !bottomOK || y < c.restrict.Y || y >= restrictBottom {
			return
		}
		lo, hi = max(lo, c.restrict.X), min(hi, restrictRight)
	}
	b := mask.Bounds()
	my, yOK := checkedEffectAdd(b.Min.Y, y-box.Y)
	if !yOK || my < b.Min.Y || my >= b.Max.Y {
		return
	}
	// Clamp the span so every mask lookup is in bounds without a per-pixel test.
	lo = max(lo, box.X+(b.Min.X-b.Min.X))
	hi = min(hi, box.X+(b.Max.X-b.Min.X))
	if lo >= hi {
		return
	}
	if c.Stride <= 0 || len(c.Pix) < 4 || y > (len(c.Pix)-4)/c.Stride {
		return
	}
	rowOffset := y * c.Stride
	if hi-1 > (len(c.Pix)-rowOffset-4)/4 {
		return
	}
	base := col.premultiply()
	maskRow := (my - b.Min.Y) * mask.Stride
	for x := lo; x < hi; x++ {
		coverage := uint32(mask.Pix[maskRow+(x-box.X)])
		if coverage == 0 {
			continue
		}
		alpha := uint32(col.A) * coverage / 255
		if alpha == 0 {
			continue
		}
		src := base
		if coverage != 255 {
			for i := range src {
				src[i] = byte(uint32(src[i]) * coverage / 255)
			}
		}
		offset := rowOffset + x*4
		blendPixel(c.Pix[offset:offset+4], src, alpha)
	}
}

func blendWeatherPixel(c *Canvas, box ui.Rect, mask *image.Alpha, x, y int, col Color, coverage uint8) {
	if c == nil || mask == nil || col.A == 0 || coverage == 0 ||
		box.W <= 0 || box.H <= 0 || box.W > maxEffectDimension || box.H > maxEffectDimension ||
		box.W > maxEffectPixels/box.H {
		return
	}
	boxRight, ok := checkedEffectAdd(box.X, box.W)
	if !ok {
		return
	}
	boxBottom, ok := checkedEffectAdd(box.Y, box.H)
	if !ok {
		return
	}
	if x < box.X || y < box.Y || x >= boxRight || y >= boxBottom ||
		x < 0 || y < 0 || x >= c.Width || y >= c.Height {
		return
	}
	if c.restrict.W > 0 && c.restrict.H > 0 {
		restrictRight, rightOK := checkedEffectAdd(c.restrict.X, c.restrict.W)
		restrictBottom, bottomOK := checkedEffectAdd(c.restrict.Y, c.restrict.H)
		if !rightOK || !bottomOK || x < c.restrict.X || y < c.restrict.Y || x >= restrictRight || y >= restrictBottom {
			return
		}
	}
	b := mask.Bounds()
	mx, xOK := checkedEffectAdd(b.Min.X, x-box.X)
	my, yOK := checkedEffectAdd(b.Min.Y, y-box.Y)
	if !xOK || !yOK || !image.Pt(mx, my).In(b) {
		return
	}
	maskCoverage := uint32(mask.AlphaAt(mx, my).A)
	combined := uint32(coverage) * maskCoverage / 255
	alpha := uint32(col.A) * combined / 255
	if alpha == 0 {
		return
	}
	src := col.premultiply()
	for i := range src {
		src[i] = byte(uint32(src[i]) * combined / 255)
	}
	if c.Stride <= 0 || len(c.Pix) < 4 || y > (len(c.Pix)-4)/c.Stride {
		return
	}
	rowOffset := y * c.Stride
	if x > (len(c.Pix)-rowOffset-4)/4 {
		return
	}
	offset := rowOffset + x*4
	blendPixel(c.Pix[offset:offset+4], src, alpha)
}

func weatherAlpha(base uint8, amount float64) uint8 {
	amount = clampEffect(amount, 0, 1)
	return uint8(math.Round(float64(base) * amount))
}

func weatherCoverage(coverage float64) uint8 {
	return uint8(math.Round(clampEffect(coverage, 0, 1) * 255))
}

func effectIntensity(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return clampEffect(value, 0, 1)
}

func weatherSmoothstep(edge0, edge1, value float64) float64 {
	if edge1 <= edge0 {
		if value >= edge1 {
			return 1
		}
		return 0
	}
	t := clampEffect((value-edge0)/(edge1-edge0), 0, 1)
	return t * t * (3 - 2*t)
}

func clampEffect(value, lo, hi float64) float64 {
	if math.IsNaN(value) {
		return lo
	}
	if value < lo {
		return lo
	}
	if value > hi {
		return hi
	}
	return value
}

func positiveMod(value, modulus float64) float64 {
	if modulus <= 0 || math.IsNaN(value) || math.IsNaN(modulus) || math.IsInf(value, 0) || math.IsInf(modulus, 0) {
		return 0
	}
	value = math.Mod(value, modulus)
	if value < 0 {
		value += modulus
	}
	return value
}

func weatherUnit(seed, index, salt uint64) float64 {
	return float64(weatherMix(seed+index*0x9e3779b97f4a7c15+salt*0xd1b54a32d192ed03)>>11) * (1.0 / 9007199254740992.0)
}

func weatherMix(value uint64) uint64 {
	value ^= value >> 30
	value *= 0xbf58476d1ce4e5b9
	value ^= value >> 27
	value *= 0x94d049bb133111eb
	return value ^ value>>31
}
