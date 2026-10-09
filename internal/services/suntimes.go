// SunTimes computes sunrise and sunset locally from a latitude, longitude
// and wall-clock date. It is the NOAA sunrise equation, good to about a
// minute at the latitudes a desktop lives at, with no network call and no
// dependency. It is pure: the same inputs always give the same minute, which
// is what lets the night light recompute after a restart mid-fade.
//
// The shared light/dark schedule work reuses this function, so nothing here
// knows about colour temperature.
package services

import (
	"math"
	"time"
)

// PolarState reports what the sun did on a day at an extreme latitude.
type PolarState uint8

const (
	// PolarNone means the sun rose and set normally.
	PolarNone PolarState = iota
	// PolarDay means the sun never set, so the day temperature holds all day.
	PolarDay
	// PolarNight means the sun never rose, so the night temperature holds.
	PolarNight
)

const (
	sunZenith = 90.833 // degrees, including refraction and the solar disc
	degToRad  = math.Pi / 180
	radToDeg  = 180 / math.Pi
)

// SunTimes returns sunrise and sunset for the calendar day that day falls on
// in loc, at the given coordinates. At a polar latitude both are the zero
// time and polar says which temperature the caller should hold instead. A nil
// loc means UTC.
func SunTimes(lat, lon float64, day time.Time, loc *time.Location) (sunrise, sunset time.Time, polar PolarState) {
	if loc == nil {
		loc = time.UTC
	}
	y, m, d := day.In(loc).Date()
	n := dayOfYear(y, m, d)

	// Both events share one hour-angle cosine, so they always agree on the
	// polar case and the sunrise call is the one that carries it.
	rise, polar := sunTimesOne(lat, lon, n, true)
	if polar != PolarNone {
		return time.Time{}, time.Time{}, polar
	}
	set, _ := sunTimesOne(lat, lon, n, false)

	// The equation counts UTC minutes from the same calendar day, so the
	// zone offset is applied here. It can push an event past midnight for a
	// far-eastern or far-western longitude; wrapping keeps the clock time
	// on the day the caller asked about.
	_, offsetSeconds := time.Date(y, m, d, 12, 0, 0, 0, loc).Zone()
	offset := float64(offsetSeconds) / 60

	midnight := time.Date(y, m, d, 0, 0, 0, 0, loc)
	return midnight.Add(time.Duration(wrapMinutes(rise+offset)) * time.Minute),
		midnight.Add(time.Duration(wrapMinutes(set+offset)) * time.Minute), PolarNone
}

// dayOfYear is the NOAA day number, which is what its anomaly terms expect.
func dayOfYear(y int, m time.Month, d int) int {
	year := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Sub(year).Hours()/24) + 1
}

// sunTimesOne runs the NOAA sunrise equation for one of the two events and
// returns the minute offset from local midnight. The cosine of the hour angle
// is out of range exactly at a polar latitude: above 1 the sun never clears
// the horizon, below -1 it never drops below it.
func sunTimesOne(lat, lon float64, n int, rising bool) (minutes float64, polar PolarState) {
	lngHour := lon / 15
	t := float64(n) + (6-lngHour)/24
	if !rising {
		t = float64(n) + (18-lngHour)/24
	}

	// Sun's mean anomaly, then its ecliptic longitude.
	anomaly := 0.9856*t - 3.289
	ecliptic := wrapDeg(anomaly + 1.916*math.Sin(anomaly*degToRad) + 0.020*math.Sin(2*anomaly*degToRad) + 282.634)

	// Right ascension in hours. atan2 keeps the sun's own quadrant, which
	// plain atan(tan) cannot: the solstice puts the ecliptic within a
	// hundredth of a degree of 90 and the naive form wraps there.
	ra := radToDeg * math.Atan2(0.91764*math.Sin(ecliptic*degToRad), math.Cos(ecliptic*degToRad))
	ra /= 15

	sinDec := 0.39782 * math.Sin(ecliptic*degToRad)
	cosDec := math.Cos(math.Asin(sinDec))

	cosH := (math.Cos(sunZenith*degToRad) - sinDec*math.Sin(lat*degToRad)) / (cosDec * math.Cos(lat*degToRad))
	if cosH > 1 {
		return 0, PolarNight
	}
	if cosH < -1 {
		return 0, PolarDay
	}

	hour := radToDeg * math.Acos(cosH)
	if rising {
		hour = 360 - hour
	}
	hour /= 15

	// Local mean time of the event, then the wall clock in loc.
	meanTime := hour + ra - 0.06571*t - 6.622
	return wrapHours(meanTime-lngHour) * 60, PolarNone
}

func wrapDeg(deg float64) float64 {
	deg = math.Mod(deg, 360)
	if deg < 0 {
		deg += 360
	}
	return deg
}

func wrapHours(hours float64) float64 {
	hours = math.Mod(hours, 24)
	if hours < 0 {
		hours += 24
	}
	return hours
}

func wrapMinutes(minutes float64) float64 {
	minutes = math.Mod(minutes, 24*60)
	if minutes < 0 {
		minutes += 24 * 60
	}
	return minutes
}
