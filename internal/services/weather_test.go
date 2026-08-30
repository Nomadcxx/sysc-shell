package services

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// currentWeatherBody is the shape Open-Meteo returns for the requested fields.
const currentWeatherBody = `{"current":{"temperature_2m":18.4,"weather_code":3}}`

// weatherAt points a service at a test server and leases it at a short
// interval so one fetch happens promptly. The production fetch floor is
// disabled here so a test that needs two fetches is not parked for 30s.
func weatherAt(t *testing.T, handler http.Handler) (*Weather, *Lease) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	w := NewWeather(0, 0, UnitCelsius)
	w.endpoint = server.URL
	w.minInterval = 0
	t.Cleanup(w.Close)

	lease, err := w.Acquire(50 * time.Millisecond)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(lease.Release)
	return w, lease
}

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

func TestASuccessfulFetchPublishesAnObservation(t *testing.T) {
	t.Parallel()
	w, _ := weatherAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprint(rw, currentWeatherBody)
	}))

	select {
	case reading := <-w.Updates():
		if !reading.Observed {
			t.Fatal("a successful fetch published an unobserved reading")
		}
		if reading.Temperature != 18.4 || reading.Code != 3 {
			t.Fatalf("reading = %+v, want 18.4 and code 3", reading)
		}
		if !reading.FailedSince.IsZero() {
			t.Fatal("a successful fetch reported a failure")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no reading arrived within three seconds")
	}
}

// The request must carry the configured coordinates and unit, and ask for
// only the fields the bar renders.
func TestTheRequestAsksForOnlyWhatTheBarRenders(t *testing.T) {
	t.Parallel()
	queries := make(chan string, 1)
	w, _ := weatherAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		select {
		case queries <- r.URL.RawQuery:
		default:
		}
		fmt.Fprint(rw, currentWeatherBody)
	}))
	<-w.Updates()

	query := <-queries
	current, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got := current.Get("current"); got != "temperature_2m,weather_code" {
		t.Fatalf("current = %q, want temperature_2m,weather_code", got)
	}
	if current.Get("latitude") == "" || current.Get("longitude") == "" {
		t.Fatalf("query %q is missing coordinates", query)
	}
	for _, unwanted := range []string{"daily", "forecast_days", "relative_humidity", "wind_speed"} {
		if current.Has(unwanted) {
			t.Fatalf("query %q requests %q, which no widget renders", query, unwanted)
		}
	}
}

// A failure after a success must keep the observation and only mark it stale.
func TestAFailurePreservesTheLastGoodReading(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	w, _ := weatherAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			fmt.Fprint(rw, currentWeatherBody)
			return
		}
		http.Error(rw, "unavailable", http.StatusServiceUnavailable)
	}))

	first := <-w.Updates()
	if !first.Observed {
		t.Fatal("the first fetch did not observe")
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case reading := <-w.Updates():
			if !reading.Stale() {
				continue
			}
			if reading.Temperature != 18.4 {
				t.Fatalf("a stale reading lost its observation: %+v", reading)
			}
			return
		case <-deadline:
			t.Fatal("no stale reading arrived after the server began failing")
		}
	}
}

// A server that never responds must not outlive the budget.
func TestAStalledServerFailsWithinTheBudget(t *testing.T) {
	t.Parallel()
	w, _ := weatherAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))

	select {
	case reading := <-w.Updates():
		if reading.Observed {
			t.Fatal("a stalled server produced an observation")
		}
		if reading.FailedSince.IsZero() {
			t.Fatal("a stalled fetch did not report a failure")
		}
	case <-time.After(connectAndReadBudget + 4*time.Second):
		t.Fatal("a stalled fetch outlived its budget")
	}
}

// An oversized body must be rejected rather than buffered.
func TestAnOversizedResponseIsRejected(t *testing.T) {
	t.Parallel()
	w, _ := weatherAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Write([]byte(`{"current":{"temperature_2m":1,"weather_code":0,"pad":"`))
		chunk := strings.Repeat("x", 4096)
		for written := 0; written < maxResponseBytes+8192; written += len(chunk) {
			rw.Write([]byte(chunk))
		}
	}))

	select {
	case reading := <-w.Updates():
		if reading.Observed {
			t.Fatal("an oversized response produced an observation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no reading arrived for an oversized response")
	}
}

func TestBackoffIsBounded(t *testing.T) {
	t.Parallel()
	for attempt, want := range map[int]time.Duration{
		0: retryDelay,
		1: retryDelay,
		2: retryDelay,
		3: backoffBase,
		4: 2 * backoffBase,
		9: backoffCap,
	} {
		if got := retryAfter(attempt); got != want {
			t.Fatalf("retryAfter(%d) = %v, want %v", attempt, got, want)
		}
	}
}

// However short the lease interval, fetches cannot exceed the floor. Without
// this a one-second widget interval would issue sixty requests a minute.
func TestTheMinimumFetchFloorHolds(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(rw, currentWeatherBody)
	}))
	t.Cleanup(server.Close)

	w := NewWeather(0, 0, UnitCelsius)
	w.endpoint = server.URL
	t.Cleanup(w.Close)

	lease, err := w.Acquire(time.Millisecond)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release()

	// Wait well past several lease intervals but far short of the floor.
	time.Sleep(2 * time.Second)

	if got := calls.Load(); got > 1 {
		t.Fatalf("the server was called %d times inside the fetch floor, want at most 1", got)
	}
}
