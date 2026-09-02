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

func historyFilter(chip string, ts, now time.Time) bool {
	ts = ts.In(now.Location())
	switch chip {
	case "all":
		return true
	case "1h":
		d := now.Sub(ts)
		return d >= 0 && d <= time.Hour
	case "today":
		return sameLocalDay(ts, now)
	case "yesterday":
		return sameLocalDay(ts, now.AddDate(0, 0, -1))
	case "7d":
		d := now.Sub(ts)
		return d >= 0 && d <= 7*24*time.Hour
	case "older":
		return now.Sub(ts) > 7*24*time.Hour
	}
	return false
}

func sameLocalDay(a, b time.Time) bool {
	y1, m1, d1 := a.Date()
	y2, m2, d2 := b.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}
