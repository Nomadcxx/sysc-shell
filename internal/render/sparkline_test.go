package render

import (
	"math"
	"testing"
)

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

func TestSparklinePoints(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []float64
		window int
		inset  float32
		want   []fpt
	}{
		{"fills width", []float64{0, 1}, 0, 0, []fpt{{0, 10}, {10, 0}}},
		{"window draws short history from the right", []float64{0, 1}, 3, 0, []fpt{{5, 10}, {10, 0}}},
		{"one sample sits on the right edge", []float64{0.5}, 0, 0, []fpt{{10, 5}}},
		{"out of range values clamp", []float64{-1, 2}, 0, 0, []fpt{{0, 10}, {10, 0}}},
		{"inset keeps the line off every edge", []float64{0, 1}, 0, 1, []fpt{{1, 9}, {9, 1}}},
		{"window smaller than samples is ignored", []float64{0, 0.5, 1}, 2, 0, []fpt{{0, 10}, {5, 5}, {10, 0}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := sparklinePoints(tc.values, tc.window, 11, 11, tc.inset)
			if len(got) != len(tc.want) {
				t.Fatalf("points = %v, want %v", got, tc.want)
			}
			for i := range got {
				if !near(got[i].X, tc.want[i].X) || !near(got[i].Y, tc.want[i].Y) {
					t.Fatalf("points = %v, want %v", got, tc.want)
				}
			}
		})
	}
	if got := sparklinePoints(nil, 0, 11, 11, 0); got != nil {
		t.Fatalf("empty series = %v, want nil", got)
	}
}

func TestSmoothLineKeepsEndpointsAndPassesMidpoints(t *testing.T) {
	in := []fpt{{0, 10}, {5, 0}, {10, 10}}
	out := smoothLine(in)
	if out[0] != in[0] || out[len(out)-1] != in[2] {
		t.Fatalf("endpoints = %v .. %v", out[0], out[len(out)-1])
	}
	// The curve ends each span on the midpoint between samples.
	mid := fpt{7.5, 5}
	found := false
	for _, p := range out {
		if near(p.X, mid.X) && near(p.Y, mid.Y) {
			found = true
		}
	}
	if !found {
		t.Fatalf("smoothed line %v never reaches the midpoint %v", out, mid)
	}
	if got := smoothLine(in[:2]); len(got) != 2 {
		t.Fatalf("two points should pass through unchanged, got %v", got)
	}
}

func TestContoursArePositivelyOriented(t *testing.T) {
	contours := strokeContours([]fpt{{0, 5}, {10, 5}, {10, 0}}, 2)
	contours = append(contours, circleContour(fpt{5, 5}, 2, 12))
	for i, c := range contours {
		if signedArea(c) <= 0 {
			t.Fatalf("contour %d has area %v, want positive so overlaps add", i, signedArea(c))
		}
	}
}

func TestRasterizeAntialiasesADiagonalStroke(t *testing.T) {
	mask := rasterize(20, 20, strokeContours([]fpt{{2, 18}, {18, 2}}, 1.5))
	partial, full := 0, 0
	for _, a := range mask.Pix {
		switch {
		case a == 255:
			full++
		case a > 0:
			partial++
		}
	}
	if partial == 0 {
		t.Fatal("stroke has no partial-coverage pixels: it is not anti-aliased")
	}
	if full+partial == 0 {
		t.Fatal("stroke painted nothing")
	}
}
