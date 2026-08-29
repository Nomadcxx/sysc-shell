package wayland

import "testing"

func newFocusOwner() (*owner, *OutputHost, *OutputHost, *[]Event) {
	s := newHostSet()
	a, _ := s.add(1, nil)
	b, _ := s.add(2, nil)
	seen := new([]Event)
	for _, h := range []*OutputHost{a, b} {
		h.alive = true
		h.app = HostCallbacks{Handle: func(e Event) bool {
			*seen = append(*seen, e)
			return false
		}}
	}
	a.connector, b.connector = "DP-1", "DP-3"
	return &owner{hosts: s}, a, b, seen
}

func TestFocusFollowsEnterAndLeaveBetweenSurfaces(t *testing.T) {
	t.Parallel()
	o, a, b, seen := newFocusOwner()

	o.enterSurface(a, 10, 5, 77)
	if o.focus.host != a {
		t.Fatal("enter did not focus host A")
	}
	o.leaveSurface(a)
	if o.focus.host != nil {
		t.Fatal("leave did not clear focus")
	}
	o.enterSurface(b, 3, 4, 78)
	if o.focus.host != b || o.focus.x != 3 || o.focus.y != 4 {
		t.Fatalf("focus = %+v, want host B at 3,4", o.focus)
	}
	if o.focus.serial != 78 {
		t.Fatalf("serial = %d, want 78", o.focus.serial)
	}
	if len(*seen) != 3 {
		t.Fatalf("delivered %d events, want enter, leave, enter", len(*seen))
	}
}

func TestOutOfOrderLeaveForAnUnfocusedSurfaceIsIgnored(t *testing.T) {
	t.Parallel()
	o, a, b, _ := newFocusOwner()

	o.enterSurface(b, 1, 1, 1)
	o.leaveSurface(a) // a stale leave for the surface focus already left
	if o.focus.host != b {
		t.Fatal("a stale leave cleared focus for the wrong surface")
	}
}

func TestClearingFocusDeliversALeave(t *testing.T) {
	t.Parallel()
	o, a, _, seen := newFocusOwner()

	o.enterSurface(a, 2, 2, 5)
	o.clearFocus()

	if o.focus.host != nil {
		t.Fatal("clearFocus did not clear the host")
	}
	last := (*seen)[len(*seen)-1]
	if last.Kind != EventPointerLeave {
		t.Fatalf("last event kind = %d, want EventPointerLeave", last.Kind)
	}
}

func TestClearingIdleFocusDeliversNothing(t *testing.T) {
	t.Parallel()
	o, _, _, seen := newFocusOwner()

	o.clearFocus()
	if len(*seen) != 0 {
		t.Fatalf("clearFocus with no focus delivered %d events", len(*seen))
	}
}

func TestFocusCoordinatesArePreservedForButtons(t *testing.T) {
	t.Parallel()
	o, a, _, seen := newFocusOwner()

	// Enter carries coordinates; a press with no intervening motion must reuse
	// them rather than acting at the origin.
	o.enterSurface(a, 12.75, 6.25, 1)
	o.deliver(a, Event{Kind: EventPointerPress, X: o.focus.x, Y: o.focus.y, Button: 272})

	press := (*seen)[len(*seen)-1]
	if press.X != 12.75 || press.Y != 6.25 {
		t.Fatalf("press at %v,%v, want the enter coordinates 12.75,6.25", press.X, press.Y)
	}
}
