package render

import (
	"fmt"
	"image"
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Color is a straight-alpha sRGB colour.
type Color struct{ R, G, B, A uint8 }

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
	radiusSquared := float64(radius) * float64(radius)
	for y := 0; y < r.H; y++ {
		edgeY := min(y, r.H-1-y)
		inset := 0
		if edgeY < radius {
			dy := float64(radius-edgeY) - 0.5
			dx := math.Sqrt(max(0, radiusSquared-dy*dy))
			inset = max(0, int(math.Ceil(float64(radius)-dx-0.5)))
		}
		fillRect(c, ui.Rect{X: r.X + inset, Y: r.Y + y, W: r.W - 2*inset, H: 1}, col)
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
