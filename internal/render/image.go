package render

import (
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
			alpha := uint32(img.Pix[offset+3])
			if alpha == 0 {
				continue
			}
			dst := y*c.Stride + x*4
			if dst+4 > len(c.Pix) {
				continue
			}
			blendPixel(c.Pix[dst:dst+4], [4]byte(img.Pix[offset:offset+4]), alpha)
		}
	}
}
