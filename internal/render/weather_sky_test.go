package render

import (
	"math"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestWeatherSkyStopsMatchTheSpecTableAndPull(t *testing.T) {
	style := testStyle
	style.Container = Color{R: 0, G: 0, B: 0, A: 255}
	day := weatherSkyStops(weatherClear, 1, style)
	want := LerpColor(weatherHex("#2a63cf"), style.Container, weatherThemePull)
	if day[0] != want {
		t.Fatalf("clear day top = %+v, want %+v (spec stop pulled 15%%)", day[0], want)
	}
	night := weatherSkyStops(weatherClear, 0, style)
	if night[0] != LerpColor(weatherHex("#050925"), style.Container, weatherThemePull) {
		t.Fatalf("clear night top = %+v", night[0])
	}
	dusk := weatherSkyStops(weatherClear, .5, style)
	if dusk[2].R <= dusk[2].B {
		t.Fatalf("mid-twilight horizon %+v is not warm", dusk[2])
	}
	storm := weatherSkyStops(weatherThunderstorm, .5, style)
	clear := weatherSkyStops(weatherClear, .5, style)
	if int(storm[2].R)-int(storm[2].B) >= int(clear[2].R)-int(clear[2].B) {
		t.Fatal("a storm's twilight glow is as strong as a clear sky's")
	}
}

func TestWeatherBandPeaksAtTwilight(t *testing.T) {
	for _, tc := range []struct{ d, want float64 }{{0, 0}, {1, 0}, {.5, 1}, {.25, .5}} {
		if got := weatherBand(tc.d); got != tc.want {
			t.Errorf("band(%v) = %v, want %v", tc.d, got, tc.want)
		}
	}
}

func TestWeatherStarsOnlyAtNightAndClear(t *testing.T) {
	paint := func(kind ui.EffectVariant, daylight float64) int {
		c := newTestCanvas(t, 120, 80)
		mask := fullMask(120, 80)
		spec := rainSpec(7)
		spec.Variant, spec.Daylight = kind, daylight
		paintWeatherStars(c, ui.Rect{W: 120, H: 80}, mask, spec, .3)
		n := 0
		for i := 3; i < len(c.Pix); i += 4 {
			if c.Pix[i] != 0 {
				n++
			}
		}
		return n
	}
	if paint(ui.WeatherClear, 0) == 0 {
		t.Error("clear night has no stars")
	}
	if paint(ui.WeatherClear, 1) != 0 || paint(ui.WeatherRain, 0) != 0 {
		t.Error("stars painted by day or behind rain")
	}
}

func TestWeatherCelestialCrossfadesAndSunSinksAtTwilight(t *testing.T) {
	brightRow := func(daylight float64) int {
		const w, h = 160, 100
		c := newTestCanvas(t, w, h)
		spec := rainSpec(7)
		spec.Variant, spec.Daylight, spec.SceneBias = ui.WeatherClear, daylight, 0
		paintWeatherCelestial(c, ui.Rect{W: w, H: h}, fullMask(w, h), spec, .2)
		best, row := 0, -1
		for y := 0; y < h; y++ {
			sum := 0
			for x := 0; x < w; x++ {
				sum += int(pixelAt(t, c, x, y).A)
			}
			if sum > best {
				best, row = sum, y
			}
		}
		return row
	}
	noon, sunset := brightRow(1), brightRow(.5)
	if noon < 0 || sunset <= noon {
		t.Fatalf("sun centre row noon=%d sunset=%d, want the sunset sun lower", noon, sunset)
	}
}

func TestWeatherFlashIsAPulseOncePerLoop(t *testing.T) {
	spec := rainSpec(7)
	spec.Variant = ui.WeatherThunderstorm
	peak := 0.0
	for i := 0; i < 900; i++ {
		peak = math.Max(peak, weatherFlash(spec, float64(i)/900))
	}
	if peak < .9 {
		t.Fatalf("flash peak %.2f, want a real strike", peak)
	}
	if weatherFlash(spec, .05) != 0 {
		t.Fatal("flash is lit outside its pulse")
	}
	spec.Variant = ui.WeatherRain
	if weatherFlash(spec, .47) != 0 {
		t.Fatal("rain flashes")
	}
}

func TestWeatherCloudsAreLitAboveAndShadedBelow(t *testing.T) {
	const w, h = 200, 120
	c := newTestCanvas(t, w, h)
	spec := rainSpec(7)
	spec.Variant, spec.Daylight, spec.Intensity = ui.WeatherCloudy, 1, 1
	paintWeatherClouds(c, ui.Rect{W: w, H: h}, fullMask(w, h), testStyle, spec, weatherCloudy, .3, false, 0)
	paintWeatherClouds(c, ui.Rect{W: w, H: h}, fullMask(w, h), testStyle, spec, weatherCloudy, .3, true, 0)
	upper := meanWeatherColor(c.Pix, w, ui.Rect{W: w, H: h / 3})
	lower := meanWeatherColor(c.Pix, w, ui.Rect{Y: h / 2, W: w, H: h / 3})
	if upper == [3]byte{} || lower == [3]byte{} {
		t.Fatal("clouds painted nothing")
	}
	lit, shade := weatherCloudColours(weatherCloudy, 1, testStyle)
	if int(lit.R)+int(lit.G)+int(lit.B) <= int(shade.R)+int(shade.G)+int(shade.B) {
		t.Fatalf("lit %+v is not brighter than shade %+v", lit, shade)
	}
	a, _ := weatherCloudColours(weatherCloudy, .51, testStyle)
	b, _ := weatherCloudColours(weatherCloudy, .54, testStyle)
	if a != b {
		t.Fatal("cloud palette is not quantised; every frame of a dusk would rebake")
	}
}

func TestWeatherNearRainStreaksAreLargerThanFarOnes(t *testing.T) {
	const w, h = 160, 120
	litPixels := func(layer int) int {
		c := newTestCanvas(t, w, h)
		spec := rainSpec(7)
		spec.Intensity = 1
		paintWeatherRain(c, ui.Rect{W: w, H: h}, fullMask(w, h), spec, .3, layer)
		n := 0
		for i := 3; i < len(c.Pix); i += 4 {
			if c.Pix[i] != 0 {
				n++
			}
		}
		return n
	}
	particles := func(layer int) int {
		n := 0
		for i := 0; i < weatherRainCount; i++ {
			if weatherDepth(7, i) == layer {
				n++
			}
		}
		return n
	}
	far, near := litPixels(0), litPixels(2)
	if far == 0 || near == 0 {
		t.Fatal("a rain layer painted nothing")
	}
	// Near streaks are longer and wider (defocused), so each covers more of
	// the card than a far one.
	if near/max(particles(2), 1) <= far/max(particles(0), 1) {
		t.Errorf("near rain covers %d px per streak, far %d; near should be larger",
			near/max(particles(2), 1), far/max(particles(0), 1))
	}
}

func TestWeatherPrecipitationLeavesTheCardBeforeItWraps(t *testing.T) {
	// A particle wraps from the bottom back to the top. If any of it is still
	// on the card at that instant, it pops out of existence mid-fall.
	const w, h = 160, 120
	for _, v := range []float64{0, math.Nextafter(1, 0)} {
		if y0, y1 := weatherRainSpan(v, h, weatherRainLayers[2].length*h); y1 > 0 && y0 < h {
			t.Errorf("near rain at v=%v spans rows %.1f..%.1f, inside the card", v, y0, y1)
		}
		r := weatherFlakeRadius(h, 2, true)
		if y := weatherSnowY(v, h, r); y+r > 0 && y-r < h {
			t.Errorf("near flake at v=%v is centred on row %.1f, inside the card", v, y)
		}
	}
}
