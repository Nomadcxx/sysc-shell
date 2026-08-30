package shell

import (
	"testing"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// fixtureSnapshot carries one of every source with invented values.
func fixtureSnapshot() services.Snapshot {
	return services.Snapshot{
		CollectedAt: time.Date(2026, 8, 30, 15, 4, 5, 0, time.UTC),
		CPU:         &metrics.CPUSnapshot{Usage: metrics.CPUUsage{Fraction: 0.42, Valid: true}},
		Memory: &metrics.MemorySnapshot{
			Memory: metrics.Capacity{TotalBytes: 1000, UsedBytes: 250},
		},
		Filesystem: &metrics.FilesystemSnapshot{Filesystems: []metrics.Filesystem{{
			MountPoint: "/fixture",
			Capacity:   metrics.Capacity{TotalBytes: 200, UsedBytes: 100},
		}}},
		Block: &metrics.BlockSnapshot{Devices: []metrics.BlockDevice{{
			Name:  "nvme9n1",
			Rates: metrics.BlockRates{ReadBytesPerSecond: 3_200_000, Valid: true},
		}}},
		Network: &metrics.NetworkSnapshot{Interfaces: []metrics.NetworkInterface{{
			Name:  "eth9",
			Rates: metrics.NetworkRates{ReceiveBytesPerSecond: 1_500_000, Valid: true},
		}}},
	}
}

func TestFractionSourcesFormatAsPercentages(t *testing.T) {
	t.Parallel()
	snap := fixtureSnapshot()
	cases := []struct {
		item config.Item
		want string
	}{
		{config.Item{ID: "cpu"}, "42%"},
		{config.Item{ID: "memory"}, "25%"},
		{config.Item{ID: "filesystem", Path: "/fixture"}, "50%"},
	}
	for _, c := range cases {
		if got := formatMetric(c.item, snap); got != c.want {
			t.Fatalf("%s formatted %q, want %q", c.item.ID, got, c.want)
		}
	}
}

func TestRateSourcesFormatInDecimalUnits(t *testing.T) {
	t.Parallel()
	snap := fixtureSnapshot()

	block := config.Item{ID: "block", Device: "nvme9n1", Direction: "read"}
	if got := formatMetric(block, snap); got != "3.2 MB/s" {
		t.Fatalf("block rate = %q, want 3.2 MB/s", got)
	}
	network := config.Item{ID: "network", Interface: "eth9", Direction: "rx"}
	if got := formatMetric(network, snap); got != "1.5 MB/s" {
		t.Fatalf("network rate = %q, want 1.5 MB/s", got)
	}
}

// An absent source, an absent subject and an invalid value all render the same
// placeholder, so a consumer never distinguishes them.
func TestUnavailableMetricsRenderThePlaceholder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		item config.Item
		snap services.Snapshot
	}{
		{"source absent", config.Item{ID: "cpu"}, services.Snapshot{}},
		{
			"mount absent",
			config.Item{ID: "filesystem", Path: "/nowhere"},
			fixtureSnapshot(),
		},
		{
			"device absent",
			config.Item{ID: "block", Device: "absent0", Direction: "read"},
			fixtureSnapshot(),
		},
		{
			"value invalid",
			config.Item{ID: "cpu"},
			services.Snapshot{CPU: &metrics.CPUSnapshot{
				Usage: metrics.CPUUsage{Fraction: 0.9, Valid: false},
			}},
		},
	}
	for _, c := range cases {
		if got := formatMetric(c.item, c.snap); got != noWorkspace {
			t.Fatalf("%s rendered %q, want %q", c.name, got, noWorkspace)
		}
	}
}

// The first rate sample is always invalid, because there is no previous
// counter to compare against.
func TestTheFirstRateSampleRendersThePlaceholder(t *testing.T) {
	t.Parallel()
	snap := services.Snapshot{Network: &metrics.NetworkSnapshot{
		Interfaces: []metrics.NetworkInterface{{
			Name:  "eth9",
			Rates: metrics.NetworkRates{Valid: false},
		}},
	}}
	item := config.Item{ID: "network", Interface: "eth9", Direction: "rx"}
	if got := formatMetric(item, snap); got != noWorkspace {
		t.Fatalf("first sample rendered %q, want %q", got, noWorkspace)
	}
}

func TestDirectionSelectsTheCounter(t *testing.T) {
	t.Parallel()
	snap := services.Snapshot{Network: &metrics.NetworkSnapshot{
		Interfaces: []metrics.NetworkInterface{{
			Name: "eth9",
			Rates: metrics.NetworkRates{
				ReceiveBytesPerSecond:  1_000_000,
				TransmitBytesPerSecond: 2_000_000,
				Valid:                  true,
			},
		}},
	}}
	rx := config.Item{ID: "network", Interface: "eth9", Direction: "rx"}
	tx := config.Item{ID: "network", Interface: "eth9", Direction: "tx"}
	if got := formatMetric(rx, snap); got != "1.0 MB/s" {
		t.Fatalf("rx = %q, want 1.0 MB/s", got)
	}
	if got := formatMetric(tx, snap); got != "2.0 MB/s" {
		t.Fatalf("tx = %q, want 2.0 MB/s", got)
	}
}

// A meter needs a fraction. Rate sources have none, and the loader already
// rejects a meter on them, so this reports absent rather than guessing.
func TestOnlyFractionSourcesReportAFraction(t *testing.T) {
	t.Parallel()
	snap := fixtureSnapshot()

	if got, ok := metricFraction(config.Item{ID: "cpu"}, snap); !ok || got != 0.42 {
		t.Fatalf("cpu fraction = %v/%v, want 0.42/true", got, ok)
	}
	item := config.Item{ID: "network", Interface: "eth9", Direction: "rx"}
	if _, ok := metricFraction(item, snap); ok {
		t.Fatal("a rate source reported a fraction")
	}
}

func TestEveryMetricIDMapsToASelector(t *testing.T) {
	t.Parallel()
	want := map[string]services.Selector{
		"cpu":     {Source: services.SourceCPU},
		"memory":  {Source: services.SourceMemory},
		"battery": {Source: services.SourceBattery},
	}
	for id, sel := range want {
		got, ok := metricSelector(config.Item{ID: id})
		if !ok || got != sel {
			t.Fatalf("%s mapped to %v/%v, want %v", id, got, ok, sel)
		}
	}
	if _, ok := metricSelector(config.Item{ID: "clock"}); ok {
		t.Fatal("a non-metric id mapped to a selector")
	}
}

// A selector must carry the widget's subject and direction, not just its
// source, or two widgets watching different interfaces would be one lease and
// one history ring.
func TestASelectorCarriesTheSubjectAndDirection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		item config.Item
		want services.Selector
	}{
		{
			config.Item{ID: "filesystem", Path: "/fixture"},
			services.Selector{Source: services.SourceFilesystem, Subject: "/fixture"},
		},
		{
			config.Item{ID: "block", Device: "nvme9n1", Direction: "write"},
			services.Selector{Source: services.SourceBlock, Subject: "nvme9n1", Direction: "write"},
		},
		{
			config.Item{ID: "network", Interface: "eth9", Direction: "rx"},
			services.Selector{Source: services.SourceNetwork, Subject: "eth9", Direction: "rx"},
		},
	}
	for _, c := range cases {
		got, ok := metricSelector(c.item)
		if !ok || got != c.want {
			t.Fatalf("%+v mapped to %v/%v, want %v", c.item, got, ok, c.want)
		}
	}

	rx, _ := metricSelector(config.Item{ID: "network", Interface: "eth9", Direction: "rx"})
	tx, _ := metricSelector(config.Item{ID: "network", Interface: "eth9", Direction: "tx"})
	if rx == tx {
		t.Fatal("the two directions of one interface share a selector")
	}
}
