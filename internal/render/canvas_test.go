package render

import (
	"math"
	"testing"
)

func TestLerpColorEndpointsAndClamping(t *testing.T) {
	t.Parallel()
	from := Color{R: 0x1d, G: 0x20, B: 0x25, A: 0xff}
	to := Color{R: 0x3a, G: 0x41, B: 0x49, A: 0xff}
	for _, tc := range []struct {
		name     string
		progress float64
		want     Color
	}{
		{"before start", -1, from},
		{"start", 0, from},
		{"end", 1, to},
		{"past end", 5, to},
		{"NaN", math.NaN(), from},
	} {
		if got := LerpColor(from, to, tc.progress); got != tc.want {
			t.Errorf("LerpColor(%s) = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestLerpColorMidpointRoundsPerChannel(t *testing.T) {
	t.Parallel()
	got := LerpColor(Color{R: 0, G: 0, B: 0, A: 0}, Color{R: 255, G: 10, B: 1, A: 200}, 0.5)
	want := Color{R: 128, G: 5, B: 1, A: 100}
	if got != want {
		t.Fatalf("midpoint = %+v, want %+v", got, want)
	}
}

func TestLerpColorCarriesAlpha(t *testing.T) {
	t.Parallel()
	// A panel appearing while the palette changes fades opacity and hue in the
	// same transition, so alpha must not be pinned to either endpoint.
	from := Color{R: 0x28, G: 0x2c, B: 0x33, A: 0x00}
	to := Color{R: 0x28, G: 0x2c, B: 0x33, A: 0xff}
	mid := LerpColor(from, to, 0.5)
	if mid.A != 128 {
		t.Errorf("alpha at midpoint = %d, want 128", mid.A)
	}
	if mid.R != 0x28 || mid.G != 0x2c || mid.B != 0x33 {
		t.Errorf("colour drifted while only alpha changed: %+v", mid)
	}
}

func TestLerpColorDescendsWithoutWrapping(t *testing.T) {
	t.Parallel()
	// uint8 subtraction that underflows would wrap to 255 and flash white.
	got := LerpColor(Color{R: 255, G: 255, B: 255, A: 255}, Color{}, 0.5)
	want := Color{R: 128, G: 128, B: 128, A: 128}
	if got != want {
		t.Fatalf("descending midpoint = %+v, want %+v", got, want)
	}
	for i := 0; i <= 20; i++ {
		p := float64(i) / 20
		c := LerpColor(Color{R: 255, A: 255}, Color{R: 0, A: 255}, p)
		if i > 0 {
			prev := LerpColor(Color{R: 255, A: 255}, Color{R: 0, A: 255}, float64(i-1)/20)
			if c.R > prev.R {
				t.Fatalf("channel rose at %v: %d after %d", p, c.R, prev.R)
			}
		}
	}
}
