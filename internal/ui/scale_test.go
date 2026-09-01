package ui

import "testing"

func TestScale120Physical(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		scale   Scale120
		logical int
		want    int
	}{
		{"unit scale keeps the bar height", ScaleUnit, 48, 48},
		{"unit scale keeps the output width", ScaleUnit, 3440, 3440},
		{"unit scale keeps zero", ScaleUnit, 0, 0},
		{"1.5 scales the bar height", 180, 48, 72},
		{"1.5 rounds a half pixel up", 180, 1, 2},
		{"1.25 scales the bar height", 150, 48, 60},
		{"1.25 scales the output width", 150, 3440, 4300},
		{"1.3 rounds 62.4 down", 156, 48, 62},
		{"1.75 scales the bar height", 210, 48, 84},
		// Measured on Niri 26.04: DP-1 runs a 3440x1440 mode, and at scale 1.5
		// Niri reports a logical size of 2293x960. The conversion must map that
		// logical width back onto the exact mode width.
		{"1.5 maps a measured logical width onto its mode", 180, 2293, 3440},
		{"1.5 maps a measured logical height onto its mode", 180, 960, 1440},
		{"0.5 halves the bar height", 60, 48, 24},
		{"0.5 rounds a half pixel up", 60, 1, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.scale.Physical(tc.logical); got != tc.want {
				t.Fatalf("Scale120(%d).Physical(%d) = %d, want %d", tc.scale, tc.logical, got, tc.want)
			}
		})
	}
}

// TestScale120PhysicalRectKeepsNeighboursAdjacent proves rectangles are mapped
// by their edges. Scaling width independently would open gaps or overlaps
// between adjacent nodes at fractional scales.
func TestScale120PhysicalRectKeepsNeighboursAdjacent(t *testing.T) {
	t.Parallel()

	const scale Scale120 = 180
	left := scale.PhysicalRect(Rect{X: 0, Y: 0, W: 7, H: 7})
	right := scale.PhysicalRect(Rect{X: 7, Y: 7, W: 7, H: 7})

	if left.X+left.W != right.X {
		t.Errorf("horizontal gap: left ends at %d, right starts at %d", left.X+left.W, right.X)
	}
	if left.Y+left.H != right.Y {
		t.Errorf("vertical gap: left ends at %d, right starts at %d", left.Y+left.H, right.Y)
	}
}

func TestScale120PhysicalRectAtUnitScale(t *testing.T) {
	t.Parallel()

	r := Rect{X: 12, Y: 4, W: 120, H: 40}
	if got := ScaleUnit.PhysicalRect(r); got != r {
		t.Fatalf("unit scale changed %+v into %+v", r, got)
	}
}

func TestScale120Valid(t *testing.T) {
	t.Parallel()

	for _, s := range []Scale120{ScaleUnit, 60, 180, 1} {
		if !s.Valid() {
			t.Errorf("Scale120(%d) reported invalid", s)
		}
	}
	for _, s := range []Scale120{0, -1, -120} {
		if s.Valid() {
			t.Errorf("Scale120(%d) reported valid", s)
		}
	}
}

func TestLogicalRoundsUpSoTextIsNotClipped(t *testing.T) {
	t.Parallel()
	const scale = Scale120(150) // Niri 1.25
	for _, phys := range []int{1, 7, 17, 100, 101, 149, 150} {
		got := scale.Logical(phys)
		if back := scale.Physical(got); back < phys {
			t.Errorf("Logical(%d)=%d converts back to %d, which is less than the measurement", phys, got, back)
		}
	}
	if got := ScaleUnit.Logical(42); got != 42 {
		t.Errorf("at scale 1 Logical(42) = %d, want 42", got)
	}
}
