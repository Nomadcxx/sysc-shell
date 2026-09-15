package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

const (
	// DefaultEndpoint is the only remote host this shell contacts for weather.
	DefaultEndpoint = "https://api.open-meteo.com/v1/forecast"
	// MaxResponseBytes caps the body. A current-weather payload is a few
	// hundred bytes; a seven-day forecast is still far under this.
	MaxResponseBytes = 64 << 10
)

func RequestURL(endpoint string, q Query) string {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	v := url.Values{}
	v.Set("latitude", strconv.FormatFloat(q.Latitude, 'f', -1, 64))
	v.Set("longitude", strconv.FormatFloat(q.Longitude, 'f', -1, 64))
	v.Set("current", "temperature_2m,weather_code,apparent_temperature,is_day,relative_humidity_2m,wind_speed_10m,wind_direction_10m,uv_index")
	v.Set("timezone", "auto")
	if q.Unit == UnitFahrenheit {
		v.Set("temperature_unit", "fahrenheit")
	}
	if q.Daily {
		v.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,sunrise,sunset,uv_index_max,precipitation_probability_max,precipitation_sum")
		v.Set("forecast_days", "7")
	}
	if q.Hourly {
		v.Set("hourly", "temperature_2m,relative_humidity_2m,precipitation_probability,weather_code,is_day,wind_speed_10m")
		v.Set("forecast_hours", "168")
	}
	return endpoint + "?" + v.Encode()
}

func Decode(body []byte) (Forecast, error) {
	var wire struct {
		Current *struct {
			Temperature *float64 `json:"temperature_2m"`
			Code        *int     `json:"weather_code"`
			Apparent    *float64 `json:"apparent_temperature"`
			IsDay       *bool    `json:"is_day"`
			Humidity    *float64 `json:"relative_humidity_2m"`
			WindSpeed   *float64 `json:"wind_speed_10m"`
			WindDir     *float64 `json:"wind_direction_10m"`
			UVIndex     *float64 `json:"uv_index"`
		} `json:"current"`
		Daily *struct {
			Time    []string  `json:"time"`
			Code    []int     `json:"weather_code"`
			High    []float64 `json:"temperature_2m_max"`
			Low     []float64 `json:"temperature_2m_min"`
			Sunrise []string  `json:"sunrise"`
			Sunset  []string  `json:"sunset"`
			UVMax   []float64 `json:"uv_index_max"`
			PrecipP []float64 `json:"precipitation_probability_max"`
			PrecipS []float64 `json:"precipitation_sum"`
		} `json:"daily"`
		Hourly *struct {
			Time    []string  `json:"time"`
			Code    []int     `json:"weather_code"`
			Temp    []float64 `json:"temperature_2m"`
			IsDay   []*bool   `json:"is_day"`
			Hum     []float64 `json:"relative_humidity_2m"`
			PrecipP []float64 `json:"precipitation_probability"`
			Wind    []float64 `json:"wind_speed_10m"`
		} `json:"hourly"`
		Elevation            *float64 `json:"elevation"`
		Timezone             string   `json:"timezone"`
		TimezoneAbbreviation string   `json:"timezone_abbreviation"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return Forecast{}, fmt.Errorf("weather: decode: %w", err)
	}
	if wire.Current == nil || wire.Current.Temperature == nil || wire.Current.Code == nil {
		return Forecast{}, fmt.Errorf("weather: response carries no current observation")
	}
	fc := Forecast{
		Current: Current{
			Temperature:   *wire.Current.Temperature,
			Code:          *wire.Current.Code,
			Apparent:      wire.Current.Apparent,
			IsDay:         wire.Current.IsDay,
			Humidity:      wire.Current.Humidity,
			WindSpeed:     wire.Current.WindSpeed,
			WindDirection: wire.Current.WindDir,
			UVIndex:       wire.Current.UVIndex,
		},
		Elevation:            wire.Elevation,
		Timezone:             wire.Timezone,
		TimezoneAbbreviation: wire.TimezoneAbbreviation,
	}
	if wire.Daily == nil {
		return fc, nil
	}
	n := len(wire.Daily.Time)
	n = min(n, len(wire.Daily.Code), len(wire.Daily.High), len(wire.Daily.Low), len(wire.Daily.Sunrise), len(wire.Daily.Sunset))
	fc.Daily = make([]Day, n)
	for i := 0; i < n; i++ {
		fc.Daily[i] = Day{
			Date: wire.Daily.Time[i], Code: wire.Daily.Code[i],
			High: wire.Daily.High[i], Low: wire.Daily.Low[i],
			Sunrise: wire.Daily.Sunrise[i], Sunset: wire.Daily.Sunset[i],
		}
	}
	// The optional daily arrays pad the required six rather than truncating
	// them: a body without uv_index_max still carries seven days, each with a
	// nil UV figure. A present but short array covers the days it has.
	fillDaily(fc.Daily, wire.Daily.UVMax, func(d *Day, v float64) { d.UVIndexMax = &v })
	fillDaily(fc.Daily, wire.Daily.PrecipP, func(d *Day, v float64) { d.PrecipitationProbability = &v })
	fillDaily(fc.Daily, wire.Daily.PrecipS, func(d *Day, v float64) { d.Precipitation = &v })
	if wire.Hourly == nil {
		return fc, nil
	}
	n := len(wire.Hourly.Time)
	n = min(n, len(wire.Hourly.Code), len(wire.Hourly.Temp))
	fc.Hourly = make([]Hour, n)
	for i := 0; i < n; i++ {
		h := Hour{Time: wire.Hourly.Time[i], Code: wire.Hourly.Code[i], Temperature: wire.Hourly.Temp[i]}
		if i < len(wire.Hourly.IsDay) {
			h.IsDay = wire.Hourly.IsDay[i]
		}
		fc.Hourly[i] = h
	}
	fillHourly(fc.Hourly, wire.Hourly.Hum, func(h *Hour, v float64) { h.Humidity = &v })
	fillHourly(fc.Hourly, wire.Hourly.PrecipP, func(h *Hour, v float64) { h.PrecipProbability = &v })
	fillHourly(fc.Hourly, wire.Hourly.Wind, func(h *Hour, v float64) { h.WindSpeed = &v })
	return fc, nil
}

// fillHourly copies one optional hourly array onto the hours it covers, the
// daily pattern: a missing array leaves every hour nil, a short one covers
// the hours it has.
func fillHourly(hours []Hour, values []float64, set func(*Hour, float64)) {
	for i := 0; i < min(len(hours), len(values)); i++ {
		set(&hours[i], values[i])
	}
}

// fillDaily copies one optional daily array onto the days it covers. A
// missing array leaves every day nil; a short array covers the days it has.
func fillDaily(days []Day, values []float64, set func(*Day, float64)) {
	for i := 0; i < min(len(days), len(values)); i++ {
		set(&days[i], values[i])
	}
}

func Fetch(ctx context.Context, client *http.Client, q Query) (Forecast, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, RequestURL(q.Endpoint, q), nil)
	if err != nil {
		return Forecast{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Forecast{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Forecast{}, fmt.Errorf("weather: status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return Forecast{}, err
	}
	if len(body) > MaxResponseBytes {
		return Forecast{}, fmt.Errorf("weather: response exceeds %d bytes", MaxResponseBytes)
	}
	return Decode(body)
}
