package services

import (
	"testing"
	"time"
)

// TestSunTimes checks the engine against three places NOAA's own page
// publishes: a mid-latitude summer day, a southern-hemisphere summer day,
// and a date inside the midnight sun.
func TestSunTimes(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skipf("no Europe/London tzdata: %v", err)
	}
	sydney, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Skipf("no Australia/Sydney tzdata: %v", err)
	}

	cases := []struct {
		name              string
		lat, lon          float64
		day               string
		loc               *time.Location
		wantRise, wantSet string
	}{
		{"london midsummer", 51.5074, -0.1278, "2026-06-21", london, "04:43", "21:21"},
		{"sydney midsummer", -33.8688, 151.2093, "2026-12-21", sydney, "05:41", "20:05"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			day, err := time.ParseInLocation("2006-01-02 15:04", tc.day+" 12:00", tc.loc)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			rise, set, polar := SunTimes(tc.lat, tc.lon, day, tc.loc)
			if polar != PolarNone {
				t.Fatalf("polar = %v, want PolarNone", polar)
			}
			checkMinute(t, "sunrise", rise, tc.wantRise, tc.loc)
			checkMinute(t, "sunset", set, tc.wantSet, tc.loc)
		})
	}
}

// TestSunTimesPolarDay covers the midnight sun: no sunrise, no sunset, and a
// polar state the night light can hold a temperature for.
func TestSunTimesPolarDay(t *testing.T) {
	tromso, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Skipf("no Europe/Oslo tzdata: %v", err)
	}
	day := time.Date(2026, 6, 21, 12, 0, 0, 0, tromso)
	_, _, polar := SunTimes(69.6496, 18.9560, day, tromso)
	if polar != PolarDay {
		t.Errorf("polar = %v, want PolarDay", polar)
	}
	// The same date half a year later is polar night, not polar day.
	winter := time.Date(2026, 12, 21, 12, 0, 0, 0, tromso)
	_, _, polar = SunTimes(69.6496, 18.9560, winter, tromso)
	if polar != PolarNight {
		t.Errorf("polar = %v, want PolarNight", polar)
	}
}

func checkMinute(t *testing.T, what string, got time.Time, want string, loc *time.Location) {
	t.Helper()
	wantMin, err := time.ParseInLocation("2006-01-02 15:04", got.Format("2006-01-02")+" "+want, loc)
	if err != nil {
		t.Fatalf("parse want %s: %v", want, err)
	}
	delta := got.Sub(wantMin)
	if delta < 0 {
		delta = -delta
	}
	if delta > 2*time.Minute {
		t.Errorf("%s = %s, want within 2 min of %s (off by %s)", what, got.Format("15:04"), want, delta)
	}
}
