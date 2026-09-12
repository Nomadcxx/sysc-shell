package render

import (
	"image"

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

// paintImageSmooth composites a raster with bilinear sampling.
//
// paintImage stays nearest-neighbour on purpose: the icon worker produces the
// size the node asked for. A backdrop is the opposite case -- one capture scaled
// to whatever the panel measures -- and nearest banding is visible across a
// large flat ground. This is a separate entry point rather than a flag, so the
// icon contract is untouched.
//
// Weights are 8-bit fixed point, and the half-texel offset centres each
// destination pixel inside its source cell rather than on its corner. Sampling
// clamps at the edges, so no read leaves the source.
func paintImageSmooth(c *Canvas, box ui.Rect, img *ui.Image) {
	if img == nil || img.Width <= 0 || img.Height <= 0 || box.W <= 0 || box.H <= 0 {
		return
	}
	if img.Stride < img.Width*4 || len(img.Pix) < img.Height*img.Stride {
		return
	}
	x0, y0, x1, y1 := c.clip(box)
	for y := y0; y < y1; y++ {
		fy := ((y-box.Y)*img.Height*256)/box.H - 128
		sy0 := clampInt(fy>>8, 0, img.Height-1)
		sy1 := clampInt(sy0+1, 0, img.Height-1)
		wy := fy & 255
		if fy < 0 {
			wy = 0
		}
		for x := x0; x < x1; x++ {
			fx := ((x-box.X)*img.Width*256)/box.W - 128
			sx0 := clampInt(fx>>8, 0, img.Width-1)
			sx1 := clampInt(sx0+1, 0, img.Width-1)
			wx := fx & 255
			if fx < 0 {
				wx = 0
			}
			o00 := sy0*img.Stride + sx0*4
			o01 := sy0*img.Stride + sx1*4
			o10 := sy1*img.Stride + sx0*4
			o11 := sy1*img.Stride + sx1*4
			var px [4]byte
			for ch := range 4 {
				upper := int(img.Pix[o00+ch])*(256-wx) + int(img.Pix[o01+ch])*wx
				lower := int(img.Pix[o10+ch])*(256-wx) + int(img.Pix[o11+ch])*wx
				px[ch] = byte((upper*(256-wy) + lower*wy) >> 16)
			}
			if px[3] == 0 {
				continue
			}
			dst := y*c.Stride + x*4
			if dst+4 > len(c.Pix) {
				continue
			}
			blendPixel(c.Pix[dst:dst+4], px, uint32(px[3]))
		}
	}
}
