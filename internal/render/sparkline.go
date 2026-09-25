package render

import (
	"image"
	"math"

	"golang.org/x/image/vector"
)

// fpt is a point in the graph box's physical pixel space.
type fpt struct{ X, Y float32 }

// smoothSteps is how many line segments approximate each quadratic span. At
// sparkline sizes eight is indistinguishable from the true curve.
const smoothSteps = 8

// sparklinePoints maps normalised samples, oldest first, onto a w×h box. The
// newest sample sits on the right edge. window is how many samples the full
// width represents; a window smaller than the samples present is ignored.
// inset keeps the line, and the dot on it, off every edge of the box.
func sparklinePoints(values []float64, window, w, h int, inset float32) []fpt {
	n := len(values)
	if n == 0 || w <= 0 || h <= 0 {
		return nil
	}
	window = max(window, n)
	right := float32(w-1) - inset
	span := max(float32(w-1)-2*inset, 0)
	height := max(float32(h-1)-2*inset, 0)
	step := float32(0)
	if window > 1 {
		step = span / float32(window-1)
	}
	out := make([]fpt, n)
	for i, v := range values {
		v = min(max(v, 0), 1)
		out[i] = fpt{
			X: right - float32(n-1-i)*step,
			Y: inset + height*float32(1-v),
		}
	}
	return out
}

// smoothLine draws quadratic curves through the midpoints between samples,
// with each sample as the control point, flattened to a polyline. It keeps the
// first and last samples exactly.
func smoothLine(points []fpt) []fpt {
	if len(points) < 3 {
		return points
	}
	out := []fpt{points[0]}
	start := points[0]
	for i := 1; i < len(points)-1; i++ {
		ctrl := points[i]
		end := fpt{(points[i].X + points[i+1].X) / 2, (points[i].Y + points[i+1].Y) / 2}
		for k := 1; k <= smoothSteps; k++ {
			t := float32(k) / smoothSteps
			u := 1 - t
			out = append(out, fpt{
				X: u*u*start.X + 2*u*t*ctrl.X + t*t*end.X,
				Y: u*u*start.Y + 2*u*t*ctrl.Y + t*t*end.Y,
			})
		}
		start = end
	}
	return append(out, points[len(points)-1])
}

// strokeContours outlines a polyline as one quad per segment plus a round
// join at every interior point. Every contour is positively oriented, so where
// they overlap the rasterizer's accumulated coverage clamps at full rather
// than cancelling.
func strokeContours(line []fpt, width float32) [][]fpt {
	half := width / 2
	var out [][]fpt
	for i := 0; i+1 < len(line); i++ {
		a, b := line[i], line[i+1]
		dx, dy := b.X-a.X, b.Y-a.Y
		l := float32(math.Hypot(float64(dx), float64(dy)))
		if l == 0 {
			continue
		}
		nx, ny := -dy/l*half, dx/l*half
		out = append(out, oriented([]fpt{
			{a.X + nx, a.Y + ny}, {b.X + nx, b.Y + ny},
			{b.X - nx, b.Y - ny}, {a.X - nx, a.Y - ny},
		}))
	}
	for i := 1; i+1 < len(line); i++ {
		out = append(out, circleContour(line[i], half, 8))
	}
	return out
}

// areaContour closes the line down to floor, the box's lower edge.
func areaContour(line []fpt, floor float32) []fpt {
	if len(line) < 2 {
		return nil
	}
	out := append([]fpt(nil), line...)
	out = append(out, fpt{line[len(line)-1].X, floor}, fpt{line[0].X, floor})
	return oriented(out)
}

func circleContour(c fpt, r float32, segments int) []fpt {
	out := make([]fpt, segments)
	for i := range out {
		a := 2 * math.Pi * float64(i) / float64(segments)
		out[i] = fpt{c.X + r*float32(math.Cos(a)), c.Y + r*float32(math.Sin(a))}
	}
	return oriented(out)
}

func signedArea(poly []fpt) float32 {
	var sum float32
	for i := range poly {
		j := (i + 1) % len(poly)
		sum += poly[i].X*poly[j].Y - poly[j].X*poly[i].Y
	}
	return sum / 2
}

func oriented(poly []fpt) []fpt {
	if signedArea(poly) < 0 {
		for i, j := 0, len(poly)-1; i < j; i, j = i+1, j-1 {
			poly[i], poly[j] = poly[j], poly[i]
		}
	}
	return poly
}

// rasterize fills contours into a w×h coverage mask.
func rasterize(w, h int, contours [][]fpt) *image.Alpha {
	dst := image.NewAlpha(image.Rect(0, 0, w, h))
	r := vector.NewRasterizer(w, h)
	for _, c := range contours {
		if len(c) < 3 {
			continue
		}
		r.MoveTo(c[0].X, c[0].Y)
		for _, p := range c[1:] {
			r.LineTo(p.X, p.Y)
		}
		r.ClosePath()
	}
	r.Draw(dst, dst.Bounds(), image.Opaque, image.Point{})
	return dst
}
