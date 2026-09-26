package render

import (
	"image"
	"image/color"
	"math"
	"sync"
)

type maskKey struct {
	radius, w, h int
	square       Corners
}

// Corners marks corners of a rounded rectangle that stay square.
type Corners uint8

const (
	SquareTL Corners = 1 << iota
	SquareTR
	SquareBL
	SquareBR
)

type ringKey struct{ radius, w, h, width int }

type Elevation int

const (
	ElevPanel Elevation = iota
	ElevMenu
)

type shadowKey struct {
	w, h, radius int
	e            Elevation
}

var (
	maskMu  sync.Mutex
	masks   = map[maskKey]*image.Alpha{}
	rings   = map[ringKey]*image.Alpha{}
	shadows = map[shadowKey]*image.Alpha{}
)

// RoundedMask returns a cached antialiased rounded-rectangle alpha mask.
func RoundedMask(radius, w, h int) *image.Alpha { return CornerMask(radius, w, h, 0) }

// CornerMask is RoundedMask with the marked corners left square. A surface
// joined to something along an edge fills through it in one pass: squaring a
// rounded fill afterwards blends twice where the two overlap, which on a
// translucent fill paints a denser band.
func CornerMask(radius, w, h int, square Corners) *image.Alpha {
	key := maskKey{radius, w, h, square}
	maskMu.Lock()
	defer maskMu.Unlock()
	if mask := masks[key]; mask != nil {
		return mask
	}
	mask := image.NewAlpha(image.Rect(0, 0, max(w, 0), max(h, 0)))
	if w > 0 && h > 0 {
		radius = min(radius, min(w, h)/2)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				a := roundedCoverage(radius, w, h, x, y)
				if square&quadrant(x, y, w, h) != 0 {
					a = 255
				}
				mask.SetAlpha(x, y, color.Alpha{A: a})
			}
		}
	}
	masks[key] = mask
	return mask
}

// RingMask returns a cached antialiased band of the given width, drawn inward
// from a rounded rectangle's bounds so the stroke never grows the box.
//
// It is the difference of two coverage fields rather than two filled shapes.
// Stacking one fill on another leaves the border as the arithmetic difference
// of two independently rounded silhouettes, which thins and breaks up around
// the corners; sampling both distances per pixel keeps the band a constant
// width the whole way round.
func RingMask(radius, w, h, width int) *image.Alpha {
	key := ringKey{radius, w, h, width}
	maskMu.Lock()
	defer maskMu.Unlock()
	if mask := rings[key]; mask != nil {
		return mask
	}
	mask := image.NewAlpha(image.Rect(0, 0, max(w, 0), max(h, 0)))
	if w > 0 && h > 0 && width > 0 {
		radius = min(radius, min(w, h)/2)
		width = min(width, min(w, h)/2)
		iw, ih := w-2*width, h-2*width
		ir := max(radius-width, 0)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				outer := uint32(roundedCoverage(radius, w, h, x, y))
				if outer == 0 {
					continue
				}
				// The bounds test is the caller's job: roundedCoverage
				// answers 255 for a zero radius without looking at the
				// point, so a square ring would cancel itself out.
				ix, iy := x-width, y-width
				var inner uint32
				if iw > 0 && ih > 0 && ix >= 0 && ix < iw && iy >= 0 && iy < ih {
					inner = uint32(roundedCoverage(ir, iw, ih, ix, iy))
				}
				if inner >= outer {
					continue
				}
				mask.SetAlpha(x, y, color.Alpha{A: uint8(outer - inner)})
			}
		}
	}
	rings[key] = mask
	return mask
}

// quadrant is the corner of a w x h box that pixel (x, y) lies nearest.
func quadrant(x, y, w, h int) Corners {
	left, top := 2*x+1 <= w, 2*y+1 <= h
	switch {
	case top && left:
		return SquareTL
	case top:
		return SquareTR
	case left:
		return SquareBL
	default:
		return SquareBR
	}
}

func roundedCoverage(radius, w, h, x, y int) uint8 {
	if radius <= 0 {
		return 255
	}
	px := math.Abs(float64(x)+0.5-float64(w)/2) - float64(w/2-radius)
	py := math.Abs(float64(y)+0.5-float64(h)/2) - float64(h/2-radius)
	outside := math.Hypot(max(px, 0.0), max(py, 0.0))
	distance := outside + min(max(px, py), 0.0) - float64(radius)
	coverage := min(max(0.5-distance, 0.0), 1.0)
	return uint8(coverage * 255)
}

// ShadowTexture returns a cached pre-blurred shadow texture.
// ponytail: two elevations and cached sizes; revisit only if memory or variety demands it.
func ShadowTexture(w, h, radius int, e Elevation) *image.Alpha {
	key := shadowKey{w, h, radius, e}
	maskMu.Lock()
	defer maskMu.Unlock()
	if shadow := shadows[key]; shadow != nil {
		return shadow
	}
	spread, strength := 12, 0.55
	if e == ElevMenu {
		spread, strength = 8, 0.45
	}
	texture := image.NewAlpha(image.Rect(0, 0, max(w+2*spread, 0), max(h+2*spread, 0)))
	if w > 0 && h > 0 {
		inner := roundedMaskWithoutCache(radius, w, h)
		for y := 0; y < h; y++ {
			copy(texture.Pix[(y+spread)*texture.Stride+spread:], inner.Pix[y*inner.Stride:(y+1)*inner.Stride])
		}
		for pass := 0; pass < 3; pass++ {
			texture = blurAlpha(texture, max(spread/3, 1))
		}
		for i := range texture.Pix {
			texture.Pix[i] = uint8(float64(texture.Pix[i]) * strength)
		}
	}
	shadows[key] = texture
	return texture
}

func roundedMaskWithoutCache(radius, w, h int) *image.Alpha {
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	radius = min(radius, min(w, h)/2)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			mask.SetAlpha(x, y, color.Alpha{A: roundedCoverage(radius, w, h, x, y)})
		}
	}
	return mask
}

func blurAlpha(src *image.Alpha, radius int) *image.Alpha {
	dst := image.NewAlpha(src.Bounds())
	for y := 0; y < src.Rect.Dy(); y++ {
		for x := 0; x < src.Rect.Dx(); x++ {
			var sum, count int
			for ox := max(0, x-radius); ox <= min(src.Rect.Dx()-1, x+radius); ox++ {
				sum += int(src.AlphaAt(ox, y).A)
				count++
			}
			dst.SetAlpha(x, y, color.Alpha{A: uint8(sum / count)})
		}
	}
	final := image.NewAlpha(src.Bounds())
	for y := 0; y < src.Rect.Dy(); y++ {
		for x := 0; x < src.Rect.Dx(); x++ {
			var sum, count int
			for oy := max(0, y-radius); oy <= min(src.Rect.Dy()-1, y+radius); oy++ {
				sum += int(dst.AlphaAt(x, oy).A)
				count++
			}
			final.SetAlpha(x, y, color.Alpha{A: uint8(sum / count)})
		}
	}
	return final
}

func shadowSpread(e Elevation) int {
	if e == ElevMenu {
		return 8
	}
	return 12
}

// glyphShape discriminates the drawn chrome glyphs sharing the mask cache.
// Without it a clear cross and a magnifier of the same size and stroke would
// answer to the same key and whichever was rasterised first would win.
type glyphShape uint8

const (
	glyphSearch glyphShape = iota
	glyphClear
)

type glyphKey struct {
	shape        glyphShape
	size, stroke int
}

var glyphs = map[glyphKey]*image.Alpha{}

// SearchGlyphMask returns a cached antialiased magnifier: a lens ring and a
// handle, both drawn as distance fields so the stroke keeps its width all the
// way round and the diagonal does not stair-step.
//
// It replaces a filled square with a smaller square punched out of it in the
// well colour, plus a staircase of 3x3 blocks for the handle. That could not
// antialias, and the punch-out silently assumed the well was flat behind the
// glyph, so it broke as soon as the field carried anything else.
//
// Proportions follow Material's search icon in a 24-unit box: a lens of radius
// 6.5 centred at (10.5, 10.5) and a handle running to (20.5, 20.5).
func SearchGlyphMask(size, stroke int) *image.Alpha {
	key := glyphKey{glyphSearch, size, stroke}
	maskMu.Lock()
	defer maskMu.Unlock()
	if mask := glyphs[key]; mask != nil {
		return mask
	}
	mask := image.NewAlpha(image.Rect(0, 0, max(size, 0), max(size, 0)))
	if size > 0 && stroke > 0 {
		u := float64(size) / 24
		cx, cy, r := 10.5*u, 10.5*u, 6.5*u
		// The handle starts on the lens edge so the join is solid rather than
		// a gap the antialiasing has to bridge.
		d := r / math.Sqrt2
		x0, y0 := cx+d, cy+d
		x1, y1 := 20.5*u, 20.5*u
		half := float64(stroke) / 2
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				px, py := float64(x)+0.5, float64(y)+0.5
				lens := math.Abs(math.Hypot(px-cx, py-cy) - r)
				edge := math.Min(lens, segmentDistance(px, py, x0, y0, x1, y1))
				coverage := min(max(0.5-(edge-half), 0.0), 1.0)
				if coverage <= 0 {
					continue
				}
				mask.SetAlpha(x, y, color.Alpha{A: uint8(coverage * 255)})
			}
		}
	}
	glyphs[key] = mask
	return mask
}

// ClearGlyphMask returns a cached antialiased cross: two strokes across the
// box, drawn as distance fields for the same reason the magnifier is. It is
// the trailing affordance of a search well that already holds a query.
//
// Proportions follow Material's close icon in a 24-unit box, whose arms run
// corner to corner between 6.5 and 17.5 on both axes. Drawn rather than
// rasterised from close.svg: there is no SVG path on this side yet, and a
// cross is two segments.
func ClearGlyphMask(size, stroke int) *image.Alpha {
	key := glyphKey{glyphClear, size, stroke}
	maskMu.Lock()
	defer maskMu.Unlock()
	if mask := glyphs[key]; mask != nil {
		return mask
	}
	mask := image.NewAlpha(image.Rect(0, 0, max(size, 0), max(size, 0)))
	if size > 0 && stroke > 0 {
		u := float64(size) / 24
		lo, hi := 6.5*u, 17.5*u
		half := float64(stroke) / 2
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				px, py := float64(x)+0.5, float64(y)+0.5
				edge := math.Min(
					segmentDistance(px, py, lo, lo, hi, hi),
					segmentDistance(px, py, hi, lo, lo, hi))
				coverage := min(max(0.5-(edge-half), 0.0), 1.0)
				if coverage <= 0 {
					continue
				}
				mask.SetAlpha(x, y, color.Alpha{A: uint8(coverage * 255)})
			}
		}
	}
	glyphs[key] = mask
	return mask
}

// segmentDistance is the distance from a point to a line segment.
func segmentDistance(px, py, x0, y0, x1, y1 float64) float64 {
	dx, dy := x1-x0, y1-y0
	length := dx*dx + dy*dy
	if length == 0 {
		return math.Hypot(px-x0, py-y0)
	}
	t := min(max(((px-x0)*dx+(py-y0)*dy)/length, 0), 1)
	return math.Hypot(px-(x0+t*dx), py-(y0+t*dy))
}
