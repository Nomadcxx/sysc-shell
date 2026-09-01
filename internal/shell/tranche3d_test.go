package shell

import (
	"runtime"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// The whole tranche's goroutines must stop with the registry.
func TestClosingTheRegistryStopsTheWeatherGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()

	reg := NewRegistry(weatherConfig())
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})
	reg.Close()

	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > before {
		t.Fatalf("goroutines = %d after Close, want at most the starting %d", got, before)
	}
}

// An accepted reload must not restart a weather service still in use.
func TestAnAcceptedReloadDoesNotRestartTheWeatherService(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(weatherConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	if got := reg.Weather().Starts(); got != 1 {
		t.Fatalf("starts = %d before reload, want 1", got)
	}

	prepared, err := reg.PrepareConfig(weatherConfig(), identities(map[uint32]string{1: "DP-9"}))
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	prepared.Commit()

	if got := reg.Weather().Starts(); got != 1 {
		t.Fatalf("starts = %d after reload, want 1; the service restarted", got)
	}
}

// A stale reading must keep rendering rather than blanking the widget.
func TestAStaleReadingKeepsTheWidgetRendering(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(weatherConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	reg.UpdateWeather(services.Reading{
		Observed: true, Temperature: 18, Unit: services.UnitCelsius,
		FetchedAt:   time.Now().Add(-2 * time.Hour),
		FailedSince: time.Now().Add(-time.Hour),
	})

	if got := reg.bars[1].left[0].inner.Text; got == noWorkspace || got == "" {
		t.Fatalf("a stale reading rendered %q, want the aged value", got)
	}
}
