package render

import (
	"fmt"
	"image"
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Color is a straight-alpha sRGB colour.
type Color struct{ R, G, B, A uint8 }

// LerpColor interpolates two straight-alpha colours channel by channel.
// Alpha travels with the rest so a control can fade in and change hue in one
// transition, which is what a theme crossfade over an appearing panel needs.
// Progress is the eased value; it is clamped here so a reversal that overshoots
// cannot produce a colour outside the pair.
func LerpColor(from, to Color, progress float64) Color {
	p := progress
	if p <= 0 || p != p {
		return from
	}
	if p >= 1 {
		return to
	}
	// Round the result, not the delta, so fading a pair in either direction
	// passes through the same channel values. Both endpoints are uint8 and p is
	// clamped to [0,1], so the result cannot leave the range and wrap.
	lerp := func(a, b uint8) uint8 {
		return uint8(math.Round(float64(a) + (float64(b)-float64(a))*p))
	}
	return Color{R: lerp(from.R, to.R), G: lerp(from.G, to.G), B: lerp(from.B, to.B), A: lerp(from.A, to.A)}
}

// premultiply returns the colour in the canvas's memory order: B, G, R, A.
func (c Color) premultiply() [4]byte {
	a := uint32(c.A)
	scale := func(v uint8) byte { return byte(uint32(v) * a / 255) }
	return [4]byte{scale(c.B), scale(c.G), scale(c.R), c.A}
}

// Canvas is a little-endian premultiplied ARGB8888 buffer, held in memory as
// B, G, R, A bytes. Its coordinates are buffer pixels.
type Canvas struct {
	Pix           []byte
	Width, Height int
	Stride        int
	restrict      ui.Rect
}

// NewCanvas wraps shared-memory bytes after validating the geometry.
func NewCanvas(pix []byte, width, height, stride int) (*Canvas, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("render: canvas %dx%d has a non-positive dimension", width, height)
	}
	row := width * 4
	if row/4 != width {
		return nil, fmt.Errorf("render: canvas width %d overflows a row", width)
	}
	if stride < row {
		return nil, fmt.Errorf("render: stride %d is below the %d-byte row", stride, row)
	}
	need := stride * height
	if need/height != stride {
		return nil, fmt.Errorf("render: canvas %dx%d overflows its buffer size", width, height)
	}
	if len(pix) < need {
		return nil, fmt.Errorf("render: buffer holds %d bytes, need %d", len(pix), need)
	}
	return &Canvas{Pix: pix, Width: width, Height: height, Stride: stride}, nil
}

// clip intersects a rectangle with the canvas bounds.
func (c *Canvas) clip(r ui.Rect) (x0, y0, x1, y1 int) {
	if c.restrict.W > 0 && c.restrict.H > 0 {
		x0n := max(r.X, c.restrict.X)
		y0n := max(r.Y, c.restrict.Y)
		x1n := min(r.X+r.W, c.restrict.X+c.restrict.W)
		y1n := min(r.Y+r.H, c.restrict.Y+c.restrict.H)
		r = ui.Rect{X: x0n, Y: y0n, W: max(x1n-x0n, 0), H: max(y1n-y0n, 0)}
	}
	x0, y0 = max(r.X, 0), max(r.Y, 0)
	x1, y1 = min(r.X+r.W, c.Width), min(r.Y+r.H, c.Height)
	return x0, y0, x1, y1
}

// fillRect blends a colour over a clipped rectangle.
func fillRect(c *Canvas, r ui.Rect, col Color) {
	if col.A == 0 {
		return
	}
	x0, y0, x1, y1 := c.clip(r)
	src := col.premultiply()
	for y := y0; y < y1; y++ {
		row := c.Pix[y*c.Stride:]
		for x := x0; x < x1; x++ {
			blendPixel(row[x*4:x*4+4], src, uint32(col.A))
		}
	}
}

// fillRoundedRect fills one clipped rounded rectangle. Each scanline computes
// its corner inset from the pixel centre, then reuses the rectangle blender.
func fillRoundedRect(c *Canvas, r ui.Rect, radius int, col Color) {
	if r.W <= 0 || r.H <= 0 || col.A == 0 {
		return
	}
	radius = min(radius, min(r.W, r.H)/2)
	if radius <= 0 {
		fillRect(c, r, col)
		return
	}
	for y := 0; y < r.H; y++ {
		inset := roundedInset(y, r.H, radius)
		fillRect(c, ui.Rect{X: r.X + inset, Y: r.Y + y, W: r.W - 2*inset, H: 1}, col)
	}
}

func roundedInset(y, height, radius int) int {
	edgeY := min(y, height-1-y)
	if edgeY >= radius {
		return 0
	}
	radiusSquared := float64(radius) * float64(radius)
	dy := float64(radius-edgeY) - 0.5
	dx := math.Sqrt(max(0, radiusSquared-dy*dy))
	return max(0, int(math.Ceil(float64(radius)-dx-0.5)))
}

// strokeRoundedRect outlines one clipped rounded rectangle inward from its
// bounds, so the stroke never grows the node's box. Each scanline reuses the
// same corner inset the fill uses, then paints the band between the outer edge
// and the inset inner edge; rows above and below the inner rectangle are solid.
// This is the whole stroke surface the catalogue needs -- boundaries and focus
// rings -- not a general path engine.
func strokeRoundedRect(c *Canvas, r ui.Rect, radius, width int, col Color) {
	if r.W <= 0 || r.H <= 0 || width <= 0 || col.A == 0 {
		return
	}
	radius = min(radius, min(r.W, r.H)/2)
	width = min(width, min(r.W, r.H)/2)
	inner := ui.Rect{X: r.X + width, Y: r.Y + width, W: r.W - 2*width, H: r.H - 2*width}
	innerRadius := max(0, radius-width)
	for y := 0; y < r.H; y++ {
		outer := 0
		if radius > 0 {
			outer = roundedInset(y, r.H, radius)
		}
		left, right := r.X+outer, r.X+r.W-outer
		iy := y - width
		if inner.W <= 0 || inner.H <= 0 || iy < 0 || iy >= inner.H {
			fillRect(c, ui.Rect{X: left, Y: r.Y + y, W: right - left, H: 1}, col)
			continue
		}
		gap := 0
		if innerRadius > 0 {
			gap = roundedInset(iy, inner.H, innerRadius)
		}
		il, ir := inner.X+gap, inner.X+inner.W-gap
		fillRect(c, ui.Rect{X: left, Y: r.Y + y, W: max(0, il-left), H: 1}, col)
		fillRect(c, ui.Rect{X: ir, Y: r.Y + y, W: max(0, right-ir), H: 1}, col)
	}
}

// StrokeRounded outlines a rounded rectangle inward from its bounds.
func (c *Canvas) StrokeRounded(r ui.Rect, radius, width int, col Color) {
	strokeRoundedRect(c, r, radius, width, col)
}

// clearOutsideRoundedRect restores transparency after children paint. Child
// bounds may reach a body corner when padding is zero, but the final surface
// silhouette must remain the same rounded rectangle as its background.
func clearOutsideRoundedRect(c *Canvas, r ui.Rect, radius int, attachEdge string) {
	radius = min(radius, min(r.W, r.H)/2)
	for y := 0; y < c.Height; y++ {
		row := c.Pix[y*c.Stride : y*c.Stride+c.Width*4]
		if y < r.Y || y >= r.Y+r.H || r.W <= 0 || r.H <= 0 {
			clear(row)
			continue
		}
		inset := 0
		if radius > 0 {
			ly := y - r.Y
			square := (attachEdge == "top" && ly < radius) || (attachEdge == "bottom" && ly >= r.H-radius)
			if !square {
				inset = roundedInset(ly, r.H, radius)
			}
		}
		x0 := max(0, min(c.Width, r.X+inset))
		x1 := max(x0, min(c.Width, r.X+r.W-inset))
		clear(row[:x0*4])
		clear(row[x1*4:])
	}
}

// blendMask blends a colour through an alpha coverage mask placed at x, y.
func blendMask(c *Canvas, mask *image.Alpha, x, y int, col Color) {
	if col.A == 0 || mask == nil {
		return
	}
	b := mask.Bounds()
	x0, y0, x1, y1 := c.clip(ui.Rect{X: x, Y: y, W: b.Dx(), H: b.Dy()})
	src := col.premultiply()
	for py := y0; py < y1; py++ {
		row := c.Pix[py*c.Stride:]
		for px := x0; px < x1; px++ {
			cov := uint32(mask.AlphaAt(b.Min.X+px-x, b.Min.Y+py-y).A)
			if cov == 0 {
				continue
			}
			alpha := uint32(col.A) * cov / 255
			var s [4]byte
			for i := range src {
				s[i] = byte(uint32(src[i]) * cov / 255)
			}
			blendPixel(row[px*4:px*4+4], s, alpha)
		}
	}
}

// FillRounded fills a rounded rectangle using the cached alpha mask.
func (c *Canvas) FillRounded(r ui.Rect, radius int, col Color) {
	blendMask(c, RoundedMask(radius, r.W, r.H), r.X, r.Y, col)
}

// DrawShadow composites a cached shadow around a panel rectangle.
func (c *Canvas) DrawShadow(r ui.Rect, radius int, e Elevation, col Color) {
	spread := shadowSpread(e)
	blendMask(c, ShadowTexture(r.W, r.H, radius, e), r.X-spread, r.Y-spread, col)
}

// blendPixel composites a premultiplied source over a premultiplied destination.
func blendPixel(dst []byte, src [4]byte, alpha uint32) {
	if alpha == 255 {
		copy(dst, src[:])
		return
	}
	inv := 255 - alpha
	for i := range dst {
		dst[i] = byte(uint32(src[i]) + uint32(dst[i])*inv/255)
	}
}
