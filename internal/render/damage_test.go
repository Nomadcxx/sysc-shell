package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestDamageMovedCoversOldAndNewBounds(t *testing.T) {
	t.Parallel()
	// A node that moved must repair the region it left. Damaging only the new
	// bounds leaves the old pixels on screen, which reads as corrupted state
	// rather than as a paint fault.
	var d DamageSet
	d.Moved(ui.Rect{X: 0, Y: 0, W: 10, H: 10}, ui.Rect{X: 50, Y: 0, W: 10, H: 10})

	rects := d.Rects()
	if len(rects) == 0 {
		t.Fatal("no damage recorded")
	}
	covers := func(x, y int) bool {
		for _, r := range rects {
			if r.Contains(x, y) {
				return true
			}
		}
		return false
	}
	if !covers(5, 5) {
		t.Error("the vacated region is not damaged")
	}
	if !covers(55, 5) {
		t.Error("the new region is not damaged")
	}
}

func TestDamageEmptyMeansWholeBuffer(t *testing.T) {
	t.Parallel()
	// The fallback is the default. A surface that tracked nothing must get
	// today's behaviour, not zero damage.
	var d DamageSet
	if len(d.Rects()) != 0 {
		t.Error("a fresh set should report no rectangles")
	}
	if !d.Full() {
		t.Error("a set with no rectangles must report Full so the caller damages everything")
	}
}

func TestDamageMarkingFullDiscardsRectangles(t *testing.T) {
	t.Parallel()
	var d DamageSet
	d.Add(ui.Rect{X: 1, Y: 1, W: 2, H: 2})
	d.MarkFull()
	if !d.Full() {
		t.Error("MarkFull did not take")
	}
	if len(d.Rects()) != 0 {
		t.Error("a full set must not also report rectangles; the caller would damage twice")
	}
}

func TestDamageIgnoresEmptyRects(t *testing.T) {
	t.Parallel()
	var d DamageSet
	for _, r := range []ui.Rect{{}, {W: 0, H: 5}, {W: 5, H: 0}, {W: -1, H: -1}} {
		d.Add(r)
	}
	if len(d.Rects()) != 0 {
		t.Errorf("degenerate rects were recorded: %v", d.Rects())
	}
}

func TestDamageResetClears(t *testing.T) {
	t.Parallel()
	// Reset must clear both the rectangles and the full flag. The flag is only
	// observable through behaviour: Add drops every rectangle while a set is
	// full, so a set that accepts one again is a set that was genuinely reset.
	//
	// This deliberately does not assert !Full(). A reset set holds no
	// rectangles, and an empty set reports Full by design so that "tracked
	// nothing" still damages everything. Asserting otherwise would contradict
	// TestDamageEmptyMeansWholeBuffer, which pins that invariant: Reset
	// restores the zero value, and the zero value is Full.
	var d DamageSet
	d.Add(ui.Rect{W: 4, H: 4})
	d.MarkFull()
	d.Reset()
	if len(d.Rects()) != 0 {
		t.Errorf("Reset left rectangles behind: %v", d.Rects())
	}
	d.Add(ui.Rect{W: 2, H: 2})
	if len(d.Rects()) != 1 {
		t.Error("Reset did not clear the full flag; Add is still dropping rectangles")
	}
}
