package render

import (
	"math"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
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

func TestSparklinePointsMapsNonFiniteSamplesToZero(t *testing.T) {
	points := sparklinePoints([]float64{math.NaN(), math.Inf(1), math.Inf(-1)}, 0, 11, 11, 0)
	if len(points) != 3 {
		t.Fatalf("points = %v, want three baseline points", points)
	}
	for _, p := range points {
		if math.IsNaN(float64(p.Y)) || math.IsInf(float64(p.Y), 0) || !near(p.Y, 10) {
			t.Fatalf("non-finite sample mapped to %v, want finite baseline y=10", p)
		}
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

func TestStrokeContoursSkipsJoinsOnNearlyStraightSegments(t *testing.T) {
	flat := make([]fpt, 1000)
	for i := range flat {
		flat[i] = fpt{X: float32(i), Y: 5}
	}
	if got, want := len(strokeContours(flat, 1.5)), len(flat)-1; got != want {
		t.Fatalf("flat contour count = %d, want %d segment quads without joins", got, want)
	}
	if got := len(strokeContours([]fpt{{0, 0}, {1, 0}, {1, 1}}, 1.5)); got != 3 {
		t.Fatalf("right-angle contour count = %d, want two segments and one join", got)
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

func BenchmarkPaintGraph(b *testing.B) {
	for _, tc := range []struct {
		name string
		w, h int
	}{
		{"240x28", 240, 28},
		{"331x64", 331, 64},
	} {
		b.Run(tc.name, func(b *testing.B) {
			values := make([]float64, 120)
			for i := range values {
				values[i] = float64((i*17)%101) / 100
			}
			pix := make([]byte, tc.w*tc.h*4)
			canvas, err := NewCanvas(pix, tc.w, tc.h, tc.w*4)
			if err != nil {
				b.Fatal(err)
			}
			node := &ui.Node{Kind: ui.KindGraph, Values: values, SecondValues: values, Window: len(values)}
			box := ui.Rect{W: tc.w, H: tc.h}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := paintGraph(canvas, node, box, testStyle); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestPaintGraphAllocationCountDoesNotGrowWithHistory(t *testing.T) {
	allocations := func(samples int) float64 {
		values := make([]float64, samples)
		for i := range values {
			values[i] = float64((i*17)%101) / 100
		}
		canvas, err := NewCanvas(make([]byte, 240*28*4), 240, 28, 240*4)
		if err != nil {
			t.Fatal(err)
		}
		node := &ui.Node{Kind: ui.KindGraph, Values: values, Window: samples}
		box := ui.Rect{W: 240, H: 28}
		return testing.AllocsPerRun(20, func() {
			if err := paintGraph(canvas, node, box, testStyle); err != nil {
				t.Fatal(err)
			}
		})
	}
	short, full := allocations(8), allocations(120)
	if short != full {
		t.Fatalf("allocs/op changed with history size: 8 samples = %v, 120 samples = %v", short, full)
	}
}
