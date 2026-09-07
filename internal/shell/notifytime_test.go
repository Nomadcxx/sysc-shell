package shell

import (
	"slices"
	"testing"
	"time"
)

func TestFormatNotifyTime(t *testing.T) {
	now := time.Date(2026, 9, 3, 15, 4, 0, 0, time.Local)
	cases := []struct {
		ts   time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-90 * time.Minute), "13:34"},
		{now.Add(-26 * time.Hour), "Wednesday, 13:04"},
	}
	for _, c := range cases {
		if got := formatNotifyTime(c.ts, now); got != c.want {
			t.Fatalf("formatNotifyTime(%v) = %q, want %q", c.ts, got, c.want)
		}
	}
}

func TestHistoryFilter(t *testing.T) {
	now := time.Date(2026, 9, 3, 15, 0, 0, 0, time.Local)
	chips := []string{"all", "today", "yesterday", "earlier"}
	cases := []struct {
		name string
		ts   time.Time
		want []string
	}{
		{"30m ago", now.Add(-30 * time.Minute), []string{"all", "today"}},
		{"2h ago today", now.Add(-2 * time.Hour), []string{"all", "today"}},
		{"yesterday noon", time.Date(2026, 9, 2, 12, 0, 0, 0, time.Local), []string{"all", "yesterday"}},
		{"3d ago", now.Add(-3 * 24 * time.Hour), []string{"all", "earlier"}},
		{"8d ago", now.Add(-8 * 24 * time.Hour), []string{"all", "earlier"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, chip := range chips {
				got := historyFilter(chip, c.ts, now)
				want := slices.Contains(c.want, chip)
				if got != want {
					t.Fatalf("historyFilter(%q, %v) = %v, want %v", chip, c.ts, got, want)
				}
			}
		})
	}
}

func TestHistoryFilterFourBuckets(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 0, 0, 0, time.Local)
	cases := []struct {
		name   string
		ts     time.Time
		bucket string
		want   bool
	}{
		{"today midday", now.Add(-2 * time.Hour), "today", true},
		{"today at local midnight", time.Date(2026, 9, 7, 0, 0, 0, 0, time.Local), "today", true},
		{"yesterday just before midnight", time.Date(2026, 9, 6, 23, 59, 59, 0, time.Local), "yesterday", true},
		{"yesterday is not today", now.AddDate(0, 0, -1), "today", false},
		{"two days back is earlier", now.AddDate(0, 0, -2), "earlier", true},
		{"yesterday is not earlier", now.AddDate(0, 0, -1), "earlier", false},
		{"ten days back is earlier", now.AddDate(0, 0, -10), "earlier", true},
		{"all takes everything", now.AddDate(0, 0, -30), "all", true},
	}
	for _, c := range cases {
		if got := historyFilter(c.bucket, c.ts, now); got != c.want {
			t.Fatalf("%s: historyFilter(%q) = %v, want %v", c.name, c.bucket, got, c.want)
		}
	}
	for _, retired := range []string{"1h", "7d", "older"} {
		if historyFilter(retired, now, now) {
			t.Fatalf("retired bucket %q still matches", retired)
		}
	}
}
