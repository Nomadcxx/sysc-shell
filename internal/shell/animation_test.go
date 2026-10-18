package shell

import (
	"math"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// fakeClock drives an animator without sleeping.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }

func newTestAnimator(reduced bool) (*animator, *fakeClock) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	return newAnimator(clock.now, reduced, render.MotionSet{}), clock
}

// newMotionAnimator builds an animator with an explicit motion table for tests
// that need a token other than the base one.
func newMotionAnimator(motion render.MotionSet) (*animator, *fakeClock) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	return newAnimator(clock.now, motion.Reduced, motion), clock
}

// settleSprings advances the fake clock in whole physics steps until the
// animator reports settled, so a completion assertion does not read the token
// as a deadline: a spring is within a thousandth of its target at about the
// token, and reaches its exact endpoint later.
func settleSprings(a *animator, clock *fakeClock) {
	for i := 0; i < 4000 && !a.Settled(); i++ {
		clock.add(springStep)
	}
}

func TestDecorativeLoopsDoNotKeepInteractionCadence(t *testing.T) {
	a, clock := newTestAnimator(false)
	a.TargetLoop("wordmark", animGradient, 0, 1, time.Second, ui.GradientLoop)
	a.TargetSweep("weather", animEffect, time.Second)
	if a.Settled() || !a.SettledExceptEffects() {
		t.Fatal("decorative loops must keep painting at the effect cadence")
	}
	for _, channel := range []animChannel{animHover, animPress, animTheme, animProgress, animSweep, animSprite} {
		switch channel {
		case animSweep:
			a.TargetSweep("active", channel, time.Second)
		case animSprite:
			a.TargetCycle("active", time.Second, 2, 3)
		default:
			a.Target("active", channel, 1)
		}
		if a.SettledExceptEffects() {
			t.Fatalf("active channel %v must keep the interaction cadence", channel)
		}
		a.Forget("active")
	}
	a.Target("button", animHover, 1)
	clock.add(time.Second)
	if !a.SettledExceptEffects() {
		t.Fatal("finished hover must return to the effect cadence")
	}
}

func TestStateLayerPolicyGatesHoverOnly(t *testing.T) {
	t.Parallel()
	tree := func() *ui.Node {
		return &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{
			{Kind: ui.KindButton, Action: "b", Bounds: ui.Rect{W: 10, H: 10}},
		}}
	}
	for _, tc := range []struct {
		name  string
		pol   interaction
		want  ui.Interaction
		notWn ui.Interaction
	}{
		{"bar paints clickables at rest", interaction{stateLayer: false}, ui.StatePressed, ui.StateHovered},
		{"panels tint hover and press", interaction{stateLayer: true}, ui.StatePressed | ui.StateHovered, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := tree()
			pol := tc.pol
			pol.hover, pol.press = "b", "b"
			pol.apply(root, nil)
			n := root.Children[0]
			if !n.State.Has(tc.want) || n.State.Has(tc.notWn) {
				t.Fatalf("state = %v, want %v without %v", n.State, tc.want, tc.notWn)
			}
		})
	}
}

func TestRippleRestartsAtTheNewOrigin(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.TargetRipple("lock", 3, 4)
	clock.add(400 * time.Millisecond)
	if phase, x, y, ok := a.Ripple("lock"); !ok || phase <= 0 || phase >= 1 || x != 3 || y != 4 {
		t.Fatalf("in-flight ripple = %v at %v,%v (ok %v)", phase, x, y, ok)
	}
	a.TargetRipple("lock", 8, 9)
	if phase, x, y, _ := a.Ripple("lock"); phase > 0.1 || x != 8 || y != 9 {
		t.Fatalf("second press left phase %v at %v,%v", phase, x, y)
	}
	clock.add(2 * time.Second)
	if phase, _, _, _ := a.Ripple("lock"); phase != 1 {
		t.Fatalf("settled ripple phase = %v, want 1", phase)
	}
	if !a.Settled() {
		t.Error("a settled ripple must not keep the surface requesting frames")
	}
	if _, _, _, ok := a.Ripple("ghost"); ok {
		t.Error("a node with no ripple reported one")
	}
}

func TestRippleRunsLinearlyForItsFullRecipe(t *testing.T) {
	t.Parallel()
	a, _ := newTestAnimator(false)
	if got := a.duration(animRipple, true); got != 800*time.Millisecond {
		t.Fatalf("ripple duration = %v, want 800ms", got)
	}
	a.TargetRipple("lock", 0, 0)
	if got := a.easeFor(animRipple)(0.5); got != 0.5 {
		t.Fatalf("ripple ease at midpoint = %v, want linear", got)
	}
}

func TestRippleSnapsUnderReducedMotion(t *testing.T) {
	t.Parallel()
	a, _ := newTestAnimator(true)
	a.TargetRipple("lock", 1, 2)
	if phase, _, _, _ := a.Ripple("lock"); phase != 1 {
		t.Fatalf("reduced-motion ripple phase = %v, want 1", phase)
	}
	if !a.Settled() {
		t.Error("reduced-motion ripple requested frames")
	}
}

func TestRippleOriginIsForgottenWithItsNode(t *testing.T) {
	t.Parallel()
	a, _ := newTestAnimator(false)
	a.TargetRipple("lock", 3, 4)
	a.Forget("lock")
	if _, _, _, ok := a.Ripple("lock"); ok {
		t.Error("forgotten node still reported a ripple")
	}
}

func TestRetargetDoesNotRestartRipple(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.TargetRipple("lock", 3, 4)
	clock.add(400 * time.Millisecond)
	a.Retarget()
	clock.add(400 * time.Millisecond)
	if phase, _, _, _ := a.Ripple("lock"); phase != 1 {
		t.Fatalf("theme retarget left ripple at %v, want settled", phase)
	}
}

func TestApplyWritesPressProgressAndRipple(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{
		{Kind: ui.KindButton, Action: "b", Bounds: ui.Rect{W: 10, H: 10}},
	}}
	interaction{press: "b", stateLayer: true}.apply(root, a)
	a.TargetRipple("b", 2, 3)
	clock.add(30 * time.Millisecond)
	interaction{press: "b", stateLayer: true}.apply(root, a)
	n := root.Children[0]
	if n.PressProgress <= 0 || n.PressProgress >= 1 {
		t.Fatalf("press progress = %v, want a value in flight", n.PressProgress)
	}
	if n.Ripple.X != 2 || n.Ripple.Y != 3 || n.Ripple.Phase <= 0 || n.Ripple.Phase >= 1 {
		t.Fatalf("ripple on the copy = %+v", n.Ripple)
	}
}

func TestInteractionKeepsHoverStateThroughFadeOut(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	n := &ui.Node{Kind: ui.KindButton, Action: "b"}
	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{n}}
	pointer := interaction{stateLayer: true}
	pointer.setHover("b")
	pointer.apply(root, a)
	if n.State.Has(ui.StateHovered) || n.HoverProgress != 0 {
		t.Fatalf("hover at transition start = %v, %v", n.State, n.HoverProgress)
	}
	clock.add(40 * time.Millisecond)
	pointer.apply(root, a)
	if !n.State.Has(ui.StateHovered) || n.HoverProgress <= 0 || n.HoverProgress >= 1 {
		t.Fatalf("hover did not enter through its channel: state=%v progress=%v", n.State, n.HoverProgress)
	}
	pointer.setHover("")
	pointer.apply(root, a)
	clock.add(40 * time.Millisecond)
	pointer.apply(root, a)
	if !n.State.Has(ui.StateHovered) || n.HoverProgress <= 0 {
		t.Fatalf("hover state vanished before fading: state=%v progress=%v", n.State, n.HoverProgress)
	}
	settleSprings(a, clock)
	pointer.apply(root, a)
	if n.State.Has(ui.StateHovered) || n.HoverProgress != 0 {
		t.Fatalf("hover remained after settling: state=%v progress=%v", n.State, n.HoverProgress)
	}
}

func TestInteractionKeepsPressStateThroughReleaseFade(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	n := &ui.Node{Kind: ui.KindButton, Action: "b"}
	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{n}}
	pointer := interaction{stateLayer: true}
	pointer.setPress("b")
	pointer.apply(root, a)
	clock.add(30 * time.Millisecond)
	pointer.apply(root, a)
	if !n.State.Has(ui.StatePressed) || n.PressProgress <= 0 || n.PressProgress >= 1 {
		t.Fatalf("press did not enter through its channel: state=%v progress=%v", n.State, n.PressProgress)
	}
	pointer.setPress("")
	pointer.apply(root, a)
	clock.add(30 * time.Millisecond)
	pointer.apply(root, a)
	if !n.State.Has(ui.StatePressed) || n.PressProgress <= 0 || n.PressProgress >= 1 {
		t.Fatalf("press state vanished before fading: state=%v progress=%v", n.State, n.PressProgress)
	}
	clock.add(120 * time.Millisecond)
	pointer.apply(root, a)
	if n.State.Has(ui.StatePressed) || n.PressProgress != 0 {
		t.Fatalf("press remained after settling: state=%v progress=%v", n.State, n.PressProgress)
	}
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
		{"press in", animPress, true, 75 * time.Millisecond},
		{"press out", animPress, false, 150 * time.Millisecond},
		{"hover", animHover, true, 150 * time.Millisecond},
		{"hover out", animHover, false, 150 * time.Millisecond},
		{"selection", animSelect, true, 300 * time.Millisecond},
		// Panel visibility used to be 200 and 150, neither of which is a token.
		// It maps onto the table now and keeps the asymmetry: a panel leaves
		// faster than it arrives.
		{"panel enter", animVisible, true, 300 * time.Millisecond},
		{"panel exit", animVisible, false, 150 * time.Millisecond},
		{"progress fill", animProgress, true, 300 * time.Millisecond},
		{"progress drain", animProgress, false, 300 * time.Millisecond},
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
	settleSprings(a, clock)
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
	settleSprings(a, clock)
	if !a.Settled() {
		t.Error("still unsettled after the transition settled")
	}
	// Re-aiming at the value it already holds must not restart the clock; an
	// unchanged target is what keeps pointer motion from scheduling frames.
	a.Target("lock", animHover, 1)
	if !a.Settled() {
		t.Error("re-aiming at the current target restarted the clock")
	}
}

func TestAnimatorLoopPingPongsUntilForgotten(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.TargetLoop("wordmark", animGradient, -0.45, 0.45, 2*time.Second, ui.GradientPingPong)

	if a.Settled() {
		t.Fatal("ping-pong gradient settled at its start")
	}
	clock.add(time.Second)
	if got := a.Value("wordmark", animGradient); math.Abs(got) > 1e-9 {
		t.Fatalf("half-trip value = %v, want midpoint 0", got)
	}
	clock.add(time.Second)
	if got := a.Value("wordmark", animGradient); got != 0.45 {
		t.Fatalf("trip-end value = %v, want 0.45", got)
	}
	if a.Settled() {
		t.Fatal("ping-pong gradient settled at its turn")
	}
	clock.add(2 * time.Second)
	if got := a.Value("wordmark", animGradient); got != -0.45 {
		t.Fatalf("round-trip value = %v, want -0.45", got)
	}
	if a.Settled() {
		t.Fatal("ping-pong gradient settled after a full cycle")
	}
	a.Forget("wordmark")
	if !a.Settled() {
		t.Fatal("forgotten gradient kept the animator unsettled")
	}
}

func TestAnimatorLoopTargetKeepsItsPhase(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.TargetLoop("wordmark", animGradient, 0, 1, 2*time.Second, ui.GradientLoop)
	clock.add(500 * time.Millisecond)
	a.TargetLoop("wordmark", animGradient, 0, 1, 2*time.Second, ui.GradientLoop)
	clock.add(500 * time.Millisecond)
	if got := a.Value("wordmark", animGradient); got != 0.5 {
		t.Fatalf("identical target reset phase: value = %v, want 0.5", got)
	}
	clock.add(time.Second)
	if got := a.Value("wordmark", animGradient); got != 0 {
		t.Fatalf("loop did not restart at the trip boundary: value = %v", got)
	}
}

func TestAnimatorLoopParksUnderReducedMotion(t *testing.T) {
	t.Parallel()
	a, _ := newTestAnimator(true)
	a.TargetLoop("wordmark", animGradient, -0.45, 0.45, 2*time.Second, ui.GradientPingPong)
	if got := a.Value("wordmark", animGradient); math.Abs(got) > 1e-9 {
		t.Fatalf("reduced-motion value = %v, want midpoint 0", got)
	}
	if !a.Settled() {
		t.Fatal("reduced-motion gradient requested more frames")
	}
}

func TestAnimatorLoopWithNoTripDoesNotRequestFrames(t *testing.T) {
	t.Parallel()
	a, _ := newTestAnimator(false)
	a.TargetLoop("wordmark", animGradient, -0.45, 0.45, 0, ui.GradientPingPong)
	if !a.Settled() {
		t.Fatal("zero-duration loop would request frames forever")
	}
}

func TestAnimatorSweepWrapsLinearly(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.TargetSweep("media-title", animSweep, 2*time.Second)
	if a.Settled() {
		t.Fatal("sweep settled at its start")
	}
	clock.add(500 * time.Millisecond)
	if got := a.Value("media-title", animSweep); got != 0.25 {
		t.Fatalf("quarter sweep = %v, want 0.25", got)
	}
	clock.add(1500 * time.Millisecond)
	if got := a.Value("media-title", animSweep); got != 0 {
		t.Fatalf("sweep did not wrap at its trip: %v", got)
	}
	if a.Settled() {
		t.Fatal("sweep settled after wrapping")
	}
}

func TestAnimatorSweepParksUnderReducedMotion(t *testing.T) {
	t.Parallel()
	a, _ := newTestAnimator(true)
	a.TargetSweep("media-title", animSweep, 2*time.Second)
	if got := a.Value("media-title", animSweep); got != 0 {
		t.Fatalf("reduced-motion sweep = %v, want 0", got)
	}
	if !a.Settled() {
		t.Fatal("reduced-motion sweep requested frames")
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
	settleSprings(a, clock)
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

func TestAnimateSurfacePacesPublishes(t *testing.T) {
	t.Parallel()
	// The ticker runs at 16 ms. With a 50 ms cap, a run of unsettled frames
	// must publish far fewer times than it ticks.
	stop := make(chan struct{})
	var published int
	done := make(chan struct{})
	go func() {
		animateSurface(stop, func() bool { return false }, func() { published++ }, func() time.Duration { return 50 * time.Millisecond })
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	close(stop)
	<-done

	if published == 0 {
		t.Fatal("pacing suppressed every frame; the animation would never paint")
	}
	if published > 8 {
		t.Errorf("published %d times in 200ms with a 50ms cap; pacing is not applied", published)
	}
}

func TestAnimateSurfaceAlwaysPublishesTheSettlingFrame(t *testing.T) {
	t.Parallel()
	// The last frame must paint even if it lands inside the cap window, or a
	// settled value is left unpainted and the surface keeps a stale pixel.
	stop := make(chan struct{})
	defer close(stop)
	var published int
	done := make(chan struct{})
	go func() {
		animateSurface(stop, func() bool { return true }, func() { published++ }, func() time.Duration { return time.Hour })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("animateSurface did not return after settling")
	}
	if published != 1 {
		t.Errorf("published %d times, want exactly 1 settling frame despite the hour-long cap", published)
	}
}

func TestAnimateSurfaceStopsOnStop(t *testing.T) {
	t.Parallel()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		animateSurface(stop, func() bool { return false }, func() {}, func() time.Duration { return time.Millisecond })
		close(done)
	}()
	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("animateSurface ignored stop")
	}
}

func TestProgressGlideRetargetsFromMidFlight(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.Target("b", animProgress, 0.2)
	clock.add(150 * time.Millisecond)
	mid := a.Value("b", animProgress)
	if mid <= 0 || mid >= 0.2 {
		t.Fatalf("mid-flight value = %v, want strictly inside the glide", mid)
	}
	// A new target continues from wherever the value is, not from zero.
	a.Target("b", animProgress, 0.8)
	if got := a.Value("b", animProgress); math.Abs(got-mid) > 0.01 {
		t.Fatalf("value after retarget = %v, want near %v", got, mid)
	}
	settleSprings(a, clock)
	if got := a.Value("b", animProgress); got != 0.8 {
		t.Fatalf("settled value = %v, want exactly 0.8", got)
	}
	if !a.Settled() {
		t.Fatal("animator still busy after the glide window")
	}
}

func TestProgressGlideWritesTheTargetUnderReducedMotion(t *testing.T) {
	t.Parallel()
	a, _ := newTestAnimator(true)
	a.Target("b", animProgress, 0.8)
	if got := a.Value("b", animProgress); got != 0.8 {
		t.Fatalf("reduced-motion value = %v, want the target at once", got)
	}
	if !a.Settled() {
		t.Fatal("reduced-motion glide left work in flight")
	}
}

func TestResolveProgressMotionGlidesRetargetsAndRetires(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)

	meter := func(value float64) *ui.Node {
		return &ui.Node{Kind: ui.KindMeter, Key: "battery", Value: value, Animate: true}
	}
	static := &ui.Node{Kind: ui.KindMeter, Key: "cpu", Value: 0.5}

	// First resolve starts the glide from zero.
	first := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{meter(0.2), static}}
	resolveProgressMotion(a, first)
	if got := first.Children[0].Value; got != 0 {
		t.Fatalf("first resolve value = %v, want the glide's start", got)
	}
	if first.Children[1].Value != 0.5 {
		t.Fatalf("static meter moved to %v", first.Children[1].Value)
	}

	// A later revision lands exactly on its target.
	clock.add(300 * time.Millisecond)
	second := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{meter(0.2), static}}
	resolveProgressMotion(a, second)
	if got := second.Children[0].Value; got != 0.2 {
		t.Fatalf("settled value = %v, want exactly 0.2", got)
	}

	// A higher target glides from where the value is, never backward.
	third := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{meter(0.8), static}}
	resolveProgressMotion(a, third)
	clock.add(75 * time.Millisecond)
	mid := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{meter(0.8), static}}
	resolveProgressMotion(a, mid)
	if got := mid.Children[0].Value; got <= 0.2 || got >= 0.8 {
		t.Fatalf("retargeted value = %v, want strictly between 0.2 and 0.8", got)
	}
	clock.add(300 * time.Millisecond)
	again := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{meter(0.8), static}}
	resolveProgressMotion(a, again)
	if got := again.Children[0].Value; got != 0.8 {
		t.Fatalf("settled value = %v, want exactly 0.8", got)
	}

	// A node that left the tree retires its key.
	gone := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{static}}
	resolveProgressMotion(a, gone)
	for key := range a.values {
		if key.channel == animProgress {
			t.Fatalf("stale progress key %q survived the retire", key.node)
		}
	}
}

func TestSpringTargetPreservesVelocity(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.Target("hover", animHover, 1)
	// Sampled early in the flight: at this spring's stiffness the carried
	// speed outlasts one physics step here, while a late reversal is already
	// being decelerated by the same spring force.
	clock.add(10 * time.Millisecond)
	before := a.Value("hover", animHover)
	key := animKey{node: "hover", channel: animHover}
	oldVelocity := a.values[key].velocity
	if oldVelocity <= 0 {
		t.Fatalf("no forward velocity: %g", oldVelocity)
	}
	a.Target("hover", animHover, 0)
	if got := a.Value("hover", animHover); got != before {
		t.Fatalf("retarget jumped from %g to %g", before, got)
	}
	if got := a.values[key].velocity; got != oldVelocity {
		t.Fatalf("retarget speed = %g, want %g", got, oldVelocity)
	}
	clock.add(2 * time.Millisecond)
	_ = a.Value("hover", animHover)
	if a.values[key].position <= before {
		t.Fatal("reversal erased the old forward momentum")
	}
}

func TestSpringSamplingPartitionsAgree(t *testing.T) {
	t.Parallel()
	// The stored value depends on how much time passed, never on how the
	// caller chopped it: 17 ms + 31 ms must equal one 48 ms advance.
	one, c1 := newTestAnimator(false)
	split, c2 := newTestAnimator(false)
	one.Target("m", animProgress, 0.8)
	split.Target("m", animProgress, 0.8)
	c1.add(48 * time.Millisecond)
	_ = one.Value("m", animProgress)
	c2.add(17 * time.Millisecond)
	_ = split.Value("m", animProgress)
	c2.add(31 * time.Millisecond)
	_ = split.Value("m", animProgress)
	key := animKey{node: "m", channel: animProgress}
	x, y := one.values[key], split.values[key]
	if x.position != y.position || x.velocity != y.velocity || x.remainder != y.remainder {
		t.Fatalf("partition changed physics: %+v / %+v", x, y)
	}
	// Settling and reading at the same timestamp integrate nothing twice.
	before := split.values[key]
	_ = split.Value("m", animProgress)
	_ = split.Settled()
	if after := split.values[key]; after.position != before.position || after.velocity != before.velocity {
		t.Fatalf("same-time reads integrated twice: %+v -> %+v", before, after)
	}
}

func TestSpringClockDoesNotRewind(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.Target("h", animHover, 1)
	clock.add(16 * time.Millisecond)
	first := a.Value("h", animHover)
	key := animKey{node: "h", channel: animHover}
	position := a.values[key].position
	// A backwards clock neither rewinds accepted time nor moves the value.
	clock.add(-8 * time.Millisecond)
	if got := a.Value("h", animHover); got != first {
		t.Fatalf("backwards clock moved the value %g -> %g", first, got)
	}
	// Returning to the highest accepted timestamp still integrates nothing.
	clock.add(8 * time.Millisecond)
	if got := a.Value("h", animHover); got != first {
		t.Fatalf("replayed timestamp moved the value %g -> %g", first, got)
	}
	clock.add(springStep)
	_ = a.Value("h", animHover)
	if got := a.values[key].position; got == position {
		t.Fatal("no physics step after the clock advanced")
	}
}

func TestSpringLongGapSnaps(t *testing.T) {
	t.Parallel()
	// A slow token keeps the value visibly moving across the whole test, so
	// the two gap behaviours are distinguishable.
	slow := render.MotionSet{Durations: theme.MotionTokens{
		Shorter: 60 * time.Second, Short: 60 * time.Second, Medium: 60 * time.Second,
	}}
	bounded, clock := newMotionAnimator(slow)
	bounded.Target("h", animHover, 1)
	clock.add(40 * time.Millisecond)
	if got := bounded.Value("h", animHover); got <= 0 || got >= 1 {
		t.Fatalf("value not in flight: %g", got)
	}
	// Under the ceiling the value integrates through bounded fixed steps.
	clock.add(1900 * time.Millisecond)
	if got := bounded.Value("h", animHover); got <= 0 || got >= 1 {
		t.Fatalf("a sub-ceiling gap snapped instead of integrating: %g", got)
	}
	// At the ceiling it snaps to the target and clears the physics state.
	snapped, clock2 := newMotionAnimator(slow)
	snapped.Target("h", animHover, 1)
	clock2.add(2 * time.Second)
	if got := snapped.Value("h", animHover); got != 1 {
		t.Fatalf("gap value = %g, want the target", got)
	}
	key := animKey{node: "h", channel: animHover}
	if v := snapped.values[key]; v.velocity != 0 || v.remainder != 0 {
		t.Fatalf("gap left physics state: %+v", v)
	}
	if !snapped.Settled() {
		t.Fatal("gap left the animator unsettled")
	}
}

func TestSpringNonfiniteTargetKeepsState(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.Target("h", animHover, 1)
	clock.add(30 * time.Millisecond)
	_ = a.Value("h", animHover)
	key := animKey{node: "h", channel: animHover}
	before := a.values[key]
	// Drain the pending wake so these assertions measure only the calls here.
	select {
	case <-a.wake:
	default:
	}
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		a.Target("h", animHover, bad)
		if len(a.wake) != 0 {
			t.Fatalf("non-finite target %v nudged the loop", bad)
		}
	}
	if got := a.values[key]; got.position != before.position || got.velocity != before.velocity || got.to != before.to {
		t.Fatalf("non-finite target changed state: %+v -> %+v", before, got)
	}
	// Re-aiming at the stored target is a no-op as well.
	a.Target("h", animHover, before.to)
	if len(a.wake) != 0 {
		t.Fatal("unchanged target nudged the loop")
	}
}

func TestSpringRetargetKeepsFractionalClock(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.Target("m", animProgress, 0.8)
	clock.add(17 * time.Millisecond)
	_ = a.Value("m", animProgress)
	key := animKey{node: "m", channel: animProgress}
	if got := a.values[key].remainder; got != time.Millisecond {
		t.Fatalf("remainder after 17 ms = %v, want 1ms", got)
	}
	position, velocity := a.values[key].position, a.values[key].velocity
	// A retarget consumes whole old-target steps, keeps the fraction, and
	// leaves position and velocity alone.
	a.Target("m", animProgress, 0.5)
	if got := a.values[key]; got.remainder != time.Millisecond || got.position != position || got.velocity != velocity {
		t.Fatalf("retarget disturbed the lattice: %+v", got)
	}
	clock.add(time.Millisecond)
	_ = a.Value("m", animProgress)
	// The finishing millisecond completes one lattice interval, and the new
	// target is what pulls it.
	if got := a.values[key].remainder; got != 0 {
		t.Fatalf("remainder after the finishing millisecond = %v", got)
	}
	if got := a.values[key].velocity; !(got < velocity) {
		t.Fatalf("velocity %g did not turn toward the lowered target", got)
	}
	settledAt := a.values[key].position
	_ = a.Value("m", animProgress)
	if got := a.values[key].position; got != settledAt {
		t.Fatal("same-time read integrated again")
	}
}

func TestSpringRestHasExactEndpoint(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.Target("h", animHover, 1)
	key := animKey{node: "h", channel: animHover}
	steps := 0
	for steps = 0; steps < 4000 && !a.Settled(); steps++ {
		clock.add(springStep)
	}
	if steps >= 4000 {
		t.Fatal("spring never settled")
	}
	if v := a.values[key]; v.position != 1 || v.velocity != 0 {
		t.Fatalf("rest state = %+v, want the exact endpoint", v)
	}
}

func TestSpringSettlesWithoutValueReads(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.Target("h", animHover, 1)
	key := animKey{node: "h", channel: animHover}
	for i := 0; i < 4000 && !a.SettledExceptEffects(); i++ {
		clock.add(springStep)
	}
	if !a.SettledExceptEffects() || !a.Settled() {
		t.Fatal("settlement did not advance through the queries alone")
	}
	if v := a.values[key]; v.position != 1 || v.velocity != 0 {
		t.Fatalf("query-only settlement left %+v", v)
	}
}

func TestSpringResetAndForgetDropVelocity(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.Target("h", animHover, 1)
	clock.add(40 * time.Millisecond)
	_ = a.Value("h", animHover)
	key := animKey{node: "h", channel: animHover}
	if a.values[key].velocity == 0 {
		t.Fatal("no velocity to drop")
	}
	a.Reset("h", animHover)
	a.Target("h", animHover, 1)
	if got := a.values[key].position; got != 0 {
		t.Fatalf("Reset did not start from zero: %g", got)
	}
	// Forget removes every channel of one key and leaves another key alone.
	a.Target("other", animHover, 1)
	a.Forget("h")
	if a.has("h", animHover) || a.has("h", animProgress) {
		t.Fatal("Forget kept a channel")
	}
	if !a.has("other", animHover) {
		t.Fatal("Forget removed an unrelated key")
	}
}

func TestSpringRetargetPaletteKeepsVelocity(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.Target("h", animHover, 1)
	clock.add(40 * time.Millisecond)
	_ = a.Value("h", animHover)
	key := animKey{node: "h", channel: animHover}
	position, velocity := a.values[key].position, a.values[key].velocity
	a.Retarget()
	if got := a.values[key]; got.position != position || got.velocity != velocity {
		t.Fatalf("palette retarget moved the spring: %+v", got)
	}
}

func TestSpringCatalogueUsesResolvedMotion(t *testing.T) {
	t.Parallel()
	for _, speed := range []int{25, 100, 400} {
		tokens := theme.BaseMotion.AtSpeed(speed)
		for _, tc := range []struct {
			name    string
			channel animChannel
			to      float64
			token   time.Duration
		}{
			{"hover", animHover, 1, tokens.Short},
			{"select", animSelect, 1, tokens.Medium},
			{"visible rising", animVisible, 1, tokens.Medium},
			{"visible falling", animVisible, 0, tokens.Short},
			{"progress", animProgress, 0.8, tokens.Medium},
		} {
			for _, curve := range []theme.Curve{theme.CurveOutCubic, theme.CurveOutQuart} {
				motion := render.MotionSet{Durations: tokens, Spatial: curve}
				a, _ := newMotionAnimator(motion)
				if got := a.duration(tc.channel, tc.to > 0); got != tc.token {
					t.Fatalf("%s speed %d: duration = %v, want %v", tc.name, speed, got, tc.token)
				}
				a.Target("meter", tc.channel, tc.to)
				spring := a.values[animKey{node: "meter", channel: tc.channel}].spring
				if spring == nil {
					t.Fatalf("%s speed %d curve %v is not a spring", tc.name, speed, curve)
				}
				factor := 1.0
				if curve == theme.CurveOutQuart {
					factor = 1.1
				}
				omega := 12 * factor / tc.token.Seconds()
				if math.Abs(spring.Stiffness-omega*omega) > 1e-9 || math.Abs(spring.Damping-2*omega) > 1e-9 {
					t.Fatalf("%s speed %d curve %v: k=%g c=%g, want %g/%g",
						tc.name, speed, curve, spring.Stiffness, spring.Damping, omega*omega, 2*omega)
				}
				if spring.PositionEpsilon != 0.001 || math.Abs(spring.VelocityEpsilon-0.001/tc.token.Seconds()) > 1e-12 {
					t.Fatalf("%s speed %d curve %v: epsilons %g/%g",
						tc.name, speed, curve, spring.PositionEpsilon, spring.VelocityEpsilon)
				}
			}
		}
	}
}

func TestSpringPresetsStableAtEverySpeed(t *testing.T) {
	t.Parallel()
	for _, speed := range []int{25, 100, 400} {
		for _, curve := range []theme.Curve{theme.CurveOutCubic, theme.CurveOutQuart} {
			for _, tc := range []struct {
				name    string
				channel animChannel
				to      float64
			}{
				{"hover", animHover, 1},
				{"select", animSelect, 1},
				{"visible", animVisible, 1},
				{"progress", animProgress, 0.8},
			} {
				motion := render.MotionSet{Durations: theme.BaseMotion.AtSpeed(speed), Spatial: curve}
				a, clock := newMotionAnimator(motion)
				a.Target("t", tc.channel, tc.to)
				token := a.duration(tc.channel, true)
				key := animKey{node: "t", channel: tc.channel}
				settled := false
				steps := 0
				for steps = 0; steps < 4000 && !settled; steps++ {
					clock.add(springStep)
					settled = a.Settled()
					v := a.values[key]
					if math.IsNaN(v.position) || math.IsInf(v.position, 0) ||
						math.IsNaN(v.velocity) || math.IsInf(v.velocity, 0) {
						t.Fatalf("%s speed %d curve %v: non-finite state %+v", tc.name, speed, curve, v)
					}
					if v.position < 0 || v.position > 1 {
						t.Fatalf("%s speed %d curve %v: step from rest left [0,1]: %g",
							tc.name, speed, curve, v.position)
					}
					if time.Duration(steps)*springStep > 10*token {
						break
					}
				}
				if !settled {
					t.Fatalf("%s speed %d curve %v did not settle within ten tokens", tc.name, speed, curve)
				}
			}
		}
	}
}

func TestSpringPaintBoundsKeepMomentum(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	spring := a.springFor(animHover, true)
	if spring == nil {
		t.Fatal("hover is not a spring")
	}
	key := animKey{node: "h", channel: animHover}
	// A hard shove near the target carries the stored position past 1; the
	// painted value clamps while the stored state keeps its speed.
	a.values[key] = animValue{from: 0.9, to: 1, spring: spring,
		position: 0.9, velocity: 200, advanced: clock.now()}
	clock.add(springStep)
	if got := a.Value("h", animHover); got != 1 {
		t.Fatalf("painted value = %g, want the clamped 1", got)
	}
	if got := a.values[key]; got.position <= 1 || got.velocity == 0 {
		t.Fatalf("clamping erased the stored momentum: %+v", got)
	}
	// A reverse crossing below zero clamps the same way.
	a.values[key] = animValue{from: 0.1, to: 0, spring: spring,
		position: 0.1, velocity: -200, advanced: clock.now()}
	clock.add(springStep)
	if got := a.Value("h", animHover); got != 0 {
		t.Fatalf("painted value = %g, want the clamped 0", got)
	}
	if got := a.values[key].position; got >= 0 {
		t.Fatalf("clamping erased the reverse momentum: %g", got)
	}
}

func TestPanelPaintStaysInsideTheSlideAndOpacityFrames(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	spring := a.springFor(animVisible, true)
	if spring == nil {
		t.Fatal("visibility is not a spring")
	}
	key := animKey{node: "v", channel: animVisible}
	for _, pos := range []float64{-0.5, 0, 0.125, 0.25, 0.5, 0.75, 1, 1.5} {
		a.values[key] = animValue{to: 1, spring: spring, position: pos, velocity: 0, advanced: clock.now()}
		if slide := a.PanelSlide("v"); slide < 0 || slide > panelSlidePx {
			t.Fatalf("position %g slid %d px", pos, slide)
		}
		if opacity := a.PanelOpacity("v"); opacity < 0 || opacity > 1 {
			t.Fatalf("position %g painted opacity %g", pos, opacity)
		}
	}
	// Every integer in the eight-pixel range is produced, rounded rather than
	// truncated as the slide is retraced in either direction.
	seen := map[int]bool{}
	for i := 0; i <= panelSlidePx; i++ {
		pos := 1 - float64(i)/float64(panelSlidePx)
		a.values[key] = animValue{to: 1, spring: spring, position: pos, velocity: 0, advanced: clock.now()}
		seen[a.PanelSlide("v")] = true
	}
	for i := 0; i <= panelSlidePx; i++ {
		if !seen[i] {
			t.Fatalf("slide value %d was never produced", i)
		}
	}
}

func TestSpringExcludedChannelsKeepTheirCurves(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	for _, tc := range []struct {
		name    string
		channel animChannel
	}{
		{"press", animPress},
		{"theme", animTheme},
		{"ripple", animRipple},
	} {
		a.Target("k", tc.channel, 1)
		key := animKey{node: "k", channel: tc.channel}
		if got := a.values[key].spring; got != nil {
			t.Fatalf("%s became a spring", tc.name)
		}
		if got := a.values[key].dur; got != a.duration(tc.channel, true) {
			t.Fatalf("%s lost its duration curve: %v", tc.name, got)
		}
		a.Forget("k")
	}
	// Loops and cycles keep their phase and never become springs.
	a.TargetLoop("g", animGradient, 0, 1, 2*time.Second, ui.GradientLoop)
	a.TargetSweep("s", animSweep, 2*time.Second)
	a.TargetCycle("c", 2*time.Second, 4, 7)
	start := a.values[animKey{node: "c", channel: animSprite}].start
	clock.add(500 * time.Millisecond)
	a.TargetCycle("c", 2*time.Second, 4, 7)
	for _, key := range []animKey{
		{node: "g", channel: animGradient},
		{node: "s", channel: animSweep},
		{node: "c", channel: animSprite},
	} {
		if got := a.values[key]; got.spring != nil || got.loop == ui.GradientNone {
			t.Fatalf("%v lost its loop: %+v", key, got)
		}
	}
	if got := a.values[animKey{node: "c", channel: animSprite}].start; got != start {
		t.Fatalf("unchanged cycle restarted its phase: %v, want %v", got, start)
	}
}

func TestSpringBadgePopRemainsEased(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	badge := badgeKeyPrefix + "app"
	a.Target(badge, animProgress, 1)
	key := animKey{node: badge, channel: animProgress}
	if a.values[key].spring != nil {
		t.Fatal("badge pop became a spring")
	}
	// The one-shot phase still peaks on the eased midpoint.
	clock.add(a.duration(animProgress, true) / 2)
	if got, want := a.Value(badge, animProgress), ui.EaseOutCubic(0.5); got != want {
		t.Fatalf("badge midpoint = %g, want eased %g", got, want)
	}
	// A normal meter keeps the spring.
	a.Target("meter", animProgress, 0.5)
	if a.values[animKey{node: "meter", channel: animProgress}].spring == nil {
		t.Fatal("meter progress lost its spring")
	}
}

func TestSpringAndSpriteRestCoexist(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.TargetCycle("s", 200*time.Millisecond, 4, 9)
	a.Target("h", animHover, 1)
	// A moving spring keeps the ordinary tick: the loop cannot sleep to a
	// sprite pose while interaction work is in flight.
	if rest, ok := a.SpriteRest(); ok || rest != 0 {
		t.Fatalf("sprite rest = %v/%v while a spring moves", rest, ok)
	}
	key := animKey{node: "h", channel: animHover}
	for i := 0; i < 4000; i++ {
		clock.add(springStep)
		if a.Value("h", animHover) == 1 && a.values[key].velocity == 0 {
			break
		}
	}
	// Once the spring rests, the next pose boundary is the sleep.
	rest, ok := a.SpriteRest()
	if !ok || rest <= 0 || rest > 200*time.Millisecond/4+time.Millisecond {
		t.Fatalf("sprite rest after settle = %v/%v", rest, ok)
	}
	// Input wakes the loop immediately.
	a.Target("h", animHover, 0)
	if len(a.wake) == 0 {
		t.Fatal("a new target did not wake the loop")
	}
}

func TestSpringConfigureReducedMotionSettlesSpatialState(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.Target("h", animHover, 1)
	a.Target("m", animProgress, 0.8)
	a.Target("p", animVisible, 1)
	clock.add(40 * time.Millisecond)
	oldOpacity := a.PanelOpacity("p")
	a.Configure(render.MotionSet{Durations: theme.BaseMotion}, true)
	if a.Value("h", animHover) != 1 || a.Value("m", animProgress) != 0.8 {
		t.Fatal("reduced motion did not snap target-seeking content")
	}
	if a.PanelSlide("p") != 0 || a.PanelOpacity("p") != oldOpacity {
		t.Fatal("reduced motion moved the panel or jumped its opacity")
	}
	clock.add(reducedPanelCap)
	if a.PanelOpacity("p") != 1 || !a.Settled() {
		t.Fatal("capped reduced fade did not finish")
	}
}

func TestSpringConfigureReducedMotionClampsVisibilityOvershoot(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	key := animKey{node: "p", channel: animVisible}
	a.values[key] = animValue{
		from: 0.95, to: 0, spring: a.springFor(animVisible, false),
		position: 0.95, velocity: 100, advanced: clock.now(),
	}
	clock.add(springStep)
	if got := a.Value("p", animVisible); got != 1 || a.values[key].position <= 1 {
		t.Fatalf("spring did not preserve bounded paint over overshoot: value=%g state=%+v", got, a.values[key])
	}

	a.Configure(render.MotionSet{Durations: theme.BaseMotion, Reduced: true}, true)
	if got := a.PanelOpacity("p"); got != 1 {
		t.Fatalf("reduced-motion fade started from unpainted opacity %g", got)
	}
	if a.values[key].spring != nil || a.Settled() {
		t.Fatal("visibility did not retain the capped reduced-motion fade")
	}
}

func TestSpringTargetAfterReducedMotionStartsFromCurrentTime(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.Target("h", animHover, 1)
	clock.add(20 * time.Millisecond)
	_ = a.Value("h", animHover)

	a.Configure(render.MotionSet{Durations: theme.BaseMotion, Reduced: true}, true)
	a.Configure(render.MotionSet{Durations: theme.BaseMotion}, false)
	a.Target("h", animHover, 0)

	if got := a.Value("h", animHover); got != 1 {
		t.Fatalf("new spring advanced from a stale timestamp: got %g, want its start position 1", got)
	}
	clock.add(springStep)
	if got := a.Value("h", animHover); got >= 1 || got <= 0 {
		t.Fatalf("new spring did not begin moving after one step: %g", got)
	}
}

func TestSpringConfigureSpeedPreservesState(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.Target("h", animHover, 1)
	clock.add(40 * time.Millisecond)
	_ = a.Value("h", animHover)
	key := animKey{node: "h", channel: animHover}
	before := a.values[key]
	fast := render.MotionSet{Durations: theme.BaseMotion.AtSpeed(400), Spatial: theme.CurveOutQuart}
	a.Configure(fast, false)
	after := a.values[key]
	if after.position != before.position || after.velocity != before.velocity || after.to != before.to {
		t.Fatalf("configure restarted the spring: %+v -> %+v", before, after)
	}
	token := theme.BaseMotion.AtSpeed(400).Short.Seconds()
	omega := 12 * 1.1 / token
	if math.Abs(after.spring.Stiffness-omega*omega) > 1e-9 || math.Abs(after.spring.Damping-2*omega) > 1e-9 {
		t.Fatalf("coefficients not replaced: %+v", after.spring)
	}
	// The same settings again change nothing.
	again := a.values[key]
	a.Configure(fast, false)
	if got := a.values[key]; got.position != again.position || got.velocity != again.velocity || got.to != again.to {
		t.Fatal("re-configuring moved a value")
	}
}

func TestSpringConfigureReducedMotionParksLoops(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.TargetLoop("g", animGradient, -1, 1, 2*time.Second, ui.GradientLoop)
	a.TargetSweep("s", animSweep, 2*time.Second)
	a.TargetSweep("e", animEffect, 2*time.Second)
	a.TargetCycle("c", 2*time.Second, 4, 7)
	a.Target("r", animRipple, 1)
	a.Target("p", animVisible, 1)
	clock.add(50 * time.Millisecond)
	a.Configure(render.MotionSet{Durations: theme.BaseMotion}, true)
	if got := a.Value("g", animGradient); got != 0 {
		t.Fatalf("gradient parked at %g, want its midpoint 0", got)
	}
	if got := a.Value("s", animSweep); got != 0 {
		t.Fatalf("sweep parked at %g, want 0", got)
	}
	if got := a.Value("e", animEffect); got != 0 {
		t.Fatalf("effect parked at %g, want 0", got)
	}
	if got := a.Value("c", animSprite); got != 0 {
		t.Fatalf("sprite parked at %g, want 0", got)
	}
	if got := a.Value("r", animRipple); got != 1 {
		t.Fatalf("ripple parked at %g, want its target", got)
	}
	clock.add(reducedPanelCap)
	if !a.Settled() {
		t.Fatal("parked surfaces still requested frames")
	}
	// Disabling reduced motion changes nothing settled, and motion resumes on
	// the next genuine target change.
	a.Configure(render.MotionSet{Durations: theme.BaseMotion}, false)
	if !a.Settled() {
		t.Fatal("disabling reduced motion resurrected settled work")
	}
	a.Target("h", animHover, 1)
	if a.Settled() {
		t.Fatal("motion did not resume on a genuine target change")
	}
}

// The spring predicate drives the real frame loop: it publishes the settling
// frame at the exact endpoint and then retires, and a stop before the first
// wake publishes nothing. No scheduler change was needed for springs.
func TestSpringSchedulerPublishesFinalFrameAndStops(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.Target("h", animHover, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	published, exact := 0, false
	go func() {
		defer close(done)
		animateSurface(stop, func() bool { return a.Settled() }, func() {
			published++
			if a.Value("h", animHover) == 1 {
				exact = true
			}
			// Advance the animator clock from the loop itself, so the spring
			// settles in bounded time under the real ticker.
			clock.add(50 * time.Millisecond)
		}, func() time.Duration { return time.Millisecond })
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("spring frame loop did not retire")
	}
	if !exact {
		t.Fatal("the settling frame did not publish the exact endpoint")
	}
	if published < 2 {
		t.Fatalf("published %d frames, want at least the settling frame after the first", published)
	}
	count := published

	// The loop is done: nothing publishes after it.
	time.Sleep(30 * time.Millisecond)
	if published != count {
		t.Fatal("a settled spring kept requesting frames")
	}

	// A stop before the first wake publishes nothing.
	stopped := make(chan struct{})
	early := make(chan struct{})
	close(stopped)
	go func() {
		animateSurface(stopped, func() bool { return false }, func() { t.Error("published after stop") },
			func() time.Duration { return time.Millisecond })
		close(early)
	}()
	select {
	case <-early:
	case <-time.After(2 * time.Second):
		t.Fatal("animateSurface ignored an immediate stop")
	}
}
