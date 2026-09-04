package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"testing"
	"time"
)

// fakeClock drives an animator without sleeping.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }

func newTestAnimator(reduced bool) (*animator, *fakeClock) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	return newAnimator(clock.now, reduced, render.MotionSet{}), clock
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
		// Panel visibility used to be 200 and 150, neither of which is a token.
		// It maps onto the table now and keeps the asymmetry: a panel leaves
		// faster than it arrives.
		{"panel enter", animVisible, true, 180 * time.Millisecond},
		{"panel exit", animVisible, false, 120 * time.Millisecond},
	} {
		if got := a.duration(tc.channel, tc.rising); got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAnimatorUsesOutQuartOnlyForSelection(t *testing.T) {
	t.Parallel()
	// Both curves are out-eased, so they only separate away from the endpoints.
	if got, want := newAnimator(nil, false, render.MotionSet{}).easeFor(animSelect)(0.5), 0.9375; got != want {
		t.Errorf("selection ease at midpoint = %v, want out-quart %v", got, want)
	}
	for _, ch := range []animChannel{animHover, animPress, animVisible} {
		if got, want := newAnimator(nil, false, render.MotionSet{}).easeFor(ch)(0.5), 0.875; got != want {
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
	clock.add(theme.BaseMotion.Short)
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
	clock.add(theme.BaseMotion.Short)
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
	clock.add(theme.BaseMotion.Medium)
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
	clock.add(theme.BaseMotion.Shorter)
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
	clock.add(theme.BaseMotion.Short)
	if got := a.Value("lock", animHover); got != 1 {
		t.Errorf("retargeted transition ended at %v, want 1", got)
	}
}

// TestAnimatorScalesWithMotionSpeedOnce covers the bounded speed factor at its
// ends. The table handed to the animator is already divided, and the animator
// must not divide it again: scaling twice is the failure this guards, and it
// only shows at a speed that is not 100.
func TestAnimatorScalesWithMotionSpeedOnce(t *testing.T) {
	t.Parallel()
	for _, speed := range []int{25, 100, 400} {
		want := theme.BaseMotion.AtSpeed(speed)
		a := newAnimator(nil, false, render.MotionSet{Durations: want})
		if got := a.duration(animHover, true); got != want.Short {
			t.Errorf("speed %d: hover = %v, want %v", speed, got, want.Short)
		}
		if got := a.duration(animSelect, true); got != want.Medium {
			t.Errorf("speed %d: selection = %v, want %v", speed, got, want.Medium)
		}
	}
	// A quarter speed is four times longer, and four times speed is a quarter.
	slow := theme.BaseMotion.AtSpeed(25).Short
	fast := theme.BaseMotion.AtSpeed(400).Short
	if slow <= theme.BaseMotion.Short || fast >= theme.BaseMotion.Short {
		t.Errorf("speed does not move the token: 25%%=%v 100%%=%v 400%%=%v", slow, theme.BaseMotion.Short, fast)
	}
}

// TestAnimatorExpressiveUsesTheQuartCurve checks the motion style reaches the
// easing. Standard settles spatial recipes with out-cubic; expressive settles
// them harder, which is the whole difference between the two styles.
func TestAnimatorExpressiveUsesTheQuartCurve(t *testing.T) {
	t.Parallel()
	std := newAnimator(nil, false, render.MotionSet{Spatial: theme.CurveOutCubic})
	exp := newAnimator(nil, false, render.MotionSet{Spatial: theme.CurveOutQuart})
	if got := std.easeFor(animVisible)(0.5); got != 0.875 {
		t.Errorf("standard visibility ease = %v, want out-cubic 0.875", got)
	}
	if got := exp.easeFor(animVisible)(0.5); got != 0.9375 {
		t.Errorf("expressive visibility ease = %v, want out-quart 0.9375", got)
	}
	// Selection settles hard in either style.
	if got := std.easeFor(animSelect)(0.5); got != 0.9375 {
		t.Errorf("selection ease = %v, want out-quart 0.9375", got)
	}
}

// TestAnimatorReducedMotionCapsTheFade keeps the one surviving transition
// bounded. The table can be slowed fourfold, and a fade that long is exactly
// what reduced motion exists to prevent.
func TestAnimatorReducedMotionCapsTheFade(t *testing.T) {
	t.Parallel()
	slow := newAnimator(nil, true, render.MotionSet{Durations: theme.BaseMotion.AtSpeed(25)})
	if got := slow.duration(animVisible, true); got != reducedPanelCap {
		t.Errorf("reduced fade at quarter speed = %v, want the %v cap", got, reducedPanelCap)
	}
	for _, ch := range []animChannel{animHover, animPress, animSelect, animTheme} {
		if got := slow.duration(ch, true); got != 0 {
			t.Errorf("reduced motion left channel %d running for %v", ch, got)
		}
	}
}
