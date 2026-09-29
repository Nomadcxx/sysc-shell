package ui

import (
	"math"
	"testing"
)

func TestMorphRadiusEasesTowardHalf(t *testing.T) {
	for _, tc := range []struct {
		base     int
		progress float64
		want     int
	}{
		{16, 0, 16}, {16, 1, 8}, {16, 0.5, 12},
		{17, 1, 8}, {1, 1, 1}, {0, 1, 1},
		{16, 2, 8}, {16, -1, 16},
	} {
		if got := MorphRadius(tc.base, tc.progress); got != tc.want {
			t.Errorf("MorphRadius(%d, %v) = %d, want %d", tc.base, tc.progress, got, tc.want)
		}
	}
}

func TestRippleDiscGrowsThenFades(t *testing.T) {
	box := Rect{W: 10, H: 10}
	farthest := math.Hypot(10, 10)
	for _, tc := range []struct {
		name       string
		phase      float64
		wantRadius float64
		wantAlpha  float64
	}{
		{"start", 0, 0, 1},
		{"mid-grow", 0.3125, 0.875 * farthest, 1},
		{"grow ends", 0.625, farthest, 1},
		{"mid-fade", 0.8125, farthest, 0.5},
		{"done", 1, farthest, 0},
		{"past the end clamps", 1.5, farthest, 0},
		{"nan phase clamps", math.NaN(), 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			radius, alpha := RippleDisc(tc.phase, box, 0, 0)
			if math.IsNaN(radius) || math.IsNaN(alpha) ||
				math.Abs(radius-tc.wantRadius) > 1e-9 || math.Abs(alpha-tc.wantAlpha) > 1e-9 {
				t.Fatalf("RippleDisc(%v) = %v, %v, want %v, %v",
					tc.phase, radius, alpha, tc.wantRadius, tc.wantAlpha)
			}
		})
	}
	if r, a := RippleDisc(0.5, box, 10, 10); r <= 0 || a != 1 {
		t.Fatalf("origin at a corner still grows: %v, %v", r, a)
	}
	if r, _ := RippleDisc(0.5, Rect{}, 0, 0); r != 0 {
		t.Fatalf("empty box radius = %v, want 0", r)
	}
	if r, _ := RippleDisc(0.5, Rect{W: -1, H: 10}, 0, 0); r != 0 {
		t.Fatalf("invalid box radius = %v, want 0", r)
	}
}
