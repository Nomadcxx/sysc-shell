package shell

import (
	"testing"
	"time"
)

// fakeClock drives an animator without sleeping.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }

func newTestAnimator(reduced bool) (*animator, *fakeClock) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	return newAnimator(clock.now, reduced), clock
}

func TestAnimatorUsesCatalogueDurations(t *testing.T) {
	t.Parallel()
	a, _ := newTestAnimator(false)
	for _, tc := range []struct {
		name    string
		channel animChannel
		rising  bool
		want    time.Duration
	}{
		{"press in", animPress, true, 80 * time.Millisecond},
		{"press out", animPress, false, 120 * time.Millisecond},
		{"hover", animHover, true, 120 * time.Millisecond},
		{"hover out", animHover, false, 120 * time.Millisecond},
		{"selection", animSelect, true, 180 * time.Millisecond},
		{"panel enter", animVisible, true, 200 * time.Millisecond},
		{"panel exit", animVisible, false, 150 * time.Millisecond},
	} {
		if got := a.duration(tc.channel, tc.rising); got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAnimatorUsesOutQuartOnlyForSelection(t *testing.T) {
	t.Parallel()
	// Both curves are out-eased, so they only separate away from the endpoints.
	if got, want := easeFor(animSelect)(0.5), 0.9375; got != want {
		t.Errorf("selection ease at midpoint = %v, want out-quart %v", got, want)
	}
	for _, ch := range []animChannel{animHover, animPress, animVisible} {
		if got, want := easeFor(ch)(0.5), 0.875; got != want {
			t.Errorf("channel %d ease at midpoint = %v, want out-cubic %v", ch, got, want)
		}
	}
}

func TestAnimatorReversesFromTheRenderedValue(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.Target("lock", animHover, 1)
	clock.add(30 * time.Millisecond)
	mid := a.Value("lock", animHover)
	if mid <= 0 || mid >= 1 {
		t.Fatalf("value mid-flight = %v, want strictly between 0 and 1", mid)
	}

	// Turning around must not snap back to 1 or jump to 0.
	a.Target("lock", animHover, 0)
	if got := a.Value("lock", animHover); got != mid {
		t.Errorf("reversal starts at %v, want the rendered %v", got, mid)
	}
	clock.add(animHoverDuration)
	if got := a.Value("lock", animHover); got != 0 {
		t.Errorf("reversal end = %v, want 0", got)
	}
}

func TestAnimatorStopsRequestingFramesWhenSettled(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	if !a.Settled() {
		t.Error("a fresh animator is unsettled with nothing in flight")
	}
	a.Target("lock", animHover, 1)
	if a.Settled() {
		t.Error("Settled during a transition")
	}
	clock.add(animHoverDuration)
	if !a.Settled() {
		t.Error("still unsettled after the transition elapsed")
	}
	// Re-aiming at the value it already holds must not restart the clock; an
	// unchanged target is what keeps pointer motion from scheduling frames.
	a.Target("lock", animHover, 1)
	if !a.Settled() {
		t.Error("re-aiming at the current target restarted the clock")
	}
}

func TestAnimatorSnapsInteractionUnderReducedMotion(t *testing.T) {
	t.Parallel()
	a, _ := newTestAnimator(true)
	for _, ch := range []animChannel{animHover, animPress, animSelect} {
		a.Target("lock", ch, 1)
		if got := a.Value("lock", ch); got != 1 {
			t.Errorf("channel %d = %v under reduced motion, want an immediate 1", ch, got)
		}
	}
	if !a.Settled() {
		t.Error("reduced motion left interaction state unsettled")
	}
}

func TestReducedMotionFadesPanelsWithoutMoving(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(true)
	a.Target("power", animVisible, 1)

	if got := a.duration(animVisible, true); got > 150*time.Millisecond {
		t.Errorf("panel fade = %v, want no more than 150ms under reduced motion", got)
	}
	if got := a.PanelSlide("power"); got != 0 {
		t.Errorf("panel slid %d px under reduced motion, want an opacity-only change", got)
	}
	clock.add(75 * time.Millisecond)
	if got := a.PanelOpacity("power"); got <= 0 || got >= 1 {
		t.Errorf("mid-fade opacity = %v, want a value in flight", got)
	}
}

func TestPanelEntersFromItsAnchoredEdge(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.Target("power", animVisible, 1)

	if got := a.PanelSlide("power"); got != panelSlidePx {
		t.Errorf("panel starts %d px out, want %d", got, panelSlidePx)
	}
	if got := a.PanelOpacity("power"); got != 0 {
		t.Errorf("panel starts at opacity %v, want 0", got)
	}
	clock.add(animPanelInDuration)
	if got := a.PanelSlide("power"); got != 0 {
		t.Errorf("settled panel is %d px out, want 0", got)
	}
	if got := a.PanelOpacity("power"); got != 1 {
		t.Errorf("settled opacity = %v, want 1", got)
	}
}

func TestPressScalesTheVisualRectangleOnly(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	if got := a.PressScale("lock"); got != 1 {
		t.Errorf("resting scale = %v, want 1", got)
	}
	a.Target("lock", animPress, 1)
	clock.add(animPressInDuration)
	if got := a.PressScale("lock"); got != pressScale {
		t.Errorf("pressed scale = %v, want %v", got, pressScale)
	}
}

func TestAnimatorForgetsNodesThatLeftTheTree(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.Target("lock", animHover, 1)
	a.Target("lock", animPress, 1)
	a.Target("logout", animHover, 1)
	// Let the values leave their start point, or every reading is 0 and the
	// assertions below cannot tell a dropped node from a fresh one.
	clock.add(40 * time.Millisecond)

	a.Forget("lock")
	if got := a.Value("lock", animHover); got != 0 {
		t.Errorf("forgotten node still resolves %v", got)
	}
	if got := a.Value("logout", animHover); got == 0 {
		t.Error("Forget removed an unrelated node")
	}
}

func TestRetargetContinuesFromTheRenderedValue(t *testing.T) {
	t.Parallel()
	// A palette change retargets what is in flight rather than restarting it,
	// so a control mid-hover does not jump when the theme reloads.
	a, clock := newTestAnimator(false)
	a.Target("lock", animHover, 1)
	clock.add(40 * time.Millisecond)
	before := a.Value("lock", animHover)

	a.Retarget()
	if got := a.Value("lock", animHover); got != before {
		t.Errorf("retarget moved the rendered value from %v to %v", before, got)
	}
	clock.add(animHoverDuration)
	if got := a.Value("lock", animHover); got != 1 {
		t.Errorf("retargeted transition ended at %v, want 1", got)
	}
}
