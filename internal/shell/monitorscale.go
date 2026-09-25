package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// monitorMetric names a reading that carries activity and critical thresholds.
type monitorMetric int

const (
	metricCPU monitorMetric = iota
	metricCPUTemp
	metricGPU
	metricGPUTemp
	metricMemory
	metricStorage
)

// monitorThresholds are Noctalia's activity and critical defaults, in percent
// or degrees Celsius. They are fixed: configuration would be a surface with
// no consumer.
var monitorThresholds = map[monitorMetric][2]float64{
	metricCPU:     {50, 90},
	metricCPUTemp: {60, 85},
	metricGPU:     {50, 90},
	metricGPUTemp: {60, 85},
	metricMemory:  {60, 90},
	metricStorage: {80, 95},
}

// Rate scale floors keep idle noise flat instead of drawing a blip as a
// saturated link.
const (
	networkRateFloor = 64 << 10
	diskRateFloor    = 1 << 20
	// temperatureFloor and temperatureCeiling fix the temperature scale so a
	// warm room does not rescale the line.
	temperatureFloor   = 20.0
	temperatureCeiling = 100.0
)

func thresholdTone(m monitorMetric, value float64) ui.Tone {
	t := monitorThresholds[m]
	switch {
	case value >= t[1]:
		return ui.ToneError
	case value >= t[0]:
		return ui.ToneActivity
	}
	return ui.ToneNormal
}

// rateCeiling is the shared full-scale value for one rate chart: the window's
// peak with ten per cent headroom, never below the floor.
func rateCeiling(floor float64, series ...[]float64) float64 {
	peak := 0.0
	for _, s := range series {
		for _, v := range s {
			peak = max(peak, v)
		}
	}
	return max(peak*1.1, floor)
}

func scaleSeries(values []float64, ceiling float64) []float64 {
	if len(values) == 0 || ceiling <= 0 {
		return nil
	}
	out := make([]float64, len(values))
	for i, v := range values {
		out[i] = min(max(v/ceiling, 0), 1)
	}
	return out
}

// temperatureSeries rescales the metrics ring's °C/100 samples onto the fixed
// 20–100 °C chart range.
func temperatureSeries(fractions []float64) []float64 {
	if len(fractions) == 0 {
		return nil
	}
	out := make([]float64, len(fractions))
	span := temperatureCeiling - temperatureFloor
	for i, f := range fractions {
		out[i] = min(max((f*100-temperatureFloor)/span, 0), 1)
	}
	return out
}

// peakCaption names an auto-scaled chart's magnitude. A rate chart without
// one misstates it.
func peakCaption(series ...[]float64) string {
	peak, seen := 0.0, false
	for _, s := range series {
		for _, v := range s {
			peak, seen = max(peak, v), true
		}
	}
	if !seen {
		return ""
	}
	return "peak " + formatRate(peak)
}

// meanCoreGHz averages the cores that reported a frequency.
func meanCoreGHz(snap services.Snapshot) (float64, bool) {
	if snap.CPU == nil {
		return 0, false
	}
	sum, n := 0.0, 0
	for _, core := range snap.CPU.Cores {
		if core.FrequencyValid {
			sum += float64(core.FrequencyHz)
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n) / 1e9, true
}
