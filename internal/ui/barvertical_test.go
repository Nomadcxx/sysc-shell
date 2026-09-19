package ui

import (
	"testing"
)

// sized measures each rune as ten logical pixels wide and twenty tall, so a
// run of n runes is 10n wide and 20n tall. Unlike fixed, its height varies,
// which is what a vertical bar's main-axis extents need from a measure.
func sized(s string, _ TextAttrs) (int, int) {
	n := len([]rune(s))
	return n * 10, n * 20
}

// The lanes transpose fixed: the left lane lands at the top of a tall narrow
// band and the right lane at the lower end, identically on a left and a right
// edge, because the axis knows nothing about which edge the bar hangs on.
func TestVerticalBarTransposesTheLanesWithoutMirroring(t *testing.T) {
	t.Parallel()
	content := Rect{X: 0, Y: 0, W: 100, H: 600}
	left, center, right := text("a"), text("mid"), text("zzzzz")

	over, err := ArrangeBar(content, Vertical, []*Node{left}, []*Node{center}, []*Node{right}, 6, sized)
	if err != nil {
		t.Fatalf("ArrangeBar: %v", err)
	}
	if over.Any() {
		t.Fatalf("overflow = %+v, want everything placed", over)
	}
	if left.Bounds.Y != 0 {
		t.Fatalf("left lane Y = %d, want the top of the band", left.Bounds.Y)
	}
	if want := (600 - 60) / 2; center.Bounds.Y != want {
		t.Fatalf("centre Y = %d, want %d", center.Bounds.Y, want)
	}
	if right.Bounds.Y+right.Bounds.H != 600 {
		t.Fatalf("right lane ends at %d, want the lower edge of the band",
			right.Bounds.Y+right.Bounds.H)
	}
	// Every placed item is centred on the cross axis.
	for _, pair := range []struct {
		name  string
		got   Rect
		cross int
	}{
		{"left", left.Bounds, 10},
		{"centre", center.Bounds, 30},
		{"right", right.Bounds, 50},
	} {
		if want := (100 - pair.cross) / 2; pair.got.X != want || pair.got.W != pair.cross {
			t.Fatalf("%s bounds = %+v, want cross extent %d centred at X %d",
				pair.name, pair.got, pair.cross, want)
		}
	}
}

// The collision total order holds along the vertical main axis: the centre
// keeps its natural extent while it fits, the sides give way by dropping
// whole items, and only a centre taller than the band takes the band.
func TestVerticalTotalOrder(t *testing.T) {
	t.Parallel()
	t.Run("sides give way", func(t *testing.T) {
		content := Rect{X: 0, Y: 0, W: 40, H: 100}
		left, center, right := text("a"), text("mid"), text("zzzzz")

		over, err := ArrangeBar(content, Vertical, []*Node{left}, []*Node{center}, []*Node{right}, 6, sized)
		if err != nil {
			t.Fatalf("ArrangeBar: %v", err)
		}
		if center.Bounds.Y != 20 || center.Bounds.H != 60 {
			t.Fatalf("centre = %+v, want its natural extent at the band centre", center.Bounds)
		}
		if (left.Bounds != Rect{}) || (right.Bounds != Rect{}) {
			t.Fatalf("sides = %+v/%+v, want both dropped whole", left.Bounds, right.Bounds)
		}
		if over.Left != 1 || over.Right != 1 || over.Center != 0 {
			t.Fatalf("overflow = %+v, want one drop per side", over)
		}
	})

	t.Run("centre taller than the band takes the band", func(t *testing.T) {
		content := Rect{X: 0, Y: 0, W: 40, H: 50}
		left, center, right := text("a"), text("zzz"), text("zz")

		over, err := ArrangeBar(content, Vertical, []*Node{left}, []*Node{center}, []*Node{right}, 6, sized)
		if err != nil {
			t.Fatalf("ArrangeBar: %v", err)
		}
		if center.Bounds != (Rect{X: 5, Y: 0, W: 30, H: 50}) {
			t.Fatalf("centre = %+v, want the whole band, cross-centred", center.Bounds)
		}
		if (left.Bounds != Rect{}) || (right.Bounds != Rect{}) {
			t.Fatalf("sides = %+v/%+v, want both cleared", left.Bounds, right.Bounds)
		}
		if over.Left != 1 || over.Right != 1 || over.Center != 0 {
			t.Fatalf("overflow = %+v, want the sides dropped and no centre count", over)
		}
	})
}

// The anchored mark reads on a vertical bar as pinned to the vertical centre,
// its before-flank above and its after-flank below. A composition that cannot
// fit falls back to the plain variant, whose truncating escape takes the band.
func TestVerticalAnchoredMarkPinsToTheCentre(t *testing.T) {
	t.Parallel()
	t.Run("flanks transpose", func(t *testing.T) {
		content := Rect{X: 0, Y: 0, W: 40, H: 600}
		mark := &Node{Kind: KindWordmark, ImageW: 20, ImageH: 40}
		before, after := text("time"), text("med")
		center := []*Node{before, mark, after}

		over, err := ArrangeBar(content, Vertical, nil, center, nil, 6, fixed)
		if err != nil {
			t.Fatalf("ArrangeBar: %v", err)
		}
		if over.Any() {
			t.Fatalf("overflow = %+v, want everything placed", over)
		}
		if mark.Bounds.Y+mark.Bounds.H/2 != 300 {
			t.Fatalf("mark centre Y = %d, want the vertical centre 300; mark=%+v",
				mark.Bounds.Y+mark.Bounds.H/2, mark.Bounds)
		}
		if mark.Bounds.X != 10 || mark.Bounds.W != 20 {
			t.Fatalf("mark = %+v, want cross extent 20 centred in the strip", mark.Bounds)
		}
		if before.Bounds.Y+before.Bounds.H > mark.Bounds.Y {
			t.Fatalf("before-flank %+v overlaps the mark %+v", before.Bounds, mark.Bounds)
		}
		if want := 280 - 6 - 20; before.Bounds.Y != want {
			t.Fatalf("before-flank Y = %d, want %d", before.Bounds.Y, want)
		}
		if after.Bounds.Y < mark.Bounds.Y+mark.Bounds.H {
			t.Fatalf("after-flank %+v overlaps the mark %+v", after.Bounds, mark.Bounds)
		}
		if want := 280 + 40 + 6; after.Bounds.Y != want {
			t.Fatalf("after-flank Y = %d, want %d", after.Bounds.Y, want)
		}
	})

	t.Run("falls back when the mark does not fit", func(t *testing.T) {
		content := Rect{X: 0, Y: 0, W: 40, H: 600}
		mark := &Node{Kind: KindWordmark, ImageW: 20, ImageH: 700}

		over, err := ArrangeBar(content, Vertical, []*Node{text("a")}, []*Node{mark}, []*Node{text("z")}, 6, fixed)
		if err != nil {
			t.Fatalf("ArrangeBar: %v", err)
		}
		if mark.Bounds != (Rect{X: 10, Y: 0, W: 20, H: 600}) {
			t.Fatalf("mark = %+v, want the truncating escape: the whole band, cross-centred", mark.Bounds)
		}
		if over.Center != 0 || over.Left != 1 || over.Right != 1 {
			t.Fatalf("overflow = %+v, want the sides dropped and no centre count", over)
		}
	})
}

// A title wider than the strip is cross-clamped at measure, not dropped: the
// elastic shortfall mechanism stays a main-axis mechanism, and the painter
// ellipsizes inside the granted box.
func TestWideTitleIsCrossClampedNotDropped(t *testing.T) {
	t.Parallel()
	content := Rect{X: 0, Y: 0, W: 40, H: 300}
	title := text("aaaaaaaaaa") // 100 wide, 20 tall

	over, err := ArrangeBar(content, Vertical, nil, []*Node{title}, nil, 6, fixed)
	if err != nil {
		t.Fatalf("ArrangeBar: %v", err)
	}
	if over.Center != 0 {
		t.Fatalf("overflow = %+v, want the title kept", over)
	}
	if title.Bounds != (Rect{X: 0, Y: 140, W: 40, H: 20}) {
		t.Fatalf("title = %+v, want cross extent clamped to the strip, main extent kept", title.Bounds)
	}
}
