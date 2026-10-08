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
