package render

import (
	"image"
	"testing"
)

// The shell derives the mark's box from WordmarkAspect while the painter
// scales the master to whatever box it was given. If the constant and the
// asset ever disagree the mark stretches, so pin them to each other.
func TestWordmarkAspectMatchesTheAsset(t *testing.T) {
	t.Parallel()
	master, err := wordmarkSource()
	if err != nil {
		t.Fatalf("decode embedded master: %v", err)
	}
	b := master.Bounds()
	got := float64(b.Dx()) / float64(b.Dy())
	if diff := got - WordmarkAspect; diff > 0.001 || diff < -0.001 {
		t.Fatalf("asset aspect %.6f but WordmarkAspect is %.6f; update the "+
			"constant when regenerating sysc-mark.png", got, WordmarkAspect)
	}
}

func TestWordmarkWidthFollowsHeight(t *testing.T) {
	t.Parallel()
	if got := WordmarkWidth(23); got != 167 {
		t.Fatalf("WordmarkWidth(23) = %d, want 167", got)
	}
	if got := WordmarkWidth(0); got != 0 {
		t.Fatalf("WordmarkWidth(0) = %d, want 0", got)
	}
	if got := WordmarkWidth(-4); got != 0 {
		t.Fatalf("WordmarkWidth(-4) = %d, want 0", got)
	}
}

func TestWordmarkRastersAtTheRequestedSize(t *testing.T) {
	t.Parallel()
	mask, err := Wordmark(167, 23)
	if err != nil {
		t.Fatal(err)
	}
	if mask == nil {
		t.Fatal("no mask")
	}
	if got := mask.Bounds(); got != image.Rect(0, 0, 167, 23) {
		t.Fatalf("mask bounds = %v, want 167x23", got)
	}
	// The mark must actually carry coverage, or a broken embed would paint
	// nothing and look like a layout bug instead of an asset one.
	var ink int
	for _, a := range mask.Pix {
		if a > 128 {
			ink++
		}
	}
	if ink == 0 {
		t.Fatal("mask is entirely transparent")
	}
	if ink == len(mask.Pix) {
		t.Fatal("mask is entirely opaque; the alpha channel did not survive")
	}
}

func TestWordmarkCachesPerSize(t *testing.T) {
	t.Parallel()
	a, err := Wordmark(80, 11)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Wordmark(80, 11)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("same size returned a different mask; the cache is not hitting")
	}
	c, err := Wordmark(96, 13)
	if err != nil {
		t.Fatal(err)
	}
	if c == a {
		t.Fatal("a different size returned the cached mask")
	}
}

func TestWordmarkRejectsAnEmptyBox(t *testing.T) {
	t.Parallel()
	for _, wh := range [][2]int{{0, 10}, {10, 0}, {-1, -1}} {
		mask, err := Wordmark(wh[0], wh[1])
		if err != nil || mask != nil {
			t.Fatalf("Wordmark(%d,%d) = %v, %v; want nil, nil", wh[0], wh[1], mask, err)
		}
	}
}
