package render

import (
	"encoding/binary"
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
//
// A canvas may cover only part of the surface it paints, which is what a
// group-opacity node needs: the offscreen it composites through is sized to the
// node's own clipped box rather than to the whole surface. Ox and Oy place that
// box on the surface, so every painter keeps working in absolute surface
// coordinates and only the byte offset into Pix is relative.
type Canvas struct {
	Pix           []byte
	Width, Height int
	Stride        int
	// Ox, Oy are the surface coordinates of Pix's first pixel.
	Ox, Oy   int
	restrict ui.Rect
}

// extent is the rectangle the buffer covers in surface coordinates.
func (c *Canvas) extent() ui.Rect {
	return ui.Rect{X: c.Ox, Y: c.Oy, W: c.Width, H: c.Height}
}

// contains reports whether a surface pixel lies inside the buffer.
func (c *Canvas) contains(x, y int) bool {
	return x >= c.Ox && x < c.Ox+c.Width && y >= c.Oy && y < c.Oy+c.Height
}

// at is the byte offset of surface pixel (x, y). The caller has checked that
// the pixel is inside the buffer; at does not.
func (c *Canvas) at(x, y int) int {
	return (y-c.Oy)*c.Stride + (x-c.Ox)*4
}

// row is the buffer from the first pixel of surface row y to the end. The
// caller has checked that y is inside the buffer; the returned slice is indexed
// by x-Ox.
func (c *Canvas) row(y int) []byte {
	return c.Pix[(y-c.Oy)*c.Stride:]
}

// ApplySurfaceTransform applies the final opacity and X/Y translation to an
// already-premultiplied frame. copy handles overlap, so translation reuses the
// compositor buffer and clears only the pixels it exposes.
func (c *Canvas) ApplySurfaceTransform(opacity float64, translateX, translateY int) {
	if c == nil || c.Width <= 0 || c.Height <= 0 || c.Stride <= 0 {
		return
	}
	pix := c.Pix[:c.Stride*c.Height]
	rowBytes := c.Width * 4
	clearPixels := func() {
		for y := 0; y < c.Height; y++ {
			clear(pix[y*c.Stride : y*c.Stride+rowBytes])
		}
	}
	if translateY >= c.Height || translateY <= -c.Height {
		clearPixels()
	} else if translateY > 0 {
		for y := c.Height - 1; y >= translateY; y-- {
			copy(pix[y*c.Stride:y*c.Stride+rowBytes], pix[(y-translateY)*c.Stride:(y-translateY)*c.Stride+rowBytes])
		}
		for y := 0; y < translateY; y++ {
			clear(pix[y*c.Stride : y*c.Stride+rowBytes])
		}
	} else if translateY < 0 {
		rows := -translateY
		for y := 0; y < c.Height-rows; y++ {
			copy(pix[y*c.Stride:y*c.Stride+rowBytes], pix[(y+rows)*c.Stride:(y+rows)*c.Stride+rowBytes])
		}
		for y := c.Height - rows; y < c.Height; y++ {
			clear(pix[y*c.Stride : y*c.Stride+rowBytes])
		}
	}
	if translateX >= c.Width || translateX <= -c.Width {
		clearPixels()
	} else if translateX > 0 {
		offset := translateX * 4
		for y := 0; y < c.Height; y++ {
			row := pix[y*c.Stride : y*c.Stride+rowBytes]
			copy(row[offset:], row[:rowBytes-offset])
			clear(row[:offset])
		}
	} else if translateX < 0 {
		shift := -translateX
		offset := shift * 4
		for y := 0; y < c.Height; y++ {
			row := pix[y*c.Stride : y*c.Stride+rowBytes]
			copy(row[:rowBytes-offset], row[offset:])
			clear(row[rowBytes-offset:])
		}
	}
	if opacity >= 1 {
		return
	}
	if opacity <= 0 || math.IsNaN(opacity) {
		clearPixels()
		return
	}
	for y := 0; y < c.Height; y++ {
		row := pix[y*c.Stride : y*c.Stride+rowBytes]
		for i := range row {
			row[i] = byte(math.Round(float64(row[i]) * opacity))
		}
	}
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

// clip intersects a rectangle with the canvas bounds. The result is in surface
// coordinates, so callers keep painting in absolute space whichever part of the
// surface this buffer covers.
func (c *Canvas) clip(r ui.Rect) (x0, y0, x1, y1 int) {
	if c.restrict.W > 0 && c.restrict.H > 0 {
		x0n := max(r.X, c.restrict.X)
		y0n := max(r.Y, c.restrict.Y)
		x1n := min(r.X+r.W, c.restrict.X+c.restrict.W)
		y1n := min(r.Y+r.H, c.restrict.Y+c.restrict.H)
		r = ui.Rect{X: x0n, Y: y0n, W: max(x1n-x0n, 0), H: max(y1n-y0n, 0)}
	}
	x0, y0 = max(r.X, c.Ox), max(r.Y, c.Oy)
	x1, y1 = min(r.X+r.W, c.Ox+c.Width), min(r.Y+r.H, c.Oy+c.Height)
	return x0, y0, x1, y1
}

// fillRect blends a colour over a clipped rectangle. blendFill routes a masked
// fill's full-coverage interior through it, so the loop works on raw channel
// runs rather than forming a destination slice per pixel: the panel body is
// 2240x1640 at scale 2, and the slice arithmetic and the blend's branch cost a
// third of a Settings frame on their own (GH #455).
//
// A solid colour makes the over blend an affine map of each destination byte,
// so the map is built once as four 256-entry tables -- one per channel -- and
// a pixel becomes four lookups. Each entry is the exact expression the per-byte
// loop used, byte(uint32(src[ch]) + uint32(dst)*inv/255), so the output is
// unchanged. A run that is already clear takes the colour outright, which is
// what the over blend computes for an empty destination, and a run of full
// alpha always does. The tables are built on the first run that needs them.
func fillRect(c *Canvas, r ui.Rect, col Color) {
	if col.A == 0 {
		return
	}
	x0, y0, x1, y1 := c.clip(r)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	src := col.premultiply()
	n := (x1 - x0) * 4
	inv := uint32(255 - col.A)
	var tile [64]byte
	for i := range tile {
		tile[i] = src[i&3]
	}
	var lut [4][256]byte
	lutReady := false
	for y := y0; y < y1; y++ {
		row := c.row(y)[(x0-c.Ox)*4:]
		row = row[:n]
		if col.A == 255 || clearRun(row) {
			for i := 0; i < n; i += len(tile) {
				copy(row[i:], tile[:])
			}
			continue
		}
		if !lutReady {
			for ch := range lut {
				s := uint32(src[ch])
				for d := range lut[ch] {
					lut[ch][d] = byte(s + uint32(d)*inv/255)
				}
			}
			lutReady = true
		}
		for i := 0; i < n; i += 4 {
			row[i] = lut[0][row[i]]
			row[i+1] = lut[1][row[i+1]]
			row[i+2] = lut[2][row[i+2]]
			row[i+3] = lut[3][row[i+3]]
		}
	}
}

// clearRun reports whether every channel of a destination run is zero. It
// reads the run 64 bits at a time: every row of every solid fill tests its
// destination here, so the check has to cost well under the blend it skips.
func clearRun(dst []byte) bool {
	for len(dst) >= 8 {
		if binary.LittleEndian.Uint64(dst) != 0 {
			return false
		}
		dst = dst[8:]
	}
	for _, v := range dst {
		if v != 0 {
			return false
		}
	}
	return true
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
		inset := ui.RoundedInset(y, r.H, radius)
		fillRect(c, ui.Rect{X: r.X + inset, Y: r.Y + y, W: r.W - 2*inset, H: 1}, col)
	}
}

// blendFill blends a rounded-rectangle fill mask over its own box, visiting
// only the frame a fill can antialias.
//
// A fill's coverage dips only near the silhouette, and a pixel whose centre
// sits inside the corner arcs' inner rectangle is half a pixel or more past
// every edge, where the coverage field saturates at 255. The frame fillBands
// measures carries everything else -- the arcs, their seams with the straight
// edges, and the half-pixel fringe the mask draws along odd edges. It blends
// through the mask; the interior is blended at full coverage and never reads a
// coverage byte. Before this the whole box went through blendMaskAt: the panel
// body is 2240x1640 at scale 2, and the per-pixel coverage load, bounds check
// and slice formation cost 32.5 ms of a 33.5 ms Settings frame (GH #455). The
// four regions are disjoint, so no pixel blends twice, and the interior's
// fillRect is the same arithmetic blendMaskAt performs for a coverage of 255,
// so the output is unchanged.
func blendFill(c *Canvas, r ui.Rect, radius int, square Corners, col Color) {
	if r.W <= 0 || r.H <= 0 || col.A == 0 {
		return
	}
	top := fillBands(radius, r.W, r.H)
	if top <= 1 {
		// A zero radius fills every pixel of its mask, so the mask buys
		// nothing: a whole-surface wash can go straight to the rectangle.
		fillRect(c, r, col)
		return
	}
	if 2*top >= r.H || 2*top >= r.W {
		// The frame would meet in the middle: a capsule or a small control has
		// no interior to fill. The mask covers only what the clip can see: a
		// frame repaints part of a large surface, and a mask for the whole
		// surface can be too big for the geometry cache to hold (GH #454).
		x0, y0, x1, y1 := c.clip(r)
		if x0 >= x1 || y0 >= y1 {
			return
		}
		mask := CornerMaskIn(radius, r.W, r.H, square, image.Rect(x0-r.X, y0-r.Y, x1-r.X, y1-r.Y))
		blendMask(c, mask, x0, y0, col)
		return
	}
	midH := r.H - 2*top
	blendFillBand(c, r, radius, square, col, ui.Rect{W: r.W, H: top})
	blendFillBand(c, r, radius, square, col, ui.Rect{Y: r.H - top, W: r.W, H: top})
	blendFillBand(c, r, radius, square, col, ui.Rect{Y: top, W: top, H: midH})
	blendFillBand(c, r, radius, square, col, ui.Rect{X: r.W - top, Y: top, W: top, H: midH})
	fillRect(c, ui.Rect{X: r.X + top, Y: r.Y + top, W: r.W - 2*top, H: midH}, col)
}

// blendFillBand blends one frame band of a rounded fill through a mask built
// for that band alone. The band is a fixed region of the box, so its mask is
// one geometry-cache entry however far the surface scrolls, while a tall
// card's whole mask runs to tens of megabytes that the cache refuses and the
// frame rebuilds (GH #454). The regions blendFill visits stay disjoint, so no
// pixel blends twice.
func blendFillBand(c *Canvas, r ui.Rect, radius int, square Corners, col Color, band ui.Rect) {
	if x0, y0, x1, y1 := c.clip(ui.Rect{X: r.X + band.X, Y: r.Y + band.Y, W: band.W, H: band.H}); x0 >= x1 || y0 >= y1 {
		return
	}
	mask := CornerMaskIn(radius, r.W, r.H, square, image.Rect(band.X, band.Y, band.X+band.W, band.Y+band.H))
	blendMask(c, mask, r.X+band.X, r.Y+band.Y, col)
}

// blendRing blends a ring mask over a clipped rounded rectangle's own box,
// visiting only the bands a ring can reach.
//
// A ring of width width and corner radius radius is empty everywhere except its
// top and bottom rows, and, between them, its left and right columns, so the
// rectangle's interior never has to be scanned. Before this, every pixel of the
// box was loaded, tested and skipped: the panel root rim strokes the whole
// surface, so each panel frame paid about 20 percent of its time on coverage
// that is zero (GH #373). The four regions are disjoint, so no pixel blends
// twice.
func blendRing(c *Canvas, r ui.Rect, radius, width int, col Color) {
	if r.W <= 0 || r.H <= 0 || width <= 0 || col.A == 0 {
		return
	}
	top, side := strokeBands(radius, r.W, r.H, width)
	if 2*top >= r.H || 2*side >= r.W {
		// The bands would meet in the middle: a thin or short rectangle has no
		// empty interior to skip. The mask covers only what the clip can see,
		// for the same reason as the fill (GH #454).
		x0, y0, x1, y1 := c.clip(r)
		if x0 >= x1 || y0 >= y1 {
			return
		}
		mask := RingMaskIn(radius, r.W, r.H, width, image.Rect(x0-r.X, y0-r.Y, x1-r.X, y1-r.Y))
		blendMask(c, mask, x0, y0, col)
		return
	}
	midH := r.H - 2*top
	blendRingBand(c, r, radius, width, col, ui.Rect{W: r.W, H: top})
	blendRingBand(c, r, radius, width, col, ui.Rect{Y: r.H - top, W: r.W, H: top})
	blendRingBand(c, r, radius, width, col, ui.Rect{Y: top, W: side, H: midH})
	blendRingBand(c, r, radius, width, col, ui.Rect{X: r.W - side, Y: top, W: side, H: midH})
}

// blendRingBand blends one band of a ring through a mask built for that band
// alone, with the same geometry-cache argument as blendFillBand.
func blendRingBand(c *Canvas, r ui.Rect, radius, width int, col Color, band ui.Rect) {
	if x0, y0, x1, y1 := c.clip(ui.Rect{X: r.X + band.X, Y: r.Y + band.Y, W: band.W, H: band.H}); x0 >= x1 || y0 >= y1 {
		return
	}
	mask := RingMaskIn(radius, r.W, r.H, width, image.Rect(band.X, band.Y, band.X+band.W, band.Y+band.H))
	blendMask(c, mask, r.X+band.X, r.Y+band.Y, col)
}

// strokeRoundedRect outlines one clipped rounded rectangle inward from its
// bounds, so the stroke never grows the node's box. It draws the cached ring
// mask, which carries the same antialiased coverage the rounded fill uses.
//
// This was a pair of scanline fills whose insets were rounded to whole pixels.
// A wide band survives that, but a hairline border does not: the quantised
// corner reads as a staircase, and the eye sees a broken line rather than a
// thin one.
func strokeRoundedRect(c *Canvas, r ui.Rect, radius, width int, col Color) {
	blendRing(c, r, radius, width, col)
}

// col is the buffer from surface pixel (x, Oy) to the end. The caller has
// checked that x is inside the buffer; the returned slice is indexed by y-Oy.
func (c *Canvas) col(x int) []byte {
	return c.Pix[(x-c.Ox)*4:]
}

// clearOutsideRoundedRect restores transparency after children paint. Child
// bounds may reach a body corner when padding is zero, but the final surface
// silhouette must remain the body's: rounded where square does not mark a
// corner, widened beside the attached edge by each side's joint. Rows past the
// body are emptied.
func clearOutsideRoundedRect(c *Canvas, r ui.Rect, radius int, square Corners, jointL, jointR int, attachEdge string) {
	radius = min(radius, min(r.W, r.H)/2)
	clipX0, clipY0, clipX1, clipY1 := c.clip(c.extent())
	if clipX0 >= clipX1 || clipY0 >= clipY1 {
		return
	}
	if attachEdge == "left" || attachEdge == "right" {
		for x := clipX0; x < clipX1; x++ {
			col := c.col(x)
			if x < r.X || x >= r.X+r.W || r.W <= 0 || r.H <= 0 {
				for y := clipY0; y < clipY1; y++ {
					clear(col[(y-c.Oy)*c.Stride : (y-c.Oy)*c.Stride+4])
				}
				continue
			}
			local, near := x-r.X, x-r.X
			if attachEdge == "right" {
				near = r.X + r.W - 1 - x
			}
			topCorner, bottomCorner := SquareTL, SquareBL
			if local > r.W-1-local {
				topCorner, bottomCorner = SquareTR, SquareBR
			}
			insetTop, insetBottom := 0, 0
			if radius > 0 {
				if square&topCorner == 0 {
					insetTop = ui.RoundedInset(local, r.W, radius)
				}
				if square&bottomCorner == 0 {
					insetBottom = ui.RoundedInset(local, r.W, radius)
				}
			}
			extTop, extBottom := ui.FilletExtent(near, jointL), ui.FilletExtent(near, jointR)
			y0 := max(c.Oy, r.Y+insetTop-extTop)
			y1 := min(c.Oy+c.Height, r.Y+r.H-insetBottom+extBottom)
			for y := clipY0; y < min(y0, clipY1); y++ {
				clear(col[(y-c.Oy)*c.Stride : (y-c.Oy)*c.Stride+4])
			}
			for y := max(y1, clipY0); y < clipY1; y++ {
				clear(col[(y-c.Oy)*c.Stride : (y-c.Oy)*c.Stride+4])
			}
		}
		return
	}
	for y := clipY0; y < clipY1; y++ {
		rowStart := (y-c.Oy)*c.Stride + (clipX0-c.Ox)*4
		row := c.Pix[rowStart : rowStart+(clipX1-clipX0)*4]
		if y < r.Y || y >= r.Y+r.H || r.W <= 0 || r.H <= 0 {
			clear(row)
			continue
		}
		ly := y - r.Y
		near := ly
		if attachEdge == "bottom" {
			near = r.H - 1 - ly
		}
		extL, extR := 0, 0
		if attachEdge == "top" || attachEdge == "bottom" {
			extL, extR = ui.FilletExtent(near, jointL), ui.FilletExtent(near, jointR)
		}
		insetL, insetR := 0, 0
		if radius > 0 {
			inset := ui.RoundedInset(ly, r.H, radius)
			left, right := SquareTL, SquareTR
			if ly > r.H-1-ly {
				left, right = SquareBL, SquareBR
			}
			if square&left == 0 {
				insetL = inset
			}
			if square&right == 0 {
				insetR = inset
			}
		}
		x0 := max(clipX0, min(c.Ox+c.Width, r.X+insetL-extL))
		x1 := max(x0, min(clipX1, r.X+r.W-insetR+extR))
		clear(row[:(x0-clipX0)*4])
		clear(row[(x1-clipX0)*4:])
	}
}

// fillAttachFillets paints the concave wedges joining a panel to the bar, one
// outside each side edge of the body at its own radius, tapering over the rows
// beside the attached edge.
func fillAttachFillets(c *Canvas, r ui.Rect, left, right int, attachEdge string, col Color) {
	if col.A == 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	if attachEdge == "left" || attachEdge == "right" {
		for depth := 0; depth < left && depth < r.W; depth++ {
			x := r.X + depth
			if attachEdge == "right" {
				x = r.X + r.W - 1 - depth
			}
			for out := 0; out < left; out++ {
				y := r.Y - 1 - out
				if c.contains(x, y) {
					coverage := ui.FilletCoverage(out, depth, left)
					if coverage > 0 {
						blendCoverage(c, x, y, col, float64(coverage)/255)
					}
				}
			}
		}
		for depth := 0; depth < right && depth < r.W; depth++ {
			x := r.X + depth
			if attachEdge == "right" {
				x = r.X + r.W - 1 - depth
			}
			for out := 0; out < right; out++ {
				y := r.Y + r.H + out
				if c.contains(x, y) {
					coverage := ui.FilletCoverage(out, depth, right)
					if coverage > 0 {
						blendCoverage(c, x, y, col, float64(coverage)/255)
					}
				}
			}
		}
		return
	}
	if attachEdge != "top" && attachEdge != "bottom" {
		return
	}
	side := func(fillet int, px func(x int) int) {
		for y := 0; y < fillet && y < r.H; y++ {
			row := r.Y + y
			if attachEdge == "bottom" {
				row = r.Y + r.H - 1 - y
			}
			if row < c.Oy || row >= c.Oy+c.Height {
				continue
			}
			for x := 0; x < fillet; x++ {
				coverage := ui.FilletCoverage(x, y, fillet)
				if coverage == 0 {
					continue
				}
				if p := px(x); p >= c.Ox && p < c.Ox+c.Width {
					blendCoverage(c, p, row, col, float64(coverage)/255)
				}
			}
		}
	}
	side(left, func(x int) int { return r.X - 1 - x })
	side(right, func(x int) int { return r.X + r.W + x })
}

// fillEdgeFillets paints the concave wedges past the far edge of a surface
// attached along attachEdge, curving it into the screen's side at the left end,
// the right end, or both. Each is ui.FilletCoverage with x counted in from the
// body's side and y out from its far edge.
func fillEdgeFillets(c *Canvas, r ui.Rect, fillet int, attachEdge string, left, right bool, col Color) {
	if fillet <= 0 || col.A == 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	if attachEdge == "left" || attachEdge == "right" {
		for out := 0; out < fillet; out++ {
			x := r.X + r.W + out
			if attachEdge == "right" {
				x = r.X - 1 - out
			}
			if x < c.Ox || x >= c.Ox+c.Width {
				continue
			}
			for along := 0; along < fillet && along < r.H; along++ {
				coverage := ui.FilletCoverage(along, out, fillet)
				if coverage == 0 {
					continue
				}
				alpha := float64(coverage) / 255
				if left {
					y := r.Y + along
					if c.contains(x, y) {
						blendCoverage(c, x, y, col, alpha)
					}
				}
				if right {
					y := r.Y + r.H - 1 - along
					if c.contains(x, y) {
						blendCoverage(c, x, y, col, alpha)
					}
				}
			}
		}
		return
	}
	if attachEdge != "top" && attachEdge != "bottom" {
		return
	}
	for y := 0; y < fillet; y++ {
		row := r.Y + r.H + y
		if attachEdge == "bottom" {
			row = r.Y - 1 - y
		}
		if row < c.Oy || row >= c.Oy+c.Height {
			continue
		}
		for x := 0; x < fillet && x < r.W; x++ {
			coverage := ui.FilletCoverage(x, y, fillet)
			if coverage == 0 {
				continue
			}
			alpha := float64(coverage) / 255
			if px := r.X + x; left && c.contains(px, row) {
				blendCoverage(c, px, row, col, alpha)
			}
			if px := r.X + r.W - 1 - x; right && c.contains(px, row) {
				blendCoverage(c, px, row, col, alpha)
			}
		}
	}
}

// blendMask blends a colour through an alpha coverage mask placed at x, y.
// coverageRow is a mask's coverage for one canvas row, given that the mask's
// top-left sits at y. Indexing it by px-x yields the byte AlphaAt would return
// for (Min.X+px-x, Min.Y+py-y), because that point's offset into Pix reduces to
// (py-y)*Stride + (px-x) whatever the mask's origin is.
//
// The callers clip to the mask's own width and height first, so every index is
// in range and the generic accessor's per-pixel bounds check buys nothing. It
// was not free: AlphaAt's image.Point.In and PixOffset were 57 percent of a bar
// repaint between them.
func coverageRow(mask *image.Alpha, py, y int) []uint8 {
	return mask.Pix[(py-y)*mask.Stride:]
}

// BlendText composites a rasterised run onto the live pixels at x, y in col,
// without clearing anything. A surface that paints its own background (the
// screenshot selector's frozen frame) uses it to draw labels.
func (c *Canvas) BlendText(m Mask, x, y int, col Color) {
	if m.Color != nil {
		blendMaskImage(c, m.Alpha, x, y, m.Color)
		return
	}
	blendMask(c, m.Alpha, x, y, col)
}

// fillBands returns the frame width a rounded fill's coverage can reach.
//
// A pixel whose centre sits inside the corner arcs' inner rectangle is half a
// pixel or more past every edge of the silhouette, so its coverage saturates
// at 255; every pixel less than full coverage therefore lies in the frame of
// radius pixels around the box. That frame carries the corner arcs, their
// seams with the straight edges, and the half-pixel fringe the mask draws
// along an odd edge (roundedCoverage halves the true half-width but floors the
// inset, so an odd dimension shifts its silhouette half a pixel inward). The
// pad is one pixel beyond radius: the antialiased edge reaches half a pixel
// past the geometric silhouette, and the outermost row that carries coverage
// sits on the far side of it. TestFillBandsCoverTheMask pins the bands to the
// mask over the shipped geometries and an exhaustive sweep of small shapes
// (GH #455).
func fillBands(radius, w, h int) int {
	return max(min(radius, min(w, h)/2), 0) + 1
}

// strokeBands returns the row and column counts a ring's coverage can reach.
// A ring of width w and corner radius r is empty everywhere except its top and
// bottom rows, and, between those, its left and right columns, so the banded
// blend in blendRing can skip the rectangle's interior entirely.
//
// The pad is one row and one column beyond max(radius, width): the antialiased
// arc edge reaches half a pixel past the geometric silhouette, and the outermost
// row that carries coverage sits on the far side of it. Taking only
// max(radius, width) misses coverage on small rings. An exhaustive sweep over
// every radius and width of every box up to 48x48 (960,400 shapes) found no
// coverage outside these bands, and TestStrokeBandsCoverTheRing pins the
// shipped geometries.
func strokeBands(radius, w, h, width int) (top, side int) {
	radius = max(min(radius, min(w, h)/2), 0)
	width = max(min(width, min(w, h)/2), 0)
	return max(radius, width) + 1, width + 1
}

// blendMaskBand blends only sub of a coverage mask placed at x, y, where sub is
// in the mask's own coordinates. StrokeRounded uses it to visit a ring's bands
// instead of its whole rectangle: for a 2 px rim that is under two percent of
// the pixels blendMask would scan.
func blendMaskBand(c *Canvas, mask *image.Alpha, sub ui.Rect, x, y int, col Color) {
	if col.A == 0 || mask == nil || sub.W <= 0 || sub.H <= 0 {
		return
	}
	b := mask.Bounds()
	sub.X, sub.Y = max(sub.X, 0), max(sub.Y, 0)
	right := min(sub.X+sub.W, max(b.Dx(), 0))
	bottom := min(sub.Y+sub.H, max(b.Dy(), 0))
	sub.W, sub.H = right-sub.X, bottom-sub.Y
	if sub.W <= 0 || sub.H <= 0 {
		return
	}
	blendMaskAt(c, mask, x, y, sub, col)
}

// blendMask is blendMaskBand over a mask's whole bounds.
func blendMask(c *Canvas, mask *image.Alpha, x, y int, col Color) {
	if col.A == 0 || mask == nil {
		return
	}
	b := mask.Bounds()
	blendMaskAt(c, mask, x, y, ui.Rect{W: b.Dx(), H: b.Dy()}, col)
}

// blendMaskAt blends col through mask at x, y, visiting only sub, a region in
// the mask's own coordinates. The mask carries its coverage in Pix from its
// origin, so coverage for canvas pixel px is indexed by px-x however the
// visited region is offset: the origin stays the reference for both the
// coverage lookup and the clip.
func blendMaskAt(c *Canvas, mask *image.Alpha, x, y int, sub ui.Rect, col Color) {
	x0, y0, x1, y1 := c.clip(ui.Rect{X: x + sub.X, Y: y + sub.Y, W: sub.W, H: sub.H})
	if x0 >= x1 || y0 >= y1 {
		return
	}
	src := col.premultiply()
	// Full coverage needs no mask multiplication. An opaque source or a
	// clear destination also needs no over blend (including the bar ground).
	opaque := col.A == 255
	for py := y0; py < y1; py++ {
		row := c.row(py)
		cov := coverageRow(mask, py, y)
		for px := x0; px < x1; px++ {
			coverage := uint32(cov[px-x])
			if coverage == 0 {
				continue
			}
			dst := row[(px-c.Ox)*4 : (px-c.Ox)*4+4 : (px-c.Ox)*4+4]
			if coverage == 255 {
				if opaque || dst[0]|dst[1]|dst[2]|dst[3] == 0 {
					dst[0], dst[1], dst[2], dst[3] = src[0], src[1], src[2], src[3]
				} else {
					blendPixel(dst, src, uint32(col.A))
				}
				continue
			}
			alpha := uint32(col.A) * coverage / 255
			var s [4]byte
			for i := range src {
				s[i] = byte(uint32(src[i]) * coverage / 255)
			}
			blendPixel(dst, s, alpha)
		}
	}
}

// FillRounded fills a rounded rectangle using the cached alpha mask. Like
// StrokeRounded it visits only the bands the mask can differ in, so a large
// fill does not scan its whole box for coverage it knows is full (GH #455).
func (c *Canvas) FillRounded(r ui.Rect, radius int, col Color) {
	blendFill(c, r, radius, 0, col)
}

// StrokeRounded outlines a rounded rectangle inward from its bounds using the
// cached ring mask. Unlike strokeRoundedRect it antialiases both edges of the
// band, which is what a hairline border needs: a one-pixel stroke quantised to
// whole pixels reads as a staircase rather than a line.
func (c *Canvas) StrokeRounded(r ui.Rect, radius, width int, col Color) {
	blendRing(c, r, radius, width, col)
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
