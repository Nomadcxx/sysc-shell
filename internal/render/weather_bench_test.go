package render

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

var weatherSheetDir = flag.String("sheet", "", "write a weather contact sheet to this directory")

var weatherVariants = []struct {
	name    string
	variant ui.EffectVariant
}{
	{"clear", ui.WeatherClear}, {"partly", ui.WeatherPartlyCloudy}, {"cloudy", ui.WeatherCloudy},
	{"fog", ui.WeatherFog}, {"rain", ui.WeatherRain}, {"snow", ui.WeatherSnow},
	{"heavysnow", ui.WeatherHeavySnow}, {"storm", ui.WeatherThunderstorm},
}

// Physical sizes at the laptop's scale 1.25: the Weather page hero and the
// Home B1 clock card.
var weatherSizes = []struct {
	name string
	w, h int
}{{"hero", 745, 420}, {"home", 444, 123}}

func weatherNode(v ui.EffectVariant, daylight float64, w, h int, phase float64) *ui.Node {
	return &ui.Node{Kind: ui.KindEffect, Bounds: ui.Rect{W: w, H: h}, EffectPhase: phase, Effect: ui.EffectSpec{
		Program: ui.EffectWeather, Variant: v, Seed: 0x534f4d455f574541, Intensity: .72, Speed: 1, Daylight: daylight, SceneBias: 1}}
}

func BenchmarkWeatherFrame(b *testing.B) {
	for _, v := range weatherVariants {
		for _, s := range weatherSizes {
			b.Run(v.name+"/"+s.name, func(b *testing.B) {
				c, err := NewCanvas(make([]byte, s.w*s.h*4), s.w, s.h, s.w*4)
				if err != nil {
					b.Fatal(err)
				}
				n := weatherNode(v.variant, 1, s.w, s.h, .3)
				_ = paintEffect(c, n, testStyle) // warm the sprite cache
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					n.EffectPhase = float64(i%100) / 100
					if err := paintEffect(c, n, testStyle); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkWeatherBake(b *testing.B) {
	for _, v := range weatherVariants {
		b.Run(v.name, func(b *testing.B) {
			c, err := NewCanvas(make([]byte, 745*420*4), 745, 420, 745*4)
			if err != nil {
				b.Fatal(err)
			}
			n := weatherNode(v.variant, 1, 745, 420, .3)
			for i := 0; i < b.N; i++ {
				weatherSpriteReset()
				if err := paintEffect(c, n, testStyle); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestWeatherContactSheet(t *testing.T) {
	if *weatherSheetDir == "" {
		t.Skip("pass -sheet=DIR to write the contact sheet")
	}
	for _, v := range weatherVariants {
		for _, d := range []struct {
			name     string
			daylight float64
		}{{"day", 1}, {"dusk", .5}, {"night", 0}} {
			for _, s := range weatherSizes {
				c := newTestCanvas(t, s.w, s.h)
				if err := paintEffect(c, weatherNode(v.variant, d.daylight, s.w, s.h, .3), testStyle); err != nil {
					t.Fatal(err)
				}
				img := image.NewNRGBA(image.Rect(0, 0, s.w, s.h))
				for y := 0; y < s.h; y++ {
					for x := 0; x < s.w; x++ {
						p := pixelAt(t, c, x, y)
						img.Set(x, y, color.NRGBA{R: p.R, G: p.G, B: p.B, A: 255})
					}
				}
				path := filepath.Join(*weatherSheetDir, fmt.Sprintf("%s-%s-%s.png", v.name, d.name, s.name))
				f, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := png.Encode(f, img); err != nil {
					f.Close()
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
