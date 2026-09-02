package ui

import "math"

// Transitions are pure values here. Timing, clocks, and frame scheduling live
// in the shell, which owns one animator per surface; this file only turns a
// normalised progress into geometry so the same easing is reachable from
// layout, paint, and tests without a running clock.

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
