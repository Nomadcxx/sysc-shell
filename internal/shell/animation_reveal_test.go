package shell

import (
	"math"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// revealPeak runs a 0->100 reveal spring and reports its peak and when it
// came, sampled every 2 ms for 1.5 s.
func revealPeak(a *animator, clock *fakeClock) (peak float64, at time.Duration) {
	a.Target("reveal", animReveal, 100)
	for step := time.Duration(0); step <= 1500*time.Millisecond; step += 2 * time.Millisecond {
		if v := a.Value("reveal", animReveal); v > peak {
			peak, at = v, step
		}
		clock.add(2 * time.Millisecond)
	}
	return peak, at
}

// The reveal is elastic: it passes its mark by under a tenth and settles.
func TestRevealSpringOvershootsAndSettles(t *testing.T) {
	a, clock := newTestAnimator(false)
	a.Target("reveal", animReveal, 100)
	peak := 0.0
	for step := time.Duration(0); step < 900*time.Millisecond; step += 2 * time.Millisecond {
		peak = math.Max(peak, a.Value("reveal", animReveal))
		clock.add(2 * time.Millisecond)
	}
	if peak <= 105 || peak >= 112 {
		t.Fatalf("peak = %.2f, want an overshoot between 105 and 112", peak)
	}
	if v := a.Value("reveal", animReveal); math.Abs(v-100) > 0.5 {
		t.Fatalf("value at 0.9 s = %.3f, want within half a pixel of 100", v)
	}
	clock.add(400 * time.Millisecond)
	if !a.Settled() || a.Value("reveal", animReveal) != 100 {
		t.Fatalf("not at rest by 1.3 s: %.4f", a.Value("reveal", animReveal))
	}
}

// Motion speed scales the spring like every other recipe: twice the speed
// reaches the peak in half the time.
func TestRevealSpringScalesWithSpeed(t *testing.T) {
	base, baseClock := newTestAnimator(false)
	_, baseAt := revealPeak(base, baseClock)
	fast := theme.BaseMotion
	fast.Long /= 2
	quick, quickClock := newMotionAnimator(render.MotionSet{Durations: fast})
	_, quickAt := revealPeak(quick, quickClock)
	if ratio := float64(quickAt) / float64(baseAt); ratio < 0.45 || ratio > 0.55 {
		t.Fatalf("peak at %v at speed 2 against %v at speed 1, ratio %.2f, want about half", quickAt, baseAt, ratio)
	}
}

// A curve may start later than it is set, and holds its start value until
// then; the surface keeps asking for frames until it ends.
func TestTargetCurveDelayAndEase(t *testing.T) {
	a, clock := newTestAnimator(false)
	linear := func(p float64) float64 { return p }
	a.TargetCurve("tint", animRevealTint, 1, 250*time.Millisecond, time.Second, linear)
	clock.add(100 * time.Millisecond)
	if v := a.Value("tint", animRevealTint); v != 0 {
		t.Fatalf("value before the delay = %v, want 0", v)
	}
	clock.add(650 * time.Millisecond)
	if v := a.Value("tint", animRevealTint); math.Abs(v-0.5) > 1e-9 {
		t.Fatalf("value midway = %v, want 0.5", v)
	}
	if a.Settled() {
		t.Fatal("settled midway")
	}
	clock.add(500 * time.Millisecond)
	if v := a.Value("tint", animRevealTint); v != 1 || !a.Settled() {
		t.Fatalf("value at the end = %v settled %v, want 1 and settled", v, a.Settled())
	}
}

func TestTargetCurveReducedSnaps(t *testing.T) {
	a, _ := newTestAnimator(true)
	a.TargetCurve("tint", animRevealTint, 0.4, 250*time.Millisecond, time.Second, func(p float64) float64 { return p })
	if v := a.Value("tint", animRevealTint); v != 0.4 || !a.Settled() {
		t.Fatalf("reduced curve = %v settled %v, want 0.4 at once", v, a.Settled())
	}
}

// Releasing a lean mid-curve springs from where the lean had got to.
func TestTargetRetargetsCurveIntoSpring(t *testing.T) {
	a, clock := newTestAnimator(false)
	a.TargetCurve("reveal", animReveal, 12, 0, time.Second, func(p float64) float64 { return p })
	clock.add(500 * time.Millisecond)
	a.Target("reveal", animReveal, 100)
	if v := a.Value("reveal", animReveal); math.Abs(v-6) > 1e-9 {
		t.Fatalf("spring started at %v, want the lean's 6", v)
	}
	clock.add(20 * time.Millisecond)
	if v := a.Value("reveal", animReveal); v <= 6 {
		t.Fatalf("spring did not move off 6: %v", v)
	}
}
