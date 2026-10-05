package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func cardHeights(n int, h int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = h
	}
	return out
}

func TestToastLayoutClearsTheBar(t *testing.T) {
	geom := toastGeometry{OutputW: 1920, OutputH: 1080, Corner: toastTopRight, BarZone: 48}
	rects, _ := toastLayout(geom, []int{80})
	if rects[0].Y < 48+toastMargin {
		t.Fatalf("card Y = %d, want >= %d (bar + margin)", rects[0].Y, 48+toastMargin)
	}
}

func TestToastCardHeightFollowsContent(t *testing.T) {
	root := &ui.Node{Kind: ui.KindColumn, Padding: 12, Gap: 6, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "summary"},
		{Kind: ui.KindText, Text: "a longer body line that must not be cropped"},
		{Kind: ui.KindText, Text: "second body line so the stack clears 96"},
		{Kind: ui.KindButton, Text: "Open", Padding: 4},
	}}
	measure := func(text string, _ ui.TextAttrs) (int, int) { return len(text) * 8, 16 }
	h, err := ui.ContentHeight(root, toastCardWidth, measure)
	if err != nil {
		t.Fatal(err)
	}
	if h <= 96 {
		t.Fatalf("content height %d still fits the 96 guess; use a taller tree", h)
	}
	if got := toastCardHeight(root, toastCardWidth, measure); got != h {
		t.Fatalf("card height = %d, want exactly the content height %d", got, h)
	}
}

func TestToastLayoutStacksFromTopRight(t *testing.T) {
	rects, queued := toastLayout(toastGeometry{OutputW: 1920, OutputH: 1080, Corner: toastTopRight}, cardHeights(2, 120))
	if len(queued) != 0 {
		t.Fatalf("queued = %v, want none", queued)
	}
	if len(rects) != 2 {
		t.Fatalf("rects = %+v", rects)
	}
	if rects[0].W != toastCardWidth {
		t.Fatalf("card width = %d, want %d", rects[0].W, toastCardWidth)
	}
	// Right edge flush with the output minus the margin; second card below.
	if rects[0].X+rects[0].W != 1920-toastMargin {
		t.Fatalf("right edge = %d", rects[0].X+rects[0].W)
	}
	if rects[1].Y <= rects[0].Y {
		t.Fatalf("second card did not stack down: %+v", rects)
	}
}

func TestToastLayoutCoversEveryCorner(t *testing.T) {
	geom := toastGeometry{OutputW: 800, OutputH: 600}
	for _, corner := range []toastCorner{toastTopLeft, toastTopRight, toastBottomLeft, toastBottomRight} {
		geom.Corner = corner
		rects, _ := toastLayout(geom, cardHeights(1, 100))
		if len(rects) != 1 {
			t.Fatalf("corner %d: no rect", corner)
		}
		r := rects[0]
		if r.X < 0 || r.Y < 0 || r.X+r.W > 800 || r.Y+r.H > 600 {
			t.Fatalf("corner %d placed %v outside 800x600", corner, r)
		}
	}
	// Bottom corners stack upward.
	geom.Corner = toastBottomLeft
	rects, _ := toastLayout(geom, cardHeights(2, 100))
	if rects[1].Y >= rects[0].Y {
		t.Fatalf("bottom corner stacked downward: %+v", rects)
	}
}

func TestToastLayoutQueuesWhatOverflows(t *testing.T) {
	geom := toastGeometry{OutputW: 800, OutputH: 250, Corner: toastTopRight}
	// 3 cards of 100px cannot fit with margins in 250px of height.
	rects, queued := toastLayout(geom, cardHeights(3, 100))
	if len(rects) == 3 {
		t.Fatalf("all cards placed in 250px: %+v", rects)
	}
	if len(queued) == 0 {
		t.Fatal("nothing queued despite overflow")
	}
	for _, r := range rects {
		if r.Y+r.H > 250 {
			t.Fatalf("rect %v exceeds the output", r)
		}
	}
}

func TestToastLayoutClampsToANarrowOutput(t *testing.T) {
	geom := toastGeometry{OutputW: 300, OutputH: 800, Corner: toastTopRight}
	rects, _ := toastLayout(geom, cardHeights(1, 100))
	if rects[0].W > 300-2*toastMargin && rects[0].W > toastCardWidth {
		t.Fatalf("card not clamped: %+v", rects[0])
	}
}

func TestToastLayoutPromotesTheQueueAfterACardCloses(t *testing.T) {
	geom := toastGeometry{OutputW: 800, OutputH: 250, Corner: toastTopRight}
	// With one card closed the queue head moves into the visible set.
	rects, queued := toastLayout(geom, cardHeights(2, 100))
	if len(queued) != 0 || len(rects) != 2 {
		t.Fatalf("after close: rects=%d queued=%d", len(rects), len(queued))
	}
}

func TestToastInputRegionIsTheUnionOfCards(t *testing.T) {
	geom := toastGeometry{OutputW: 1920, OutputH: 1080, Corner: toastTopRight}
	rects, _ := toastLayout(geom, cardHeights(2, 120))
	region := toastInputRegion(rects)
	if len(region) != 2 {
		t.Fatalf("region = %+v", region)
	}
	if region[0] != rects[0] || region[1] != rects[1] {
		t.Fatalf("region %+v is not the card union %+v", region, rects)
	}

	// No cards: an empty region, which takes no pointer input at all. That is
	// deliberately not "no region", which would make the whole output
	// clickable.
	if got := toastInputRegion(nil); got == nil || len(got) != 0 {
		t.Fatalf("empty region = %+v, want a non-nil empty slice", got)
	}
}

func TestToastLayoutClearsEveryBarEdge(t *testing.T) {
	const (
		outW = 1920
		outH = 1080
		zone = 48
	)
	cases := []struct {
		edge   string
		corner toastCorner
	}{
		{edge: "top", corner: toastTopRight},
		{edge: "right", corner: toastTopRight},
		{edge: "bottom", corner: toastBottomRight},
		{edge: "left", corner: toastTopRight},
	}
	for _, tc := range cases {
		t.Run(tc.edge, func(t *testing.T) {
			geom := toastGeometry{
				OutputW: outW, OutputH: outH,
				Corner: tc.corner, BarZone: zone, BarEdge: tc.edge,
			}
			rects, queued := toastLayout(geom, cardHeights(2, 100))
			if len(queued) != 0 || len(rects) != 2 {
				t.Fatalf("placed %d queued %v", len(rects), queued)
			}
			anchor, next := rects[0], rects[1]
			if anchor.W != toastCardWidth {
				t.Fatalf("width = %d, want %d", anchor.W, toastCardWidth)
			}
			switch tc.edge {
			case "top", "left":
				// Top-right stack, clear of a top bar. A left bar does not meet
				// that corner, so the same anchor stays.
				if anchor.Y != zone+toastMargin {
					t.Fatalf("Y = %d, want %d", anchor.Y, zone+toastMargin)
				}
				if anchor.X+anchor.W != outW-toastMargin {
					t.Fatalf("right edge = %d, want %d", anchor.X+anchor.W, outW-toastMargin)
				}
				if next.Y <= anchor.Y {
					t.Fatalf("stack grew upward: %+v", rects)
				}
			case "right":
				if anchor.X+anchor.W != outW-toastMargin-zone {
					t.Fatalf("right edge = %d, want %d (clear of the bar)", anchor.X+anchor.W, outW-toastMargin-zone)
				}
				if anchor.Y != toastMargin {
					t.Fatalf("Y = %d, want %d (a right bar is not a top inset)", anchor.Y, toastMargin)
				}
				if next.Y <= anchor.Y {
					t.Fatalf("stack grew upward: %+v", rects)
				}
			case "bottom":
				if anchor.Y+anchor.H != outH-toastMargin-zone {
					t.Fatalf("bottom edge = %d, want %d", anchor.Y+anchor.H, outH-toastMargin-zone)
				}
				if anchor.Y == zone+toastMargin {
					t.Fatal("bar thickness was reserved at the top")
				}
				if next.Y >= anchor.Y {
					t.Fatalf("bottom stack grew downward: %+v", rects)
				}
			}
			if overlapsBarStrip(anchor, tc.edge, outW, outH, zone) || overlapsBarStrip(next, tc.edge, outW, outH, zone) {
				t.Fatalf("cards %+v overlap the %s bar", rects, tc.edge)
			}
		})
	}
}

// A right bar must not spend its thickness on the vertical limit: two cards
// that fit beside the bar are queued when that thickness is taken off the top.
func TestToastLayoutRightBarDoesNotShrinkTheStack(t *testing.T) {
	geom := toastGeometry{OutputW: 800, OutputH: 250, Corner: toastTopRight, BarZone: 48, BarEdge: "right"}
	rects, queued := toastLayout(geom, cardHeights(2, 100))
	if len(rects) != 2 || len(queued) != 0 {
		t.Fatalf("rects=%d queued=%v, want both cards beside a right bar", len(rects), queued)
	}
	for _, r := range rects {
		if r.X+r.W > 800-48 {
			t.Fatalf("card %+v crosses the right bar", r)
		}
	}
}

// A full stack on a bottom bar stops above the bar. The thickness is not a
// top inset, so the anchor card sits on the bar and later cards grow upward.
func TestToastLayoutBottomStackStopsAboveTheBar(t *testing.T) {
	const zone = 48
	geom := toastGeometry{OutputW: 800, OutputH: 400, Corner: toastBottomRight, BarZone: zone, BarEdge: "bottom"}
	rects, queued := toastLayout(geom, cardHeights(6, 100))
	if len(rects) == 0 || len(queued) == 0 {
		t.Fatalf("rects=%d queued=%d, want a partial stack", len(rects), len(queued))
	}
	if rects[0].Y+rects[0].H != 400-toastMargin-zone {
		t.Fatalf("anchor bottom = %d, want %d", rects[0].Y+rects[0].H, 400-toastMargin-zone)
	}
	if rects[0].Y == zone+toastMargin {
		t.Fatal("bar thickness was reserved at the top")
	}
	for i, r := range rects {
		if r.Y < toastMargin || r.Y+r.H > 400-zone {
			t.Fatalf("card %d %+v overlaps the bottom bar or the top margin", i, r)
		}
		if i > 0 && r.Y >= rects[i-1].Y {
			t.Fatalf("card %d did not stack upward", i)
		}
	}
}

// A narrow output still has to clear a right bar: the card shrinks rather
// than running across the strip.
func TestToastLayoutNarrowOutputClearsARightBar(t *testing.T) {
	const zone = 40
	geom := toastGeometry{OutputW: 300, OutputH: 800, Corner: toastTopRight, BarZone: zone, BarEdge: "right"}
	rects, _ := toastLayout(geom, cardHeights(1, 100))
	if len(rects) != 1 {
		t.Fatal("no card")
	}
	if rects[0].X < 0 || rects[0].X+rects[0].W > 300-zone {
		t.Fatalf("card %+v crosses the right bar", rects[0])
	}
}

func overlapsBarStrip(r ui.Rect, edge string, outW, outH, zone int) bool {
	switch edge {
	case "top":
		return r.Y < zone && r.Y+r.H > 0
	case "bottom":
		return r.Y+r.H > outH-zone && r.Y < outH
	case "left":
		return r.X < zone && r.X+r.W > 0
	case "right":
		return r.X+r.W > outW-zone && r.X < outW
	default:
		return false
	}
}

var _ = ui.Rect{} // keep the import honest while layout returns ui.Rect
