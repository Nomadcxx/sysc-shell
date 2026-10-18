package ui

import "math"

// Transitions are pure values here: normalised easing for duration-driven
// channels, and a clock-free spring for channels that must preserve velocity
// across a retarget. Timing, clocks, and frame scheduling live in the shell,
// which owns one animator per surface; this file only turns a normalised
// progress into geometry or advances one fixed physics step, so the same math
// is reachable from layout, paint, and tests without a running clock.

// clampProgress folds a progress value into [0,1]. Every helper below clamps
// once, at entry, so callers never have to pre-clamp and a reversal that
// overshoots cannot escape the curve.
func clampProgress(progress float64) float64 {
	if progress <= 0 || progress != progress { // NaN sorts to the start.
		return 0
	}
	if progress >= 1 {
		return 1
	}
	return progress
}

// EaseOutCubic is the catalogue's default curve: fast departure, soft arrival.
func EaseOutCubic(progress float64) float64 {
	t := 1 - clampProgress(progress)
	return 1 - t*t*t
}

// EaseOutQuart settles harder than cubic and is reserved for selection, where
// the moving indicator should arrive decisively.
func EaseOutQuart(progress float64) float64 {
	t := 1 - clampProgress(progress)
	return 1 - t*t*t*t
}

// lerpInt rounds the interpolated value rather than truncating it, so a single
// pixel of travel actually moves instead of stalling at the origin. Rounding
// the result rather than the delta keeps the two directions symmetric: a hover
// that reverses retraces the pixels it advanced through.
func lerpInt(from, to int, progress float64) int {
	return int(math.Round(float64(from) + float64(to-from)*progress))
}

// LerpRect interpolates a rectangle in logical pixels. Progress is the eased
// value, not raw time.
func LerpRect(from, to Rect, progress float64) Rect {
	p := clampProgress(progress)
	return Rect{
		X: lerpInt(from.X, to.X, p),
		Y: lerpInt(from.Y, to.Y, p),
		W: lerpInt(from.W, to.W, p),
		H: lerpInt(from.H, to.H, p),
	}
}

// ScaleRectAbout grows or shrinks r by s about its centre.
func ScaleRectAbout(r Rect, s float64) Rect {
	w := int(math.Round(float64(r.W) * s))
	h := int(math.Round(float64(r.H) * s))
	return Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-h)/2, W: w, H: h}
}

// Spring is a mass-one spring in consistent units: Stiffness and Damping are
// plain coefficients, and the epsilons are the two rest thresholds a stored
// value must satisfy before it counts as settled. It carries no clock.
type Spring struct {
	Stiffness, Damping               float64
	PositionEpsilon, VelocityEpsilon float64
}

// springStepMax bounds one Step call. The shell integrates on a fixed 2 ms
// lattice, so a larger interval is a caller bug; settling is the safe recovery
// rather than a hop the caller cannot reproduce.
const springStepMax = 0.002

// Step advances exactly one positive interval of at most 0.002 seconds with
// semi-implicit Euler (velocity first, then position). Rest needs both the
// position and the velocity below their thresholds: crossing the target once
// is not rest. At rest it returns the exact target and zero velocity.
//
// Invalid coefficients, thresholds or state, an oversized interval, or a
// non-finite interval settle the value instead of integrating from a state the
// caller cannot trust. A non-finite target is refused outright with a zero
// state. A finite nonpositive interval does not move the value and keeps its
// current rest classification.
func (s Spring) Step(position, velocity, target, seconds float64) (float64, float64, bool) {
	if !isFinite(target) {
		return 0, 0, true
	}
	if !s.valid() || !isFinite(position) || !isFinite(velocity) ||
		!isFinite(seconds) || seconds > springStepMax {
		return target, 0, true
	}
	if seconds <= 0 {
		return position, velocity, s.atRest(position, velocity, target)
	}
	if s.atRest(position, velocity, target) {
		return target, 0, true
	}
	velocity += (s.Stiffness*(target-position) - s.Damping*velocity) * seconds
	position += velocity * seconds
	if s.atRest(position, velocity, target) {
		return target, 0, true
	}
	return position, velocity, false
}

// valid reports whether the coefficients and thresholds are finite and
// positive, which is what makes a step reproducible.
func (s Spring) valid() bool {
	return s.Stiffness > 0 && isFinite(s.Stiffness) &&
		s.Damping > 0 && isFinite(s.Damping) &&
		s.PositionEpsilon > 0 && isFinite(s.PositionEpsilon) &&
		s.VelocityEpsilon > 0 && isFinite(s.VelocityEpsilon)
}

// atRest applies both settle thresholds: proximity alone is not rest, because
// a value passing through its target at speed must keep moving.
func (s Spring) atRest(position, velocity, target float64) bool {
	return math.Abs(position-target) <= s.PositionEpsilon &&
		math.Abs(velocity) <= s.VelocityEpsilon
}

// isFinite reports a real number that is neither NaN nor an infinity.
func isFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
