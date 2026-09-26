package shell

import (
	"testing"
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

// A resting loop publishes once per pose, not once per tick, and a new
// target cuts the rest short.
func TestRestingLoopPublishesPerPoseAndWakesForOtherMotion(t *testing.T) {
	t.Parallel()
	stop := make(chan struct{})
	wake := make(chan struct{}, 1)
	published := make(chan time.Time, 64)
	done := make(chan struct{})
	go func() {
		animateSurfaceResting(stop, wake, func() bool { return false },
			func() { published <- time.Now() }, func() time.Duration { return 0 },
			func() (time.Duration, bool) { return 150 * time.Millisecond, true })
		close(done)
	}()
	time.Sleep(400 * time.Millisecond)
	if n := len(published); n < 2 || n > 5 {
		t.Fatalf("published %d times in 400ms resting 150ms per pose", n)
	}
	for len(published) > 0 {
		<-published
	}
	sent := time.Now()
	wake <- struct{}{}
	select {
	case at := <-published:
		if at.Sub(sent) > 50*time.Millisecond {
			t.Fatalf("wake took %v to publish", at.Sub(sent))
		}
	case <-time.After(140 * time.Millisecond):
		t.Fatal("wake did not cut the rest short")
	}
	close(stop)
	<-done
}
