package ui

import (
	"math"
	"testing"
)

func TestEffectSpecValidation(t *testing.T) {
	tests := []struct {
		name string
		spec EffectSpec
		ok   bool
	}{
		{"weather", EffectSpec{Program: EffectWeather, Intensity: 0.7, Speed: 1}, true},
		{"unknown program", EffectSpec{Program: EffectProgram(99)}, false},
		{"nan intensity", EffectSpec{Program: EffectWeather, Intensity: math.NaN()}, false},
		{"intensity above one", EffectSpec{Program: EffectWeather, Intensity: 1.1}, false},
		{"negative speed", EffectSpec{Program: EffectWeather, Speed: -1}, false},
		{"leading-edge scene bias", EffectSpec{Program: EffectWeather, Intensity: .7, Speed: 1, SceneBias: -1}, true},
		{"trailing-edge scene bias", EffectSpec{Program: EffectWeather, Intensity: .7, Speed: 1, SceneBias: 1}, true},
		{"scene bias below minus one", EffectSpec{Program: EffectWeather, Intensity: .7, Speed: 1, SceneBias: -1.01}, false},
		{"scene bias above one", EffectSpec{Program: EffectWeather, Intensity: .7, Speed: 1, SceneBias: 1.01}, false},
		{"nan scene bias", EffectSpec{Program: EffectWeather, Intensity: .7, Speed: 1, SceneBias: math.NaN()}, false},
		{"inf scene bias", EffectSpec{Program: EffectWeather, Intensity: .7, Speed: 1, SceneBias: math.Inf(1)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.spec.Validate() == nil; got != tt.ok {
				t.Fatalf("Validate() = %v, want %v", got, tt.ok)
			}
		})
	}
}

func TestEffectSpecValidatesDaylight(t *testing.T) {
	ok := EffectSpec{Program: EffectWeather, Variant: WeatherClear, Intensity: .5, Speed: 1}
	for _, d := range []float64{0, .5, 1} {
		ok.Daylight = d
		if err := ok.Validate(); err != nil {
			t.Errorf("daylight %v rejected: %v", d, err)
		}
	}
	for _, d := range []float64{-.01, 1.01, math.NaN(), math.Inf(1)} {
		ok.Daylight = d
		if err := ok.Validate(); err == nil {
			t.Errorf("daylight %v accepted", d)
		}
	}
}
