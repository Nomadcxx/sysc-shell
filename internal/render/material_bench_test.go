package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The cost stop gate for the material's second stage (plan B3): the per-frame
// work must stay inside the band the existing blur was measured at, 4.94 ms for
// the worst regular panel, against a 16.67 ms frame.
//
// Both benchmarks run at the *reduced* capture size, which is what the material
// actually touches: the settings panel at scale 1 is 1120x820 logical, so its
// capture is 280x205; the whole output is 3440x1440, so its capture is 860x360.

func materialImage(w, h int) *ui.Image {
	img := chequer(w, h, 8)
	// An edge for the bevel to work on, since a uniform field would let the
	// compiler's branch prediction do the interesting part.
	for y := 0; y < h; y++ {
		for x := w / 3; x < 2*w/3; x++ {
			o := y*img.Stride + x*4
			img.Pix[o], img.Pix[o+1], img.Pix[o+2] = 0x20, 0x20, 0x20
		}
	}
	return img
}

func BenchmarkMaterializePanel(b *testing.B) {
	sharp := materialImage(280, 205)
	blurred := materialImage(280, 205)
	m := Material{Tone: ToneSmoke, Intensity: 60}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := Materialize(sharp, blurred, m); got == nil {
			b.Fatal("no ground")
		}
	}
}

func BenchmarkRefractPanel(b *testing.B) {
	src := materialImage(280, 205)
	r := Refraction{W: 280, H: 205, Radius: 3, Bend: 1, Fringe: 1, Dome: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := Refract(src, r); got == nil {
			b.Fatal("no refracted capture")
		}
	}
}

func BenchmarkRefractFullOutput(b *testing.B) {
	src := materialImage(860, 360)
	r := Refraction{W: 860, H: 360, Radius: 3, Bend: 1, Fringe: 1, Dome: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := Refract(src, r); got == nil {
			b.Fatal("no refracted capture")
		}
	}
}

// BenchmarkBusynessPanel covers the auto-frost decision, which runs once per
// surface resolve rather than per frame but must still be free of surprises.
func BenchmarkBusynessPanel(b *testing.B) {
	sharp := materialImage(280, 205)
	blurred := materialImage(280, 205)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Busyness(sharp, blurred, autoFrostPoints)
	}
}
