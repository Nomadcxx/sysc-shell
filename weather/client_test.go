package weather

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const currentBody = `{"current":{"temperature_2m":18.4,"weather_code":3}}`

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

func TestRequestURLCarriesCoordinatesCurrentFieldsAndTimezone(t *testing.T) {
	t.Parallel()
	raw := RequestURL("https://api.open-meteo.com/v1/forecast", Query{
		Latitude: 51.5, Longitude: -0.13, Unit: UnitCelsius,
	})
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("latitude") != "51.5" || q.Get("longitude") != "-0.13" {
		t.Fatalf("coords = %q", raw)
	}
	if q.Get("current") != "temperature_2m,weather_code" {
		t.Fatalf("current = %q", q.Get("current"))
	}
	if q.Get("timezone") != "auto" {
		t.Fatalf("timezone = %q", q.Get("timezone"))
	}
	if q.Has("temperature_unit") || q.Has("daily") || q.Has("forecast_days") {
		t.Fatalf("celsius current-only leaked extras: %q", raw)
	}
}

func TestRequestURLAsksFahrenheitAndSevenDailyWhenConfigured(t *testing.T) {
	t.Parallel()
	raw := RequestURL("https://example.test/forecast", Query{
		Latitude: 1, Longitude: 2, Unit: UnitFahrenheit, Daily: true,
	})
	q, err := url.ParseQuery(strings.TrimPrefix(raw, "https://example.test/forecast?"))
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("temperature_unit") != "fahrenheit" {
		t.Fatalf("unit = %q", q.Get("temperature_unit"))
	}
	if q.Get("forecast_days") != "7" {
		t.Fatalf("forecast_days = %q", q.Get("forecast_days"))
	}
	if got := q.Get("daily"); !strings.Contains(got, "weather_code") || !strings.Contains(got, "temperature_2m_max") {
		t.Fatalf("daily = %q", got)
	}
}

func TestDecodeCurrentPreservesWMOCode(t *testing.T) {
	t.Parallel()
	fc, err := Decode([]byte(`{"current":{"temperature_2m":-3.2,"weather_code":123}}`))
	if err != nil {
		t.Fatal(err)
	}
	if fc.Current.Temperature != -3.2 || fc.Current.Code != 123 {
		t.Fatalf("%+v", fc.Current)
	}
	if len(fc.Daily) != 0 {
		t.Fatalf("daily = %d", len(fc.Daily))
	}
}

func TestDecodeSevenDailyValues(t *testing.T) {
	t.Parallel()
	fc, err := Decode([]byte(forecastBody))
	if err != nil {
		t.Fatal(err)
	}
	if fc.Current.Temperature != 18.4 || fc.Current.Code != 3 {
		t.Fatalf("current = %+v", fc.Current)
	}
	if len(fc.Daily) != 7 {
		t.Fatalf("days = %d", len(fc.Daily))
	}
	d := fc.Daily[2]
	if d.Date != "2026-09-04" || d.Code != 71 || d.High != 8.5 || d.Low != -1.5 {
		t.Fatalf("day = %+v", d)
	}
	if d.Sunrise != "2026-09-04T06:16" || d.Sunset != "2026-09-04T18:40" {
		t.Fatalf("sun = %+v", d)
	}
}

func TestDecodeRejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	if _, err := Decode([]byte(`{not json`)); err == nil {
		t.Fatal("malformed JSON was accepted")
	}
	if _, err := Decode([]byte(`{}`)); err == nil {
		t.Fatal("empty object was accepted")
	}
}

func TestFetchPublishesCurrentOnHTTP200(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, currentBody)
	}))
	t.Cleanup(srv.Close)
	fc, err := Fetch(context.Background(), srv.Client(), Query{Endpoint: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if fc.Current.Temperature != 18.4 || fc.Current.Code != 3 {
		t.Fatalf("%+v", fc.Current)
	}
}

func TestFetchHTTPFailure(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	if _, err := Fetch(context.Background(), srv.Client(), Query{Endpoint: srv.URL}); err == nil {
		t.Fatal("HTTP 503 was accepted")
	}
}

func TestFetchTimeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	client := &http.Client{Timeout: 50 * time.Millisecond}
	if _, err := Fetch(ctx, client, Query{Endpoint: srv.URL}); err == nil {
		t.Fatal("stalled fetch succeeded")
	}
}
