// Night light: the colour temperature a schedule asks for, and the one
// goroutine that publishes it to the Wayland owner. KelvinAt and
// evaluate are pure — a schedule is a function of wall-clock, location and
// settings — so the transitions are table-testable without a clock or a
// compositor, and a restart computes the same value at the same instant.
package services

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	wayland "github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

// Mode names the four night-light behaviours. Config and the settings entries
// spell them exactly like this.
const (
	NightLightOff    = "off"
	NightLightSunset = "sunset"
	NightLightCustom = "custom"
	NightLightAlways = "always"
)

// Reasons the schedule or the compositor can leave on screen. They are UI
// text: the Settings status line and nightlight.status show them verbatim.
const (
	nightLightNoControl      = "Your compositor does not offer gamma control (wlr-gamma-control)."
	nightLightBusy           = "Unavailable: another app controls colour (wlsunset?)"
	nightLightNeedsLocation  = "Needs a location: set one under Weather, or use Custom times."
	nightLightWaitingCity    = "Waiting for weather location."
	nightLightPolarDay       = "Polar day: staying at the day temperature."
	nightLightPolarNight     = "Polar night: staying at the night temperature."
	nightLightTransitionTick = 30 * time.Second
	nightLightManualFade     = time.Second
	nightLightManualTick     = 50 * time.Millisecond
	// nightLightIdleCap re-arms the timer at least this often so a wall-clock
	// jump, a timezone change or a suspend cannot park the schedule past its
	// next boundary without a recompute.
	nightLightIdleCap = 10 * time.Minute
)

// NightLightSchedule is the resolved policy: what the settings and the
// location say, with no clock of its own. Start and End are offsets from
// local midnight, so 20:00 is 20*time.Hour.
type NightLightSchedule struct {
	Mode        string
	NightK      int
	DayK        int
	Transition  time.Duration
	Start, End  time.Duration
	Lat, Lon    float64
	HasLocation bool
}

// fadeWindow is one centred transition: from -> to, ramping over
// [start, end]. A zero-length window is an instant step.
type fadeWindow struct {
	start, end time.Time
	from, to   int
}

func (w fadeWindow) mix(t time.Time) int {
	span := float64(w.end.Sub(w.start))
	if span <= 0 {
		return w.to
	}
	return lerpMired(w.from, w.to, float64(t.Sub(w.start))/span)
}

// lerpMired interpolates in mireds (1e6/K), not in kelvin: brightness falls
// with temperature, so a straight kelvin ramp looks wrong in the middle.
func lerpMired(from, to int, f float64) int {
	if from <= 0 {
		return to
	}
	if to <= 0 {
		return from
	}
	mired := 1e6/float64(from) + (1e6/float64(to)-1e6/float64(from))*f
	if mired <= 0 {
		return 0
	}
	return int(math.Round(1e6 / mired))
}

// windows returns the transitions around now: the previous, current and next
// calendar day, so a window that starts before midnight still ends after it.
// A polar result wins over windows, because there is no sunrise to aim at.
func (s NightLightSchedule) windows(now time.Time, loc *time.Location) ([]fadeWindow, PolarState) {
	if s.Mode == NightLightSunset {
		return s.sunWindows(now, loc)
	}
	return s.customWindows(now, loc)
}

func (s NightLightSchedule) sunWindows(now time.Time, loc *time.Location) ([]fadeWindow, PolarState) {
	type solarBoundary struct {
		at     time.Time
		rising bool
	}
	events := make([]solarBoundary, 0, 10)
	var todayPolar PolarState
	for day := -2; day <= 2; day++ {
		rise, set, polar := SunTimes(s.Lat, s.Lon, now.AddDate(0, 0, day), loc)
		if polar != PolarNone {
			if day == 0 {
				todayPolar = polar
			}
			continue
		}
		events = append(events, solarBoundary{at: rise, rising: true}, solarBoundary{at: set})
	}
	if todayPolar != PolarNone {
		return nil, todayPolar
	}
	sort.Slice(events, func(i, j int) bool { return events[i].at.Before(events[j].at) })
	transition := s.Transition
	for i := 1; i < len(events); i++ {
		gap := events[i].at.Sub(events[i-1].at)
		if gap <= 0 {
			transition = 0
		} else if gap < transition {
			transition = gap
		}
	}
	half := transition / 2
	out := make([]fadeWindow, 0, len(events))
	for _, event := range events {
		from, to := s.DayK, s.NightK
		if event.rising {
			from, to = s.NightK, s.DayK
		}
		out = append(out, fadeWindow{event.at.Add(-half), event.at.Add(half), from, to})
	}
	return out, PolarNone
}

func (s NightLightSchedule) customWindows(now time.Time, loc *time.Location) ([]fadeWindow, PolarState) {
	transition := s.Transition
	// A centred transition longer than the shorter of the two gaps would
	// overlap the next one, so clamp it to the gap it has to fit.
	for _, gap := range []time.Duration{wrapDay(s.End - s.Start), wrapDay(s.Start - s.End)} {
		if transition > gap {
			transition = gap
		}
	}
	half := transition / 2
	out := make([]fadeWindow, 0, 6)
	for day := -1; day <= 1; day++ {
		d := now.AddDate(0, 0, day)
		start, end := localClock(d, s.Start, loc), localClock(d, s.End, loc)
		out = append(out,
			fadeWindow{start.Add(-half), start.Add(half), s.DayK, s.NightK},
			fadeWindow{end.Add(-half), end.Add(half), s.NightK, s.DayK})
	}
	return out, PolarNone
}

// nextBoundary finds the next sunrise/sunset or custom clock event, independent
// of where its centered fade starts. Polar days are skipped until a solar
// event returns; a schedule with no upcoming boundary yields zero.
func (s NightLightSchedule) nextBoundary(now time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = now.Location()
		if loc == nil {
			loc = time.UTC
		}
	}
	switch s.Mode {
	case NightLightCustom:
		windows, _ := s.customWindows(now, loc)
		var next time.Time
		for _, w := range windows {
			boundary := w.start.Add(w.end.Sub(w.start) / 2)
			if boundary.After(now) && (next.IsZero() || boundary.Before(next)) {
				next = boundary
			}
		}
		return next
	case NightLightSunset:
		if !s.HasLocation {
			return time.Time{}
		}
		for day := 0; day <= 370; day++ {
			rise, set, polar := SunTimes(s.Lat, s.Lon, now.AddDate(0, 0, day), loc)
			if polar != PolarNone {
				continue
			}
			next := time.Time{}
			for _, boundary := range []time.Time{rise, set} {
				if boundary.After(now) && (next.IsZero() || boundary.Before(next)) {
					next = boundary
				}
			}
			if !next.IsZero() {
				return next
			}
		}
	}
	return time.Time{}
}

// localClock turns a time-of-day offset into wall-clock fields. Adding an
// elapsed duration to midnight shifts custom schedules by an hour on DST days.
func localClock(day time.Time, clock time.Duration, loc *time.Location) time.Time {
	clock = wrapDay(clock)
	hour := int(clock / time.Hour)
	minute := int(clock % time.Hour / time.Minute)
	second := int(clock % time.Minute / time.Second)
	nanosecond := int(clock % time.Second)
	year, month, date := day.In(loc).Date()
	return time.Date(year, month, date, hour, minute, second, nanosecond, loc)
}

// wrapDay maps a signed day offset onto [0, 24h).
func wrapDay(d time.Duration) time.Duration {
	d %= 24 * time.Hour
	if d < 0 {
		d += 24 * time.Hour
	}
	return d
}

// evaluate is the whole policy for one instant: the temperature to hold, the
// instant it next changes, and the reason to show when it cannot be honoured.
func (s NightLightSchedule) evaluate(now time.Time, loc *time.Location) (kelvin int, next time.Time, reason string) {
	switch s.Mode {
	case "", NightLightOff:
		return 0, time.Time{}, ""
	case NightLightAlways:
		return s.NightK, time.Time{}, ""
	case NightLightSunset:
		if !s.HasLocation {
			return 0, time.Time{}, nightLightNeedsLocation
		}
	case NightLightCustom:
	default:
		return 0, time.Time{}, ""
	}
	if loc == nil {
		loc = now.Location()
	}
	if loc == nil {
		loc = time.UTC
	}
	windows, polar := s.windows(now, loc)
	switch polar {
	case PolarDay:
		return s.DayK, time.Time{}, nightLightPolarDay
	case PolarNight:
		return s.NightK, time.Time{}, nightLightPolarNight
	}
	if len(windows) == 0 {
		return 0, time.Time{}, ""
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].start.Before(windows[j].start) })
	// Before the first window the level is its opening value; every window
	// that has finished leaves the level it faded to.
	level := windows[0].from
	var nextChange time.Time
	for _, w := range windows {
		if now.Before(w.start) {
			if nextChange.IsZero() {
				nextChange = w.start
			}
			continue
		}
		if now.Before(w.end) {
			level = w.mix(now)
			nextChange = w.end
			break
		}
		level = w.to
	}
	return level, nextChange, ""
}

// KelvinAt is the schedule as a pure function of the wall clock. A zero
// kelvin means "hold no temperature": the caller restores the outputs.
func KelvinAt(s NightLightSchedule, now time.Time) (kelvin int, next time.Time) {
	kelvin, next, _ = s.evaluate(now, now.Location())
	return kelvin, next
}

// NightLightState is the immutable snapshot the shell renders and the IPC
// status verb returns.
type NightLightState struct {
	Mode       string
	Active     bool
	Kelvin     int
	Target     int
	NextChange time.Time
	// Supported reports a gamma control was seen on at least one output.
	// This differs from Available: another colour client can revoke a
	// supported control, which should remain visible as an unavailable tile.
	Supported bool
	Available bool
	Reason    string
	Override  bool
	// Source names where the location came from, for the Settings status line.
	Source string
}

type nightLightOutputState struct {
	state     wayland.GammaState
	connector string
}

// NightLightOptions configures NewNightLight. Zero values choose a local
// schedule with no location.
type NightLightOptions struct {
	// Location reports the coordinates the weather service resolved. ok is
	// false while a configured city is still geocoding.
	Location func() (lat, lon float64, ok bool)
	// LocationPending distinguishes an unresolved city from no configured place.
	LocationPending func() bool
	// Source names the location's origin, e.g. "London".
	Source   func() string
	Requests chan wayland.GammaRequest
	// ReducedMotion skips the short fade used for manual toggles.
	ReducedMotion bool
	// Now is a seam for tests. Nil uses time.Now.
	Now func() time.Time
}

// NightLightService owns the schedule timer, the override and the published
// state. Every setter takes the lock, mutates and kicks Run; Run is the only
// goroutine that evaluates the schedule or sends requests.
type NightLightService struct {
	mu    sync.Mutex
	sched NightLightSchedule
	// over marks a temporary manual state; override selects night (on) versus
	// neutral (off). overrideUntil is captured when the user acts so it cannot
	// slide forward as the schedule advances.
	over          bool
	override      bool
	overrideUntil time.Time
	loc           *time.Location
	source        string
	state         NightLightState

	reqs            chan wayland.GammaRequest
	updates         chan NightLightState
	location        func() (lat, lon float64, ok bool)
	pending         func() bool
	sourceFn        func() string
	locationPending bool
	reducedMotion   bool
	manualFade      bool
	manualFadeStart time.Time
	manualFadeEnd   time.Time
	manualFadeFrom  int
	manualFadeTo    int
	now             func() time.Time
	kick            chan struct{}

	// failed and unsupported mirror the Wayland owner's view. A failed
	// control is not retried on a timer, so a failed night light must not
	// re-send on every tick either.
	failed       bool
	partialFail  bool
	unsupported  bool
	hint         string
	gammaOutputs map[uint32]nightLightOutputState
	sent         wayland.GammaRequest
	sentAny      bool
}

func NewNightLight(opt NightLightOptions) *NightLightService {
	reqs := opt.Requests
	if reqs == nil {
		reqs = make(chan wayland.GammaRequest, 8)
	}
	now := opt.Now
	if now == nil {
		now = time.Now
	}
	s := &NightLightService{
		sched:         NightLightSchedule{Mode: NightLightOff},
		loc:           time.Local,
		reqs:          reqs,
		updates:       make(chan NightLightState, 1),
		location:      opt.Location,
		pending:       opt.LocationPending,
		sourceFn:      opt.Source,
		reducedMotion: opt.ReducedMotion,
		now:           now,
		kick:          make(chan struct{}, 1),
		state:         NightLightState{Mode: NightLightOff},
		gammaOutputs:  make(map[uint32]nightLightOutputState),
	}
	return s
}

// Requests carries the colour-temperature asks to the Wayland owner.
func (s *NightLightService) Requests() <-chan wayland.GammaRequest { return s.reqs }

// Updates carries the latest immutable snapshot. A slow renderer gets the
// newest state rather than making the schedule goroutine wait.
func (s *NightLightService) Updates() <-chan NightLightState { return s.updates }

// State returns the current snapshot.
func (s *NightLightService) State() NightLightState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Schedule replaces the policy from a config reload. It recomputes now, so a
// changed temperature takes effect at once.
func (s *NightLightService) Schedule(sched NightLightSchedule) {
	s.mu.Lock()
	s.sched = sched
	s.clearManualFadeLocked()
	s.rebaseOverrideLocked(s.now())
	s.mu.Unlock()
	s.Wake()
}

// Configure applies the schedule and its accessibility behavior atomically.
func (s *NightLightService) Configure(sched NightLightSchedule, reducedMotion bool) {
	s.mu.Lock()
	s.sched = sched
	s.reducedMotion = reducedMotion
	s.clearManualFadeLocked()
	s.rebaseOverrideLocked(s.now())
	s.mu.Unlock()
	s.Wake()
}

// Location updates the coordinates the sunset mode uses.
func (s *NightLightService) Location(lat, lon float64, ok bool, source string) {
	s.mu.Lock()
	s.sched.Lat, s.sched.Lon, s.sched.HasLocation = lat, lon, ok && s.sched.Mode == NightLightSunset
	s.source = source
	s.locationPending = false
	s.mu.Unlock()
	s.Wake()
}

// SetKelvin raises or lowers the night temperature. A manual change is a user
// action, so it also retries a control the compositor revoked.
func (s *NightLightService) SetKelvin(kelvin int) {
	s.mu.Lock()
	s.sched.NightK = kelvin
	s.clearManualFadeLocked()
	s.mu.Unlock()
	s.Wake()
}

// SetReducedMotion controls the short transition used by manual overrides.
func (s *NightLightService) SetReducedMotion(reduced bool) {
	s.mu.Lock()
	changed := s.reducedMotion != reduced
	s.reducedMotion = reduced
	if reduced {
		s.clearManualFadeLocked()
	}
	s.mu.Unlock()
	if changed {
		s.Wake()
	}
}

// SetOverride forces the night temperature on (or off) until the next
// schedule boundary. Off and Always modes have no boundary, so their
// overrides last until changed or the shell restarts. The override is never
// persisted.
func (s *NightLightService) SetOverride(on bool) {
	s.mu.Lock()
	s.syncLocation()
	now := s.now()
	s.expireOverrideLocked(now)
	from := s.currentKelvinLocked(now)
	s.setOverrideLocked(on)
	s.beginManualFadeLocked(now, from)
	s.mu.Unlock()
	s.Wake()
}

// Toggle is the tile: on when the schedule is not already holding the night
// temperature, off when it is.
func (s *NightLightService) Toggle() {
	s.mu.Lock()
	s.syncLocation()
	now := s.now()
	s.expireOverrideLocked(now)
	from := s.currentKelvinLocked(now)
	if s.failed {
		// The unavailable tile is also the recovery action. Keep the requested
		// state warm and retry the compositor control instead of toggling it off.
		s.setOverrideLocked(true)
		s.beginManualFadeLocked(now, from)
		s.mu.Unlock()
		s.Wake()
		return
	}
	if s.over {
		if s.override {
			if s.sched.Mode == NightLightOff {
				s.clearOverrideLocked()
			} else {
				s.setOverrideLocked(false)
			}
		} else {
			s.setOverrideLocked(true)
		}
	} else {
		kelvin, _, _ := s.sched.evaluate(now, s.loc)
		s.setOverrideLocked(!nightLightActive(kelvin, s.sched.DayK))
	}
	s.beginManualFadeLocked(now, from)
	s.mu.Unlock()
	s.Wake()
}

func nightLightActive(kelvin, dayKelvin int) bool {
	return kelvin > 0 && kelvin < dayKelvin
}

func (s *NightLightService) targetKelvinLocked(now time.Time) int {
	kelvin, _, _ := s.sched.evaluate(now, s.loc)
	if s.over && (s.overrideUntil.IsZero() || now.Before(s.overrideUntil)) {
		if s.override {
			return s.sched.NightK
		}
		return 0
	}
	return kelvin
}

func (s *NightLightService) currentKelvinLocked(now time.Time) int {
	if s.manualFade && now.Before(s.manualFadeEnd) {
		fraction := float64(now.Sub(s.manualFadeStart)) / float64(s.manualFadeEnd.Sub(s.manualFadeStart))
		return lerpMired(s.manualFadeFrom, s.manualFadeTo, fraction)
	}
	return s.targetKelvinLocked(now)
}

func (s *NightLightService) beginManualFadeLocked(now time.Time, from int) {
	s.clearManualFadeLocked()
	if s.reducedMotion {
		return
	}
	to := s.targetKelvinLocked(now)
	if from <= 0 {
		from = s.sched.DayK
	}
	if to <= 0 {
		to = s.sched.DayK
	}
	if from == to {
		return
	}
	s.manualFade = true
	s.manualFadeStart = now
	s.manualFadeEnd = now.Add(nightLightManualFade)
	s.manualFadeFrom = from
	s.manualFadeTo = to
}

func (s *NightLightService) clearManualFadeLocked() {
	s.manualFade = false
	s.manualFadeStart = time.Time{}
	s.manualFadeEnd = time.Time{}
	s.manualFadeFrom = 0
	s.manualFadeTo = 0
}

func (s *NightLightService) setOverrideLocked(on bool) {
	if s.sched.Mode == NightLightOff && !on {
		s.clearOverrideLocked()
		return
	}
	s.syncLocation()
	now := s.now()
	s.over, s.override = true, on
	s.overrideUntil = s.sched.nextBoundary(now, s.loc)
}

func (s *NightLightService) rebaseOverrideLocked(now time.Time) {
	if !s.over {
		return
	}
	s.syncLocation()
	if s.sched.Mode == NightLightOff && !s.override {
		s.clearOverrideLocked()
		return
	}
	s.overrideUntil = s.sched.nextBoundary(now, s.loc)
}

func (s *NightLightService) clearOverrideLocked() {
	s.over, s.override = false, false
	s.overrideUntil = time.Time{}
}

func (s *NightLightService) expireOverrideLocked(now time.Time) {
	if s.over && !s.overrideUntil.IsZero() && !now.Before(s.overrideUntil) {
		s.clearOverrideLocked()
	}
}

// GammaEvent records what the Wayland owner could do with the request. A
// failed control names the app holding it, when /proc says who that is.
func (s *NightLightService) GammaEvent(ev wayland.GammaEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch ev.State {
	case wayland.GammaUnsupported:
		s.unsupported = true
	case wayland.GammaFailed:
		s.unsupported = false
		s.gammaOutputs[ev.Global] = nightLightOutputState{state: wayland.GammaFailed, connector: ev.Connector}
		s.hint = ""
		if who := colorController(); who != "" {
			s.hint = who
		}
	case wayland.GammaReady:
		s.unsupported = false
		s.gammaOutputs[ev.Global] = nightLightOutputState{state: wayland.GammaReady, connector: ev.Connector}
		s.hint = ""
	case wayland.GammaRemoved:
		delete(s.gammaOutputs, ev.Global)
	}
	s.recomputeGammaFailure()
	s.publish(false)
}

func (s *NightLightService) recomputeGammaFailure() {
	ready, failed := 0, 0
	for _, output := range s.gammaOutputs {
		if output.state == wayland.GammaReady {
			ready++
		}
		if output.state == wayland.GammaFailed {
			failed++
		}
	}
	s.failed = failed > 0 && ready == 0
	s.partialFail = failed > 0 && ready > 0
}

// Wake recomputes and re-sends: for a config change, a resume, a timezone
// change or a clock jump. It is the only retry path for a failed control.
func (s *NightLightService) Wake() {
	s.mu.Lock()
	s.publish(true)
	s.mu.Unlock()
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Recompute refreshes a weather-driven location or clock state without
// retrying controls the compositor revoked.
func (s *NightLightService) Recompute() {
	s.mu.Lock()
	s.publish(false)
	s.mu.Unlock()
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Run owns the timer until ctx ends.
func (s *NightLightService) Run(ctx context.Context) {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	s.Wake()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.kick:
		case <-timer.C:
		}
		s.mu.Lock()
		delay := s.publish(false)
		s.mu.Unlock()
		stop := timer.Stop()
		if !stop {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(delay)
	}
}

// publish evaluates the schedule, stores the snapshot and sends the request
// when it changed. force is a user action: it retries a failed control. The
// returned duration is when to recompute next.
func (s *NightLightService) publish(force bool) time.Duration {
	now := s.now()
	s.syncLocation()

	sched := s.sched
	kelvin, next, reason := sched.evaluate(now, s.loc)
	if sched.Mode == NightLightSunset && !sched.HasLocation && s.locationPending {
		reason = nightLightWaitingCity
	}
	if s.over {
		if !s.overrideUntil.IsZero() && !now.Before(s.overrideUntil) {
			s.clearOverrideLocked()
			kelvin, next, reason = sched.evaluate(now, s.loc)
		} else if s.override {
			kelvin = sched.NightK
		} else {
			kelvin = 0
		}
		if s.over && !s.overrideUntil.IsZero() {
			next = s.overrideUntil
		}
	}

	target := kelvin
	if s.over {
		if s.override {
			target = sched.NightK
		} else {
			target = 0
		}
	} else if !next.IsZero() {
		if t, _, _ := sched.evaluate(next, s.loc); t > 0 {
			target = t
		}
	}
	if s.manualFade {
		if !now.Before(s.manualFadeEnd) {
			s.clearManualFadeLocked()
		} else {
			fraction := float64(now.Sub(s.manualFadeStart)) / float64(s.manualFadeEnd.Sub(s.manualFadeStart))
			kelvin = lerpMired(s.manualFadeFrom, s.manualFadeTo, fraction)
		}
	}

	supported := !s.unsupported && len(s.gammaOutputs) > 0
	available := supported && !s.failed && s.hasReadyOutput()
	switch {
	case s.unsupported:
		reason = nightLightNoControl
	case s.failed:
		reason = nightLightBusy
		if s.hint != "" {
			reason = fmt.Sprintf("%s %s is running.", nightLightBusy, s.hint)
		}
	case s.partialFail:
		var connectors []string
		for _, output := range s.gammaOutputs {
			if output.state == wayland.GammaFailed && output.connector != "" {
				connectors = append(connectors, output.connector)
			}
		}
		sort.Strings(connectors)
		reason = "Some outputs cannot adjust colour"
		if len(connectors) > 0 {
			reason += " (" + strings.Join(connectors, ", ") + ")"
		}
		if s.hint != "" {
			reason += "; " + s.hint + " is running"
		}
		reason += "; other outputs remain active."
	case !supported || !available:
		reason = "Waiting for gamma control."
	}

	state := NightLightState{
		Mode:       sched.Mode,
		Active:     available && nightLightActive(kelvin, sched.DayK),
		Kelvin:     kelvin,
		Target:     target,
		NextChange: next,
		Supported:  supported,
		Available:  available,
		Reason:     reason,
		Override:   s.over,
		Source:     s.source,
	}
	if state != s.state {
		s.state = state
		select {
		case s.updates <- state:
		default:
			select {
			case <-s.updates:
			default:
			}
			select {
			case s.updates <- state:
			default:
			}
		}
	}

	switch {
	case s.unsupported:
		// Nothing to send: the compositor will not accept a ramp.
	case s.failed && !force:
		// The owner does not retry on its own, so a tick must not either.
	case force || !s.sentAny || s.sent.Kelvin != kelvin || s.sent.Neutral != (kelvin == 0):
		req := wayland.GammaRequest{Kelvin: kelvin, Neutral: kelvin == 0, Retry: force}
		s.sent, s.sentAny = req, true
		s.sendRequest(req)
	}

	return s.delay(now, kelvin, next)
}

func (s *NightLightService) sendRequest(req wayland.GammaRequest) {
	select {
	case s.reqs <- req:
		return
	default:
	}
	// Keep the newest value so a busy owner still receives the end of a fade.
	select {
	case <-s.reqs:
	default:
	}
	select {
	case s.reqs <- req:
	default:
		slog.Warn("night light: gamma request dropped, owner busy")
	}
}

func (s *NightLightService) hasReadyOutput() bool {
	for _, output := range s.gammaOutputs {
		if output.state == wayland.GammaReady {
			return true
		}
	}
	return false
}

// delay is when the schedule can next change value: the end of a running
// transition, the next boundary, or the cap that catches a clock jump.
func (s *NightLightService) delay(now time.Time, kelvin int, next time.Time) time.Duration {
	if s.manualFade {
		return cap(delay(s.manualFadeEnd.Sub(now)), nightLightManualTick)
	}
	if s.loc == nil {
		s.loc = time.Local
	}
	if kelvin > 0 && !next.IsZero() && s.sched.Transition > 0 {
		windows, _ := s.sched.windows(now, s.loc)
		for _, w := range windows {
			if !now.Before(w.start) && now.Before(w.end) && w.end.Equal(next) {
				return transitionTick(w, now, kelvin)
			}
		}
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc).AddDate(0, 0, 1)
	if next.IsZero() {
		if s.sched.Mode == NightLightSunset {
			// Sun times are per day, so recompute at local midnight.
			return cap(delay(day.Sub(now)), nightLightIdleCap)
		}
		return nightLightIdleCap
	}
	return cap(delay(next.Sub(now)), nightLightIdleCap)
}

// transitionTick chooses the longest step no greater than 30 seconds that
// keeps each mired-space update within 50 K. The shortest configured fade and
// widest temperature range exceed that limit at a fixed 30-second cadence.
func transitionTick(w fadeWindow, now time.Time, kelvin int) time.Duration {
	limit := min(nightLightTransitionTick, w.end.Sub(now))
	if limit <= 0 || math.Abs(float64(w.mix(now.Add(limit))-kelvin)) <= 50 {
		return max(delay(limit), time.Millisecond)
	}
	lo, hi := time.Duration(0), limit
	for i := 0; i < 20; i++ {
		mid := lo + (hi-lo)/2
		if math.Abs(float64(w.mix(now.Add(mid))-kelvin)) <= 50 {
			lo = mid
		} else {
			hi = mid
		}
	}
	return max(lo, time.Millisecond)
}

func delay(d time.Duration) time.Duration {
	if d < 0 {
		return time.Second
	}
	return d
}

func cap(d, max time.Duration) time.Duration {
	if d <= 0 || d > max {
		return max
	}
	return d
}

// syncLocation pulls the weather service's coordinates into the schedule on
// the Run goroutine, so no lock is shared with it.
func (s *NightLightService) syncLocation() {
	if s.location != nil {
		lat, lon, ok := s.location()
		s.sched.Lat, s.sched.Lon = lat, lon
		s.sched.HasLocation = ok && s.sched.Mode == NightLightSunset
	}
	if s.sourceFn != nil {
		s.source = s.sourceFn()
	}
	if s.pending != nil {
		s.locationPending = s.pending() && s.sched.Mode == NightLightSunset
	}
}

// colorController names a running app that is likely to hold gamma control,
// so the Settings line can say who to close instead of only guessing.
func colorController() string {
	wanted := []string{"wlsunset", "gammastep", "hyprsunset", "redshift"}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile("/proc/" + entry.Name() + "/comm")
		if err != nil {
			continue
		}
		comm := strings.TrimSpace(string(raw))
		for _, want := range wanted {
			if comm == want || strings.HasPrefix(comm, want+"-") {
				return comm
			}
		}
	}
	return ""
}
