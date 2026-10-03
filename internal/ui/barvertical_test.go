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

func TestVerticalBarUsesIntrinsicColumnHeight(t *testing.T) {
	t.Parallel()
	first := text("hi")
	second := text("there")
	column := &Node{Kind: KindColumn, Gap: 3, Children: []*Node{first, second}}
	pill := &Node{Kind: KindCapsule, Padding: 4, Children: []*Node{column}}
	content := Rect{X: 7, Y: 10, W: 80, H: 300}
	if _, err := ArrangeBar(content, Vertical, nil, []*Node{pill}, nil, 6, fixed); err != nil {
		t.Fatal(err)
	}
	if want := (Rect{X: 18, Y: 134, W: 58, H: 51}); pill.Bounds != want {
		t.Fatalf("pill = %+v, want intrinsic two-line box %+v", pill.Bounds, want)
	}
	if first.Bounds.H != 20 || second.Bounds.H != 20 || second.Bounds.Y != first.Bounds.Y+23 {
		t.Fatalf("upright column rows = %+v/%+v", first.Bounds, second.Bounds)
	}
}

func TestVerticalBarAnchorsColumnPill(t *testing.T) {
	t.Parallel()
	mark := &Node{Kind: KindWordmark, ImageW: 20, ImageH: 11}
	column := &Node{Kind: KindColumn, Gap: 4, Children: []*Node{
		mark, {Kind: KindSeparator}, text("12:00"), text("Thu"),
	}}
	pill := &Node{Kind: KindCapsule, Padding: 4, Children: []*Node{column}}
	before := text("a")
	after := &Node{Kind: KindImage, ImageW: 10, ImageH: 40}
	content := Rect{W: 80, H: 500}
	if _, err := ArrangeBar(content, Vertical, nil, []*Node{before, pill, after}, nil, 6, fixed); err != nil {
		t.Fatal(err)
	}
	if pill.Bounds.Y+pill.Bounds.H/2 != 250 {
		t.Fatalf("centre pill = %+v; its outer box must be pinned", pill.Bounds)
	}
	if before.Bounds.Y+before.Bounds.H > pill.Bounds.Y || after.Bounds.Y < pill.Bounds.Y+pill.Bounds.H {
		t.Fatalf("flanks overlap anchored pill: %+v/%+v/%+v", before.Bounds, pill.Bounds, after.Bounds)
	}
	if mark.Bounds.W == 0 || mark.Bounds.H == 0 {
		t.Fatalf("wordmark was not laid out inside the pill: %+v", mark.Bounds)
	}
}

func TestVerticalBarDropsWholeItemsAndClearsDescendants(t *testing.T) {
	t.Parallel()
	label := &Node{Kind: KindText, Text: "long label", MaxWidth: 50, Action: "label"}
	leaf := &Node{Kind: KindText, Text: "open", Action: "open"}
	pill := &Node{Kind: KindCapsule, Padding: 4, Children: []*Node{{Kind: KindColumn, Children: []*Node{leaf}}}}
	center := text("mid")
	if _, err := ArrangeBar(Rect{W: 80, H: 300}, Vertical, []*Node{label, pill}, []*Node{center}, nil, 6, fixed); err != nil {
		t.Fatal(err)
	}
	oldX, oldY := leaf.Bounds.X+1, leaf.Bounds.Y+1
	if pill.Bounds == (Rect{}) || leaf.Bounds == (Rect{}) {
		t.Fatal("first layout never placed the nested hit target")
	}
	over, err := ArrangeBar(Rect{W: 80, H: 50}, Vertical, []*Node{label, pill}, []*Node{center}, nil, 6, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if over.Left != 2 || label.Bounds != (Rect{}) || pill.Bounds != (Rect{}) || leaf.Bounds != (Rect{}) {
		t.Fatalf("dropped bounds = label %+v pill %+v leaf %+v; overflow %+v", label.Bounds, pill.Bounds, leaf.Bounds, over)
	}
	root := &Node{Kind: KindColumn, Bounds: Rect{W: 80, H: 50}, Children: []*Node{label, pill}}
	if action, ok := Hit(root, oldX, oldY); ok {
		t.Fatalf("stale hit = %q at %d,%d", action, oldX, oldY)
	}
}

func TestVerticalBarExactFitAndNegativeBounds(t *testing.T) {
	t.Parallel()
	right := text("r")
	over, err := ArrangeBar(Rect{W: 40, H: 52}, Vertical, nil, nil, []*Node{right}, 6, fixed)
	if err != nil || over.Any() || right.Bounds != (Rect{X: 15, Y: 32, W: 10, H: 20}) {
		t.Fatalf("exact fit = %+v, overflow %+v, err %v", right.Bounds, over, err)
	}
	if _, err := ArrangeBar(Rect{W: -1, H: 52}, Vertical, nil, nil, nil, 6, fixed); err == nil {
		t.Fatal("negative side-strip width accepted")
	}
}

func TestVerticalOversizedCentreClipsDescendants(t *testing.T) {
	t.Parallel()
	last := &Node{Kind: KindText, Text: "last", Action: "activate"}
	column := &Node{Kind: KindColumn, Gap: 4, Children: []*Node{text("one"), text("two"), text("three"), last}}
	pill := &Node{Kind: KindCapsule, Padding: 4, Children: []*Node{column}}
	if _, err := ArrangeBar(Rect{W: 80, H: 40}, Vertical, nil, []*Node{pill}, nil, 6, fixed); err != nil {
		t.Fatal(err)
	}
	if pill.Bounds.H != 40 || !pill.ClipBounds {
		t.Fatalf("oversized pill = %+v, clip %v", pill.Bounds, pill.ClipBounds)
	}
	if last.Bounds.Y < pill.Bounds.Y+pill.Bounds.H {
		t.Fatalf("last child %+v did not extend beyond clipped pill %+v", last.Bounds, pill.Bounds)
	}
	if action, ok := Hit(pill, last.Bounds.X+1, last.Bounds.Y+1); ok {
		t.Fatalf("child beyond clipped pill remained interactive: %q", action)
	}
	if _, err := ArrangeBar(Rect{W: 80, H: 200}, Vertical, nil, []*Node{pill}, nil, 6, fixed); err != nil {
		t.Fatal(err)
	}
	if pill.ClipBounds || last.Bounds == (Rect{}) {
		t.Fatalf("relayout kept clip %v or lost last child %+v", pill.ClipBounds, last.Bounds)
	}
}
