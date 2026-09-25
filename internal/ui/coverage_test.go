package ui

import "testing"

// The joint is concave: full against the corner, running tangent into the
// edge and the side, and thin across the diagonal. The shape it replaced was
// a quarter disc centred on the corner, which bulged out as a rounded block.
func TestFilletIsConcaveAndTangent(t *testing.T) {
	t.Parallel()
	const r = 12
	if got := FilletCoverage(0, 0, r); got != 255 {
		t.Fatalf("corner pixel = %d, want 255", got)
	}
	// Along the edge the fill reaches most of the radius; across the
	// diagonal it is only a few pixels deep.
	if got := FilletSpan(0, r); got < 8 {
		t.Fatalf("span along the edge = %d, want at least 8 of %d", got, r)
	}
	if got := FilletSpan(r/2, r); got > 2 {
		t.Fatalf("span halfway down = %d, want at most 2: the arc must bow toward the corner", got)
	}
	if got := FilletCoverage(r/2, r/2, r); got != 0 {
		t.Fatalf("diagonal midpoint = %d, want 0 for a concave arc", got)
	}
	for y := 1; y < r; y++ {
		if FilletSpan(y, r) > FilletSpan(y-1, r) {
			t.Fatalf("span grew at row %d", y)
		}
	}
	if FilletSpan(r-1, r) != 0 || FilletExtent(r, r) != 0 {
		t.Fatal("the fillet reaches past its radius")
	}
}

func TestFilletIsSymmetricInItsTwoAxes(t *testing.T) {
	t.Parallel()
	for x := 0; x < 14; x++ {
		for y := 0; y < 14; y++ {
			if FilletCoverage(x, y, 14) != FilletCoverage(y, x, 14) {
				t.Fatalf("coverage (%d,%d) differs from (%d,%d)", x, y, y, x)
			}
		}
	}
}

func TestFilletSpanNeverExceedsExtent(t *testing.T) {
	t.Parallel()
	for _, r := range []int{1, 5, 12, 18} {
		for y := 0; y < r; y++ {
			if FilletSpan(y, r) > FilletExtent(y, r) {
				t.Fatalf("r=%d row %d: span %d > extent %d", r, y, FilletSpan(y, r), FilletExtent(y, r))
			}
		}
	}
}
