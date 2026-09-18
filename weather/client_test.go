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

const enrichedCurrentBody = `{
  "elevation":64.0,
  "timezone":"Australia/Sydney",
  "timezone_abbreviation":"GMT+10",
  "current":{
    "temperature_2m":18.4,"weather_code":3,"apparent_temperature":17.1,
    "is_day":false,"relative_humidity_2m":62,"wind_speed_10m":10.4,
    "wind_direction_10m":45,"uv_index":0.0
  }
}`

const forecastBody = `{
  "current":{"temperature_2m":18.4,"weather_code":3},
  "daily":{
    "time":["2026-09-02","2026-09-03","2026-09-04","2026-09-05","2026-09-06","2026-09-07","2026-09-08"],
    "weather_code":[3,61,71,95,0,2,45],
    "temperature_2m_max":[22.1,19.0,8.5,17.0,24.0,21.0,16.0],
    "temperature_2m_min":[12.0,11.0,-1.5,9.0,13.0,12.5,10.0],
    "sunrise":["2026-09-02T06:12","2026-09-03T06:14","2026-09-04T06:16","2026-09-05T06:18","2026-09-06T06:20","2026-09-07T06:22","2026-09-08T06:24"],
    "sunset":["2026-09-02T18:44","2026-09-03T18:42","2026-09-04T18:40","2026-09-05T18:38","2026-09-06T18:36","2026-09-07T18:34","2026-09-08T18:32"],
    "uv_index_max":[4.0,3.5,2.0,3.0,5.5,4.5,3.0],
    "precipitation_probability_max":[10,80,55,90,5,20,40],
    "precipitation_sum":[0.0,4.2,1.1,8.8,0.0,0.3,0.9]
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
	if want := "temperature_2m,weather_code,apparent_temperature,is_day,relative_humidity_2m,wind_speed_10m,wind_direction_10m,uv_index"; q.Get("current") != want {
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
	for _, want := range []string{"uv_index_max", "precipitation_probability_max", "precipitation_sum"} {
		if !strings.Contains(q.Get("daily"), want) {
			t.Fatalf("daily %q is missing %q", q.Get("daily"), want)
		}
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

func TestDecodeCarriesTheEnrichedCurrentAndRootFields(t *testing.T) {
	t.Parallel()
	fc, err := Decode([]byte(enrichedCurrentBody))
	if err != nil {
		t.Fatal(err)
	}
	c := fc.Current
	if c.Apparent == nil || *c.Apparent != 17.1 {
		t.Fatalf("apparent = %v", c.Apparent)
	}
	if c.IsDay == nil || *c.IsDay {
		t.Fatalf("is_day = %v", c.IsDay)
	}
	if c.Humidity == nil || *c.Humidity != 62 {
		t.Fatalf("humidity = %v", c.Humidity)
	}
	if c.WindSpeed == nil || *c.WindSpeed != 10.4 {
		t.Fatalf("wind speed = %v", c.WindSpeed)
	}
	if c.WindDirection == nil || *c.WindDirection != 45 {
		t.Fatalf("wind direction = %v", c.WindDirection)
	}
	if c.UVIndex == nil || *c.UVIndex != 0.0 {
		t.Fatalf("uv index = %v", c.UVIndex)
	}
	if fc.Elevation == nil || *fc.Elevation != 64.0 {
		t.Fatalf("elevation = %v", fc.Elevation)
	}
	if fc.Timezone == nil || *fc.Timezone != "Australia/Sydney" ||
		fc.TimezoneAbbreviation == nil || *fc.TimezoneAbbreviation != "GMT+10" {
		t.Fatalf("timezone = %q / %q", optionalString(fc.Timezone), optionalString(fc.TimezoneAbbreviation))
	}
}

func TestDecodeAcceptsOpenMeteoNumericDayFlags(t *testing.T) {
	t.Parallel()
	body := `{
  "current":{"temperature_2m":18.4,"weather_code":0,"is_day":1},
  "hourly":{
    "time":["2026-09-16T12:00","2026-09-16T13:00"],
    "weather_code":[0,61],"temperature_2m":[18.4,17.9],"is_day":[1,0]
  }
}`

	fc, err := Decode([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if fc.Current.IsDay == nil || !*fc.Current.IsDay {
		t.Fatalf("current is_day = %v, want true", fc.Current.IsDay)
	}
	if len(fc.Hourly) != 2 || fc.Hourly[0].IsDay == nil || !*fc.Hourly[0].IsDay ||
		fc.Hourly[1].IsDay == nil || *fc.Hourly[1].IsDay {
		t.Fatalf("hourly is_day = %+v, want true then false", fc.Hourly)
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
	if d.UVIndexMax == nil || *d.UVIndexMax != 2.0 {
		t.Fatalf("uv max = %v", d.UVIndexMax)
	}
	if d.PrecipitationProbability == nil || *d.PrecipitationProbability != 55 {
		t.Fatalf("precip probability = %v", d.PrecipitationProbability)
	}
	if d.Precipitation == nil || *d.Precipitation != 1.1 {
		t.Fatalf("precip sum = %v", d.Precipitation)
	}
}

func TestDecodeTreatsAbsentOptionalFieldsAsAbsent(t *testing.T) {
	t.Parallel()
	fc, err := Decode([]byte(currentBody))
	if err != nil {
		t.Fatal(err)
	}
	c := fc.Current
	if c.Apparent != nil || c.IsDay != nil || c.Humidity != nil || c.WindSpeed != nil || c.WindDirection != nil || c.UVIndex != nil {
		t.Fatalf("optional current fields survived an absent body: %+v", c)
	}
	if fc.Elevation != nil || fc.Timezone != nil || fc.TimezoneAbbreviation != nil {
		t.Fatalf("root fields survived an absent body: %+v", fc)
	}
	if len(fc.Daily) != 0 {
		t.Fatalf("daily = %d", len(fc.Daily))
	}
}

func optionalString(value *string) string {
	if value == nil {
		return "<nil>"
	}
	return *value
}

func TestDecodeTreatsNullOptionalFieldsAsAbsent(t *testing.T) {
	t.Parallel()
	body := `{"current":{"temperature_2m":1,"weather_code":0,"apparent_temperature":null,"is_day":null,"uv_index":null}}`
	fc, err := Decode([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if fc.Current.Apparent != nil || fc.Current.IsDay != nil || fc.Current.UVIndex != nil {
		t.Fatalf("nulls decoded as present: %+v", fc.Current)
	}
}

func TestDecodeTreatsNullOptionalArraysAndRootNamesAsAbsent(t *testing.T) {
	t.Parallel()
	body := `{
	  "timezone":null,"timezone_abbreviation":null,
	  "current":{"temperature_2m":1,"weather_code":0},
	  "daily":{
	    "time":["2026-09-02"],"weather_code":[0],
	    "temperature_2m_max":[5],"temperature_2m_min":[-1],
	    "sunrise":["2026-09-02T06:12"],"sunset":["2026-09-02T18:44"],
	    "uv_index_max":[null],"precipitation_probability_max":[null],"precipitation_sum":[null]
	  },
	  "hourly":{
	    "time":["2026-09-02T06:00"],"weather_code":[0],"temperature_2m":[1],
	    "relative_humidity_2m":[null],"precipitation_probability":[null],"wind_speed_10m":[null]
	  }
	}`
	fc, err := Decode([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(fc.Daily) != 1 || fc.Daily[0].UVIndexMax != nil ||
		fc.Daily[0].PrecipitationProbability != nil || fc.Daily[0].Precipitation != nil {
		t.Fatalf("null daily values decoded as present: %+v", fc.Daily)
	}
	if len(fc.Hourly) != 1 || fc.Hourly[0].Humidity != nil ||
		fc.Hourly[0].PrecipProbability != nil || fc.Hourly[0].WindSpeed != nil {
		t.Fatalf("null hourly values decoded as present: %+v", fc.Hourly)
	}
	if fc.Timezone != nil || fc.TimezoneAbbreviation != nil {
		t.Fatalf("null root names decoded as present: %q / %q", optionalString(fc.Timezone), optionalString(fc.TimezoneAbbreviation))
	}
}

func TestDecodeKeepsDaysWhenAnOptionalDailyArrayIsMissing(t *testing.T) {
	t.Parallel()
	body := `{
	  "current":{"temperature_2m":1,"weather_code":0},
	  "daily":{
	    "time":["2026-09-02","2026-09-03"],
	    "weather_code":[0,3],
	    "temperature_2m_max":[5,6],
	    "temperature_2m_min":[-1,0],
	    "sunrise":["2026-09-02T06:12","2026-09-03T06:14"],
	    "sunset":["2026-09-02T18:44","2026-09-03T18:42"]
	  }
	}`
	fc, err := Decode([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(fc.Daily) != 2 {
		t.Fatalf("days = %d", len(fc.Daily))
	}
	if fc.Daily[0].UVIndexMax != nil || fc.Daily[0].PrecipitationProbability != nil || fc.Daily[0].Precipitation != nil {
		t.Fatalf("optional daily fields decoded from a missing array: %+v", fc.Daily[0])
	}
}

func TestDecodeOptionalArraysPadRequiredRowsWithoutTruncating(t *testing.T) {
	t.Parallel()
	body := `{
	  "current":{"temperature_2m":1,"weather_code":0},
	  "daily":{
	    "time":["2026-09-02","2026-09-03"],"weather_code":[0,3],
	    "temperature_2m_max":[5,6],"temperature_2m_min":[-1,0],
	    "sunrise":["2026-09-02T06:12","2026-09-03T06:14"],
	    "sunset":["2026-09-02T18:44","2026-09-03T18:42"],
	    "uv_index_max":[4],"precipitation_probability_max":[10],"precipitation_sum":[0]
	  },
	  "hourly":{
	    "time":["2026-09-02T06:00","2026-09-02T07:00"],"weather_code":[0,3],"temperature_2m":[1,2],
	    "relative_humidity_2m":[60],"precipitation_probability":[10],"wind_speed_10m":[5]
	  }
	}`
	fc, err := Decode([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(fc.Daily) != 2 || fc.Daily[0].UVIndexMax == nil || *fc.Daily[0].UVIndexMax != 4 ||
		fc.Daily[1].UVIndexMax != nil || fc.Daily[1].PrecipitationProbability != nil || fc.Daily[1].Precipitation != nil {
		t.Fatalf("daily optional padding = %+v", fc.Daily)
	}
	if len(fc.Hourly) != 2 || fc.Hourly[0].Humidity == nil || *fc.Hourly[0].Humidity != 60 ||
		fc.Hourly[1].Humidity != nil || fc.Hourly[1].PrecipProbability != nil || fc.Hourly[1].WindSpeed != nil {
		t.Fatalf("hourly optional padding = %+v", fc.Hourly)
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

const hourlyBody = `{
  "current":{"temperature_2m":11.0,"weather_code":0},
  "hourly":{
    "time":["2026-09-15T14:00","2026-09-15T15:00","2026-09-15T16:00"],
    "weather_code":[0,2,61],
    "temperature_2m":[14.2,13.8,12.5],
    "is_day":[true,true,false],
    "relative_humidity_2m":[62,65,71],
    "precipitation_probability":[10,20,80],
    "wind_speed_10m":[10.4,11.0,9.2]
  }
}`

func TestRequestURLCarriesTheHourlyFieldSetWhenAsked(t *testing.T) {
	t.Parallel()
	raw := RequestURL("https://example.test/forecast", Query{Latitude: 1, Longitude: 2, Hourly: true})
	q, err := url.ParseQuery(strings.TrimPrefix(raw, "https://example.test/forecast?"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "temperature_2m,relative_humidity_2m,precipitation_probability,weather_code,is_day,wind_speed_10m"; q.Get("hourly") != want {
		t.Fatalf("hourly = %q", q.Get("hourly"))
	}
	if q.Get("forecast_hours") != "168" {
		t.Fatalf("forecast_hours = %q, want 168", q.Get("forecast_hours"))
	}
	if q.Has("daily") {
		t.Fatal("an hourly request leaked a daily block")
	}
}

func TestRequestURLLeavesHourlyOffUnlessAsked(t *testing.T) {
	t.Parallel()
	raw := RequestURL("", Query{Latitude: 1, Longitude: 2, Daily: true})
	if strings.Contains(raw, "hourly") || strings.Contains(raw, "forecast_hours") {
		t.Fatalf("an hourly-less request grew hourly fields: %q", raw)
	}
}

func TestDecodeCarriesTheHourlyRows(t *testing.T) {
	t.Parallel()
	fc, err := Decode([]byte(hourlyBody))
	if err != nil {
		t.Fatal(err)
	}
	if len(fc.Hourly) != 3 {
		t.Fatalf("hours = %d, want 3", len(fc.Hourly))
	}
	h := fc.Hourly[2]
	if h.Time != "2026-09-15T16:00" || h.Code != 61 || h.Temperature != 12.5 {
		t.Fatalf("hour = %+v", h)
	}
	if h.IsDay == nil || *h.IsDay {
		t.Fatalf("is_day = %v, want false carried through", h.IsDay)
	}
	if h.Humidity == nil || *h.Humidity != 71 || h.PrecipProbability == nil || *h.PrecipProbability != 80 {
		t.Fatalf("humidity/precip = %v/%v, want 71/80", h.Humidity, h.PrecipProbability)
	}
	if h.WindSpeed == nil || *h.WindSpeed != 9.2 {
		t.Fatalf("wind = %v, want 9.2", h.WindSpeed)
	}
}

func TestDecodeTreatsAbsentHourlyFieldsAsAbsent(t *testing.T) {
	t.Parallel()
	body := `{"current":{"temperature_2m":1,"weather_code":0},"hourly":{"time":["2026-09-15T14:00"],"weather_code":[0],"temperature_2m":[9.0]}}`
	fc, err := Decode([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(fc.Hourly) != 1 {
		t.Fatalf("hours = %d, want 1", len(fc.Hourly))
	}
	h := fc.Hourly[0]
	if h.IsDay != nil || h.Humidity != nil || h.PrecipProbability != nil || h.WindSpeed != nil {
		t.Fatalf("optional hourly fields survived an absent body: %+v", h)
	}
}

func TestDecodeTreatsNullHourlyFieldsAsAbsent(t *testing.T) {
	t.Parallel()
	body := `{"current":{"temperature_2m":1,"weather_code":0},"hourly":{"time":["2026-09-15T14:00"],"weather_code":[0],"temperature_2m":[9.0],"is_day":null,"precipitation_probability":null}}`
	fc, err := Decode([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if fc.Hourly[0].IsDay != nil || fc.Hourly[0].PrecipProbability != nil {
		t.Fatalf("nulls decoded as present: %+v", fc.Hourly[0])
	}
}
