package weather

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	owm "github.com/Nomadcxx/sysc-shell/weather"
)

const forecastBody = `{
  "current":{"temperature_2m":18.4,"weather_code":3},
  "daily":{
    "time":["2026-09-02","2026-09-03","2026-09-04","2026-09-05","2026-09-06","2026-09-07","2026-09-08"],
    "weather_code":[3,61,71,95,0,2,45],
    "temperature_2m_max":[22.1,19.0,8.5,17.0,24.0,21.0,16.0],
    "temperature_2m_min":[12.0,11.0,-1.5,9.0,13.0,12.5,10.0],
    "sunrise":["2026-09-02T06:12","2026-09-03T06:14","2026-09-04T06:16","2026-09-05T06:18","2026-09-06T06:20","2026-09-07T06:22","2026-09-08T06:24"],
    "sunset":["2026-09-02T18:44","2026-09-03T18:42","2026-09-04T18:40","2026-09-05T18:38","2026-09-06T18:36","2026-09-07T18:34","2026-09-08T18:32"]
  }
}`

func TestConfigRejectsOutOfRangeCoordinates(t *testing.T) {
	t.Parallel()
	for _, values := range []map[string]any{
		{"latitude": 91.0, "longitude": 0.0},
		{"latitude": -91.0, "longitude": 0.0},
		{"latitude": 0.0, "longitude": 181.0},
		{"latitude": 0.0, "longitude": -181.0},
	} {
		if _, err := ParseConfig(values); err == nil {
			t.Fatalf("ParseConfig(%v) accepted out-of-range coordinates", values)
		}
	}
}

func TestConfigSelectsCelsiusAndFahrenheit(t *testing.T) {
	t.Parallel()
	c, err := ParseConfig(map[string]any{"latitude": 51.5, "longitude": -0.13, "unit": "celsius"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Unit != owm.UnitCelsius {
		t.Fatalf("unit = %v, want celsius", c.Unit)
	}
	f, err := ParseConfig(map[string]any{"latitude": 51.5, "longitude": -0.13, "unit": "fahrenheit"})
	if err != nil {
		t.Fatal(err)
	}
	if f.Unit != owm.UnitFahrenheit {
		t.Fatalf("unit = %v, want fahrenheit", f.Unit)
	}
}

func TestConfigDefaultIntervalIsFifteenMinutes(t *testing.T) {
	t.Parallel()
	c, err := ParseConfig(map[string]any{"latitude": 0.0, "longitude": 0.0})
	if err != nil {
		t.Fatal(err)
	}
	if c.Interval != 15*time.Minute {
		t.Fatalf("interval = %v, want 15m", c.Interval)
	}
}

func serviceAt(t *testing.T, handler http.Handler, cfg Config) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	if cfg.Interval == 0 {
		cfg.Interval = 50 * time.Millisecond
	}
	cfg.Enabled = true
	s := newService(cfg)
	s.endpoint = server.URL
	s.minInterval = 0
	s.start()
	t.Cleanup(s.Close)
	return s
}

func TestServiceFetchesImmediately(t *testing.T) {
	t.Parallel()
	hit := make(chan struct{}, 1)
	s := serviceAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		select {
		case hit <- struct{}{}:
		default:
		}
		fmt.Fprint(rw, forecastBody)
	}), Config{Latitude: 51.5, Longitude: -0.13, Interval: 15 * time.Minute})

	select {
	case <-hit:
	case <-time.After(2 * time.Second):
		t.Fatal("the first fetch waited for the interval instead of running immediately")
	}
	select {
	case snap := <-s.Updates():
		if !snap.Observed || snap.Forecast.Current.Temperature != 18.4 || len(snap.Forecast.Daily) != 7 {
			t.Fatalf("snapshot = %+v", snap)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no snapshot after the first fetch")
	}
}

func TestFetchAsksForASevenDayForecast(t *testing.T) {
	t.Parallel()
	queries := make(chan string, 1)
	s := serviceAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		select {
		case queries <- r.URL.RawQuery:
		default:
		}
		fmt.Fprint(rw, forecastBody)
	}), Config{Latitude: 51.5, Longitude: -0.13, Unit: owm.UnitFahrenheit})
	<-s.Updates()

	q, err := url.ParseQuery(<-queries)
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("latitude") != "51.5" || q.Get("longitude") != "-0.13" {
		t.Fatalf("coords missing from %q", q.Encode())
	}
	if q.Get("temperature_unit") != "fahrenheit" {
		t.Fatalf("unit = %q", q.Get("temperature_unit"))
	}
	if q.Get("forecast_days") != "7" || !strings.Contains(q.Get("daily"), "weather_code") ||
		!strings.Contains(q.Get("daily"), "temperature_2m_max") {
		t.Fatalf("daily forecast missing from %q", q.Encode())
	}
}

func TestServiceAllowsOnlyOneInFlightFetch(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	block := make(chan struct{})
	serviceAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-block
		fmt.Fprint(rw, forecastBody)
	}), Config{Latitude: 1, Longitude: 1, Interval: 20 * time.Millisecond})

	waitCalls(t, &calls, 1)
	time.Sleep(150 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("in-flight fetches = %d, want 1", got)
	}
	close(block)
}

func TestServiceCloseCancelsAnInFlightFetch(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	s := serviceAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
	}), Config{Latitude: 1, Longitude: 1})
	<-entered
	start := time.Now()
	s.Close()
	if time.Since(start) > 2*time.Second {
		t.Fatal("Close waited for the stalled fetch instead of cancelling it")
	}
}

func TestServiceStalePreservesTheLastGoodForecast(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	s := serviceAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			fmt.Fprint(rw, forecastBody)
			return
		}
		http.Error(rw, "unavailable", http.StatusServiceUnavailable)
	}), Config{Latitude: 1, Longitude: 1})

	first := <-s.Updates()
	if !first.Observed || first.Forecast.Current.Code != 3 {
		t.Fatalf("first = %+v", first)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case snap := <-s.Updates():
			if !snap.Stale() {
				continue
			}
			if snap.Forecast.Current.Temperature != 18.4 || len(snap.Forecast.Daily) != 7 {
				t.Fatalf("stale snapshot lost the forecast: %+v", snap)
			}
			return
		case <-deadline:
			t.Fatal("no stale snapshot after the server began failing")
		}
	}
}

func TestServiceDisabledLocationDoesNotFetch(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(rw, forecastBody)
	}))
	t.Cleanup(server.Close)

	cfg, err := ParseConfig(map[string]any{"enabled": false, "latitude": 1.0, "longitude": 1.0})
	if err != nil {
		t.Fatal(err)
	}
	s := newService(cfg)
	s.endpoint = server.URL
	s.minInterval = 0
	s.start()
	t.Cleanup(s.Close)

	time.Sleep(200 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatal("a disabled location fetched weather")
	}
	snap := s.Snapshot()
	if !snap.Disabled {
		t.Fatalf("snapshot = %+v, want disabled", snap)
	}
}

func TestServiceBackoffIsBounded(t *testing.T) {
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

func TestServiceReconfigureDoesNotStartASecondLoop(t *testing.T) {
	t.Parallel()
	s := serviceAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprint(rw, forecastBody)
	}), Config{Latitude: 0, Longitude: 0})
	<-s.Updates()
	if got := s.Starts(); got != 1 {
		t.Fatalf("starts = %d before reconfigure", got)
	}
	s.Reconfigure(Config{Latitude: 51.5, Longitude: -0.13, Unit: owm.UnitFahrenheit, Interval: 50 * time.Millisecond, Enabled: true})
	if got := s.Starts(); got != 1 {
		t.Fatalf("starts = %d after reconfigure, want 1", got)
	}
}

func waitCalls(t *testing.T, n *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for n.Load() < want {
		if time.Now().After(deadline) {
			t.Fatalf("calls = %d, want %d", n.Load(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
