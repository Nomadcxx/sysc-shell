package weather

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestGeocodeURLCarriesTheNameAndCount(t *testing.T) {
	t.Parallel()
	raw := GeocodeURL("https://geocoding-api.open-meteo.com/v1/search", "Brisbane")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("name") != "Brisbane" || q.Get("count") != "1" {
		t.Fatalf("query = %v", q)
	}
}

func TestGeocodeCarriesTheFirstResult(t *testing.T) {
	t.Parallel()
	body := `{"results":[{"name":"Brisbane","latitude":-27.47,"longitude":153.02,"country":"Australia","admin1":"Queensland"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	place, err := Geocode(context.Background(), srv.Client(), srv.URL, "Brisbane")
	if err != nil {
		t.Fatal(err)
	}
	if place.Name != "Brisbane" || place.Latitude != -27.47 || place.Longitude != 153.02 {
		t.Fatalf("place = %+v", place)
	}
	if place.Country != "Australia" || place.Admin1 != "Queensland" {
		t.Fatalf("place region = %+v", place)
	}
}

func TestGeocodeWithNoResultsIsAnError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	if _, err := Geocode(context.Background(), srv.Client(), srv.URL, "Nowhere"); err == nil {
		t.Fatal("a response with no results was accepted")
	}
}

func TestGeocodeRejectsMalformedAndIncompleteResults(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"malformed":         `{not json`,
		"no coordinates":    `{"results":[{"name":"Brisbane"}]}`,
		"incomplete result": `{"results":[{"latitude":-27.47,"longitude":153.02}]}`,
		"null latitude":     `{"results":[{"name":"B","latitude":null,"longitude":1.0}]}`,
		"results not array": `{"results":"many"}`,
	}
	for name, body := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		if _, err := Geocode(context.Background(), srv.Client(), srv.URL, "X"); err == nil {
			t.Fatalf("%s was accepted", name)
		}
		srv.Close()
	}
}

func TestGeocodeURLEncodesTheName(t *testing.T) {
	t.Parallel()
	raw := GeocodeURL("", "São Paulo")
	if !strings.Contains(raw, "name=S%C3%A3o+Paulo") {
		t.Fatalf("url = %q", raw)
	}
}
