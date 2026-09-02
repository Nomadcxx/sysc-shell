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
	chips := []string{"all", "1h", "today", "yesterday", "7d", "older"}
	cases := []struct {
		name string
		ts   time.Time
		want []string
	}{
		{"30m ago", now.Add(-30 * time.Minute), []string{"all", "1h", "today", "7d"}},
		{"2h ago today", now.Add(-2 * time.Hour), []string{"all", "today", "7d"}},
		{"yesterday noon", time.Date(2026, 9, 2, 12, 0, 0, 0, time.Local), []string{"all", "yesterday", "7d"}},
		{"3d ago", now.Add(-3 * 24 * time.Hour), []string{"all", "7d"}},
		{"8d ago", now.Add(-8 * 24 * time.Hour), []string{"all", "older"}},
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
