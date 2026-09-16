package render

import (
	"bytes"
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

func BenchmarkPaintWeatherEffect(b *testing.B) {
	c, err := NewCanvas(make([]byte, 256*160*4), 256, 160, 256*4)
	if err != nil {
		b.Fatal(err)
	}
	n := &ui.Node{
		Kind:        ui.KindEffect,
		Bounds:      ui.Rect{W: 256, H: 160},
		Effect:      rainSpec(7),
		EffectPhase: .25,
	}
	for i := 0; i < b.N; i++ {
		clear(c.Pix)
		if err := paintEffect(c, n, testStyle); err != nil {
			b.Fatal(err)
		}
	}
}
