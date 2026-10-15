package render

import (
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

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
	// Press is the ripple phase at the press point, zero before a press and
	// spent at one, and PressX and PressY are that point in surface-local
	// pixels.
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
	//
	// These distances are in the *capture's* pixels, which are the surface's
	// divided by the backdrop downsample factor (four today), so the band is
	// about eight logical pixels on screen.
	bevelBand = 2.0
	// bevelScale is the largest inward sample shift the bevel reaches at full
	// bend, in pixels.
	bevelScale = 0.75
	// domeCap bounds the centre magnification, so text over the middle of a
	// surface cannot smear.
	domeCap = 0.06
	// fringeScale is the largest per-channel spread at full fringe, in pixels.
	fringeScale = 0.3
	// pressRingRadius is how far the press ring has travelled at full phase,
	// and pressRingWidth its sigma.
	pressRingRadius = 6.0
	pressRingWidth  = 2.0
	// pressScale is the largest displacement the ring adds, in pixels.
	pressScale = 0.6
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

	if r.Press > 0 && r.Press < 1 {
		dx := float64(x) + 0.5 - float64(r.PressX)
		dy := float64(y) + 0.5 - float64(r.PressY)
		d := math.Hypot(dx, dy)
		if d > 0 {
			ring := pressRingRadius * r.Press
			off := d - ring
			t := math.Exp(-(off * off) / (2 * pressRingWidth * pressRingWidth))
			// The envelope rises and dies with the phase, so the ring is
			// strongest mid-travel and the surface is exactly still once the
			// ripple has finished rather than stepping back on its last frame.
			mag := pressScale * math.Sin(math.Pi*r.Press) * t
			s.DX += dx / d * mag
			s.DY += dy / d * mag
		}
	}
	return s
}

// Refract resamples src where r displaces it: the ground a material surface
// paints is the capture seen through the bevel, not a blurred copy of it.
//
// The result has src's geometry and is a new buffer. A nil source, or a
// refraction with no bending terms, returns src unchanged, so a caller can
// apply it unconditionally. All of r's distances are in src's pixels, which are
// the surface's pixels divided by the backdrop downsample factor.
func Refract(src *ui.Image, r Refraction) *ui.Image {
	if src == nil || (r.Bend <= 0 && r.Dome <= 0) {
		return src
	}
	out := &ui.Image{Width: src.Width, Height: src.Height, Stride: src.Stride,
		Pix: make([]byte, len(src.Pix))}
	for y := 0; y < src.Height; y++ {
		for x := 0; x < src.Width; x++ {
			s := r.Sample(x, y)
			o := y*out.Stride + x*4
			dx, dy := s.DX, s.DY
			if dx == 0 && dy == 0 && s.Fringe == ([3]float64{}) {
				// The flat interior, which is most of a panel: a copy.
				copy(out.Pix[o:o+4], src.Pix[o:o+4])
				continue
			}
			if s.Fringe == ([3]float64{}) {
				// One position for every channel: the pixel's taps are shared
				// rather than repeated per channel.
				samplePixel(src, float64(x)+dx, float64(y)+dy, out.Pix[o:o+4])
				out.Pix[o+3] = src.Pix[o+3]
				continue
			}
			// The fringe spreads along the displacement's direction, or along
			// +x where the bend is zero.
			mag := math.Hypot(dx, dy)
			if mag == 0 {
				dx, dy, mag = 1, 0, 1
			}
			// Pix is BGRA, and Fringe is ordered red, green, blue: the blue
			// channel takes the third offset.
			for ch := 0; ch < 3; ch++ {
				off := s.Fringe[2-ch]
				sx := float64(x) + dx + dx/mag*off
				sy := float64(y) + dy + dy/mag*off
				out.Pix[o+ch] = sampleChannel(src, sx, sy, ch)
			}
			out.Pix[o+3] = src.Pix[o+3]
		}
	}
	return out
}

// samplePixel reads one RGB pixel bilinearly into dst, sharing the weights
// across the channels: three channels at one position cost one set of taps, not
// three.
func samplePixel(src *ui.Image, fx, fy float64, dst []byte) {
	if src == nil || len(dst) < 4 {
		return
	}
	fx, fy, x0, y0, x1, y1, tx, ty := bilinear(src, fx, fy)
	at := func(x, y, ch int) float64 { return float64(src.Pix[y*src.Stride+x*4+ch]) }
	for ch := 0; ch < 3; ch++ {
		top := at(x0, y0, ch)*(1-tx) + at(x1, y0, ch)*tx
		bottom := at(x0, y1, ch)*(1-tx) + at(x1, y1, ch)*tx
		dst[ch] = byte(top*(1-ty) + bottom*ty + 0.5)
	}
}

// bilinear clamps a fractional position and answers the four taps and weights
// every channel shares.
func bilinear(src *ui.Image, fx, fy float64) (cx, cy float64, x0, y0, x1, y1 int, tx, ty float64) {
	if fx < 0 {
		fx = 0
	}
	if fy < 0 {
		fy = 0
	}
	maxX, maxY := float64(src.Width-1), float64(src.Height-1)
	if fx > maxX {
		fx = maxX
	}
	if fy > maxY {
		fy = maxY
	}
	x0, y0 = int(fx), int(fy)
	x1, y1 = x0+1, y0+1
	if x1 > src.Width-1 {
		x1 = src.Width - 1
	}
	if y1 > src.Height-1 {
		y1 = src.Height - 1
	}
	return fx, fy, x0, y0, x1, y1, fx - float64(x0), fy - float64(y0)
}

// sampleChannel reads one channel of src at a fractional position, bilinearly
// and clamped to the edge. Edges clamp rather than wrap for the same reason the
// blur's do: a wrapped sample bleeds the opposite side of the capture in.
func sampleChannel(src *ui.Image, fx, fy float64, ch int) byte {
	if src == nil || src.Width <= 0 || src.Height <= 0 {
		return 0
	}
	if fx < 0 {
		fx = 0
	}
	if fy < 0 {
		fy = 0
	}
	maxX, maxY := float64(src.Width-1), float64(src.Height-1)
	if fx > maxX {
		fx = maxX
	}
	if fy > maxY {
		fy = maxY
	}
	x0, y0 := int(fx), int(fy)
	x1, y1 := x0+1, y0+1
	if x1 > src.Width-1 {
		x1 = src.Width - 1
	}
	if y1 > src.Height-1 {
		y1 = src.Height - 1
	}
	tx, ty := fx-float64(x0), fy-float64(y0)
	at := func(x, y int) float64 {
		return float64(src.Pix[y*src.Stride+x*4+ch])
	}
	top := at(x0, y0)*(1-tx) + at(x1, y0)*tx
	bottom := at(x0, y1)*(1-tx) + at(x1, y1)*tx
	return byte(top*(1-ty) + bottom*ty + 0.5)
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
