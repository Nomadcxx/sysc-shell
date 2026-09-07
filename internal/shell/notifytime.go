package shell

import (
	"strconv"
	"time"
)

func formatNotifyTime(ts, now time.Time) string {
	d := now.Sub(ts)
	if d < time.Minute {
		return "now"
	}
	if d < time.Hour {
		return strconv.Itoa(int(d/time.Minute)) + "m ago"
	}
	ts = ts.In(now.Location())
	if sameLocalDay(ts, now) {
		return ts.Format("15:04")
	}
	return ts.Weekday().String() + ", " + ts.Format("15:04")
}

// historyFilter reports whether ts falls in the named bucket. The set is the
// four the centre's segmented row offers; anything else answers false, so a
// stale action string filters everything out rather than showing everything.
func historyFilter(bucket string, ts, now time.Time) bool {
	ts = ts.In(now.Location())
	switch bucket {
	case "all":
		return true
	case "today":
		return sameLocalDay(ts, now)
	case "yesterday":
		return sameLocalDay(ts, now.AddDate(0, 0, -1))
	case "earlier":
		return !sameLocalDay(ts, now) && !sameLocalDay(ts, now.AddDate(0, 0, -1)) && ts.Before(now)
	}
	return false
}

func sameLocalDay(a, b time.Time) bool {
	y1, m1, d1 := a.Date()
	y2, m2, d2 := b.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}
