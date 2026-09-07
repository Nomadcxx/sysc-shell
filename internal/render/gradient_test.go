package render

import "testing"

func TestGradientAxisHorizontal(t *testing.T) {
	t.Parallel()
	a := gradientAxisForDegrees(0)
	if a.x != 1 || a.y != 0 {
		t.Fatalf("axis = %+v, want x=1 y=0", a)
	}
}

func TestGradientAxisVerticalSnaps(t *testing.T) {
	t.Parallel()
	a := gradientAxisForDegrees(90)
	if a.x != 0 || a.y != 1 {
		t.Fatalf("axis = %+v, want x=0 y=1", a)
	}
}

func TestSampleStopsPinsEndpoints(t *testing.T) {
	t.Parallel()
	stops := []gradientStop{
		{at: 0, c: Color{R: 0, A: 255}},
		{at: 1, c: Color{R: 255, A: 255}},
	}
	if got := sampleStops(stops, 0); got.R != 0 {
		t.Fatalf("t=0: R=%d, want 0", got.R)
	}
	if got := sampleStops(stops, 1); got.R != 255 {
		t.Fatalf("t=1: R=%d, want 255", got.R)
	}
	mid := sampleStops(stops, 0.5)
	if mid.R < 120 || mid.R > 135 {
		t.Fatalf("t=0.5: R=%d, want ~127", mid.R)
	}
}
