package render

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestWeatherEffectIsDeterministic(t *testing.T) {
	first := paintEffectFrame(t, rainSpec(7), .25)
	second := paintEffectFrame(t, rainSpec(7), .25)
	if !bytes.Equal(first, second) {
		t.Fatal("same effect inputs produced different pixels")
	}
}

func TestWeatherEffectPhaseChangesAnimatedPixels(t *testing.T) {
	first := paintEffectFrame(t, rainSpec(7), .10)
	second := paintEffectFrame(t, rainSpec(7), .60)
	if bytes.Equal(first, second) {
		t.Fatal("changing phase did not change the weather effect")
	}
}

func TestWeatherClearPaintsAVisibleCelestialForm(t *testing.T) {
	const width, height = 160, 96
	pixels := paintWeatherVariantFrame(t, ui.WeatherClear, .25, width, height)
	upper := ui.Rect{W: width, H: height / 2}
	if got := meaningfulWeatherPixels(pixels, width, upper, testStyle.Capsule, 18); got < 30 {
		t.Fatalf("clear upper hero has %d meaningful pixels, want a visible celestial form", got)
	}
}

func TestWeatherPartlyCloudyPaintsDistinctSkyAndCloudRegions(t *testing.T) {
	const width, height = 160, 96
	pixels := paintWeatherVariantFrame(t, ui.WeatherPartlyCloudy, .25, width, height)
	top := ui.Rect{W: width, H: height / 2}
	bottom := ui.Rect{Y: height / 2, W: width, H: height / 2}
	if got := meaningfulWeatherPixels(pixels, width, top, testStyle.Capsule, 18); got < 30 {
		t.Fatalf("partly-cloudy sky has %d meaningful pixels, want a celestial form", got)
	}
	if got := meaningfulWeatherPixels(pixels, width, bottom, testStyle.Capsule, 18); got < 60 {
		t.Fatalf("partly-cloudy cloud bank has %d meaningful pixels, want a broad form", got)
	}
}

func TestWeatherNightUsesAMoonSilhouette(t *testing.T) {
	const width, height = 160, 96
	day := rainSpec(7)
	day.Variant = ui.WeatherClear
	night := day
	night.Night = true
	dayPixels := paintWeatherVariantFrameWithSpec(t, day, .25, width, height)
	nightPixels := paintWeatherVariantFrameWithSpec(t, night, .25, width, height)
	if got := differingWeatherPixels(dayPixels, nightPixels, width, ui.Rect{W: width, H: height / 2}, 18); got < 80 {
		t.Fatalf("day and night clear forms differ at only %d upper-hero pixels, want a moon-specific silhouette", got)
	}
}

func TestWeatherSkyHasReadableVerticalDepth(t *testing.T) {
	const width, height = 160, 96
	pixels := paintWeatherVariantFrame(t, ui.WeatherClear, .25, width, height)
	top := meanWeatherColor(pixels, width, ui.Rect{Y: 4, W: width, H: 20})
	bottom := meanWeatherColor(pixels, width, ui.Rect{Y: 68, W: width, H: 20})
	if got := weatherRGBDistance(top, bottom); got < 28 {
		t.Fatalf("sky depth = %d, want a readable vertical gradient", got)
	}
}

func TestWeatherCloudsHaveHighlightAndShadowLayers(t *testing.T) {
	const width, height = 240, 144
	pixels := paintWeatherVariantFrame(t, ui.WeatherCloudy, .37, width, height)
	upper := meanWeatherColor(pixels, width, ui.Rect{Y: 62, W: width, H: 24})
	lower := meanWeatherColor(pixels, width, ui.Rect{Y: 102, W: width, H: 24})
	if got := weatherRGBDistance(upper, lower); got < 12 {
		t.Fatalf("cloud layer contrast = %d, want distinct highlight and shadow layers", got)
	}
}

func TestWeatherCloudScenePreservesLandscapeComposition(t *testing.T) {
	const width, height = 720, 400
	c := newTestCanvas(t, width, height)
	box := ui.Rect{W: width, H: height}
	fillRect(c, box, testStyle.Capsule)
	paintCloudScene(c, box, RoundedMask(0, width, height), testStyle, rainSpec(7), .37, 1, 1)

	bounds, ok := weatherDifferenceBounds(c.Pix, width, testStyle.Capsule, 6)
	if !ok {
		t.Fatal("landscape cloud scene painted no measurable cloud form")
	}
	if bounds.W > height*3/2 {
		t.Fatalf("landscape cloud width = %d in %dx%d hero, want no more than %d", bounds.W, width, height, height*3/2)
	}
}

func TestWeatherCelestialAndCloudFormsRespondToPhase(t *testing.T) {
	const width, height = 160, 96
	first := paintWeatherVariantFrame(t, ui.WeatherPartlyCloudy, .10, width, height)
	second := paintWeatherVariantFrame(t, ui.WeatherPartlyCloudy, .60, width, height)
	upper := ui.Rect{W: width, H: height / 2}
	if got := differingWeatherPixels(first, second, width, upper, 12); got < 20 {
		t.Fatalf("phase changed only %d upper-hero pixels, want visible celestial/cloud motion", got)
	}
}

func TestWeatherLargeFormsHaveReadablePhaseMotion(t *testing.T) {
	const width, height = 240, 144
	for _, variant := range []ui.EffectVariant{ui.WeatherClear, ui.WeatherCloudy} {
		t.Run(fmt.Sprintf("variant-%d", variant), func(t *testing.T) {
			first := paintWeatherVariantFrame(t, variant, .05, width, height)
			second := paintWeatherVariantFrame(t, variant, .55, width, height)
			got := differingWeatherPixels(first, second, width, ui.Rect{W: width, H: height}, 12)
			if got < 3000 {
				t.Fatalf("variant %d changed only %d pixels at hero scale, want deliberate large-form motion", variant, got)
			}
		})
	}
}

func TestWeatherCloudMassBlendsItsUnionOnce(t *testing.T) {
	const width, height = 160, 96
	box := ui.Rect{W: width, H: height}
	mask := RoundedMask(0, width, height)
	cloud := Color{R: 255, G: 255, B: 255, A: 255}

	painted := newTestCanvas(t, width, height)
	fillRect(painted, box, testStyle.Capsule)
	paintCloudMass(painted, box, mask, 80, 50, 120, 40, cloud, 128)

	want := newTestCanvas(t, width, height)
	fillRect(want, box, testStyle.Capsule)
	localWeatherPixel(want, box, mask, 80, 50, cloud, 128, 1)
	if got, expected := effectPixelBytes(painted, 80, 50), effectPixelBytes(want, 80, 50); !bytes.Equal(got, expected) {
		t.Fatalf("overlapping cloud puffs stacked alpha: got %v, want one union blend %v", got, expected)
	}
}

func TestWeatherPrecipitationHasCardSizedCoverage(t *testing.T) {
	const width, height = 160, 96
	for _, variant := range []ui.EffectVariant{ui.WeatherRain, ui.WeatherSnow, ui.WeatherHeavySnow, ui.WeatherThunderstorm} {
		t.Run(fmt.Sprintf("variant-%d", variant), func(t *testing.T) {
			pixels := paintWeatherVariantFrame(t, variant, .37, width, height)
			if got := meaningfulWeatherPixels(pixels, width, ui.Rect{W: width, H: height}, testStyle.Capsule, 18); got < 50 {
				t.Fatalf("variant %d has %d meaningful pixels, want card-sized coverage", variant, got)
			}
		})
	}
}

func TestWeatherZeroSpeedParksAtOneDeterministicFrame(t *testing.T) {
	spec := rainSpec(7)
	spec.Speed = 0
	first := paintWeatherVariantFrameWithSpec(t, spec, 0, 160, 96)
	second := paintWeatherVariantFrameWithSpec(t, spec, .75, 160, 96)
	if !bytes.Equal(first, second) {
		t.Fatal("a zero-speed weather effect moved between parked phases")
	}
}

func TestWeatherLightningPulseIsDeterministicAndPhaseDependent(t *testing.T) {
	const seed uint64 = 7
	var previous float64
	positive, changed := false, false
	for i := 0; i <= 100; i++ {
		phase := float64(i) / 100
		pulse := lightningPulse(phase, 1, seed)
		if pulse > 0 {
			positive = true
		}
		if i > 0 && pulse != previous {
			changed = true
		}
		if pulse != lightningPulse(phase, 1, seed) {
			t.Fatalf("lightning pulse at phase %.2f was not deterministic", phase)
		}
		previous = pulse
	}
	if !positive {
		t.Fatal("lightning pulse never produced a flash")
	}
	if !changed {
		t.Fatal("lightning pulse did not respond to phase")
	}
}

func TestWeatherEffectRejectsInvalidSpec(t *testing.T) {
	c := newTestCanvas(t, 64, 64)
	for i := range c.Pix {
		c.Pix[i] = byte(i%251 + 1)
	}
	before := append([]byte(nil), c.Pix...)
	n := &ui.Node{
		Kind:   ui.KindEffect,
		Bounds: ui.Rect{W: 64, H: 64},
		Effect: ui.EffectSpec{Program: ui.EffectProgram(99)},
	}
	if err := paintEffect(c, n, testStyle); err == nil {
		t.Fatal("invalid effect spec was painted")
	}
	if !bytes.Equal(c.Pix, before) {
		t.Fatal("invalid effect spec modified the canvas")
	}
}

func TestWeatherEffectRendersEveryVariant(t *testing.T) {
	tests := []struct {
		name    string
		variant ui.EffectVariant
	}{
		{"clear", ui.WeatherClear},
		{"partly cloudy", ui.WeatherPartlyCloudy},
		{"cloudy", ui.WeatherCloudy},
		{"fog", ui.WeatherFog},
		{"rain", ui.WeatherRain},
		{"snow", ui.WeatherSnow},
		{"heavy snow", ui.WeatherHeavySnow},
		{"thunderstorm", ui.WeatherThunderstorm},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestCanvas(t, 64, 64)
			fillEffectSentinel(c)
			before := append([]byte(nil), effectPixelBytes(c, 32, 32)...)
			n := &ui.Node{
				Kind:   ui.KindEffect,
				Bounds: ui.Rect{W: 64, H: 64},
				Effect: ui.EffectSpec{
					Program:   ui.EffectWeather,
					Variant:   tt.variant,
					Seed:      7,
					Intensity: .8,
					Speed:     1,
				},
			}
			if err := paintEffect(c, n, testStyle); err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(effectPixelBytes(c, 32, 32), before) {
				t.Fatal("supported weather variant did not paint its in-mask pixel")
			}
		})
	}
}

func TestWeatherEffectRejectsInvalidVariantsAndDescriptors(t *testing.T) {
	tests := []struct {
		name string
		edit func(*ui.EffectSpec)
	}{
		{"invalid variant", func(spec *ui.EffectSpec) { spec.Variant = ui.EffectVariant(255) }},
		{"nan intensity", func(spec *ui.EffectSpec) { spec.Intensity = math.NaN() }},
		{"positive infinity intensity", func(spec *ui.EffectSpec) { spec.Intensity = math.Inf(1) }},
		{"negative infinity speed", func(spec *ui.EffectSpec) { spec.Speed = math.Inf(-1) }},
		{"intensity above one", func(spec *ui.EffectSpec) { spec.Intensity = 1.01 }},
		{"speed below zero", func(spec *ui.EffectSpec) { spec.Speed = -.01 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestCanvas(t, 64, 64)
			fillEffectSentinel(c)
			before := append([]byte(nil), c.Pix...)
			spec := rainSpec(7)
			tt.edit(&spec)
			n := &ui.Node{Kind: ui.KindEffect, Bounds: ui.Rect{W: 64, H: 64}, Effect: spec}
			if err := paintEffect(c, n, testStyle); err == nil {
				t.Fatal("invalid effect descriptor was accepted")
			}
			if !bytes.Equal(c.Pix, before) {
				t.Fatal("invalid effect descriptor modified the canvas")
			}
		})
	}
}

func TestWeatherEffectRejectsOverflowingBounds(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	tests := []struct {
		name   string
		bounds ui.Rect
	}{
		{"physical conversion overflow", ui.Rect{W: maxInt, H: 64}},
		{"logical edge overflow", ui.Rect{X: maxInt, W: 1, H: 64}},
		{"oversized physical bounds", ui.Rect{W: maxEffectDimension + 1, H: 64}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestCanvas(t, 64, 64)
			fillEffectSentinel(c)
			before := append([]byte(nil), c.Pix...)
			n := &ui.Node{Kind: ui.KindEffect, Bounds: tt.bounds, Effect: rainSpec(7)}
			if err := paintEffect(c, n, testStyle); err == nil {
				t.Fatal("overflowing effect bounds were accepted")
			}
			if !bytes.Equal(c.Pix, before) {
				t.Fatal("overflowing effect bounds modified the canvas")
			}
		})
	}
}

func TestWeatherEffectClipsToRoundedMask(t *testing.T) {
	c := newTestCanvas(t, 64, 64)
	fillEffectSentinel(c)
	corner := append([]byte(nil), effectPixelBytes(c, 0, 0)...)
	inside := append([]byte(nil), effectPixelBytes(c, 32, 32)...)
	n := &ui.Node{
		Kind:   ui.KindEffect,
		Bounds: ui.Rect{W: 64, H: 64},
		Radius: 12,
		Effect: rainSpec(7),
	}
	if err := paintEffect(c, n, testStyle); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(effectPixelBytes(c, 0, 0), corner) {
		t.Fatal("weather effect escaped its rounded corner")
	}
	if bytes.Equal(effectPixelBytes(c, 32, 32), inside) {
		t.Fatal("weather effect did not paint inside its rounded mask")
	}
}

func TestWeatherEffectHonorsCanvasRestriction(t *testing.T) {
	c := newTestCanvas(t, 64, 64)
	fillEffectSentinel(c)
	c.restrict = ui.Rect{X: 16, Y: 16, W: 32, H: 32}
	outside := append([]byte(nil), effectPixelBytes(c, 8, 32)...)
	inside := append([]byte(nil), effectPixelBytes(c, 32, 32)...)
	n := &ui.Node{Kind: ui.KindEffect, Bounds: ui.Rect{W: 64, H: 64}, Effect: rainSpec(7)}
	if err := paintEffect(c, n, testStyle); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(effectPixelBytes(c, 8, 32), outside) {
		t.Fatal("weather effect escaped the canvas restriction")
	}
	if bytes.Equal(effectPixelBytes(c, 32, 32), inside) {
		t.Fatal("weather effect did not paint inside the canvas restriction")
	}
}

func fillEffectSentinel(c *Canvas) {
	for i := range c.Pix {
		c.Pix[i] = byte(i%251 + 1)
	}
}

func effectPixelBytes(c *Canvas, x, y int) []byte {
	i := y*c.Stride + x*4
	return c.Pix[i : i+4]
}

func rainSpec(seed uint64) ui.EffectSpec {
	return ui.EffectSpec{
		Program:   ui.EffectWeather,
		Variant:   ui.WeatherRain,
		Seed:      seed,
		Intensity: .7,
		Speed:     1,
	}
}

func paintEffectFrame(t *testing.T, spec ui.EffectSpec, phase float64) []byte {
	t.Helper()
	c := newTestCanvas(t, 64, 64)
	n := &ui.Node{
		Kind:        ui.KindEffect,
		Bounds:      ui.Rect{W: 64, H: 64},
		Effect:      spec,
		EffectPhase: phase,
	}
	if err := paintEffect(c, n, testStyle); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), c.Pix...)
}

func paintWeatherVariantFrame(t *testing.T, variant ui.EffectVariant, phase float64, width, height int) []byte {
	t.Helper()
	spec := rainSpec(7)
	spec.Variant = variant
	return paintWeatherVariantFrameWithSpec(t, spec, phase, width, height)
}

func paintWeatherVariantFrameWithSpec(t *testing.T, spec ui.EffectSpec, phase float64, width, height int) []byte {
	t.Helper()
	c := newTestCanvas(t, width, height)
	fillRect(c, ui.Rect{W: width, H: height}, testStyle.Capsule)
	n := &ui.Node{Kind: ui.KindEffect, Bounds: ui.Rect{W: width, H: height}, Effect: spec, EffectPhase: phase}
	if err := paintEffect(c, n, testStyle); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), c.Pix...)
}

func meaningfulWeatherPixels(pixels []byte, width int, region ui.Rect, base Color, threshold uint8) int {
	count := 0
	for y := max(region.Y, 0); y < min(region.Y+region.H, len(pixels)/(width*4)); y++ {
		for x := max(region.X, 0); x < min(region.X+region.W, width); x++ {
			i := y*width*4 + x*4
			if weatherColorDistance(pixels[i:i+4], base) >= threshold {
				count++
			}
		}
	}
	return count
}

func differingWeatherPixels(first, second []byte, width int, region ui.Rect, threshold uint8) int {
	count := 0
	for y := max(region.Y, 0); y < min(region.Y+region.H, len(first)/(width*4)); y++ {
		for x := max(region.X, 0); x < min(region.X+region.W, width); x++ {
			i := y*width*4 + x*4
			if max(absWeatherByte(first[i], second[i]), max(absWeatherByte(first[i+1], second[i+1]), absWeatherByte(first[i+2], second[i+2]))) >= threshold {
				count++
			}
		}
	}
	return count
}

func weatherColorDistance(pixel []byte, base Color) uint8 {
	return uint8(max(absWeatherByte(pixel[0], base.B), max(absWeatherByte(pixel[1], base.G), absWeatherByte(pixel[2], base.R))))
}

func absWeatherByte(a, b byte) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}

func meanWeatherColor(pixels []byte, width int, region ui.Rect) [3]byte {
	var sums [3]int
	count := 0
	for y := max(region.Y, 0); y < min(region.Y+region.H, len(pixels)/(width*4)); y++ {
		for x := max(region.X, 0); x < min(region.X+region.W, width); x++ {
			i := y*width*4 + x*4
			// Canvas pixels are BGRA; return RGB for the test helpers.
			sums[0] += int(pixels[i+2])
			sums[1] += int(pixels[i+1])
			sums[2] += int(pixels[i])
			count++
		}
	}
	if count == 0 {
		return [3]byte{}
	}
	return [3]byte{byte(sums[0] / count), byte(sums[1] / count), byte(sums[2] / count)}
}

func weatherRGBDistance(a, b [3]byte) uint8 {
	return uint8(max(absWeatherByte(a[0], b[0]), max(absWeatherByte(a[1], b[1]), absWeatherByte(a[2], b[2]))))
}

func weatherDifferenceBounds(pixels []byte, width int, base Color, threshold uint8) (ui.Rect, bool) {
	height := len(pixels) / (width * 4)
	minX, minY, maxX, maxY := width, height, -1, -1
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := y*width*4 + x*4
			if weatherColorDistance(pixels[i:i+4], base) < threshold {
				continue
			}
			minX, minY = min(minX, x), min(minY, y)
			maxX, maxY = max(maxX, x), max(maxY, y)
		}
	}
	if maxX < minX || maxY < minY {
		return ui.Rect{}, false
	}
	return ui.Rect{X: minX, Y: minY, W: maxX - minX + 1, H: maxY - minY + 1}, true
}

func BenchmarkPaintWeatherEffect(b *testing.B) {
	for _, size := range []struct {
		name   string
		width  int
		height int
	}{
		{name: "card", width: 256, height: 160},
		{name: "landscape", width: 720, height: 400},
	} {
		b.Run(size.name, func(b *testing.B) {
			c, err := NewCanvas(make([]byte, size.width*size.height*4), size.width, size.height, size.width*4)
			if err != nil {
				b.Fatal(err)
			}
			n := &ui.Node{
				Kind:        ui.KindEffect,
				Bounds:      ui.Rect{W: size.width, H: size.height},
				Effect:      rainSpec(7),
				EffectPhase: .25,
			}
			for i := 0; i < b.N; i++ {
				clear(c.Pix)
				if err := paintEffect(c, n, testStyle); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
