package wayland

import (
	"testing"

	"github.com/Nomadcxx/sysc-wayland/client"
)

// newTouchOwner gives each bar of newFocusOwner a surface a finger can land on.
func newTouchOwner() (*owner, *OutputHost, *OutputHost, *[]Event) {
	o, a, b, seen := newFocusOwner()
	a.bar.surface, b.bar.surface = &client.Surface{}, &client.Surface{}
	return o, a, b, seen
}

func eventKinds(events []Event) []EventKind {
	kinds := make([]EventKind, len(events))
	for i, e := range events {
		kinds[i] = e.Kind
	}
	return kinds
}

func sameKinds(got []Event, want ...EventKind) bool {
	kinds := eventKinds(got)
	if len(kinds) != len(want) {
		return false
	}
	for i := range kinds {
		if kinds[i] != want[i] {
			return false
		}
	}
	return true
}

func TestTouchTapPressesAndReleasesWhereTheFingerIs(t *testing.T) {
	t.Parallel()
	o, a, _, seen := newTouchOwner()

	o.touchDown(a.bar.surface, 3, 10, 5, 41)
	o.touchMotion(3, 12, 6)
	o.touchUp(3, 42)

	if !sameKinds(*seen, EventPointerEnter, EventPointerPress, EventPointerMotion,
		EventPointerRelease, EventPointerLeave) {
		t.Fatalf("events = %v, want enter, press, motion, release, leave", eventKinds(*seen))
	}
	press, release := (*seen)[1], (*seen)[3]
	if press.Button != touchButton || press.Serial != 41 || press.X != 10 || press.Y != 5 {
		t.Fatalf("press = %+v, want BTN_LEFT with serial 41 at 10,5", press)
	}
	if release.Button != touchButton || release.Serial != 42 || release.X != 12 || release.Y != 6 {
		t.Fatalf("release = %+v, want BTN_LEFT with serial 42 at the last position 12,6", release)
	}
	if o.touch.unit != nil || o.focus.unit != nil {
		t.Fatalf("after the tap touch = %+v focus = %+v, want neither", o.touch, o.focus)
	}
}

func TestTouchFollowsOnlyTheFirstFinger(t *testing.T) {
	t.Parallel()
	o, a, b, seen := newTouchOwner()

	o.touchDown(a.bar.surface, 1, 1, 1, 1)
	o.touchDown(b.bar.surface, 2, 2, 2, 2)
	o.touchMotion(2, 9, 9)
	o.touchUp(2, 3)

	if !sameKinds(*seen, EventPointerEnter, EventPointerPress) {
		t.Fatalf("events = %v, want only the first finger's enter and press", eventKinds(*seen))
	}
	if o.touch.unit != a.bar {
		t.Fatal("a second finger took the touch")
	}
}

func TestTouchCancelEndsWithoutARelease(t *testing.T) {
	t.Parallel()
	o, a, _, seen := newTouchOwner()

	o.touchDown(a.bar.surface, 1, 4, 4, 1)
	o.endTouch()
	o.touchUp(1, 2)

	if !sameKinds(*seen, EventPointerEnter, EventPointerPress, EventPointerLeave) {
		t.Fatalf("events = %v, want enter, press, leave and no release", eventKinds(*seen))
	}
}

func TestTouchGivesTheMouseItsHoverBack(t *testing.T) {
	t.Parallel()
	o, a, _, seen := newTouchOwner()

	o.enterSurface(a, 30, 7, 9)
	o.touchDown(a.bar.surface, 1, 4, 4, 10)
	o.touchUp(1, 11)

	last := (*seen)[len(*seen)-1]
	if last.Kind != EventPointerMotion || last.X != 30 || last.Y != 7 {
		t.Fatalf("last event = %+v, want motion back to the mouse at 30,7", last)
	}
	if o.focus.unit != a.bar || o.focus.x != 30 || o.focus.serial != 9 {
		t.Fatalf("pointer focus = %+v, want it unchanged on bar A", o.focus)
	}
}

func TestTouchOnAClosingUnitEnds(t *testing.T) {
	t.Parallel()
	o, a, _, seen := newTouchOwner()

	o.touchDown(a.bar.surface, 1, 4, 4, 1)
	o.touchUnitGone(a.bar)
	o.touchUp(1, 2)

	if o.touch.unit != nil {
		t.Fatal("the touch outlived its unit")
	}
	if !sameKinds(*seen, EventPointerEnter, EventPointerPress, EventPointerLeave) {
		t.Fatalf("events = %v, want enter, press, leave and no release", eventKinds(*seen))
	}
}

func TestTouchOnAnUnknownSurfaceIsIgnored(t *testing.T) {
	t.Parallel()
	o, _, _, seen := newTouchOwner()

	o.touchDown(&client.Surface{}, 1, 4, 4, 1)
	o.touchUp(1, 2)

	if len(*seen) != 0 || o.touch.unit != nil {
		t.Fatalf("a finger on a foreign surface delivered %d events", len(*seen))
	}
}
