package wallpaper

import (
	"time"
)

const (
	CycleModeInterval = "interval"
	CycleModeTime     = "time"
)

// CyclePolicy is the global schedule config projected by the shell.
type CyclePolicy struct {
	Enabled  bool
	Mode     string
	Interval time.Duration
	Time     string
}

func cycleDue(policy CyclePolicy, now, intervalStart time.Time, lastDailyDate string) bool {
	if !policy.Enabled {
		return false
	}
	switch policy.Mode {
	case CycleModeInterval:
		return policy.Interval >= time.Minute && now.Sub(intervalStart) >= policy.Interval
	case CycleModeTime:
		if !validCycleTime(policy.Time) {
			return false
		}
		local := now.Local()
		return local.Format("15:04") >= policy.Time && local.Format("2006-01-02") != lastDailyDate
	default:
		return false
	}
}

func dailyCycleBaseline(now time.Time, target string) string {
	local := now.Local()
	date := local.Format("2006-01-02")
	if !validCycleTime(target) {
		return date
	}
	if local.Format("15:04") >= target {
		return date
	}
	return local.AddDate(0, 0, -1).Format("2006-01-02")
}

func validCycleTime(value string) bool {
	parsed, err := time.Parse("15:04", value)
	return err == nil && parsed.Format("15:04") == value
}

func nextCycleEntry(entries []Entry, current string) (Entry, bool) {
	unique := make([]Entry, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir || entry.Kind == KindUnknown || entry.Path == "" {
			continue
		}
		if _, ok := seen[entry.Path]; ok {
			continue
		}
		seen[entry.Path] = struct{}{}
		unique = append(unique, entry)
	}
	if len(unique) < 2 {
		return Entry{}, false
	}
	start := -1
	for i := range unique {
		if unique[i].Path == current {
			start = i
			break
		}
	}
	if start < 0 {
		start = len(unique) - 1
	}
	for offset := 1; offset < len(unique); offset++ {
		entry := unique[(start+offset)%len(unique)]
		if entry.Path != current {
			return entry, true
		}
	}
	return Entry{}, false
}
