package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// placed builds a section of nodes already carrying the bounds a layout pass
// would have written.
func placed(rects ...ui.Rect) []*ui.Node {
	out := make([]*ui.Node, 0, len(rects))
	for _, r := range rects {
		out = append(out, &ui.Node{Kind: ui.KindCapsule, Bounds: r})
	}
	return out
}

// A section that dropped items softens into the bar at the edge it was cut.
// Sections drop the tail of their declaration order, so that edge is always
// the trailing edge of the run that survived.
func TestOverflowFadeSitsAtTheTrailingEdgeOfWhatSurvived(t *testing.T) {
	right := placed(ui.Rect{X: 900, Y: 7, W: 120, H: 30}, ui.Rect{X: 1028, Y: 7, W: 92, H: 30})
	fades := overflowFades([][]*ui.Node{nil, nil, right}, ui.BarOverflow{Right: 3}, 40, ui.Horizontal)

	if len(fades) != 1 {
		t.Fatalf("fades = %d, want one for the one section that dropped", len(fades))
	}
	got := fades[0].Bounds
	// The last survivor ends at 1120, so the fade covers 1080..1120.
	want := ui.Rect{X: 1080, Y: 7, W: 40, H: 30}
	if got != want {
		t.Fatalf("fade bounds = %+v, want %+v", got, want)
	}
	if fades[0].Kind != ui.KindEdgeFade {
		t.Fatalf("fade kind = %v, want KindEdgeFade", fades[0].Kind)
	}
}

// The fade is a signal, never a control: it cannot be clicked, focused or
// reached by keyboard, because there is nothing to activate.
func TestOverflowFadeIsInert(t *testing.T) {
	right := placed(ui.Rect{X: 900, Y: 7, W: 120, H: 30})
	fades := overflowFades([][]*ui.Node{nil, nil, right}, ui.BarOverflow{Right: 1}, 40, ui.Horizontal)
	if len(fades) != 1 {
		t.Fatalf("fades = %d, want one", len(fades))
	}
	n := fades[0]
	if n.Action != "" || n.Focusable || n.Tooltip != "" {
		t.Fatalf("fade is interactive: action=%q focusable=%v tooltip=%q", n.Action, n.Focusable, n.Tooltip)
	}
}

// A bar that fits is a bar with no chrome added. This is the regression that
// matters most: the fade must never appear on a bar that dropped nothing.
func TestNoOverflowDrawsNoFade(t *testing.T) {
	sections := [][]*ui.Node{
		placed(ui.Rect{X: 10, Y: 7, W: 100, H: 30}),
		placed(ui.Rect{X: 500, Y: 7, W: 100, H: 30}),
		placed(ui.Rect{X: 900, Y: 7, W: 100, H: 30}),
	}
	if fades := overflowFades(sections, ui.BarOverflow{}, 40, ui.Horizontal); len(fades) != 0 {
		t.Fatalf("fades = %d on a bar that fits, want none", len(fades))
	}
}

// Every section that dropped gets its own, because the left section giving way
// is not reported by a fade at the far right of the screen.
func TestEachOverflowingSectionFadesOnItsOwnEdge(t *testing.T) {
	sections := [][]*ui.Node{
		placed(ui.Rect{X: 10, Y: 7, W: 100, H: 30}),
		placed(ui.Rect{X: 500, Y: 7, W: 100, H: 30}),
		placed(ui.Rect{X: 900, Y: 7, W: 100, H: 30}),
	}
	fades := overflowFades(sections, ui.BarOverflow{Left: 1, Right: 2}, 40, ui.Horizontal)
	if len(fades) != 2 {
		t.Fatalf("fades = %d, want one per overflowing section", len(fades))
	}
	if fades[0].Bounds.X != 70 || fades[1].Bounds.X != 960 {
		t.Fatalf("fade edges = %d and %d, want 70 and 960", fades[0].Bounds.X, fades[1].Bounds.X)
	}
}

// A section can overflow with nothing left to fade over: every item dropped.
// There is no edge to soften then, and inventing a box over bare bar would
// report the overflow in a place no content ever occupied.
func TestASectionThatPlacedNothingFadesNothing(t *testing.T) {
	if fades := overflowFades([][]*ui.Node{nil, nil, nil}, ui.BarOverflow{Right: 4}, 40, ui.Horizontal); len(fades) != 0 {
		t.Fatalf("fades = %d with nothing placed, want none", len(fades))
	}
}

// A survivor narrower than the fade is covered, never overrun: the ramp starts
// at the item's own left edge rather than reaching back over its neighbour.
func TestFadeNeverReachesPastTheItemItCovers(t *testing.T) {
	right := placed(ui.Rect{X: 1000, Y: 7, W: 24, H: 30})
	fades := overflowFades([][]*ui.Node{nil, nil, right}, ui.BarOverflow{Right: 1}, 40, ui.Horizontal)
	if len(fades) != 1 {
		t.Fatalf("fades = %d, want one", len(fades))
	}
	if got, want := fades[0].Bounds, (ui.Rect{X: 1000, Y: 7, W: 24, H: 30}); got != want {
		t.Fatalf("fade bounds = %+v, want %+v clamped to the item", got, want)
	}
}

func TestSideBarOverflowFade(t *testing.T) {
	bar := newTestBar(t)
	t.Cleanup(bar.stopAnimation)
	bar.theme.BarEdge = "left"
	bar.theme.Metrics.StandardControl = 16
	bar.left = []textWidget{{node: &ui.Node{Kind: ui.KindCapsule, Bounds: ui.Rect{X: 8, Y: 30, W: 24, H: 12}}}, {node: &ui.Node{Kind: ui.KindCapsule}}}
	bar.center = []textWidget{{node: &ui.Node{Kind: ui.KindCapsule, Bounds: ui.Rect{X: 8, Y: 90, W: 24, H: 30}}}, {node: &ui.Node{Kind: ui.KindCapsule}}}
	bar.right = []textWidget{{node: &ui.Node{Kind: ui.KindCapsule, Bounds: ui.Rect{X: 8, Y: 170, W: 24, H: 40}}}, {node: &ui.Node{Kind: ui.KindCapsule}}}
	bar.overflow = ui.BarOverflow{Left: 1, Center: 2, Right: 3}
	root, _ := bar.renderViewLocked()
	var fades []*ui.Node
	for _, n := range root.Children {
		if n.Kind == ui.KindEdgeFade {
			fades = append(fades, n)
		}
	}
	want := []ui.Rect{{X: 8, Y: 30, W: 24, H: 12}, {X: 8, Y: 104, W: 24, H: 16}, {X: 8, Y: 194, W: 24, H: 16}}
	if len(fades) != len(want) {
		t.Fatalf("side fades = %d, want %d", len(fades), len(want))
	}
	for i, fade := range fades {
		if fade.Bounds != want[i] || fade.Action != "" || fade.Focusable || fade.Tooltip != "" {
			t.Errorf("fade %d bounds = %+v, want inert %+v", i, fade.Bounds, want[i])
		}
		if _, hit := ui.Hit(fade, fade.Bounds.X+1, fade.Bounds.Y+1); hit {
			t.Errorf("fade %d has a hit target", i)
		}
		if height, err := ui.ContentHeight(fade, fade.Bounds.W, func(string, ui.TextAttrs) (int, int) { return 0, 0 }); err != nil || height != 0 {
			t.Errorf("fade %d contributes height %d, error %v", i, height, err)
		}
	}
	if got := overflowFades([][]*ui.Node{nil, nil, nil}, ui.BarOverflow{Right: 1}, 16, ui.Vertical); len(got) != 0 {
		t.Fatalf("all-dropped side section has %d fades", len(got))
	}
}
