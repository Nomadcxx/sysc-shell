package render

import (
	"image"
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

type gradientAxis struct {
	x, y, bias float64
}

type gradientStop struct {
	at float64
	c  Color
}

const degToRad = math.Pi / 180

func gradientAxisForDegrees(angleDeg float64) gradientAxis {
	reduced := math.Remainder(angleDeg, 360)
	x := math.Cos(reduced * degToRad)
	y := math.Sin(reduced * degToRad)
	if math.Abs(x) < 1e-6 {
		x = 0
	}
	if math.Abs(y) < 1e-6 {
		y = 0
	}
	inv := 1 / (math.Abs(x) + math.Abs(y))
	x *= inv
	y *= inv
	return gradientAxis{x: x, y: y, bias: math.Min(0, x) + math.Min(0, y)}
}

func sampleStops(stops []gradientStop, t float64) Color {
	n := len(stops)
	if n == 0 {
		return Color{}
	}
	if t <= stops[0].at {
		return stops[0].c
	}
	last := stops[n-1]
	if t >= last.at {
		return last.c
	}
	for i := 1; i < n; i++ {
		b := stops[i]
		if t > b.at {
			continue
		}
		a := stops[i-1]
		span := b.at - a.at
		p := 0.0
		if span != 0 {
			p = (t - a.at) / span
		}
		return LerpColor(a.c, b.c, p)
	}
	return last.c
}

func sampleGradient(stops []gradientStop, axis gradientAxis, offset, ux, uy float64) Color {
	return sampleStops(stops, ux*axis.x+uy*axis.y-axis.bias-offset)
}

func sampleGradientPeriodic(stops []gradientStop, axis gradientAxis, offset, ux, uy float64) Color {
	t := ux*axis.x + uy*axis.y - axis.bias - offset
	t -= math.Floor(t)
	return sampleStops(stops, t)
}

func fillRectGradient(c *Canvas, r ui.Rect, stops []gradientStop, angle, offset float64, periodic bool) {
	if len(stops) == 0 {
		return
	}
	x0, y0, x1, y1 := c.clip(r)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	axis := gradientAxisForDegrees(angle)
	for y := y0; y < y1; y++ {
		row := c.Pix[y*c.Stride:]
		uy := (float64(y-r.Y) + 0.5) / float64(r.H)
		for x := x0; x < x1; x++ {
			var col Color
			if periodic {
				col = sampleGradientPeriodic(stops, axis, offset, (float64(x-r.X)+0.5)/float64(r.W), uy)
			} else {
				col = sampleGradient(stops, axis, offset, (float64(x-r.X)+0.5)/float64(r.W), uy)
			}
			if col.A == 0 {
				continue
			}
			blendPixel(row[x*4:x*4+4], col.premultiply(), uint32(col.A))
		}
	}
}

func blendMaskGradient(c *Canvas, mask *image.Alpha, x, y int, stops []gradientStop, angle, offset float64, periodic bool) {
	if len(stops) == 0 || mask == nil {
		return
	}
	b := mask.Bounds()
	box := ui.Rect{X: x, Y: y, W: b.Dx(), H: b.Dy()}
	x0, y0, x1, y1 := c.clip(box)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	axis := gradientAxisForDegrees(angle)
	for py := y0; py < y1; py++ {
		row := c.Pix[py*c.Stride:]
		coverage := coverageRow(mask, py, y)
		uy := (float64(py-box.Y) + 0.5) / float64(box.H)
		for px := x0; px < x1; px++ {
			cov := uint32(coverage[px-x])
			if cov == 0 {
				continue
			}
			var col Color
			if periodic {
				col = sampleGradientPeriodic(stops, axis, offset, (float64(px-box.X)+0.5)/float64(box.W), uy)
			} else {
				col = sampleGradient(stops, axis, offset, (float64(px-box.X)+0.5)/float64(box.W), uy)
			}
			src := col.premultiply()
			alpha := uint32(col.A) * cov / 255
			if alpha == 0 {
				continue
			}
			var s [4]byte
			for i := range src {
				s[i] = byte(uint32(src[i]) * cov / 255)
			}
			blendPixel(row[px*4:px*4+4], s, alpha)
		}
	}
}
