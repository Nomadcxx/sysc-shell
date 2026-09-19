package render

import (
	"image"
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// paintImage composites a decoded raster into the canvas.
//
// The source is already premultiplied in the canvas's memory order, so a fully
// opaque pixel copies and a translucent one blends source-over through the same
// helper the rest of the painter uses. Sampling is nearest-neighbour: the icon
// worker produces the size the node asked for, and resampling here would be a
// second, worse scaler.
func paintImage(c *Canvas, box ui.Rect, img *ui.Image) {
	paintImageMasked(c, box, img, nil)
}

func paintImageMasked(c *Canvas, box ui.Rect, img *ui.Image, mask *image.Alpha) {
	if img == nil || img.Width <= 0 || img.Height <= 0 {
		return
	}
	// A raster whose pixels do not match its declared geometry is malformed.
	// Painting the part that happens to be present would put garbage on
	// screen, so the whole image is dropped and the node stays empty.
	if img.Stride < img.Width*4 || len(img.Pix) < img.Height*img.Stride {
		return
	}
	if box.W <= 0 || box.H <= 0 {
		return
	}
	x0, y0, x1, y1 := c.clip(box)
	for y := y0; y < y1; y++ {
		srcY := (y - box.Y) * img.Height / box.H
		if srcY < 0 || srcY >= img.Height {
			continue
		}
		for x := x0; x < x1; x++ {
			srcX := (x - box.X) * img.Width / box.W
			if srcX < 0 || srcX >= img.Width {
				continue
			}
			offset := srcY*img.Stride + srcX*4
			if offset+4 > len(img.Pix) {
				continue
			}
			coverage := uint32(255)
			if mask != nil {
				coverage = uint32(mask.AlphaAt(x-box.X, y-box.Y).A)
			}
			alpha := uint32(img.Pix[offset+3]) * coverage / 255
			if alpha == 0 {
				continue
			}
			dst := y*c.Stride + x*4
			if dst+4 > len(c.Pix) {
				continue
			}
			var src [4]byte
			for i, channel := range img.Pix[offset : offset+4] {
				src[i] = byte(uint32(channel) * coverage / 255)
			}
			blendPixel(c.Pix[dst:dst+4], src, alpha)
		}
	}
}

// blendMaskImage composites a raster through an alpha mask with bilinear
// sampling, in the same shape as blendMask and blendMaskGradient: the mask
// supplies both the geometry and the per-pixel coverage. Its source mapping is
// kept for backdrop captures, whose capture region already matches the panel.
//
// paintImage stays nearest-neighbour on purpose: the icon worker produces the
// size the node asked for. A background image is the opposite case -- one
// decoded image scaled to whatever the card measures -- and nearest banding is
// visible across a large flat ground.
//
// The mask is what keeps a panel's corners honest. The painter clears the buffer
// and fills a *rounded* body, so those corners are genuinely transparent and the
// compositor shows what is behind them; a backdrop blitted into the raw
// rectangle would fill them in and the panel would read as a square.
//
// Weights are 8-bit fixed point, and the half-texel offset centres each
// destination pixel inside its source cell rather than on its corner. Sampling
// clamps at the edges, so no read leaves the source. The source is already
// premultiplied, so scaling all four channels by coverage keeps it that way.
func blendMaskImage(c *Canvas, mask *image.Alpha, x, y int, img *ui.Image) {
	blendMaskImageMode(c, mask, x, y, img, false)
}

// blendBackgroundImage uses the same bilinear compositor with centred
// PreserveAspectCrop mapping for a card background. It is separate from
// blendMaskImage so changing card artwork cannot change the panel blur capture.
func blendBackgroundImage(c *Canvas, mask *image.Alpha, x, y int, img *ui.Image) {
	blendMaskImageMode(c, mask, x, y, img, true)
}

func blendMaskImageMode(c *Canvas, mask *image.Alpha, x, y int, img *ui.Image, crop bool) {
	if mask == nil || img == nil || img.Width <= 0 || img.Height <= 0 {
		return
	}
	if img.Stride < img.Width*4 || len(img.Pix) < img.Height*img.Stride {
		return
	}
	b := mask.Bounds()
	box := ui.Rect{X: x, Y: y, W: b.Dx(), H: b.Dy()}
	if box.W <= 0 || box.H <= 0 {
		return
	}
	var cropX, cropY, cropW, cropH float64
	if crop {
		cropX, cropY, cropW, cropH = aspectCrop(float64(img.Width), float64(img.Height), float64(box.W), float64(box.H))
	}
	x0, y0, x1, y1 := c.clip(box)
	for py := y0; py < y1; py++ {
		row := c.Pix[py*c.Stride:]
		var sy0, sy1, wy int
		if crop {
			sy0, sy1, wy = cropSample(float64(py-box.Y), float64(box.H), cropY, cropH, img.Height)
		} else {
			sy0, sy1, wy = stretchSample(py-box.Y, box.H, img.Height)
		}
		coverage := coverageRow(mask, py, y)
		for px := x0; px < x1; px++ {
			cov := uint32(coverage[px-x])
			if cov == 0 {
				continue
			}
			var sx0, sx1, wx int
			if crop {
				sx0, sx1, wx = cropSample(float64(px-box.X), float64(box.W), cropX, cropW, img.Width)
			} else {
				sx0, sx1, wx = stretchSample(px-box.X, box.W, img.Width)
			}
			o00 := sy0*img.Stride + sx0*4
			o01 := sy0*img.Stride + sx1*4
			o10 := sy1*img.Stride + sx0*4
			o11 := sy1*img.Stride + sx1*4
			var s [4]byte
			for ch := range 4 {
				upper := int(img.Pix[o00+ch])*(256-wx) + int(img.Pix[o01+ch])*wx
				lower := int(img.Pix[o10+ch])*(256-wx) + int(img.Pix[o11+ch])*wx
				s[ch] = byte(uint32((upper*(256-wy)+lower*wy)>>16) * cov / 255)
			}
			alpha := uint32(s[3])
			if alpha == 0 {
				continue
			}
			blendPixel(row[px*4:px*4+4], s, alpha)
		}
	}
}

func stretchSample(dst, dstSize, sourceSize int) (int, int, int) {
	position := dst*sourceSize*256/dstSize - 128
	base := clampInt(position>>8, 0, sourceSize-1)
	weight := position & 255
	if position < 0 {
		weight = 0
	}
	return base, clampInt(base+1, 0, sourceSize-1), weight
}

// aspectCrop returns the centred source window whose aspect matches the
// destination. The window may have fractional edges; the renderer keeps those
// edges instead of rounding them before bilinear sampling.
func aspectCrop(srcW, srcH, dstW, dstH float64) (x, y, w, h float64) {
	if srcW <= 0 || srcH <= 0 || dstW <= 0 || dstH <= 0 {
		return
	}
	w, h = srcW, srcH
	if srcW*dstH > srcH*dstW {
		w = srcH * dstW / dstH
		x = (srcW - w) / 2
	} else {
		h = srcW * dstH / dstW
		y = (srcH - h) / 2
	}
	return
}

// cropSample maps one destination pixel to a bilinear source coordinate. The
// half-pixel offset keeps the sampling convention used by the uncropped path.
func cropSample(dst, dstSize, start, size float64, sourceSize int) (int, int, int) {
	coord := start + (dst+0.5)*size/dstSize - 0.5
	base := int(math.Floor(coord))
	weight := int(math.Round((coord - float64(base)) * 256))
	if weight >= 256 {
		base++
		weight = 0
	}
	return clampInt(base, 0, sourceSize-1), clampInt(base+1, 0, sourceSize-1), weight
}
