package render

import (
	"math"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
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

func TestRefractResamplesTheCapture(t *testing.T) {
	t.Parallel()
	// A coloured strip at the capture's left edge: the bend reads inward, so
	// the strip's colour must appear further in than it was painted.
	src := &ui.Image{Width: 16, Height: 8, Stride: 64, Pix: make([]byte, 16*8*4)}
	for y := 0; y < 8; y++ {
		o := y * src.Stride
		src.Pix[o], src.Pix[o+1], src.Pix[o+2], src.Pix[o+3] = 0x00, 0x00, 0xff, 0xff
	}
	r := Refraction{W: 16, H: 8, Bend: 1, Fringe: 1}
	got := Refract(src, r)
	if got == nil || got.Width != src.Width || got.Height != src.Height {
		t.Fatalf("refraction changed the geometry: %+v", got)
	}
	if got == src {
		t.Fatal("refraction returned the source, so nothing moved")
	}
	// With no terms the capture passes through untouched.
	if same := Refract(src, Refraction{W: 16, H: 8}); same != src {
		t.Error("a refraction with no terms allocated a new buffer")
	}
	if Refract(nil, r) != nil {
		t.Error("a nil source returned a buffer")
	}
}

func TestRefractSeparatesTheChannelsAtAnEdge(t *testing.T) {
	t.Parallel()
	// A hard edge: the fringe must move red and blue to different places, which
	// is what a chromatic edge is.
	src := &ui.Image{Width: 16, Height: 8, Stride: 64, Pix: make([]byte, 16*8*4)}
	for y := 0; y < 8; y++ {
		for x := 0; x < 16; x++ {
			o := y*src.Stride + x*4
			v := byte(0x10)
			if x >= 8 {
				v = 0xf0
			}
			src.Pix[o], src.Pix[o+1], src.Pix[o+2], src.Pix[o+3] = v, v, v, 0xff
		}
	}
	r := Refraction{W: 16, H: 8, Bend: 1, Fringe: 1}
	plain := Refract(src, Refraction{W: 16, H: 8, Bend: 1})
	got := Refract(src, r)
	blue, red := 0, 2
	differs := 0
	for i := 0; i < len(got.Pix); i += 4 {
		if got.Pix[i+blue] != plain.Pix[i+blue] || got.Pix[i+red] != plain.Pix[i+red] {
			differs++
		}
	}
	if differs == 0 {
		t.Fatal("the fringe changed nothing: red and blue are in the same place")
	}
	if got.Pix[blue] == got.Pix[red] && differs < 4 {
		t.Error("the channels are not separated enough to read as a fringe")
	}
}

func TestSampleChannelClampsAtTheEdge(t *testing.T) {
	t.Parallel()
	src := &ui.Image{Width: 2, Height: 2, Stride: 8, Pix: []byte{
		0x20, 0x20, 0x20, 0xff, 0x40, 0x40, 0x40, 0xff,
		0x60, 0x60, 0x60, 0xff, 0x80, 0x80, 0x80, 0xff,
	}}
	if got := sampleChannel(src, -5, 0, 0); got != 0x20 {
		t.Errorf("left of the edge = %#x, want the edge's 0x20", got)
	}
	if got := sampleChannel(src, 99, 0, 0); got != 0x40 {
		t.Errorf("right of the edge = %#x, want the edge's 0x40", got)
	}
	if got := sampleChannel(src, 0.5, 0, 0); got != 0x30 {
		t.Errorf("between two pixels = %#x, want the 0x30 average", got)
	}
	if got := sampleChannel(nil, 0, 0, 0); got != 0 {
		t.Errorf("a nil source read %#x", got)
	}
}

func refractedAt(press float64, x, y int) float64 {
	s := Refraction{W: 200, H: 100, Radius: 12, Press: press, PressX: 100, PressY: 50}.Sample(x, y)
	return math.Hypot(s.DX, s.DY)
}

func refractedSigned(press float64, x, y int) float64 {
	return Refraction{W: 200, H: 100, Radius: 12, Press: press, PressX: 100, PressY: 50}.Sample(x, y).DX
}
