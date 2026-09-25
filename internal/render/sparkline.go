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
		if math.IsNaN(v) || math.IsInf(v, 0) {
			v = 0
		}
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
	out := make([]fpt, 1, (len(points)-2)*smoothSteps+2)
	out[0] = points[0]
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

// strokeContours materialises a polyline as one quad per segment plus round
// joins at sharp corners. The painter streams the same geometry in traceStroke
// to avoid one allocation per contour.
func strokeContours(line []fpt, width float32) [][]fpt {
	if len(line) < 2 {
		return nil
	}
	half := width / 2
	out := make([][]fpt, 0, len(line)*2-3)
	for i := 0; i+1 < len(line); i++ {
		quad, ok := strokeQuad(line[i], line[i+1], half)
		if !ok {
			continue
		}
		out = append(out, quad[:])
	}
	for i := 1; i+1 < len(line); i++ {
		if needsRoundJoin(line[i-1], line[i], line[i+1]) {
			out = append(out, circleContour(line[i], half, 8))
		}
	}
	return out
}

func strokeQuad(a, b fpt, half float32) ([4]fpt, bool) {
	dx, dy := b.X-a.X, b.Y-a.Y
	length := float32(math.Hypot(float64(dx), float64(dy)))
	if length == 0 {
		return [4]fpt{}, false
	}
	nx, ny := -dy/length*half, dx/length*half
	quad := [4]fpt{
		{a.X + nx, a.Y + ny}, {b.X + nx, b.Y + ny},
		{b.X - nx, b.Y - ny}, {a.X - nx, a.Y - ny},
	}
	if signedArea(quad[:]) < 0 {
		quad[0], quad[3] = quad[3], quad[0]
		quad[1], quad[2] = quad[2], quad[1]
	}
	return quad, true
}

// ponytail: skip joins below 5°; at the 1.5px outline width the omitted wedge
// stays subpixel. If wider strokes expose it, replace this with a true stroker.
func needsRoundJoin(prev, at, next fpt) bool {
	ax, ay := at.X-prev.X, at.Y-prev.Y
	bx, by := next.X-at.X, next.Y-at.Y
	al, bl := math.Hypot(float64(ax), float64(ay)), math.Hypot(float64(bx), float64(by))
	if al == 0 || bl == 0 {
		return false
	}
	if ax*bx+ay*by <= 0 {
		return true
	}
	turn := math.Abs(float64(ax*by-ay*bx)) / (al * bl)
	return turn >= 0.08715574 // sin(5°)
}

// areaContour closes the line down to floor, the box's lower edge.
func areaContour(line []fpt, floor float32) []fpt {
	if len(line) < 2 {
		return nil
	}
	out := make([]fpt, 0, len(line)+2)
	out = append(out, line...)
	out = append(out, fpt{line[len(line)-1].X, floor}, fpt{line[0].X, floor})
	return oriented(out)
}

func circleContour(c fpt, r float32, segments int) []fpt {
	out := make([]fpt, segments)
	for i := range out {
		out[i] = circlePoint(c, r, i, segments)
	}
	return oriented(out)
}

func circlePoint(center fpt, radius float32, i, segments int) fpt {
	a := 2 * math.Pi * float64(i) / float64(segments)
	return fpt{center.X + radius*float32(math.Cos(a)), center.Y + radius*float32(math.Sin(a))}
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
	rasterizeContours(r, dst, contours)
	return dst
}

func rasterizeContours(r *vector.Rasterizer, dst *image.Alpha, contours [][]fpt) {
	resetRaster(r, dst)
	for _, c := range contours {
		traceContour(r, c)
	}
	drawRaster(r, dst)
}

func rasterizeArea(r *vector.Rasterizer, dst *image.Alpha, line []fpt, floor float32) {
	resetRaster(r, dst)
	if len(line) >= 2 {
		r.MoveTo(line[0].X, line[0].Y)
		for _, p := range line[1:] {
			r.LineTo(p.X, p.Y)
		}
		last := line[len(line)-1]
		first := line[0]
		r.LineTo(last.X, floor)
		r.LineTo(first.X, floor)
		r.ClosePath()
	}
	drawRaster(r, dst)
}

func rasterizeStroke(r *vector.Rasterizer, dst *image.Alpha, line []fpt, width float32) {
	resetRaster(r, dst)
	traceStroke(r, line, width)
	drawRaster(r, dst)
}

func rasterizeCircle(r *vector.Rasterizer, dst *image.Alpha, center fpt, radius float32, segments int) {
	resetRaster(r, dst)
	traceCircle(r, center, radius, segments)
	drawRaster(r, dst)
}

func resetRaster(r *vector.Rasterizer, dst *image.Alpha) {
	clear(dst.Pix)
	r.Reset(dst.Rect.Dx(), dst.Rect.Dy())
}

func drawRaster(r *vector.Rasterizer, dst *image.Alpha) {
	r.Draw(dst, dst.Bounds(), image.Opaque, image.Point{})
}

func traceContour(r *vector.Rasterizer, contour []fpt) {
	if len(contour) < 3 {
		return
	}
	r.MoveTo(contour[0].X, contour[0].Y)
	for _, p := range contour[1:] {
		r.LineTo(p.X, p.Y)
	}
	r.ClosePath()
}

func traceStroke(r *vector.Rasterizer, line []fpt, width float32) {
	if len(line) < 2 {
		return
	}
	half := width / 2
	for i := 0; i+1 < len(line); i++ {
		quad, ok := strokeQuad(line[i], line[i+1], half)
		if !ok {
			continue
		}
		r.MoveTo(quad[0].X, quad[0].Y)
		r.LineTo(quad[1].X, quad[1].Y)
		r.LineTo(quad[2].X, quad[2].Y)
		r.LineTo(quad[3].X, quad[3].Y)
		r.ClosePath()
	}
	for i := 1; i+1 < len(line); i++ {
		if needsRoundJoin(line[i-1], line[i], line[i+1]) {
			traceCircle(r, line[i], half, 8)
		}
	}
}

func traceCircle(r *vector.Rasterizer, center fpt, radius float32, segments int) {
	if segments < 3 {
		return
	}
	for i := 0; i < segments; i++ {
		p := circlePoint(center, radius, i, segments)
		if i == 0 {
			r.MoveTo(p.X, p.Y)
		} else {
			r.LineTo(p.X, p.Y)
		}
	}
	r.ClosePath()
}
