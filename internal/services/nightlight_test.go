package services

import (
	"math"
	"sort"
	"strings"
	"testing"
	"time"

	wayland "github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

// drainRequests counts the requests already queued without blocking, so a test
// can assert that a tick sent nothing.
func drainRequests(reqs <-chan wayland.GammaRequest) int {
	n := 0
	for {
		select {
		case <-reqs:
			n++
		default:
			return n
		}
	}
}

func TestNightLightManualToggleFadesOverOneSecond(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	s := NewNightLight(NightLightOptions{Now: func() time.Time { return now }})
	s.Schedule(NightLightSchedule{Mode: NightLightOff, NightK: 4000, DayK: 6500})
	s.GammaEvent(wayland.GammaEvent{Global: 1, State: wayland.GammaReady})
	drainRequests(s.Requests())

	s.Toggle()
	if got := s.State(); got.Kelvin != 6500 || got.Target != 4000 {
		t.Fatalf("at toggle = %+v, want a neutral start fading to 4000 K", got)
	}

	now = now.Add(500 * time.Millisecond)
	s.Recompute()
	if got := s.State(); got.Kelvin <= 4000 || got.Kelvin >= 6500 {
		t.Fatalf("halfway through fade = %d K, want between 4000 and 6500", got.Kelvin)
	}

	now = now.Add(500 * time.Millisecond)
	s.Recompute()
	if got := s.State(); got.Kelvin != 4000 {
		t.Fatalf("after one second = %d K, want 4000", got.Kelvin)
	}
}

func TestNightLightReducedMotionSkipsManualFade(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	s := NewNightLight(NightLightOptions{Now: func() time.Time { return now }, ReducedMotion: true})
	s.Schedule(NightLightSchedule{Mode: NightLightOff, NightK: 4000, DayK: 6500})
	s.GammaEvent(wayland.GammaEvent{Global: 1, State: wayland.GammaReady})
	drainRequests(s.Requests())

	s.Toggle()
	if got := s.State(); got.Kelvin != 4000 || got.Target != 4000 {
		t.Fatalf("toggle with reduced motion = %+v, want an immediate 4000 K", got)
	}
}

func london(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	return loc
}

// TestKelvinAtModeOff pins that off holds nothing: the caller restores the
// outputs rather than tinting them.
func TestKelvinAtModeOff(t *testing.T) {
	s := NightLightSchedule{Mode: NightLightOff, NightK: 4000, DayK: 6500}
	kelvin, next := KelvinAt(s, time.Now())
	if kelvin != 0 || !next.IsZero() {
		t.Fatalf("off = (%d, %v), want (0, zero)", kelvin, next)
	}
}

// TestKelvinAtAlwaysIgnoresTheClock pins always mode: constant night
// temperature, no boundary to resume from.
func TestKelvinAtAlwaysIgnoresTheClock(t *testing.T) {
	s := NightLightSchedule{Mode: NightLightAlways, NightK: 3500, DayK: 6500}
	for _, hour := range []int{0, 6, 12, 23} {
		kelvin, next := KelvinAt(s, time.Date(2026, 10, 8, hour, 0, 0, 0, time.UTC))
		if kelvin != 3500 || !next.IsZero() {
			t.Fatalf("hour %d = (%d, %v), want (3500, zero)", hour, kelvin, next)
		}
	}
}

// TestKelvinAtSunsetTransitionsInMireds pins the centred transition: the
// midpoint is the mired midpoint, not the kelvin midpoint, and the ends are
// exactly the day and night temperatures.
func TestKelvinAtSunsetTransitionsInMireds(t *testing.T) {
	loc := london(t)
	s := NightLightSchedule{
		Mode:       NightLightSunset,
		NightK:     4000,
		DayK:       6500,
		Transition: 30 * time.Minute,
		Lat:        51.5074,
		Lon:        -0.1278,
		// HasLocation is not consulted by KelvinAt; it is the service's gate.
		HasLocation: true,
	}
	day := time.Date(2026, 10, 8, 0, 0, 0, 0, loc)
	_, sunset, _ := SunTimes(s.Lat, s.Lon, day, loc)

	if kelvin, _ := KelvinAt(s, sunset.Add(-15*time.Minute)); kelvin != 6500 {
		t.Fatalf("transition start = %d K, want 6500", kelvin)
	}
	mid := sunset
	if kelvin, _ := KelvinAt(s, mid); kelvin != lerpMired(6500, 4000, 0.5) {
		t.Fatalf("midpoint = %d K, want %d", kelvin, lerpMired(6500, 4000, 0.5))
	}
	if kelvin, _ := KelvinAt(s, sunset.Add(15*time.Minute)); kelvin != 4000 {
		t.Fatalf("transition end = %d K, want 4000", kelvin)
	}
}

// TestKelvinAtSunsetAcceptance pins the acceptance criterion: London on
// 2026-10-08, fifteen minutes after sunset, within 50 K of the night target.
func TestKelvinAtSunsetAcceptance(t *testing.T) {
	loc := london(t)
	s := NightLightSchedule{
		Mode: NightLightSunset, NightK: 4000, DayK: 6500,
		Transition: 30 * time.Minute, Lat: 51.5074, Lon: -0.1278, HasLocation: true,
	}
	_, sunset, _ := SunTimes(s.Lat, s.Lon, time.Date(2026, 10, 8, 0, 0, 0, 0, loc), loc)
	kelvin, _ := KelvinAt(s, sunset.Add(15*time.Minute))
	if d := kelvin - 4000; d > 50 || d < -50 {
		t.Fatalf("sunset+15m = %d K, want within 50 K of 4000", kelvin)
	}
}

// TestKelvinAtIsDeterministic is the restart criterion: the same schedule and
// the same instant give the same answer however many times it is asked.
func TestKelvinAtIsDeterministic(t *testing.T) {
	loc := london(t)
	s := NightLightSchedule{
		Mode: NightLightSunset, NightK: 4000, DayK: 6500,
		Transition: 30 * time.Minute, Lat: 51.5074, Lon: -0.1278, HasLocation: true,
	}
	_, sunset, _ := SunTimes(s.Lat, s.Lon, time.Date(2026, 10, 8, 0, 0, 0, 0, loc), loc)
	at := sunset.Add(-3 * time.Minute)
	first, nextFirst := KelvinAt(s, at)
	for i := 0; i < 5; i++ {
		got, next := KelvinAt(s, at)
		if got != first || !next.Equal(nextFirst) {
			t.Fatalf("run %d = (%d, %v), want (%d, %v)", i, got, next, first, nextFirst)
		}
	}
}

// TestKelvinAtSunsetHoldsNightAcrossMidnight pins the night spanning midnight:
// after sunset and before the next sunrise the level is the night temperature,
// and the next change is the sunrise transition.
func TestKelvinAtSunsetHoldsNightAcrossMidnight(t *testing.T) {
	loc := london(t)
	s := NightLightSchedule{
		Mode: NightLightSunset, NightK: 3000, DayK: 6000,
		Transition: 30 * time.Minute, Lat: 51.5074, Lon: -0.1278, HasLocation: true,
	}
	midnight := time.Date(2026, 10, 8, 0, 30, 0, 0, loc)
	kelvin, next := KelvinAt(s, midnight)
	if kelvin != 3000 {
		t.Fatalf("just after midnight = %d K, want 3000", kelvin)
	}
	rise, _, _ := SunTimes(s.Lat, s.Lon, time.Date(2026, 10, 8, 0, 0, 0, 0, loc), loc)
	if want := rise.Add(-15 * time.Minute); !next.Equal(want) {
		t.Fatalf("next change = %v, want %v", next, want)
	}
}

// TestKelvinAtCustomCrossesMidnight pins a custom window whose end is earlier
// in the day than its start, so the night runs over midnight.
func TestKelvinAtCustomCrossesMidnight(t *testing.T) {
	s := NightLightSchedule{
		Mode: NightLightCustom, NightK: 3000, DayK: 6000,
		Transition: 30 * time.Minute,
		Start:      22 * time.Hour,
		End:        6 * time.Hour,
	}
	if kelvin, _ := KelvinAt(s, time.Date(2026, 10, 8, 23, 0, 0, 0, time.UTC)); kelvin != 3000 {
		t.Fatalf("23:00 = %d K, want 3000", kelvin)
	}
	if kelvin, _ := KelvinAt(s, time.Date(2026, 10, 8, 3, 0, 0, 0, time.UTC)); kelvin != 3000 {
		t.Fatalf("03:00 = %d K, want 3000", kelvin)
	}
	if kelvin, _ := KelvinAt(s, time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)); kelvin != 6000 {
		t.Fatalf("12:00 = %d K, want 6000", kelvin)
	}
}

// TestKelvinAtCustomClampsATransitionLongerThanTheNight pins that a 60-minute
// centred transition in an 8-hour night does not overlap the morning window.
func TestKelvinAtCustomClampsATransitionLongerThanTheNight(t *testing.T) {
	s := NightLightSchedule{
		Mode: NightLightCustom, NightK: 3000, DayK: 6000,
		Transition: 10 * time.Hour,
		Start:      22 * time.Hour,
		End:        6 * time.Hour,
	}
	// Clamped to the 8 h gap, so the fade spans 18:00–02:00 and 23:00 is
	// five eighths of the way from day to night.
	kelvin, _ := KelvinAt(s, time.Date(2026, 10, 8, 23, 0, 0, 0, time.UTC))
	want := lerpMired(6000, 3000, 0.625)
	if kelvin != want {
		t.Fatalf("23:00 = %d K, want %d", kelvin, want)
	}
}

func TestCustomTimesFollowWallClockAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	s := NightLightSchedule{Mode: NightLightCustom, NightK: 3000, DayK: 6000,
		Start: 2*time.Hour + 30*time.Minute, End: 7 * time.Hour}
	now := time.Date(2026, 3, 29, 3, 0, 0, 0, loc)
	if kelvin, _ := KelvinAt(s, now); kelvin != 3000 {
		t.Fatalf("03:00 local = %d K, want 3000 after the 02:30 wall-clock start", kelvin)
	}
}

func TestSunsetTransitionClampsToTheShortSolarGap(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	s := NightLightSchedule{Mode: NightLightSunset, NightK: 4000, DayK: 6500,
		Transition: 6 * time.Hour, Lat: 65.5, Lon: 18.9560, HasLocation: true}
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, loc)
	windows, polar := s.sunWindows(now, loc)
	if polar != PolarNone {
		t.Fatalf("day state = %v, want ordinary sunrise and sunset", polar)
	}
	clamped := false
	sort.Slice(windows, func(i, j int) bool { return windows[i].start.Before(windows[j].start) })
	for i, window := range windows {
		if window.end.Sub(window.start) < s.Transition {
			clamped = true
		}
		if i > 0 && windows[i-1].end.After(window.start) {
			t.Fatalf("solar transition windows overlap: %+v then %+v", windows[i-1], window)
		}
	}
	if !clamped {
		t.Fatal("six-hour transition was not clamped around the short midsummer solar gap")
	}
}

// TestKelvinAtZeroTransitionIsAStep pins a 0-minute transition: an instant
// change at the boundary, not a 30-minute fade.
func TestKelvinAtZeroTransitionIsAStep(t *testing.T) {
	s := NightLightSchedule{
		Mode: NightLightCustom, NightK: 3000, DayK: 6000,
		Start: 22 * time.Hour,
		End:   6 * time.Hour,
	}
	if kelvin, _ := KelvinAt(s, time.Date(2026, 10, 8, 21, 59, 0, 0, time.UTC)); kelvin != 6000 {
		t.Fatalf("before the boundary = %d K, want 6000", kelvin)
	}
	if kelvin, _ := KelvinAt(s, time.Date(2026, 10, 8, 22, 0, 0, 0, time.UTC)); kelvin != 3000 {
		t.Fatalf("at the boundary = %d K, want 3000", kelvin)
	}
}

func TestTransitionTickKeepsEveryScheduleStepWithinFiftyKelvin(t *testing.T) {
	loc := time.UTC
	s := NightLightSchedule{
		Mode: NightLightCustom, NightK: 2500, DayK: 6500,
		Transition: 15 * time.Minute,
		Start:      20 * time.Hour,
		End:        7 * time.Hour,
	}
	now := time.Date(2026, 10, 8, 19, 52, 30, 0, loc)
	kelvin, next, _ := s.evaluate(now, loc)
	service := NewNightLight(NightLightOptions{Now: func() time.Time { return now }})
	service.sched, service.loc = s, loc
	step := service.delay(now, kelvin, next)
	if step > nightLightTransitionTick {
		t.Fatalf("transition tick = %v, want at most %v", step, nightLightTransitionTick)
	}
	nextKelvin, _, _ := s.evaluate(now.Add(step), loc)
	if delta := int(math.Abs(float64(nextKelvin - kelvin))); delta > 50 {
		t.Fatalf("transition step changes %d K (%d to %d), want at most 50 K", delta, kelvin, nextKelvin)
	}
}

func TestTransitionDelayWaitsForTheUpcomingFade(t *testing.T) {
	now := time.Date(2026, 10, 8, 19, 40, 0, 0, time.UTC)
	s := NightLightSchedule{Mode: NightLightCustom, NightK: 3000, DayK: 6000,
		Transition: 15 * time.Minute, Start: 20 * time.Hour, End: 7 * time.Hour}
	service := NewNightLight(NightLightOptions{Now: func() time.Time { return now }})
	service.sched, service.loc = s, time.UTC
	kelvin, next, _ := s.evaluate(now, time.UTC)
	if got := service.delay(now, kelvin, next); got != nightLightIdleCap {
		t.Fatalf("delay before fade = %v, want idle cap %v", got, nightLightIdleCap)
	}
}

// TestEvaluatePolarPinsTheLevel pins the polar answers: no sunrise to aim at,
// so the schedule holds the day or night temperature and says so.
func TestEvaluatePolarPinsTheLevel(t *testing.T) {
	tromso := NightLightSchedule{
		Mode: NightLightSunset, NightK: 3000, DayK: 6000,
		Lat: 69.6496, Lon: 18.9560, HasLocation: true,
	}
	loc := london(t)
	kelvin, next, reason := tromso.evaluate(time.Date(2026, 6, 21, 12, 0, 0, 0, loc), loc)
	if kelvin != 6000 || !next.IsZero() || reason != nightLightPolarDay {
		t.Fatalf("polar day = (%d, %v, %q), want (6000, zero, %q)", kelvin, next, reason, nightLightPolarDay)
	}
	kelvin, next, reason = tromso.evaluate(time.Date(2026, 12, 21, 12, 0, 0, 0, loc), loc)
	if kelvin != 3000 || !next.IsZero() || reason != nightLightPolarNight {
		t.Fatalf("polar night = (%d, %v, %q), want (3000, zero, %q)", kelvin, next, reason, nightLightPolarNight)
	}
}

// TestEvaluateSunsetWithoutALocationPinsTheReason: the schedule cannot be
// honoured, so it holds nothing and the UI says which setting to change.
func TestEvaluateSunsetWithoutALocationPinsTheReason(t *testing.T) {
	s := NightLightSchedule{Mode: NightLightSunset, NightK: 3000, DayK: 6000}
	kelvin, next, reason := s.evaluate(time.Date(2026, 10, 8, 20, 0, 0, 0, time.UTC), time.UTC)
	if kelvin != 0 || !next.IsZero() || reason != nightLightNeedsLocation {
		t.Fatalf("= (%d, %v, %q), want (0, zero, %q)", kelvin, next, reason, nightLightNeedsLocation)
	}
}

// TestOverrideExpiresAtTheNextBoundary pins the tile's contract: manual night
// light lasts until the schedule moves again, then it resumes on its own.
func TestOverrideExpiresAtTheNextBoundary(t *testing.T) {
	now := time.Date(2026, 10, 8, 19, 30, 0, 0, time.Local)
	clock := now
	s := NewNightLight(NightLightOptions{Now: func() time.Time { return clock }})
	s.Schedule(NightLightSchedule{
		Mode: NightLightCustom, NightK: 3000, DayK: 6000,
		Transition: 30 * time.Minute,
		Start:      20 * time.Hour,
		End:        8 * time.Hour,
	})
	if got := s.State(); got.Kelvin != 6000 || got.Override {
		t.Fatalf("before the override = (%d K, override %v), want (6000, false)", got.Kelvin, got.Override)
	}
	s.SetOverride(true)
	if got := s.State(); got.Target != 3000 || !got.Override {
		t.Fatalf("under the override = (target %d K, override %v), want (3000, true)", got.Target, got.Override)
	} else if want := time.Date(2026, 10, 8, 20, 0, 0, 0, time.Local); !got.NextChange.Equal(want) {
		t.Fatalf("override next change = %v, want scheduled boundary %v", got.NextChange, want)
	}
	// The override ends at the scheduled 20:00 boundary, not when the centered
	// fade starts at 19:45.
	clock = time.Date(2026, 10, 8, 19, 50, 0, 0, time.Local)
	s.Wake()
	if got := s.State(); !got.Override || got.Kelvin != 3000 {
		t.Fatalf("before the captured boundary = (%d K, override %v), want (3000, true)", got.Kelvin, got.Override)
	}
	clock = time.Date(2026, 10, 8, 20, 0, 0, 0, time.Local)
	s.Wake()
	if got := s.State(); got.Override || got.Kelvin != lerpMired(6000, 3000, 0.5) {
		t.Fatalf("at the captured boundary = (%d K, override %v), want the schedule midpoint (%d, false)",
			got.Kelvin, got.Override, lerpMired(6000, 3000, 0.5))
	}
}

func TestToggleTurnsOffNightTemperatureAlreadyActive(t *testing.T) {
	clock := time.Date(2026, 10, 8, 23, 0, 0, 0, time.Local)
	s := NewNightLight(NightLightOptions{Now: func() time.Time { return clock }})
	s.Schedule(NightLightSchedule{Mode: NightLightCustom, NightK: 3000, DayK: 6000,
		Start: 22 * time.Hour, End: 6 * time.Hour})
	if got := s.State(); got.Kelvin != 3000 {
		t.Fatalf("scheduled night = %d K, want 3000", got.Kelvin)
	}
	s.Toggle()
	if got := s.State(); got.Target != 0 || !got.Override {
		t.Fatalf("after turning off = (target %d K, override %v), want (0, true)", got.Target, got.Override)
	}
}

func TestSunScheduleUsesTodaysPolarState(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	s := NightLightSchedule{Mode: NightLightSunset, NightK: 3000, DayK: 6000,
		Transition: 30 * time.Minute, Lat: 69.6496, Lon: 18.9560, HasLocation: true}
	for day := 1; day <= 365; day++ {
		date := time.Date(2026, 1, day, 12, 0, 0, 0, loc)
		_, _, today := SunTimes(s.Lat, s.Lon, date, loc)
		_, _, reason := s.evaluate(date, loc)
		if today == PolarNone && (reason == nightLightPolarDay || reason == nightLightPolarNight) {
			t.Fatalf("%s has ordinary sunrise/sunset but schedule reports %q", date.Format("2006-01-02"), reason)
		}
		if today != PolarNone && reason == "" {
			t.Fatalf("%s is polar but schedule has no polar status", date.Format("2006-01-02"))
		}
	}
}

// TestOverrideInOffModeIsNotPersisted pins the last case: with the schedule
// off, the tile warms the screen and a fresh service starts neutral.
func TestOverrideInOffModeIsNotPersisted(t *testing.T) {
	clock := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	s := NewNightLight(NightLightOptions{Now: func() time.Time { return clock }})
	s.Schedule(NightLightSchedule{Mode: NightLightOff, NightK: 3000, DayK: 6000})
	s.Toggle()
	if got := s.State(); !got.Override || got.Target != 3000 {
		t.Fatalf("after the toggle = (target %d K, override %v), want (3000, true)", got.Target, got.Override)
	}
	s.Toggle()
	if got := s.State(); got.Override || got.Target != 0 {
		t.Fatalf("after the second toggle = (target %d K, override %v), want (0, false)", got.Target, got.Override)
	}
	fresh := NewNightLight(NightLightOptions{Now: func() time.Time { return clock }})
	fresh.Schedule(NightLightSchedule{Mode: NightLightOff, NightK: 3000, DayK: 6000})
	if got := fresh.State(); got.Override || got.Kelvin != 0 {
		t.Fatalf("a fresh service = (%d K, override %v), want (0, false)", got.Kelvin, got.Override)
	}
}

// TestPublishSkipsRevokedControls pins the no-retry-loop edge: while a control
// is revoked a schedule tick sends nothing, and only a user action does.
func TestPublishSkipsRevokedControls(t *testing.T) {
	clock := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	s := NewNightLight(NightLightOptions{Now: func() time.Time { return clock }})
	s.Schedule(NightLightSchedule{Mode: NightLightAlways, NightK: 3000, DayK: 6000})
	drainRequests(s.Requests())
	s.GammaEvent(wayland.GammaEvent{Global: 1, Connector: "DP-1", State: wayland.GammaFailed})
	if got := s.State(); got.Available || got.Reason == "" {
		t.Fatalf("after a failure = (%v, %q), want unavailable with a reason", got.Available, got.Reason)
	}
	if n := drainRequests(s.Requests()); n != 0 {
		t.Fatalf("a tick after a failure sent %d requests, want 0", n)
	}
	// A user action is the only retry.
	s.Wake()
	if n := drainRequests(s.Requests()); n != 1 {
		t.Fatalf("a user action sent %d requests, want 1", n)
	}
}

func TestGammaFailureOnOneOutputDoesNotDisableAnother(t *testing.T) {
	s := NewNightLight(NightLightOptions{})
	s.Schedule(NightLightSchedule{Mode: NightLightAlways, NightK: 3000, DayK: 6000})
	drainRequests(s.Requests())
	s.GammaEvent(wayland.GammaEvent{Global: 2, Connector: "DP-2", State: wayland.GammaReady})
	s.GammaEvent(wayland.GammaEvent{Global: 1, Connector: "DP-1", State: wayland.GammaFailed})
	if state := s.State(); !state.Available || !strings.Contains(state.Reason, "DP-1") {
		t.Fatalf("one ready output plus one failed output = unavailable: %+v", state)
	}
	s.mu.Lock()
	s.sched.NightK = 3200
	s.publish(false)
	s.mu.Unlock()
	select {
	case req := <-s.Requests():
		if req.Kelvin != 3200 || req.Retry {
			t.Fatalf("scheduled request = %+v, want 3200 K without retry", req)
		}
	default:
		t.Fatal("a failed output suppressed the scheduled update for the ready output")
	}
	s.GammaEvent(wayland.GammaEvent{Global: 2, Connector: "DP-2", State: wayland.GammaRemoved})
	if state := s.State(); state.Available {
		t.Fatalf("after removing the only ready output = available: %+v", state)
	}
}

func TestUserActionMarksGammaRequestAsRetry(t *testing.T) {
	s := NewNightLight(NightLightOptions{})
	s.Schedule(NightLightSchedule{Mode: NightLightAlways, NightK: 3000, DayK: 6000})
	drainRequests(s.Requests())
	s.GammaEvent(wayland.GammaEvent{Global: 1, Connector: "DP-1", State: wayland.GammaFailed})
	s.SetOverride(true)
	select {
	case req := <-s.Requests():
		if !req.Retry {
			t.Fatalf("user action request = %+v, want explicit retry", req)
		}
	default:
		t.Fatal("user action sent no retry request")
	}
}

func TestUserActionRetriesGammaEvenWhenItRequestsNeutral(t *testing.T) {
	s := NewNightLight(NightLightOptions{ReducedMotion: true})
	s.Schedule(NightLightSchedule{Mode: NightLightAlways, NightK: 3000, DayK: 6000})
	drainRequests(s.Requests())
	s.GammaEvent(wayland.GammaEvent{Global: 1, State: wayland.GammaFailed})
	s.SetOverride(false)
	select {
	case req := <-s.Requests():
		if !req.Retry || !req.Neutral {
			t.Fatalf("neutral user action request = %+v, want an explicit neutral retry", req)
		}
	default:
		t.Fatal("neutral user action sent no retry request")
	}
}

func TestUnavailableTileRetriesTheWarmState(t *testing.T) {
	s := NewNightLight(NightLightOptions{ReducedMotion: true})
	s.Schedule(NightLightSchedule{Mode: NightLightAlways, NightK: 3000, DayK: 6000})
	s.GammaEvent(wayland.GammaEvent{Global: 1, State: wayland.GammaReady})
	s.GammaEvent(wayland.GammaEvent{Global: 1, State: wayland.GammaFailed})
	drainRequests(s.Requests())

	s.Toggle()
	if state := s.State(); state.Target != 3000 || !state.Override {
		t.Fatalf("recovery toggle = %+v, want a warm retry override", state)
	}
	select {
	case req := <-s.Requests():
		if !req.Retry || req.Kelvin != 3000 || req.Neutral {
			t.Fatalf("recovery request = %+v, want a warm retry", req)
		}
	default:
		t.Fatal("unavailable tile did not retry gamma")
	}
}

func TestGammaQueueRetainsLatestRequestWhenOwnerIsBehind(t *testing.T) {
	reqs := make(chan wayland.GammaRequest, 1)
	s := NewNightLight(NightLightOptions{Requests: reqs})
	s.Configure(NightLightSchedule{Mode: NightLightAlways, NightK: 4000, DayK: 6500}, false)
	s.SetKelvin(3500)
	if got := <-reqs; got.Kelvin != 3500 {
		t.Fatalf("queued request = %+v, want the latest 3500 K", got)
	}
}

// TestPublishNeutralWhenOff pins that mode off asks the owner to restore the
// outputs instead of sending a temperature.
func TestPublishNeutralWhenOff(t *testing.T) {
	clock := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	s := NewNightLight(NightLightOptions{Now: func() time.Time { return clock }})
	s.Schedule(NightLightSchedule{Mode: NightLightAlways, NightK: 3000, DayK: 6000})
	req := <-s.Requests()
	if req.Kelvin != 3000 || req.Neutral {
		t.Fatalf("first request = %+v, want 3000 K", req)
	}
	s.Schedule(NightLightSchedule{Mode: NightLightOff, NightK: 3000, DayK: 6000})
	req = <-s.Requests()
	if !req.Neutral || req.Kelvin != 0 {
		t.Fatalf("off request = %+v, want neutral", req)
	}
}
