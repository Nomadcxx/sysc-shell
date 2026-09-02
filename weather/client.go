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
	v.Set("current", "temperature_2m,weather_code")
	v.Set("timezone", "auto")
	if q.Unit == UnitFahrenheit {
		v.Set("temperature_unit", "fahrenheit")
	}
	if q.Daily {
		v.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,sunrise,sunset")
		v.Set("forecast_days", "7")
	}
	return endpoint + "?" + v.Encode()
}

func Decode(body []byte) (Forecast, error) {
	var wire struct {
		Current *struct {
			Temperature *float64 `json:"temperature_2m"`
			Code        *int     `json:"weather_code"`
		} `json:"current"`
		Daily *struct {
			Time    []string  `json:"time"`
			Code    []int     `json:"weather_code"`
			High    []float64 `json:"temperature_2m_max"`
			Low     []float64 `json:"temperature_2m_min"`
			Sunrise []string  `json:"sunrise"`
			Sunset  []string  `json:"sunset"`
		} `json:"daily"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return Forecast{}, fmt.Errorf("weather: decode: %w", err)
	}
	if wire.Current == nil || wire.Current.Temperature == nil || wire.Current.Code == nil {
		return Forecast{}, fmt.Errorf("weather: response carries no current observation")
	}
	fc := Forecast{Current: Current{Temperature: *wire.Current.Temperature, Code: *wire.Current.Code}}
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
	return fc, nil
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
