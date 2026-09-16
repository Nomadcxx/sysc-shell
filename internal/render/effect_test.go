package render

import (
	"bytes"
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
	n := &ui.Node{Kind: ui.KindEffect, Effect: ui.EffectSpec{Program: ui.EffectProgram(99)}}
	if err := paintEffect(c, n, testStyle); err == nil {
		t.Fatal("invalid effect spec was painted")
	}
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
