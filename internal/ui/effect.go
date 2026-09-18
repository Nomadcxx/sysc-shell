package ui

import (
	"fmt"
	"math"
)

// EffectProgram identifies a host-owned effect catalogue entry.
type EffectProgram uint8

const (
	EffectNone EffectProgram = iota
	EffectWeather
)

// EffectVariant selects a weather condition within the weather program.
type EffectVariant uint8

const (
	WeatherClear EffectVariant = iota
	WeatherPartlyCloudy
	WeatherCloudy
	WeatherFog
	WeatherRain
	WeatherSnow
	WeatherHeavySnow
	WeatherThunderstorm
)

// EffectSpec is the validated, host-owned request for one effect layer.
type EffectSpec struct {
	Program EffectProgram
	Variant EffectVariant
	// Night selects the nocturnal form for weather variants that have a
	// celestial layer. It is explicit because a weather code alone does not
	// carry day/night state.
	Night     bool
	Seed      uint64
	Intensity float64
	Speed     float64
	// SceneBias places the aspect-locked scene horizontally inside the effect
	// box: zero centres it and minus one puts it hard against the leading
	// edge. The host picks it from the hero composition, so the renderer does
	// not re-derive the composition rule from the box it is handed.
	SceneBias float64
}

// Validate checks the bounds accepted by the host effect catalogue.
func (s EffectSpec) Validate() error {
	if s.Program != EffectWeather {
		return fmt.Errorf("ui: unsupported effect program %d", s.Program)
	}
	if s.Variant > WeatherThunderstorm {
		return fmt.Errorf("ui: unsupported weather variant %d", s.Variant)
	}
	if !finiteInRange(s.Intensity, 0, 1) {
		return fmt.Errorf("ui: effect intensity %v is outside zero through one", s.Intensity)
	}
	if !finiteInRange(s.Speed, 0, 4) {
		return fmt.Errorf("ui: effect speed %v is outside zero through four", s.Speed)
	}
	if !finiteInRange(s.SceneBias, -1, 1) {
		return fmt.Errorf("ui: effect scene bias %v is outside minus one through one", s.SceneBias)
	}
	return nil
}

func finiteInRange(value, lo, hi float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= lo && value <= hi
}
