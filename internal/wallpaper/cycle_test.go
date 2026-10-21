package wallpaper

import (
	"testing"
	"time"
)

func TestNextCycleEntryAdvancesWrapsAndRecoversMissingCurrent(t *testing.T) {
	entries := []Entry{
		{Name: "a.png", Path: "/w/a.png", Kind: KindImage},
		{Name: "b.mp4", Path: "/w/b.mp4", Kind: KindVideo},
		{Name: "c.png", Path: "/w/c.png", Kind: KindImage},
	}
	for _, tc := range []struct {
		name    string
		current string
		want    string
	}{
		{"advance", "/w/a.png", "/w/b.mp4"},
		{"wrap", "/w/c.png", "/w/a.png"},
		{"missing current", "/w/missing.png", "/w/a.png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := nextCycleEntry(entries, tc.current)
			if !ok || got.Path != tc.want {
				t.Fatalf("nextCycleEntry(%q) = %+v, %v; want %q", tc.current, got, ok, tc.want)
			}
		})
	}
}

func TestNextCycleEntryNoopsWithoutAChoice(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []Entry
		current string
	}{
		{"empty", nil, ""},
		{"one item", []Entry{{Path: "/w/a.png", Kind: KindImage}}, "/w/a.png"},
		{"one replacement", []Entry{{Path: "/w/a.png", Kind: KindImage}}, "/w/missing.png"},
		{"directories only", []Entry{{Path: "/w/folder", IsDir: true}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := nextCycleEntry(tc.entries, tc.current); ok {
				t.Fatalf("nextCycleEntry() = %+v, want no choice", got)
			}
		})
	}
}

func TestCycleDueUsesIntervalAndEnableState(t *testing.T) {
	started := time.Now()
	policy := CyclePolicy{Enabled: true, Mode: CycleModeInterval, Interval: 5 * time.Minute}
	for _, tc := range []struct {
		name string
		now  time.Time
		want bool
	}{
		{"before", started.Add(5*time.Minute - time.Nanosecond), false},
		{"at deadline", started.Add(5 * time.Minute), true},
		{"disabled", started.Add(time.Hour), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := policy
			if tc.name == "disabled" {
				p.Enabled = false
			}
			if got := cycleDue(p, tc.now, started, ""); got != tc.want {
				t.Fatalf("cycleDue() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCycleDueDailyUsesLocalDateOnce(t *testing.T) {
	target := "12:00"
	before := time.Date(2026, 10, 18, 11, 59, 59, 0, time.Local)
	policy := CyclePolicy{Enabled: true, Mode: CycleModeTime, Time: target}
	yesterday := before.AddDate(0, 0, -1).Format("2006-01-02")
	today := before.Format("2006-01-02")
	for _, tc := range []struct {
		name    string
		now     time.Time
		lastDay string
		want    bool
	}{
		{"before target", before, yesterday, false},
		{"target passed", before.Add(time.Second), yesterday, true},
		{"already ran today", before.Add(time.Second), today, false},
		{"next local day", time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local), today, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cycleDue(policy, tc.now, before, tc.lastDay); got != tc.want {
				t.Fatalf("cycleDue() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCycleDueRejectsMalformedDailyTime(t *testing.T) {
	now := time.Now()
	for _, value := range []string{"8:30", "25:00"} {
		policy := CyclePolicy{Enabled: true, Mode: CycleModeTime, Time: value}
		if cycleDue(policy, now, now, "") {
			t.Errorf("cycleDue accepted non-canonical time %q", value)
		}
	}
}

func TestDailyCycleBaselineAvoidsImmediateRunAfterTarget(t *testing.T) {
	target := "12:00"
	before := time.Date(2026, 10, 18, 11, 0, 0, 0, time.Local)
	after := time.Date(2026, 10, 18, 13, 0, 0, 0, time.Local)
	if got, want := dailyCycleBaseline(before, target), before.AddDate(0, 0, -1).Format("2006-01-02"); got != want {
		t.Fatalf("baseline before target = %q, want %q", got, want)
	}
	if got, want := dailyCycleBaseline(after, target), after.Format("2006-01-02"); got != want {
		t.Fatalf("baseline after target = %q, want %q", got, want)
	}
}
