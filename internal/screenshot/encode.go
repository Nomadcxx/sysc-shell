package screenshot

import (
	"bytes"
	"image"
	"image/png"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// CropRect converts a selection in surface-logical pixels to frame pixels.
// The frame is the output in buffer pixels, so the factor is its size over the
// logical size; the edges round outward so a fractional scale never shaves a
// row the user selected. The result is clamped to the frame and is the zero
// Rect when nothing is left.
func CropRect(sel ui.Rect, logicalW, logicalH, frameW, frameH int) ui.Rect {
	if logicalW <= 0 || logicalH <= 0 || frameW <= 0 || frameH <= 0 {
		return ui.Rect{}
	}
	floor := func(v, num, den int) int {
		q := v * num / den
		if v*num%den != 0 && v*num < 0 {
			q--
		}
		return q
	}
	ceil := func(v, num, den int) int { return -floor(-v, num, den) }
	x0 := max(floor(sel.X, frameW, logicalW), 0)
	y0 := max(floor(sel.Y, frameH, logicalH), 0)
	x1 := min(ceil(sel.X+sel.W, frameW, logicalW), frameW)
	y1 := min(ceil(sel.Y+sel.H, frameH, logicalH), frameH)
	if x1 <= x0 || y1 <= y0 {
		return ui.Rect{}
	}
	return ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// Crop copies r, in img's pixels, out of img. It returns nil when r does not
// lie inside the image.
func Crop(img *ui.Image, r ui.Rect) *ui.Image {
	if img == nil || r.W <= 0 || r.H <= 0 || r.X < 0 || r.Y < 0 ||
		r.X+r.W > img.Width || r.Y+r.H > img.Height {
		return nil
	}
	out := &ui.Image{Width: r.W, Height: r.H, Stride: r.W * 4, Pix: make([]byte, r.W*r.H*4)}
	for y := range r.H {
		from := (r.Y+y)*img.Stride + r.X*4
		copy(out.Pix[y*out.Stride:(y+1)*out.Stride], img.Pix[from:from+r.W*4])
	}
	return out
}

// EncodePNG encodes a shell image. The shell's premultiplied BGRA differs
// from image.RGBA, which is also premultiplied, only in byte order.
func EncodePNG(img *ui.Image) ([]byte, error) {
	rgba := image.NewRGBA(image.Rect(0, 0, img.Width, img.Height))
	for y := range img.Height {
		src := img.Pix[y*img.Stride : y*img.Stride+img.Width*4]
		dst := rgba.Pix[y*rgba.Stride : y*rgba.Stride+img.Width*4]
		for i := 0; i < len(src); i += 4 {
			dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i+2], src[i+1], src[i], src[i+3]
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
