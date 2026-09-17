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

// currentWeatherBody is the shape Open-Meteo returns for the required fields.
const currentWeatherBody = `{"current":{"temperature_2m":18.4,"weather_code":3}}`

const dailyWeatherBody = `{
  "current":{"temperature_2m":18.4,"weather_code":3},
  "daily":{
    "time":["2026-09-10","2026-09-11"],
    "weather_code":[3,61],
    "temperature_2m_max":[20,18],
    "temperature_2m_min":[10,9],
    "sunrise":["2026-09-10T06:10","2026-09-11T06:09"],
    "sunset":["2026-09-10T18:02","2026-09-11T18:03"],
    "uv_index_max":[4.0,3.5],
    "precipitation_probability_max":[10,80],
    "precipitation_sum":[0.0,4.2]
  }
}`

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

func TestTheFirstWeatherLeaseFetchesImmediately(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprint(rw, currentWeatherBody)
	}))
	t.Cleanup(server.Close)

	w := NewWeather(0, 0, UnitCelsius)
	w.endpoint = server.URL
	w.minInterval = 0
	t.Cleanup(w.Close)
	lease, err := w.Acquire(15 * time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(lease.Release)

	select {
	case reading := <-w.Updates():
		if !reading.Observed || reading.Temperature != 18.4 {
			t.Fatalf("reading = %+v, want the first observation", reading)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the first fetch waited for the lease interval")
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

// The request must carry the configured coordinates, current conditions and
// the daily block consumed by the control centre.
func TestTheRequestAsksForTheDailyBlock(t *testing.T) {
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
	if got := current.Get("current"); got != "temperature_2m,weather_code,apparent_temperature,is_day,relative_humidity_2m,wind_speed_10m,wind_direction_10m,uv_index" {
		t.Fatalf("current = %q, want the enriched field set", got)
	}
	if current.Get("latitude") == "" || current.Get("longitude") == "" {
		t.Fatalf("query %q is missing coordinates", query)
	}
	for _, want := range []string{"daily", "forecast_days", "timezone"} {
		if !current.Has(want) {
			t.Errorf("query %q is missing %q", query, want)
		}
	}
	for _, want := range []string{"uv_index_max", "precipitation_probability_max", "precipitation_sum"} {
		if !strings.Contains(current.Get("daily"), want) {
			t.Errorf("daily %q is missing %q", current.Get("daily"), want)
		}
	}
}

func TestRequestURLReportsTheDailyBlock(t *testing.T) {
	w := NewWeather(-37.81, 144.96, UnitCelsius)
	got := w.RequestURL()
	for _, want := range []string{"daily=", "forecast_days=", "timezone=auto"} {
		if !strings.Contains(got, want) {
			t.Errorf("RequestURL() = %q, missing %q", got, want)
		}
	}
}

func TestASuccessfulFetchCarriesTheDailyForecast(t *testing.T) {
	t.Parallel()
	w, _ := weatherAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprint(rw, dailyWeatherBody)
	}))
	select {
	case reading := <-w.Updates():
		if len(reading.Daily) != 2 || reading.Daily[1].Date != "2026-09-11" || reading.Daily[1].Code != 61 {
			t.Fatalf("Daily = %+v, want the decoded two-day forecast", reading.Daily)
		}
		day := reading.Daily[1]
		if day.UVIndexMax == nil || *day.UVIndexMax != 3.5 {
			t.Fatalf("UVIndexMax = %v, want 3.5 carried through", day.UVIndexMax)
		}
		if day.PrecipitationProbability == nil || *day.PrecipitationProbability != 80 {
			t.Fatalf("PrecipitationProbability = %v, want 80 carried through", day.PrecipitationProbability)
		}
		if day.Precipitation == nil || *day.Precipitation != 4.2 {
			t.Fatalf("Precipitation = %v, want 4.2 carried through", day.Precipitation)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no reading arrived within three seconds")
	}
}

func TestASuccessfulFetchCarriesTheEnrichedCurrentFields(t *testing.T) {
	t.Parallel()
	body := `{
	  "elevation":64.0,"timezone":"Australia/Sydney","timezone_abbreviation":"GMT+10",
	  "current":{"temperature_2m":11.0,"weather_code":0,"apparent_temperature":9.5,
	    "is_day":false,"relative_humidity_2m":62,"wind_speed_10m":10.4,
	    "wind_direction_10m":45,"uv_index":0.0}
	}`
	w, _ := weatherAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprint(rw, body)
	}))
	select {
	case reading := <-w.Updates():
		if reading.Apparent == nil || *reading.Apparent != 9.5 {
			t.Fatalf("Apparent = %v, want 9.5 carried through", reading.Apparent)
		}
		if reading.IsDay == nil || *reading.IsDay {
			t.Fatalf("IsDay = %v, want false carried through", reading.IsDay)
		}
		if reading.Humidity == nil || *reading.Humidity != 62 || reading.WindSpeed == nil || *reading.WindSpeed != 10.4 {
			t.Fatalf("humidity/wind = %v/%v, want 62/10.4 carried through", reading.Humidity, reading.WindSpeed)
		}
		if reading.WindDirection == nil || *reading.WindDirection != 45 || reading.UVIndex == nil || *reading.UVIndex != 0.0 {
			t.Fatalf("direction/uv = %v/%v, want 45/0 carried through", reading.WindDirection, reading.UVIndex)
		}
		if reading.Elevation == nil || *reading.Elevation != 64.0 ||
			reading.Timezone == nil || *reading.Timezone != "Australia/Sydney" ||
			reading.TimezoneAbbreviation == nil || *reading.TimezoneAbbreviation != "GMT+10" {
			t.Fatalf("root = %+v, want elevation and timezone carried through", reading)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no reading arrived within three seconds")
	}
}

func TestACityResolvesThroughGeocodingBeforeTheForecast(t *testing.T) {
	t.Parallel()
	var geoCalls, forecastCalls atomic.Int32
	geo := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		geoCalls.Add(1)
		_, _ = rw.Write([]byte(`{"results":[{"name":"Brisbane","latitude":-27.47,"longitude":153.02,"country":"Australia"}]}`))
	}))
	t.Cleanup(geo.Close)
	var forecastQuery atomic.Value
	forecast := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		forecastCalls.Add(1)
		forecastQuery.Store(r.URL.RawQuery)
		fmt.Fprint(rw, currentWeatherBody)
	}))
	t.Cleanup(forecast.Close)

	w := NewWeather(0, 0, UnitCelsius)
	w.endpoint = forecast.URL
	w.geocodingEndpoint = geo.URL
	w.minInterval = 0
	t.Cleanup(w.Close)
	w.SetCity("Brisbane")
	lease, err := w.Acquire(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.releaseWeather(lease) })

	select {
	case reading := <-w.Updates():
		if !reading.Observed {
			t.Fatal("the city never resolved into an observation")
		}
		if reading.Location != "Brisbane" {
			t.Fatalf("location = %q, want the resolved name", reading.Location)
		}
		query, _ := forecastQuery.Load().(string)
		if !strings.Contains(query, "latitude=-27.47") || !strings.Contains(query, "longitude=153.02") {
			t.Fatalf("the forecast used the configured coordinates, not the resolved ones: %s", query)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no reading arrived within three seconds")
	}
}

func TestAGeocodeFailureLeavesTheReadingFailed(t *testing.T) {
	t.Parallel()
	geo := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		http.Error(rw, "nope", http.StatusServiceUnavailable)
	}))
	t.Cleanup(geo.Close)
	forecast := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprint(rw, currentWeatherBody)
	}))
	t.Cleanup(forecast.Close)

	w := NewWeather(0, 0, UnitCelsius)
	w.endpoint = forecast.URL
	w.geocodingEndpoint = geo.URL
	w.minInterval = 0
	t.Cleanup(w.Close)
	w.SetCity("Nowhere")
	lease, err := w.Acquire(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.releaseWeather(lease) })

	select {
	case reading := <-w.Updates():
		if reading.Observed || reading.FailedSince.IsZero() {
			t.Fatalf("reading = %+v, want the failure state", reading)
		}
		if reading.Location != "" {
			t.Fatalf("location = %q, want empty while unresolved", reading.Location)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no reading arrived within three seconds")
	}
}

func TestCityResolutionIsCachedAcrossForecastRetriesUntilTheCityChanges(t *testing.T) {
	t.Parallel()
	var geoCalls, forecastCalls atomic.Int32
	geo := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		geoCalls.Add(1)
		if r.URL.Query().Get("name") == "Beta" {
			fmt.Fprint(rw, `{"results":[{"name":"Beta","latitude":2,"longitude":3}]}`)
			return
		}
		fmt.Fprint(rw, `{"results":[{"name":"Alpha","latitude":-1,"longitude":1}]}`)
	}))
	t.Cleanup(geo.Close)
	forecast := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if forecastCalls.Add(1) == 2 {
			http.Error(rw, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(rw, currentWeatherBody)
	}))
	t.Cleanup(forecast.Close)

	w := NewWeather(0, 0, UnitCelsius)
	w.endpoint = forecast.URL
	w.geocodingEndpoint = geo.URL
	w.minInterval = 0
	t.Cleanup(w.Close)
	w.SetCity("Alpha")

	first, err := w.fetch()
	if err != nil || first.Location != "Alpha" {
		t.Fatalf("first fetch = %+v, %v", first, err)
	}
	if _, err := w.fetch(); err == nil {
		t.Fatal("the simulated forecast failure succeeded")
	}
	third, err := w.fetch()
	if err != nil || third.Location != "Alpha" {
		t.Fatalf("cached retry = %+v, %v", third, err)
	}
	if got := geoCalls.Load(); got != 1 {
		t.Fatalf("geocode calls after a forecast retry = %d, want 1", got)
	}

	w.SetCity("Beta")
	fourth, err := w.fetch()
	if err != nil || fourth.Location != "Beta" {
		t.Fatalf("city change fetch = %+v, %v", fourth, err)
	}
	if got := geoCalls.Load(); got != 2 {
		t.Fatalf("geocode calls after city change = %d, want 2", got)
	}
}

func TestCityChangeDuringGeocodeCannotReuseTheOldResolution(t *testing.T) {
	var geoCalls atomic.Int32
	var alphaStarted atomic.Bool
	started := make(chan struct{})
	releaseAlpha := make(chan struct{})
	geo := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		geoCalls.Add(1)
		if name == "Alpha" && alphaStarted.CompareAndSwap(false, true) {
			close(started)
			<-releaseAlpha
		}
		if name == "Beta" {
			fmt.Fprint(rw, `{"results":[{"name":"Beta","latitude":2,"longitude":3}]}`)
			return
		}
		fmt.Fprint(rw, `{"results":[{"name":"Alpha","latitude":-1,"longitude":1}]}`)
	}))
	t.Cleanup(geo.Close)
	forecast := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprint(rw, currentWeatherBody)
	}))
	t.Cleanup(forecast.Close)

	w := NewWeather(0, 0, UnitCelsius)
	w.endpoint = forecast.URL
	w.geocodingEndpoint = geo.URL
	w.minInterval = 0
	t.Cleanup(w.Close)
	w.SetCity("Alpha")

	result := make(chan struct {
		reading Reading
		err     error
	}, 1)
	go func() {
		reading, err := w.fetch()
		result <- struct {
			reading Reading
			err     error
		}{reading, err}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("the first geocode did not start")
	}
	w.SetCity("Beta")
	close(releaseAlpha)

	select {
	case got := <-result:
		if got.err != nil || got.reading.Location != "Beta" {
			t.Fatalf("fetch after city change = %+v, %v; want Beta", got.reading, got.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("fetch did not restart after the city changed")
	}
	if got := geoCalls.Load(); got != 2 {
		t.Fatalf("geocode calls = %d, want Alpha then Beta", got)
	}
}

func TestASuccessfulFetchCarriesTheHourlyForecast(t *testing.T) {
	t.Parallel()
	w, _ := weatherAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprint(rw, hourlyWeatherBody)
	}))
	select {
	case reading := <-w.Updates():
		if len(reading.Hourly) != 3 {
			t.Fatalf("Hourly = %d rows, want 3", len(reading.Hourly))
		}
		h := reading.Hourly[2]
		if h.Code != 61 || h.Temperature != 12.5 {
			t.Fatalf("hour = %+v, want code 61 at 12.5 carried through", h)
		}
		if h.IsDay == nil || *h.IsDay {
			t.Fatalf("IsDay = %v, want false carried through", h.IsDay)
		}
		if h.PrecipProbability == nil || *h.PrecipProbability != 80 {
			t.Fatalf("PrecipProbability = %v, want 80 carried through", h.PrecipProbability)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no reading arrived within three seconds")
	}
}

func TestTheRequestAsksForTheHourlyBlock(t *testing.T) {
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
	q, err := url.ParseQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	if want := "temperature_2m,relative_humidity_2m,precipitation_probability,weather_code,is_day,wind_speed_10m"; q.Get("hourly") != want {
		t.Fatalf("hourly = %q", q.Get("hourly"))
	}
	if q.Get("forecast_hours") != "168" {
		t.Fatalf("forecast_hours = %q, want 168", q.Get("forecast_hours"))
	}
}

func TestASuccessfulFetchLeavesAbsentOptionalFieldsAbsent(t *testing.T) {
	t.Parallel()
	w, _ := weatherAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprint(rw, currentWeatherBody)
	}))
	select {
	case reading := <-w.Updates():
		if reading.Apparent != nil || reading.IsDay != nil || reading.Humidity != nil ||
			reading.WindSpeed != nil || reading.WindDirection != nil || reading.UVIndex != nil ||
			reading.Elevation != nil || reading.Timezone != nil || reading.TimezoneAbbreviation != nil {
			t.Fatalf("absent fields decoded as present: %+v", reading)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no reading arrived within three seconds")
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

const hourlyWeatherBody = `{
  "current":{"temperature_2m":11.0,"weather_code":0},
  "hourly":{
    "time":["2026-09-15T14:00","2026-09-15T15:00","2026-09-15T16:00"],
    "weather_code":[0,2,61],
    "temperature_2m":[14.2,13.8,12.5],
    "is_day":[true,true,false],
    "precipitation_probability":[10,20,80]
  }
}`
