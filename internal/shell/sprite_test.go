package shell

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func spriteTree(frames []string, cycle time.Duration) *ui.Node {
	return &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
		Kind: ui.KindIcon, Key: "cat", Icon: frames[0], IconSize: 24, Frames: frames, Cycle: cycle,
	}}}
}

var gallop = []string{"cat-run-0", "cat-run-1", "cat-run-2", "cat-run-3"}

// resolved runs the walk on a fresh copy, as a render does, and returns the
// pose it landed on.
func resolved(a *animator, frames []string, cycle time.Duration) string {
	root := spriteTree(frames, cycle)
	resolveSpriteMotion(a, root)
	return root.Children[0].Icon
}

func TestSpriteStepsThroughItsFramesOnTheSurfaceClock(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	// 400 ms over four poses: a pose every 100 ms.
	var got []string
	for i := 0; i < 5; i++ {
		got = append(got, resolved(a, gallop, 400*time.Millisecond))
		clock.add(100 * time.Millisecond)
	}
	want := []string{"cat-run-0", "cat-run-1", "cat-run-2", "cat-run-3", "cat-run-0"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("poses = %v, want %v", got, want)
		}
	}
}

// A plugin retargets the speed on every sample; the pose on screen must stay
// put and only the rate change, or the gait hitches every two seconds.
func TestSpriteSpeedChangeKeepsThePhase(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	resolved(a, gallop, 400*time.Millisecond)
	clock.add(250 * time.Millisecond) // phase 0.625: pose 2
	if got := resolved(a, gallop, 400*time.Millisecond); got != "cat-run-2" {
		t.Fatalf("before retarget = %s", got)
	}
	if got := resolved(a, gallop, 800*time.Millisecond); got != "cat-run-2" {
		t.Fatalf("retargeting the speed moved the pose to %s", got)
	}
	// At half speed a pose lasts 200 ms, so 150 ms on the phase is 0.8125:
	// pose 3.
	clock.add(150 * time.Millisecond)
	if got := resolved(a, gallop, 800*time.Millisecond); got != "cat-run-3" {
		t.Fatalf("after 150ms at half speed = %s, want cat-run-3", got)
	}
}

// New poses are a new act; it starts from its first pose.
func TestSpriteNewPosesRestartTheCycle(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	resolved(a, gallop, 400*time.Millisecond)
	clock.add(250 * time.Millisecond)
	sit := []string{"cat-sit-0", "cat-sit-1", "cat-sit-2"}
	if got := resolved(a, sit, 3*time.Second); got != "cat-sit-0" {
		t.Fatalf("new act started at %s", got)
	}
}

func TestSpriteHoldsItsRestingPoseUnderReducedMotion(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(true)
	root := spriteTree([]string{"cat-sit-0", "cat-sit-1"}, 400*time.Millisecond)
	root.Children[0].Icon = "cat-sit-0"
	clock.add(300 * time.Millisecond)
	resolveSpriteMotion(a, root)
	if got := root.Children[0].Icon; got != "cat-sit-0" {
		t.Fatalf("reduced motion painted %s", got)
	}
	if !a.Settled() {
		t.Fatal("reduced motion left a sprite in flight")
	}
}

func TestSpriteRetiresWhenItsNodeLeaves(t *testing.T) {
	t.Parallel()
	a, _ := newTestAnimator(false)
	resolved(a, gallop, 400*time.Millisecond)
	if a.Settled() {
		t.Fatal("a running sprite reported settled")
	}
	resolveSpriteMotion(a, &ui.Node{Kind: ui.KindRow})
	if !a.Settled() {
		t.Fatal("a removed sprite kept the surface animating")
	}
}

func TestSpriteRestSleepsToTheNextPose(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	if _, ok := a.SpriteRest(); ok {
		t.Fatal("rest reported with nothing in flight")
	}
	resolved(a, gallop, 400*time.Millisecond)
	clock.add(30 * time.Millisecond)
	if d, ok := a.SpriteRest(); !ok || d != 70*time.Millisecond {
		t.Fatalf("rest = %v %v, want 70ms to the next pose", d, ok)
	}
	// Anything else in flight keeps the ordinary tick.
	a.Target("button", animHover, 1)
	if _, ok := a.SpriteRest(); ok {
		t.Fatal("rest reported while a hover was moving")
	}
}

func TestSpriteRestWithDecorativeGradient(t *testing.T) {
	a, clock := newTestAnimator(false)
	resolved(a, gallop, 400*time.Millisecond)
	a.TargetLoop("wordmark", animGradient, 0, 1, time.Second, ui.GradientLoop)
	clock.add(30 * time.Millisecond)
	if d, ok := a.SpriteRest(); !ok || d != effectFrameCap {
		t.Fatalf("gradient and sprite rest = %v %v, want effect interval", d, ok)
	}
	clock.add(40 * time.Millisecond)
	if d, ok := a.SpriteRest(); !ok || d != 30*time.Millisecond {
		t.Fatalf("rest = %v %v, want next sprite pose in 30ms", d, ok)
	}
	a.Target("button", animPress, 1)
	if _, ok := a.SpriteRest(); ok {
		t.Fatal("press must interrupt decorative rest")
	}
}

// A resting loop publishes once per pose, not once per tick, and a new
// target cuts the rest short.
//
// The loop is driven by a clock, so the test gives it one: a synctest bubble
// has a clock of its own, and inside it the only way time passes is that
// every goroutine is waiting. The deadlines below are therefore exact, and
// the assertions cannot be moved by a loaded machine -- which is what made
// this check fail on the laptop bench, where the wake needed 54ms of a 50ms
// budget.
//
// The bubble only records what it saw: a failure inside it ends the bubble's
// root goroutine, and the loop it started is still waiting, which the
// runtime reports as a deadlock and turns one failed check into a crashed
// binary. The assertions are made after the bubble has drained.
func TestRestingLoopPublishesPerPoseAndWakesForOtherMotion(t *testing.T) {
	var (
		poses     []time.Duration
		published int
		woke      bool
		wakeDelay time.Duration
	)
	synctest.Test(t, func(t *testing.T) {
		stop := make(chan struct{})
		wake := make(chan struct{}, 1)
		pubs := make(chan time.Time, 64)
		done := make(chan struct{})
		go func() {
			animateSurfaceResting(stop, wake, func() bool { return false },
				func() { pubs <- time.Now() }, func() time.Duration { return 0 },
				func() (time.Duration, bool) { return 150 * time.Millisecond, true })
			close(done)
		}()
		// The first tick publishes, and after that a publish lands on each
		// pose boundary. 400ms holds the tick and two pose boundaries.
		time.Sleep(400 * time.Millisecond)
		at := make([]time.Time, 0, 8)
		for len(pubs) > 0 {
			at = append(at, <-pubs)
		}
		published = len(at)
		for i := 1; i < len(at); i++ {
			poses = append(poses, at[i].Sub(at[i-1]))
		}
		sent := time.Now()
		wake <- struct{}{}
		select {
		case p := <-pubs:
			woke = true
			wakeDelay = p.Sub(sent)
		case <-time.After(140 * time.Millisecond):
		}
		close(stop)
		<-done
	})
	// One publish per pose, not one per tick: a per-tick loop would have
	// published about fifty times in 400ms.
	if published != 3 {
		t.Fatalf("published %d times in 400ms resting 150ms per pose, want 3", published)
	}
	for i, gap := range poses {
		if gap < 150*time.Millisecond {
			t.Fatalf("publish %d came %v after the previous one, want a pose apart", i+1, gap)
		}
	}
	if !woke {
		t.Fatal("wake did not cut the rest short")
	}
	// The wake cuts the rest short, so the publish lands in the same instant
	// as the wake rather than at the next pose.
	if wakeDelay != 0 {
		t.Fatalf("wake took %v to publish", wakeDelay)
	}
}
