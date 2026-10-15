package render

import "github.com/Nomadcxx/sysc-shell/internal/ui"

// Blur downsamples src by factor, then runs three box passes per axis at a
// proportionally reduced radius, and returns the reduced image.
//
// There is deliberately no upsample pass. The caller hands the reduced image
// to the blit it already pays for. Measured 2026-09-11 on a 1120x960 panel,
// adding a full-resolution upsample turns an 8.8x saving into 2.6x, because
// the upsample is itself a full-resolution pass with four taps per pixel.
//
// A blur is a low-pass filter, so the detail discarded by downsampling is
// detail the blur would have destroyed. Premultiplied ARGB averages linearly,
// which is why the canvas stores premultiplied pixels, so neither resample
// needs an un-premultiply round trip.
func Blur(src *ui.Image, factor, radius int) *ui.Image {
	if !blurrable(src, factor) {
		return nil
	}
	dst := downsample(src, factor)
	blurReduced(dst, factor, radius)
	return dst
}

// BlurPair is Blur with the reduced sharp capture returned beside the blurred
// one, from one downsample. A busy-area material compares the two, so it needs
// both; Blur's callers need only the blurred half and keep its cost.
//
// With a radius below one there is nothing to blur, and both halves are the
// same reduced image: a caller must not mutate one and read the other.
func BlurPair(src *ui.Image, factor, radius int) (*ui.Image, *ui.Image) {
	if !blurrable(src, factor) {
		return nil, nil
	}
	sharp := downsample(src, factor)
	if radius < 1 {
		return sharp, sharp
	}
	blurred := &ui.Image{Width: sharp.Width, Height: sharp.Height, Stride: sharp.Stride,
		Pix: make([]byte, len(sharp.Pix))}
	copy(blurred.Pix, sharp.Pix)
	blurReduced(blurred, factor, radius)
	return sharp, blurred
}

// blurrable reports whether src can be reduced by factor at all.
func blurrable(src *ui.Image, factor int) bool {
	if src == nil || factor < 1 || src.Width < factor || src.Height < factor {
		return false
	}
	return src.Stride >= src.Width*4 && len(src.Pix) >= src.Height*src.Stride
}

// blurReduced runs the three box passes per axis over an already reduced
// image, at a proportionally reduced radius.
func blurReduced(dst *ui.Image, factor, radius int) {
	if radius < 1 {
		return
	}
	r := radius / factor
	if r < 1 {
		r = 1
	}
	scratch := &ui.Image{Width: dst.Width, Height: dst.Height, Stride: dst.Stride,
		Pix: make([]byte, len(dst.Pix))}
	for i := 0; i < 3; i++ {
		boxH(dst, scratch, r)
		boxV(scratch, dst, r)
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// downsample box-averages by an integer factor.
func downsample(src *ui.Image, k int) *ui.Image {
	dw, dh := src.Width/k, src.Height/k
	dst := &ui.Image{Width: dw, Height: dh, Stride: dw * 4, Pix: make([]byte, dw*dh*4)}
	n := k * k
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var sum [4]int
			for j := 0; j < k; j++ {
				row := (y*k + j) * src.Stride
				for i := 0; i < k; i++ {
					o := row + (x*k+i)*4
					sum[0] += int(src.Pix[o])
					sum[1] += int(src.Pix[o+1])
					sum[2] += int(src.Pix[o+2])
					sum[3] += int(src.Pix[o+3])
				}
			}
			o := y*dst.Stride + x*4
			dst.Pix[o] = byte(sum[0] / n)
			dst.Pix[o+1] = byte(sum[1] / n)
			dst.Pix[o+2] = byte(sum[2] / n)
			dst.Pix[o+3] = byte(sum[3] / n)
		}
	}
	return dst
}

// boxH runs one horizontal box pass with a sliding window, so cost does not
// grow with radius. Edges clamp rather than wrap: a wrapped backdrop bleeds the
// opposite edge of the capture into the panel corner.
func boxH(src, dst *ui.Image, r int) {
	win := r*2 + 1
	for y := 0; y < src.Height; y++ {
		row := y * src.Stride
		var sum [4]int
		for i := -r; i <= r; i++ {
			o := row + clampInt(i, 0, src.Width-1)*4
			sum[0] += int(src.Pix[o])
			sum[1] += int(src.Pix[o+1])
			sum[2] += int(src.Pix[o+2])
			sum[3] += int(src.Pix[o+3])
		}
		for x := 0; x < src.Width; x++ {
			o := row + x*4
			dst.Pix[o] = byte(sum[0] / win)
			dst.Pix[o+1] = byte(sum[1] / win)
			dst.Pix[o+2] = byte(sum[2] / win)
			dst.Pix[o+3] = byte(sum[3] / win)
			out := row + clampInt(x-r, 0, src.Width-1)*4
			in := row + clampInt(x+r+1, 0, src.Width-1)*4
			sum[0] += int(src.Pix[in]) - int(src.Pix[out])
			sum[1] += int(src.Pix[in+1]) - int(src.Pix[out+1])
			sum[2] += int(src.Pix[in+2]) - int(src.Pix[out+2])
			sum[3] += int(src.Pix[in+3]) - int(src.Pix[out+3])
		}
	}
}

// boxV is the vertical counterpart. It is a separate function rather than a
// transpose because a transpose costs two extra full passes over the buffer.
func boxV(src, dst *ui.Image, r int) {
	win := r*2 + 1
	for x := 0; x < src.Width; x++ {
		col := x * 4
		var sum [4]int
		for i := -r; i <= r; i++ {
			o := clampInt(i, 0, src.Height-1)*src.Stride + col
			sum[0] += int(src.Pix[o])
			sum[1] += int(src.Pix[o+1])
			sum[2] += int(src.Pix[o+2])
			sum[3] += int(src.Pix[o+3])
		}
		for y := 0; y < src.Height; y++ {
			o := y*src.Stride + col
			dst.Pix[o] = byte(sum[0] / win)
			dst.Pix[o+1] = byte(sum[1] / win)
			dst.Pix[o+2] = byte(sum[2] / win)
			dst.Pix[o+3] = byte(sum[3] / win)
			out := clampInt(y-r, 0, src.Height-1)*src.Stride + col
			in := clampInt(y+r+1, 0, src.Height-1)*src.Stride + col
			sum[0] += int(src.Pix[in]) - int(src.Pix[out])
			sum[1] += int(src.Pix[in+1]) - int(src.Pix[out+1])
			sum[2] += int(src.Pix[in+2]) - int(src.Pix[out+2])
			sum[3] += int(src.Pix[in+3]) - int(src.Pix[out+3])
		}
	}
}
