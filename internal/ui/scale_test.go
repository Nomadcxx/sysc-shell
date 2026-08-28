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
