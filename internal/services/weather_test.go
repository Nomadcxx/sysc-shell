package services

import (
	"strings"
	"testing"
	"time"
)

func TestTheFirstWeatherLeaseStartsAndTheLastStops(t *testing.T) {
	t.Parallel()
	w := NewWeather(0, 0, UnitCelsius)
	t.Cleanup(w.Close)

	if w.Running() {
		t.Fatal("a service with no lease is running")
	}

	first, err := w.Acquire(15 * time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !w.Running() {
		t.Fatal("the first lease did not start the service")
	}

	second, err := w.Acquire(15 * time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if got := w.Starts(); got != 1 {
		t.Fatalf("starts = %d, want 1; a second consumer must share the goroutine", got)
	}

	first.Release()
	if !w.Running() {
		t.Fatal("releasing one of two leases stopped the service")
	}
	second.Release()
	if w.Running() {
		t.Fatal("releasing the last lease left the service running")
	}
}

func TestANonPositiveWeatherIntervalIsRejected(t *testing.T) {
	t.Parallel()
	w := NewWeather(0, 0, UnitCelsius)
	t.Cleanup(w.Close)

	if _, err := w.Acquire(0); err == nil {
		t.Fatal("a zero interval was accepted")
	}
	if w.Running() {
		t.Fatal("a rejected acquire started the service")
	}
}

// A reload acquires the replacement lease before releasing the outgoing one,
// so a service in continuous use must never restart.
func TestAcquireBeforeReleaseDoesNotRestartTheWeatherService(t *testing.T) {
	t.Parallel()
	w := NewWeather(0, 0, UnitCelsius)
	t.Cleanup(w.Close)

	outgoing, err := w.Acquire(15 * time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	incoming, err := w.Acquire(10 * time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	outgoing.Release()

	if got := w.Starts(); got != 1 {
		t.Fatalf("starts = %d, want 1; the service restarted across a reload", got)
	}
	incoming.Release()
}

func TestClosingTheWeatherServiceStopsTheGoroutine(t *testing.T) {
	t.Parallel()
	w := NewWeather(0, 0, UnitCelsius)
	if _, err := w.Acquire(time.Minute); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	w.Close()

	if w.Running() {
		t.Fatal("Close left the service running")
	}
	// Close must be safe to call twice; shutdown paths may each reach it.
	w.Close()
}

// Coordinates and unit are the request, so a reload has to be able to change
// them. Without this the service fetches the city it started with for the life
// of the process, however often the configuration is reloaded.
func TestReconfiguringChangesTheRequestWithoutRestarting(t *testing.T) {
	t.Parallel()
	w := NewWeather(0, 0, UnitCelsius)
	t.Cleanup(w.Close)

	lease, err := w.Acquire(time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release()

	w.Reconfigure(51.5, -0.13, UnitFahrenheit)

	if got := w.Starts(); got != 1 {
		t.Fatalf("starts = %d after Reconfigure, want 1; the service restarted", got)
	}
	if !w.Running() {
		t.Fatal("Reconfigure stopped a service that is still leased")
	}
	url := w.requestURL()
	for _, want := range []string{"latitude=51.5", "longitude=-0.13", "temperature_unit=fahrenheit"} {
		if !strings.Contains(url, want) {
			t.Fatalf("request %q does not carry %q", url, want)
		}
	}
}

// An unrelated reload calls Reconfigure with what the service already has, so
// the no-op path must not disturb a fetch that is due.
func TestReconfiguringToTheSameRequestIsANoOp(t *testing.T) {
	t.Parallel()
	w := NewWeather(51.5, -0.13, UnitCelsius)
	t.Cleanup(w.Close)

	before := w.requestURL()
	w.Reconfigure(51.5, -0.13, UnitCelsius)

	if after := w.requestURL(); after != before {
		t.Fatalf("request changed from %q to %q on an identical reconfigure", before, after)
	}
	select {
	case <-w.rearm:
		t.Fatal("an identical reconfigure re-armed the fetch")
	default:
	}
}
