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

var _ = ui.Rect{} // keep the import honest while layout returns ui.Rect
