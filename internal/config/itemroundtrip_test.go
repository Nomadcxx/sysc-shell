package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// itemFixtures is one fully-populated item per known id: every field that id
// carries, set to something that is not its default, so a writer that forgets
// to emit one is caught rather than tested past.
//
// A new id in knownItems without a line here fails TestEveryKnownItemHasA
// RoundTripFixture. That is the point: the writer encodes items in a switch,
// and an id with no branch is written as a bare {"id": …} and read back as
// defaults. gpu and temperature sat in that gap and every configuration write
// silently discarded their display and interval.
var itemFixtures = map[string]Item{
	// Boundary is derived from Format at load, never stored, so the fixture
	// carries what a load produces rather than what a document holds.
	"clock":         {ID: "clock", Format: "Monday 02 January", Boundary: time.Minute},
	"workspace":     {ID: "workspace"},
	"window-title":  {ID: "window-title", MaxWidth: 240},
	"cpu":           {ID: "cpu", Display: "radial", Interval: 2 * time.Second},
	"memory":        {ID: "memory", Display: "meter", Interval: 3 * time.Second},
	"temperature":   {ID: "temperature", Display: "radial", Interval: 4 * time.Second},
	"gpu":           {ID: "gpu", Display: "radial", Interval: 5 * time.Second},
	"filesystem":    {ID: "filesystem", Display: "meter", Interval: 6 * time.Second, Path: "/"},
	"block":         {ID: "block", Display: "graph", Interval: 7 * time.Second, Device: "nvme0n1", Direction: "read"},
	"network":       {ID: "network", Display: "graph", Interval: 8 * time.Second, Interface: "wlan0", Direction: "rx"},
	"weather":       {ID: "weather", MaxWidth: 180, ShowCondition: true},
	"battery":       {ID: "battery", Label: "percent", WarnBelow: 20, Interval: 30 * time.Second},
	"notifications": {ID: "notifications"},
	"running-apps":  {ID: "running-apps"},
	"wordmark":      {ID: "wordmark"},
	"launcher":      {ID: "launcher"},
	"wallpaper":     {ID: "wallpaper"},
	"volume":        {ID: "volume"},
	"wifi":          {ID: "wifi"},
	"media":         {ID: "media"},
	"bluetooth":     {ID: "bluetooth"},
	"clipboard":     {ID: "clipboard"},
	"plugin":        {ID: "plugin", Plugin: "org.sysc.weather", Entry: "bar", Instance: "weather-1"},
	"group":         {ID: "group", Items: []Item{{ID: "cpu", Display: "radial", Interval: 2 * time.Second}}},
}

func TestEveryKnownItemHasARoundTripFixture(t *testing.T) {
	t.Parallel()
	for id := range knownItems {
		if _, ok := itemFixtures[id]; !ok {
			t.Errorf("no round-trip fixture for item %q; add one so the writer cannot silently drop its fields", id)
		}
	}
	for id := range itemFixtures {
		if _, ok := knownItems[id]; !ok {
			t.Errorf("fixture %q is not a known item", id)
		}
	}
}

// Writing a configuration and reading it back returns what went in. This is
// the invariant the metric writer broke: a field the writer does not emit is
// indistinguishable, on the next load, from one the user never set.
func TestEveryItemSurvivesAWriteAndRead(t *testing.T) {
	t.Parallel()
	for id, item := range itemFixtures {
		cfg := Default()
		cfg.Bar.Right = []Item{item}
		if id == "weather" {
			// A weather widget makes the location required, so the document
			// has to carry one for the round trip to be readable at all.
			cfg.Weather.Configured = true
			cfg.Weather.Latitude, cfg.Weather.Longitude = -33.87, 151.21
		}
		path := filepath.Join(t.TempDir(), "config.json")
		if err := Write(path, cfg); err != nil {
			t.Errorf("%s: write: %v", id, err)
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		back, err := Parse(raw)
		if err != nil {
			t.Errorf("%s: read back: %v\n%s", id, err, raw)
			continue
		}
		if len(back.Bar.Right) != 1 {
			t.Errorf("%s: right section = %d items, want one", id, len(back.Bar.Right))
			continue
		}
		if got := back.Bar.Right[0]; !reflect.DeepEqual(got, item) {
			t.Errorf("%s did not survive a round trip:\n got %+v\nwant %+v\ndocument:\n%s", id, got, item, raw)
		}
	}
}
