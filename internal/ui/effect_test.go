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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.spec.Validate() == nil; got != tt.ok {
				t.Fatalf("Validate() = %v, want %v", got, tt.ok)
			}
		})
	}
}
