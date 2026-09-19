package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Every metric id keeps its settings across a write. The writer listed the ids
// it knew how to encode by hand, and that list fell behind the loader's: an id
// missing from it was written as a bare {"id": …} and came back with the
// defaults, so writing a configuration silently discarded what the user chose.
// A bar with four radial gauges came back with two, because gpu and
// temperature were the two ids nobody had added.
func TestEveryMetricIDSurvivesAWrite(t *testing.T) {
	ids := map[string]bool{}
	for id := range fractionSources {
		ids[id] = true
	}
	for id := range rateSources {
		ids[id] = true
	}
	for id := range ids {
		display := "radial"
		if rateSources[id] {
			// A rate has no full scale, so the loader refuses a gauge on it.
			display = "graph"
		}
		item := Item{ID: id, Display: display, Interval: 2 * time.Second}
		switch id {
		case "filesystem":
			item.Path = "/"
		case "block":
			item.Device, item.Direction = "nvme0n1", "read"
		case "network":
			item.Interface, item.Direction = "wlan0", "rx"
		}

		cfg := Default()
		cfg.Bar.Right = []Item{item}
		path := filepath.Join(t.TempDir(), "config.json")
		if err := Write(path, cfg); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		back, err := Parse(raw)
		if err != nil {
			t.Fatalf("%s: %v\n%s", id, err, raw)
		}
		if len(back.Bar.Right) != 1 {
			t.Fatalf("%s: right section = %d items, want one", id, len(back.Bar.Right))
		}
		got := back.Bar.Right[0]
		if got.Display != display {
			t.Errorf("%s: display = %q after a round trip, want %q", id, got.Display, display)
		}
		if got.Interval != 2*time.Second {
			t.Errorf("%s: interval = %v after a round trip, want 2s", id, got.Interval)
		}
	}
}
