package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// DefaultGeocodingEndpoint resolves a place name to coordinates on the same
// provider the forecast uses, so a configured city needs no second vendor.
const DefaultGeocodingEndpoint = "https://geocoding-api.open-meteo.com/v1/search"

// Place is the resolved location: the display name and the coordinates the
// forecast request needs.
type Place struct {
	Name                string
	Latitude, Longitude float64
	Country, Admin1     string
}

// GeocodeURL builds one search request for a place name.
func GeocodeURL(endpoint, name string) string {
	if endpoint == "" {
		endpoint = DefaultGeocodingEndpoint
	}
	v := url.Values{}
	v.Set("name", name)
	v.Set("count", "1")
	v.Set("language", "en")
	v.Set("format", "json")
	return endpoint + "?" + v.Encode()
}

// Geocode resolves one place name. The first result wins; a response with no
// results, an incomplete result, or a malformed body is an error, matching
// the forecast decoder's strictness.
func Geocode(ctx context.Context, client *http.Client, endpoint, name string) (Place, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, GeocodeURL(endpoint, name), nil)
	if err != nil {
		return Place{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Place{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Place{}, fmt.Errorf("weather: geocode status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return Place{}, err
	}
	if len(body) > MaxResponseBytes {
		return Place{}, fmt.Errorf("weather: geocode response exceeds %d bytes", MaxResponseBytes)
	}
	var wire struct {
		Results *[]struct {
			Name      *string  `json:"name"`
			Latitude  *float64 `json:"latitude"`
			Longitude *float64 `json:"longitude"`
			Country   *string  `json:"country"`
			Admin1    *string  `json:"admin1"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return Place{}, fmt.Errorf("weather: geocode decode: %w", err)
	}
	if wire.Results == nil || len(*wire.Results) == 0 {
		return Place{}, fmt.Errorf("weather: geocode found no place named %q", name)
	}
	first := (*wire.Results)[0]
	if first.Name == nil || first.Latitude == nil || first.Longitude == nil {
		return Place{}, fmt.Errorf("weather: geocode result for %q is incomplete", name)
	}
	place := Place{Name: *first.Name, Latitude: *first.Latitude, Longitude: *first.Longitude}
	if first.Country != nil {
		place.Country = *first.Country
	}
	if first.Admin1 != nil {
		place.Admin1 = *first.Admin1
	}
	return place, nil
}
