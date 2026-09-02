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
