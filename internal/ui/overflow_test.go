package ui

import "testing"

// placeSection grants whole items or none. Each case below states the budget in
// units of the ten-pixel rune `fixed` measures, so "aaa" is 30 wide.

func TestASectionThatFitsDropsNothing(t *testing.T) {
	t.Parallel()
	content := Rect{X: 0, Y: 0, W: 200, H: 40}
	a, b := text("aaa"), text("bbb") // 30 each, 6 spacing
	dropped, err := placeSection([]*Node{a, b}, 0, content, 200, 0, 6, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}
	if a.Bounds.W != 30 || b.Bounds.W != 30 {
		t.Fatalf("widths = %d/%d, want 30/30", a.Bounds.W, b.Bounds.W)
	}
}

func TestAnItemThatDoesNotFitIsDroppedWholeNotTruncated(t *testing.T) {
	t.Parallel()
	content := Rect{X: 0, Y: 0, W: 50, H: 40}
	a, b := text("aaa"), text("bbbbb") // 30 and 50, spacing 6: b needs 86 total
	dropped, err := placeSection([]*Node{a, b}, 0, content, 50, 0, 6, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
	if a.Bounds.W != 30 {
		t.Fatalf("first item width = %d, want its natural 30", a.Bounds.W)
	}
	if (b.Bounds != Rect{}) {
		t.Fatalf("dropped item bounds = %+v, want the zero Rect: a zero-width "+
			"but full-height box is what silent clipping looked like", b.Bounds)
	}
}

// D5 collapses in declaration order from the far end: the last-declared item
// goes first.
func TestCollapseTakesTheLastDeclaredItemFirst(t *testing.T) {
	t.Parallel()
	content := Rect{X: 0, Y: 0, W: 80, H: 40}
	a, b, c := text("aaa"), text("bbb"), text("ccc") // 30 each, spacing 6
	dropped, err := placeSection([]*Node{a, b, c}, 0, content, 70, 0, 6, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1: 30+6+30 fits in 70, the third does not", dropped)
	}
	if a.Bounds.W != 30 || b.Bounds.W != 30 {
		t.Fatalf("kept widths = %d/%d, want 30/30", a.Bounds.W, b.Bounds.W)
	}
	if (c.Bounds != Rect{}) {
		t.Fatalf("c bounds = %+v, want zero: the last-declared item goes first", c.Bounds)
	}
}

func TestASectionWithNoRoomDropsEverything(t *testing.T) {
	t.Parallel()
	content := Rect{X: 0, Y: 0, W: 100, H: 40}
	a, b := text("aaa"), text("bbb")
	dropped, err := placeSection([]*Node{a, b}, 0, content, 0, 0, 6, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 2 {
		t.Fatalf("dropped = %d, want 2", dropped)
	}
	if (a.Bounds != Rect{}) || (b.Bounds != Rect{}) {
		t.Fatalf("bounds = %+v/%+v, want both zero", a.Bounds, b.Bounds)
	}
}

func TestSpacingIsNotChargedForADroppedItem(t *testing.T) {
	t.Parallel()
	content := Rect{X: 0, Y: 0, W: 100, H: 40}
	a, b := text("aaa"), text("bbbbbbbbbb") // 30 and 100
	if _, err := placeSection([]*Node{a, b}, 0, content, 36, 0, 6, fixed); err != nil {
		t.Fatal(err)
	}
	if a.Bounds.W != 30 {
		t.Fatalf("kept width = %d, want 30: the dropped item must not consume "+
			"the spacing that precedes it", a.Bounds.W)
	}
}

// T2: the reserve is taken before selection, so the indicator's own space is
// never the thing that overflows.

func TestAReserveIsTakenBeforeSelection(t *testing.T) {
	t.Parallel()
	content := Rect{X: 0, Y: 0, W: 100, H: 40}
	a, b, c := text("aaa"), text("bbb"), text("ccc") // 30 each, spacing 6

	// Without a reserve two of the three fit in 70: 30 + 6 + 30 = 66.
	if dropped, err := placeSection([]*Node{a, b, c}, 0, content, 70, 0, 6, fixed); err != nil {
		t.Fatal(err)
	} else if dropped != 1 {
		t.Fatalf("dropped without a reserve = %d, want 1", dropped)
	}

	// Reserving 20 leaves 50, which fits only the first.
	dropped, err := placeSection([]*Node{a, b, c}, 0, content, 70, 20, 6, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 2 {
		t.Fatalf("dropped with a reserve = %d, want 2: the indicator's extent "+
			"comes out before anything is selected", dropped)
	}
	if a.Bounds.W != 30 {
		t.Fatalf("kept width = %d, want 30", a.Bounds.W)
	}
	if (b.Bounds != Rect{}) {
		t.Fatalf("b bounds = %+v, want zero", b.Bounds)
	}
}

func TestAReserveIsNotChargedWhenEverythingFits(t *testing.T) {
	t.Parallel()
	content := Rect{X: 0, Y: 0, W: 200, H: 40}
	a, b := text("aaa"), text("bbb")
	dropped, err := placeSection([]*Node{a, b}, 0, content, 66, 20, 6, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 0 {
		t.Fatalf("dropped = %d, want 0: a section that fits needs no indicator, "+
			"so its reserve must not be charged", dropped)
	}
	if a.Bounds.W != 30 || b.Bounds.W != 30 {
		t.Fatalf("widths = %d/%d, want 30/30", a.Bounds.W, b.Bounds.W)
	}
}

func TestAZeroReserveChangesNothing(t *testing.T) {
	t.Parallel()
	content := Rect{X: 0, Y: 0, W: 100, H: 40}
	a, b := text("aaa"), text("bbb")
	dropped, err := placeSection([]*Node{a, b}, 0, content, 66, 0, 6, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 0 || a.Bounds.W != 30 || b.Bounds.W != 30 {
		t.Fatalf("dropped = %d, widths = %d/%d; want 0 and 30/30",
			dropped, a.Bounds.W, b.Bounds.W)
	}
}
