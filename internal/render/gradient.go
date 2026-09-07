package render

import "math"

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
