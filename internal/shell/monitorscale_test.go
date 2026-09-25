package shell

import (
	"testing"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestThresholdTone(t *testing.T) {
	for _, tc := range []struct {
		m    monitorMetric
		v    float64
		want ui.Tone
	}{
		{metricCPU, 49.9, ui.ToneNormal}, {metricCPU, 50, ui.ToneActivity}, {metricCPU, 90, ui.ToneError},
		{metricCPUTemp, 59, ui.ToneNormal}, {metricCPUTemp, 60, ui.ToneActivity}, {metricCPUTemp, 85, ui.ToneError},
		{metricGPU, 50, ui.ToneActivity}, {metricGPUTemp, 85, ui.ToneError},
		{metricMemory, 59, ui.ToneNormal}, {metricMemory, 60, ui.ToneActivity}, {metricMemory, 90, ui.ToneError},
		{metricStorage, 79, ui.ToneNormal}, {metricStorage, 80, ui.ToneActivity}, {metricStorage, 95, ui.ToneError},
	} {
		if got := thresholdTone(tc.m, tc.v); got != tc.want {
			t.Errorf("thresholdTone(%d, %v) = %d, want %d", tc.m, tc.v, got, tc.want)
		}
	}
}

func TestRateCeiling(t *testing.T) {
	for _, tc := range []struct {
		name   string
		floor  float64
		series [][]float64
		want   float64
	}{
		{"idle stays on the floor", networkRateFloor, [][]float64{{0, 0, 0}}, networkRateFloor},
		{"blip under the floor", networkRateFloor, [][]float64{{2048}}, networkRateFloor},
		{"peak gets headroom", networkRateFloor, [][]float64{{1 << 20}}, 1.1 * (1 << 20)},
		{"series share one scale", diskRateFloor, [][]float64{{1 << 20}, {10 << 20}}, 1.1 * (10 << 20)},
		{"no samples", diskRateFloor, nil, diskRateFloor},
	} {
		if got := rateCeiling(tc.floor, tc.series...); got != tc.want {
			t.Errorf("%s: rateCeiling = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestScaleSeriesAndTemperature(t *testing.T) {
	got := scaleSeries([]float64{0, 50, 200}, 100)
	if got[0] != 0 || got[1] != 0.5 || got[2] != 1 {
		t.Fatalf("scaleSeries = %v", got)
	}
	temps := temperatureSeries([]float64{0.10, 0.20, 0.60, 1.00, 1.20})
	want := []float64{0, 0, 0.5, 1, 1}
	for i := range want {
		if temps[i] != want[i] {
			t.Fatalf("temperatureSeries = %v, want %v", temps, want)
		}
	}
	if scaleSeries(nil, 100) != nil {
		t.Fatal("scaleSeries(nil) is not nil")
	}
}

func TestPeakCaption(t *testing.T) {
	if got := peakCaption([]float64{0, 1 << 20}, []float64{3 << 20}); got != "peak "+formatRate(3<<20) {
		t.Fatalf("peakCaption = %q", got)
	}
	if got := peakCaption(nil); got != "" {
		t.Fatalf("empty peakCaption = %q, want empty", got)
	}
}

func TestMeanCoreGHz(t *testing.T) {
	snap := services.Snapshot{CPU: &metrics.CPUSnapshot{Cores: []metrics.CPUCore{
		{FrequencyHz: 4_000_000_000, FrequencyValid: true},
		{FrequencyHz: 3_000_000_000, FrequencyValid: true},
		{FrequencyHz: 9_000_000_000, FrequencyValid: false},
	}}}
	if got, ok := meanCoreGHz(snap); !ok || got != 3.5 {
		t.Fatalf("meanCoreGHz = %v, %v", got, ok)
	}
	if _, ok := meanCoreGHz(services.Snapshot{}); ok {
		t.Fatal("no CPU snapshot reported a frequency")
	}
}
