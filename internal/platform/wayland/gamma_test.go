package wayland

import "testing"

// TestGammaRamps checks the two properties the compositor and the eye care
// about: 6500 K is close to the identity ramp, and a warm temperature never
// inverts a channel.
func TestGammaRamps(t *testing.T) {
	const size = 512

	r, g, b := gammaRamps(6500, size)
	if len(r) != size || len(g) != size || len(b) != size {
		t.Fatalf("ramp lengths %d/%d/%d, want %d", len(r), len(g), len(b), size)
	}
	// The identity ramp at the last index is full scale; 6500 K lands within
	// about two percent of it.
	for name, ramp := range map[string][]uint16{"red": r, "green": g, "blue": b} {
		if last := int(ramp[size-1]); last < 63000 || last > 65535 {
			t.Errorf("6500 K %s ramp ends at %d, want near 65535", name, last)
		}
		if first := int(ramp[0]); first != 0 {
			t.Errorf("6500 K %s ramp starts at %d, want 0", name, first)
		}
		for i := 1; i < size; i++ {
			if ramp[i] < ramp[i-1] {
				t.Fatalf("6500 K %s ramp is not monotonic at %d: %d then %d", name, i, ramp[i-1], ramp[i])
			}
		}
	}

	r, g, b = gammaRamps(4000, size)
	for i := 0; i < size; i++ {
		if r[i] < g[i] || g[i] < b[i] {
			t.Fatalf("4000 K inverts a channel at %d: r=%d g=%d b=%d", i, r[i], g[i], b[i])
		}
	}
}

// TestKelvinRGBWarmerIsRedder guards the direction of the approximation:
// colder temperatures must reduce blue first. Red saturates below 6600 K, so
// green and blue carry the direction.
func TestKelvinRGBWarmerIsRedder(t *testing.T) {
	warmR, warmG, warmB := kelvinRGB(6500)
	coldR, coldG, coldB := kelvinRGB(2500)
	if warmR < coldR {
		t.Errorf("red fell with temperature: 2500 K %d, 6500 K %d", coldR, warmR)
	}
	if warmG <= coldG {
		t.Errorf("green did not rise with temperature: 2500 K %d, 6500 K %d", coldG, warmG)
	}
	if warmB <= coldB {
		t.Errorf("blue did not fall with temperature: 2500 K %d, 6500 K %d", coldB, warmB)
	}
}
