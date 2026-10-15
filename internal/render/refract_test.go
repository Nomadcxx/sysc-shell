package render

import (
	"math"
	"testing"
)

func TestRefractSampleBendsInwardAtTheEdge(t *testing.T) {
	t.Parallel()
	// The interior of a surface is untouched; the edge bends toward the middle,
	// which is what makes a card read as a slab of glass rather than a hole.
	r := Refraction{W: 200, H: 100, Radius: 12, Bend: 1}
	if got := r.Sample(100, 50); got.DX != 0 || got.DY != 0 {
		t.Fatalf("the centre bent by %+v, want the identity", got)
	}
	left := r.Sample(1, 50)
	if left.DX <= 0 {
		t.Fatalf("a left-edge pixel reads %+v, want it to read inward (positive DX)", left)
	}
	right := r.Sample(198, 50)
	if right.DX >= 0 {
		t.Fatalf("a right-edge pixel reads %+v, want it to read inward (negative DX)", right)
	}
	top := r.Sample(100, 1)
	if top.DY <= 0 {
		t.Fatalf("a top-edge pixel reads %+v, want it to read inward (positive DY)", top)
	}
	// The displacement is bounded: an edge pixel reads from within the capture.
	if mag := math.Hypot(left.DX, left.DY); mag > bevelScale {
		t.Errorf("edge displacement %.2f exceeds the bevel scale %.2f", mag, bevelScale)
	}
}

func TestRefractSampleIsIdentityWithoutTerms(t *testing.T) {
	t.Parallel()
	r := Refraction{W: 200, H: 100, Radius: 12}
	for _, p := range [][2]int{{0, 0}, {1, 50}, {100, 50}, {199, 99}} {
		if got := r.Sample(p[0], p[1]); got.DX != 0 || got.DY != 0 || got.Fringe != [3]float64{} {
			t.Errorf("pixel %v: zero refraction produced %+v", p, got)
		}
	}
	if got := (Refraction{}).Sample(4, 4); got.DX != 0 || got.DY != 0 {
		t.Errorf("a zero surface produced %+v", got)
	}
}

func TestRefractSampleIsContinuousAtTheBevelSeam(t *testing.T) {
	t.Parallel()
	// The profile must not step where the band ends, or the seam shows as a
	// line across the surface. The edge itself is the steepest part by design;
	// what matters is that the slope decays to nothing rather than cutting off.
	r := Refraction{W: 200, H: 100, Radius: 12, Bend: 1, Fringe: 1}
	step := func(x int) float64 {
		return math.Abs(r.Sample(x, 50).DX - r.Sample(x-1, 50).DX)
	}
	var atEdge, past float64
	for x := 1; x < 40; x++ {
		s := step(x)
		if x <= 2 && s > atEdge {
			atEdge = s
		}
		// Past the band the profile is off, so the step there is exactly zero:
		// a non-zero value would be the cut-off this test exists to catch.
		if x >= 10 && s > past {
			past = s
		}
	}
	if past != 0 {
		t.Errorf("the profile steps by %.3f px past the band; it cuts off", past)
	}
	if atEdge <= past {
		t.Errorf("the profile is not steepest at the edge: edge %.3f, past %.3f", atEdge, past)
	}
	if got := r.Sample(40, 50); got.DX != 0 {
		t.Errorf("the displacement has not returned to zero by x=40: %+v", got)
	}
}

func TestRefractSampleFringeOrderedAndBounded(t *testing.T) {
	t.Parallel()
	r := Refraction{W: 200, H: 100, Radius: 12, Bend: 1, Fringe: 1}
	got := r.Sample(1, 50)
	if !(got.Fringe[0] > got.Fringe[1] && got.Fringe[1] > got.Fringe[2]) {
		t.Fatalf("fringe %v is not ordered red to blue", got.Fringe)
	}
	if got.Fringe[0]-got.Fringe[2] > 2*fringeScale {
		t.Errorf("fringe spread %.2f exceeds twice the scale %.2f", got.Fringe[0]-got.Fringe[2], fringeScale)
	}
	// The spread belongs to the bevel only.
	if centre := r.Sample(100, 50); centre.Fringe != [3]float64{} {
		t.Errorf("the centre carries a fringe: %v", centre.Fringe)
	}
}

func TestRefractSampleDomeMagnifiesTowardTheCentre(t *testing.T) {
	t.Parallel()
	r := Refraction{W: 200, H: 100, Radius: 12, Dome: 1}
	// The exact centre sits between pixels, so the pixel nearest it moves only
	// a hair.
	if near := r.Sample(100, 50); math.Hypot(near.DX, near.DY) > 0.1 {
		t.Fatalf("the centre pixel moved %+v", near)
	}
	// Magnification reads nearer the middle: a pixel right of centre reads from
	// further left.
	right := r.Sample(120, 50)
	if right.DX >= 0 {
		t.Fatalf("a pixel right of centre reads %+v, want it pulled left", right)
	}
	left := r.Sample(80, 50)
	if left.DX <= 0 {
		t.Fatalf("a pixel left of centre reads %+v, want it pulled right", left)
	}
	// Bounded: the magnification never moves a sample by more than the cap.
	if f := math.Abs(right.DX) / 20; f > domeCap {
		t.Errorf("dome factor %.4f exceeds its cap %.4f", f, domeCap)
	}
}

func TestRefractSamplePressRingTravels(t *testing.T) {
	t.Parallel()
	// At rest there is no ring; at full phase the ring has left the press point
	// and the surface is still.
	rest := Refraction{W: 200, H: 100, Radius: 12, Press: 0, PressX: 100, PressY: 50}
	if got := rest.Sample(100, 50); got.DX != 0 || got.DY != 0 {
		t.Fatalf("a resting press displaced %+v", got)
	}
	mid := refractedAt(0.5, 100+pressRingRadius/2, 50)
	if mid < 0.05 {
		t.Fatalf("mid-press displaced %.3f at the ring's radius", mid)
	}
	far := refractedAt(0.5, 100+pressRingRadius*3, 50)
	if far > 0.001 {
		t.Errorf("the ring still reaches %.0f px from the press point: %.4f", pressRingRadius*3, far)
	}
	// The displacement points away from the press point, so the surface bends
	// outward under a hand.
	if d := refractedSigned(0.5, 100+pressRingRadius/2, 50); d <= 0 {
		t.Errorf("the ring pulled inward (%.3f); a press should push out", d)
	}
}

func refractedAt(press float64, x, y int) float64 {
	s := Refraction{W: 200, H: 100, Radius: 12, Press: press, PressX: 100, PressY: 50}.Sample(x, y)
	return math.Hypot(s.DX, s.DY)
}

func refractedSigned(press float64, x, y int) float64 {
	return Refraction{W: 200, H: 100, Radius: 12, Press: press, PressX: 100, PressY: 50}.Sample(x, y).DX
}
