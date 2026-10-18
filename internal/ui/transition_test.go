package ui

import (
	"math"
	"testing"
)

func TestEasingClampsAndHitsEndpoints(t *testing.T) {
	t.Parallel()
	for _, fn := range []struct {
		name string
		ease func(float64) float64
	}{{"cubic", EaseOutCubic}, {"quart", EaseOutQuart}} {
		for _, tc := range []struct {
			progress, want float64
		}{
			{-1, 0}, {-0.001, 0}, {0, 0},
			{1, 1}, {1.001, 1}, {42, 1},
			{math.NaN(), 0},
		} {
			if got := fn.ease(tc.progress); got != tc.want {
				t.Errorf("%s(%v) = %v, want %v", fn.name, tc.progress, got, tc.want)
			}
		}
	}
}

func TestEaseOutCurvesLeadTheLinearPath(t *testing.T) {
	t.Parallel()
	// An out curve covers most of its distance early; quart, being the harder
	// settle, must lead cubic at the midpoint.
	cubic, quart := EaseOutCubic(0.5), EaseOutQuart(0.5)
	if cubic != 0.875 {
		t.Errorf("EaseOutCubic(0.5) = %v, want 0.875", cubic)
	}
	if quart != 0.9375 {
		t.Errorf("EaseOutQuart(0.5) = %v, want 0.9375", quart)
	}
	if quart <= cubic {
		t.Errorf("quart %v does not lead cubic %v", quart, cubic)
	}
}

func TestEaseOutCurvesAreMonotonic(t *testing.T) {
	t.Parallel()
	prevC, prevQ := -1.0, -1.0
	for i := 0; i <= 100; i++ {
		p := float64(i) / 100
		c, q := EaseOutCubic(p), EaseOutQuart(p)
		if c < prevC || q < prevQ {
			t.Fatalf("curve went backwards at %v: cubic %v after %v, quart %v after %v", p, c, prevC, q, prevQ)
		}
		prevC, prevQ = c, q
	}
}

func TestLerpRectEndpointsAndMidpoint(t *testing.T) {
	t.Parallel()
	from := Rect{X: 0, Y: 10, W: 100, H: 40}
	to := Rect{X: 20, Y: 0, W: 200, H: 32}
	for _, tc := range []struct {
		name     string
		progress float64
		want     Rect
	}{
		{"before start", -0.5, from},
		{"start", 0, from},
		{"midpoint", 0.5, Rect{X: 10, Y: 5, W: 150, H: 36}},
		{"end", 1, to},
		{"past end", 2, to},
	} {
		if got := LerpRect(from, to, tc.progress); got != tc.want {
			t.Errorf("LerpRect(%s) = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestLerpRectReversesFromTheCurrentValue(t *testing.T) {
	t.Parallel()
	// A hover that reverses mid-flight starts from where it was drawn, not from
	// the resting bounds, so the control never jumps on the reversal frame.
	from := Rect{X: 0, W: 100, H: 40}
	to := Rect{X: 40, W: 180, H: 40}
	mid := LerpRect(from, to, EaseOutCubic(0.25))
	back := LerpRect(mid, from, 0)
	if back != mid {
		t.Fatalf("reversal start = %+v, want the rendered %+v", back, mid)
	}
	if got := LerpRect(mid, from, 1); got != from {
		t.Errorf("reversal end = %+v, want %+v", got, from)
	}
}

func TestLerpRectRoundsRatherThanTruncates(t *testing.T) {
	t.Parallel()
	// One logical pixel over an odd fraction: truncation would stall at 0 and
	// the control would appear to stick before moving. The two directions round
	// to the same pixel, so a reversal retraces its own path.
	if got := LerpRect(Rect{}, Rect{X: 1}, 0.5).X; got != 1 {
		t.Errorf("forward midpoint X = %d, want 1", got)
	}
	if got := LerpRect(Rect{X: 1}, Rect{}, 0.5).X; got != 1 {
		t.Errorf("reverse midpoint X = %d, want 1 to match the forward path", got)
	}
	if got := LerpRect(Rect{X: 10}, Rect{X: -10}, 0.5).X; got != 0 {
		t.Errorf("negative direction midpoint X = %d, want 0", got)
	}
}

func TestSpringStepUsesUpdatedVelocity(t *testing.T) {
	t.Parallel()
	// Semi-implicit Euler: velocity first, then position. The expected pair
	// pins the integration order; swapping it changes the position by one
	// velocity increment and fails.
	s := Spring{Stiffness: 100, Damping: 20,
		PositionEpsilon: 0.001, VelocityEpsilon: 0.001}
	x, v, rest := s.Step(0, 0, 1, 0.002)
	if math.Abs(x-0.0004) > 1e-12 || math.Abs(v-0.2) > 1e-12 || rest {
		t.Fatalf("step = (%g, %g, %v), want (0.0004, 0.2, false)", x, v, rest)
	}
}

func TestSpringRestNeedsPositionAndVelocity(t *testing.T) {
	t.Parallel()
	// Both thresholds gate rest: proximity with speed still moving is not
	// rest, and a stationary value away from the target is not rest either.
	s := Spring{Stiffness: 100, Damping: 20,
		PositionEpsilon: 0.001, VelocityEpsilon: 0.001}
	for _, tc := range []struct {
		x, v float64
		rest bool
	}{
		{1, 0, true},
		{1.0005, 0.0005, true},
		{1, 0.1, false},
		{0.99, 0, false},
	} {
		x, v, rest := s.Step(tc.x, tc.v, 1, 0.002)
		if rest != tc.rest || rest && (x != 1 || v != 0) {
			t.Fatalf("from (%g,%g) -> (%g,%g,%v)", tc.x, tc.v, x, v, rest)
		}
	}
}

func TestSpringStepInvalidStateSettlesSafely(t *testing.T) {
	t.Parallel()
	// Anything the caller cannot reproduce settles exactly instead of
	// integrating: bad coefficients, bad thresholds, non-finite state, or an
	// interval outside (0, 0.002]. A hard snap is the safe recovery; NaN must
	// never reach a stored value.
	valid := Spring{Stiffness: 100, Damping: 20,
		PositionEpsilon: 0.001, VelocityEpsilon: 0.001}
	for _, tc := range []struct {
		name     string
		spring   Spring
		x, v, dt float64
	}{
		{"zero stiffness", Spring{Damping: 20, PositionEpsilon: 0.001, VelocityEpsilon: 0.001}, 0, 0, 0.002},
		{"negative stiffness", Spring{Stiffness: -100, Damping: 20, PositionEpsilon: 0.001, VelocityEpsilon: 0.001}, 0, 0, 0.002},
		{"infinite stiffness", Spring{Stiffness: math.Inf(1), Damping: 20, PositionEpsilon: 0.001, VelocityEpsilon: 0.001}, 0, 0, 0.002},
		{"zero damping", Spring{Stiffness: 100, PositionEpsilon: 0.001, VelocityEpsilon: 0.001}, 0, 0, 0.002},
		{"negative damping", Spring{Stiffness: 100, Damping: -20, PositionEpsilon: 0.001, VelocityEpsilon: 0.001}, 0, 0, 0.002},
		{"NaN damping", Spring{Stiffness: 100, Damping: math.NaN(), PositionEpsilon: 0.001, VelocityEpsilon: 0.001}, 0, 0, 0.002},
		{"zero position epsilon", Spring{Stiffness: 100, Damping: 20, VelocityEpsilon: 0.001}, 0, 0, 0.002},
		{"nonpositive velocity epsilon", Spring{Stiffness: 100, Damping: 20, PositionEpsilon: 0.001, VelocityEpsilon: math.Inf(-1)}, 0, 0, 0.002},
		{"NaN position", valid, math.NaN(), 0, 0.002},
		{"infinite velocity", valid, 0, math.Inf(1), 0.002},
		{"oversized step", valid, 0, 0, 0.003},
		{"NaN step", valid, 0, 0, math.NaN()},
	} {
		x, v, rest := tc.spring.Step(tc.x, tc.v, 1, tc.dt)
		if x != 1 || v != 0 || !rest {
			t.Errorf("%s: step = (%v, %v, %v), want (1, 0, true)", tc.name, x, v, rest)
		}
	}
}

func TestSpringStepNonpositiveTimeDoesNotAdvance(t *testing.T) {
	t.Parallel()
	// A zero or backwards interval is not motion: the value keeps its exact
	// state and its current rest classification, because settling on a
	// timestamp the caller cannot advance from would erase momentum.
	s := Spring{Stiffness: 100, Damping: 20,
		PositionEpsilon: 0.001, VelocityEpsilon: 0.001}
	for _, tc := range []struct {
		name     string
		x, v     float64
		wantRest bool
	}{
		{"moving", 0, 0.2, false},
		{"passing the target at speed", 1, 0.2, false},
		{"settled", 1, 0, true},
	} {
		for _, dt := range []float64{0, -1} {
			x, v, rest := s.Step(tc.x, tc.v, 1, dt)
			if x != tc.x || v != tc.v || rest != tc.wantRest {
				t.Errorf("%s at dt %g: step = (%v, %v, %v), want (%v, %v, %v)",
					tc.name, dt, x, v, rest, tc.x, tc.v, tc.wantRest)
			}
		}
	}
}
