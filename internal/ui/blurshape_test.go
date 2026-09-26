package ui

import (
	"slices"
	"testing"
)

// covered reports every pixel the strips cover, rejecting overlap so a
// region never lists a pixel twice.
func covered(t *testing.T, strips []Rect) map[[2]int]bool {
	t.Helper()
	out := map[[2]int]bool{}
	for _, r := range strips {
		if r.W <= 0 || r.H <= 0 {
			t.Fatalf("empty strip %+v", r)
		}
		for y := r.Y; y < r.Y+r.H; y++ {
			for x := r.X; x < r.X+r.W; x++ {
				if out[[2]int{x, y}] {
					t.Fatalf("pixel (%d,%d) covered twice", x, y)
				}
				out[[2]int{x, y}] = true
			}
		}
	}
	return out
}

func TestBlurStripsOfASquareBodyIsTheBody(t *testing.T) {
	t.Parallel()
	body := Rect{X: 3, Y: 4, W: 100, H: 40}
	if got := BlurStrips(SurfaceShape{Body: body}); !slices.Equal(got, []Rect{body}) {
		t.Fatalf("strips = %v, want the body", got)
	}
	if got := BlurStrips(SurfaceShape{}); got != nil {
		t.Fatalf("empty body = %v, want nil", got)
	}
}

// Every row matches the painter's rounded inset, and straight runs merge.
func TestBlurStripsFollowTheRoundedCorners(t *testing.T) {
	t.Parallel()
	body := Rect{X: 0, Y: 0, W: 200, H: 40}
	strips := BlurStrips(SurfaceShape{Body: body, Radius: 12})
	px := covered(t, strips)
	for y := 0; y < body.H; y++ {
		in := RoundedInset(y, body.H, 12)
		if !px[[2]int{in, y}] || px[[2]int{in - 1, y}] || !px[[2]int{body.W - 1 - in, y}] || px[[2]int{body.W - in, y}] {
			t.Fatalf("row %d does not span [%d,%d)", y, in, body.W-in)
		}
	}
	if len(strips) > 2*12+1 {
		t.Fatalf("%d strips; straight rows should merge into one band", len(strips))
	}
}

func TestBlurStripsClampTheRadius(t *testing.T) {
	t.Parallel()
	a := BlurStrips(SurfaceShape{Body: Rect{W: 120, H: 28}, Radius: 40})
	b := BlurStrips(SurfaceShape{Body: Rect{W: 120, H: 28}, Radius: 14})
	if !slices.Equal(a, b) {
		t.Fatal("a radius above half the short side was not clamped to it")
	}
}

// The attached edge's corners are square; the far corners stay round.
func TestBlurStripsSquareTheAttachedEdge(t *testing.T) {
	t.Parallel()
	body := Rect{X: 10, Y: 0, W: 100, H: 60}
	for _, edge := range []string{"top", "bottom"} {
		px := covered(t, BlurStrips(SurfaceShape{Body: body, Radius: 12, AttachEdge: edge}))
		attached, far := 0, body.H-1
		if edge == "bottom" {
			attached, far = far, attached
		}
		if !px[[2]int{body.X, attached}] {
			t.Errorf("%s: attached corner is not square", edge)
		}
		if px[[2]int{body.X, far}] {
			t.Errorf("%s: far corner lost its radius", edge)
		}
	}
}

// Joints sit outside the body beside the attached edge, each side its own
// width, following FilletSpan row by row.
func TestBlurStripsCarryAsymmetricJoints(t *testing.T) {
	t.Parallel()
	body := Rect{X: 20, Y: 0, W: 100, H: 60}
	px := covered(t, BlurStrips(SurfaceShape{Body: body, Radius: 12, AttachEdge: "top", JointLeft: 12, JointRight: 5}))
	for y := 0; y < 12; y++ {
		span := FilletSpan(y, 12)
		if span > 0 && (!px[[2]int{body.X - span, y}] || px[[2]int{body.X - span - 1, y}]) {
			t.Fatalf("left joint row %d does not span %d", y, span)
		}
	}
	if px[[2]int{body.X + body.W + FilletSpan(0, 5), 0}] {
		t.Fatal("right joint is wider than 5")
	}
	if !px[[2]int{body.X + body.W, 0}] {
		t.Fatal("right joint is missing")
	}
	for x := body.X - 12; x < body.X; x++ {
		if px[[2]int{x, 12}] {
			t.Fatalf("left joint reaches row 12 at x=%d", x)
		}
	}
}

// An attached bar: square body, wedges below both ends curving into the
// screen's side edges. Mirrored for a bottom bar.
func TestBlurStripsCarryEdgeFillets(t *testing.T) {
	t.Parallel()
	body := Rect{X: 0, Y: 0, W: 1920, H: 40}
	px := covered(t, BlurStrips(SurfaceShape{Body: body, AttachEdge: "top", EdgeFillet: 12, EdgeLeft: true, EdgeRight: true}))
	for y := 0; y < 12; y++ {
		span := FilletSpan(y, 12)
		row := body.H + y
		if span > 0 && (!px[[2]int{span - 1, row}] || px[[2]int{span, row}]) {
			t.Fatalf("left end row %d does not span %d", y, span)
		}
		if span > 0 && (!px[[2]int{body.W - span, row}] || px[[2]int{body.W - span - 1, row}]) {
			t.Fatalf("right end row %d does not span %d", y, span)
		}
	}
	bottom := Rect{X: 0, Y: 12, W: 1920, H: 40}
	pb := covered(t, BlurStrips(SurfaceShape{Body: bottom, AttachEdge: "bottom", EdgeFillet: 12, EdgeLeft: true, EdgeRight: true}))
	for y := 0; y < 12; y++ {
		span := FilletSpan(y, 12)
		if span > 0 && !pb[[2]int{span - 1, bottom.Y - 1 - y}] {
			t.Fatalf("bottom bar: left end row %d missing", y)
		}
	}
}

// A panel flush against the right screen edge: no right joint, a square far
// corner on that side, and a wedge below it into the screen edge.
func TestBlurStripsForAFlushPanel(t *testing.T) {
	t.Parallel()
	body := Rect{X: 12, Y: 0, W: 300, H: 200}
	px := covered(t, BlurStrips(SurfaceShape{
		Body: body, Radius: 12, AttachEdge: "top",
		JointLeft: 12, EdgeFillet: 12, EdgeRight: true,
	}))
	right := body.X + body.W
	if px[[2]int{right, 0}] {
		t.Fatal("a flush side carries a joint")
	}
	if !px[[2]int{right - 1, body.H - 1}] {
		t.Fatal("the flush far corner is still rounded")
	}
	if !px[[2]int{right - 1, body.H}] {
		t.Fatal("no screen-edge wedge below the flush corner")
	}
	if px[[2]int{body.X, body.H}] || px[[2]int{body.X, body.H - 1}] {
		t.Fatal("the other far corner gained a wedge or lost its radius")
	}
}
