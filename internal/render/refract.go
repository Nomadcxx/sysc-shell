package render

import "math"

// Refraction is the resolved refraction of one material surface: how far the
// bevel bends the sample coordinate, how much the centre magnifies, how wide
// the chromatic spread is, and where a press ring sits.
//
// Every term is a pure function of the destination pixel and this value, so the
// mapping is testable without an image and the composite is a resample.
type Refraction struct {
	W, H   int
	Radius int
	// Bend scales the bevel's effect on the sample coordinate, Dome the centre
	// magnification, and Fringe the chromatic spread. All are 0..1; zero is no
	// refraction at all.
	Bend, Dome, Fringe float64
	// Press is the ripple phase at the press point, zero at rest, and PressX and
	// PressY are that point in surface-local pixels.
	Press          float64
	PressX, PressY int
}

// Sample is where a destination pixel reads from: DX and DY are the source
// offset in pixels, and Fringe is the extra offset each channel takes along the
// same direction — index 0 red, 1 green, 2 blue — also in pixels.
type Sample struct {
	DX, DY float64
	Fringe [3]float64
}

const (
	// bevelBand is how far inside the edge the bevel profile reaches, in
	// pixels: wide enough to read as thickness, narrow enough that a panel's
	// interior is untouched.
	bevelBand = 8.0
	// bevelScale is the largest inward sample shift the bevel reaches at full
	// bend, in pixels.
	bevelScale = 3.0
	// domeCap bounds the centre magnification, so text over the middle of a
	// surface cannot smear.
	domeCap = 0.06
	// fringeScale is the largest per-channel spread at full fringe, in pixels.
	fringeScale = 1.2
	// pressRingRadius is how far the press ring has travelled at full phase,
	// and pressRingWidth its sigma.
	pressRingRadius = 24.0
	pressRingWidth  = 8.0
	// pressScale is the largest displacement the ring adds, in pixels.
	pressScale = 2.5
)

// Sample answers where a destination pixel reads from.
//
// Three terms compose: the dome magnifies toward the centre, the bevel bends
// toward the middle along the edge normal, and a press ring displaces outward
// around the press point. A pixel in the flat interior is the identity.
func (r Refraction) Sample(x, y int) Sample {
	var s Sample
	if r.W <= 0 || r.H <= 0 {
		return s
	}
	cx, cy := float64(r.W)/2, float64(r.H)/2
	px, py := float64(x)+0.5-cx, float64(y)+0.5-cy
	radial := math.Hypot(px, py)

	if r.Dome > 0 && radial > 0 {
		half := math.Min(cx, cy)
		if half > 0 {
			// Magnification reads nearer the centre, not further out: the
			// content behind the middle of a dome appears larger.
			f := r.Dome * domeCap * (1 - math.Min(radial/half, 1))
			s.DX -= px * f
			s.DY -= py * f
		}
	}

	if r.Bend > 0 || r.Fringe > 0 {
		inset := r.inset(x, y)
		if inset < bevelBand {
			// The profile is steepest at the edge and fades to nothing at the
			// band's inner limit; squaring makes the steepening visible rather
			// than linear.
			t := 1 - math.Max(inset, 0)/bevelBand
			if radial > 0 {
				mag := r.Bend * bevelScale * t * t
				s.DX -= px / radial * mag
				s.DY -= py / radial * mag
			}
			if r.Fringe > 0 {
				spread := r.Fringe * fringeScale * t
				// Ordered red to blue along the bend, which is what reads as a
				// fringe rather than a blur.
				s.Fringe = [3]float64{spread, 0, -spread}
			}
		}
	}

	if r.Press > 0 {
		dx := float64(x) + 0.5 - float64(r.PressX)
		dy := float64(y) + 0.5 - float64(r.PressY)
		d := math.Hypot(dx, dy)
		if d > 0 {
			ring := pressRingRadius * r.Press
			off := d - ring
			t := math.Exp(-(off * off) / (2 * pressRingWidth * pressRingWidth))
			mag := pressScale * r.Press * t
			s.DX += dx / d * mag
			s.DY += dy / d * mag
		}
	}
	return s
}

// inset is how far a pixel sits inside the rounded rect's edge, in pixels. It
// uses the same signed distance the corner mask does, so the bevel and the
// silhouette it is drawn on agree.
func (r Refraction) inset(x, y int) float64 {
	radius := float64(r.Radius)
	if radius < 0 {
		radius = 0
	}
	px := math.Abs(float64(x)+0.5-float64(r.W)/2) - float64(r.W)/2 + radius
	py := math.Abs(float64(y)+0.5-float64(r.H)/2) - float64(r.H)/2 + radius
	outside := math.Hypot(math.Max(px, 0), math.Max(py, 0))
	return -(outside + math.Min(math.Max(px, py), 0) - radius)
}
