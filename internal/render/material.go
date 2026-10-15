package render

import (
	"bytes"
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Tone selects what a material does to the captured backdrop.
type Tone uint8

const (
	// ToneNone is today's paint: the blurred backdrop under the root fill.
	ToneNone Tone = iota
	// ToneSmoke lowers what is seen through the backdrop in proportion to its
	// luma, capped by the material's intensity.
	ToneSmoke
	// ToneMilk lifts the backdrop toward white, weighted by how far each pixel
	// still sits from white.
	ToneMilk
	// ToneFrost always frosts: the ground is the blurred capture.
	ToneFrost
	// ToneAuto frosts only where the content behind the surface is busy, and
	// leaves plain content clear.
	ToneAuto
)

// Material is the resolved material of one surface. The zero value is today's
// paint.
type Material struct {
	Tone Tone
	// Intensity is a percentage. It is clamped to 0..100 here as well as in the
	// theme, so a programmatic value cannot exceed the curve's design range.
	Intensity uint8
	// Refract is a percentage that disables bending at zero. The refraction
	// stage is wired by the material's second task; until then it is carried
	// and unused.
	Refract uint8
}

const (
	// smokeCap and milkCap are what the tone curves reach at full intensity.
	// The value is the recorded reference cap: past it the transfer stops
	// reading as a material and starts reading as a fill.
	smokeCap = 0.58
	milkCap  = 0.58

	// intensityMax bounds the percentage axis.
	intensityMax = 100

	// autoFrostPoints is how many places the sharp and frosted captures are
	// compared at. Five is the reference count: enough to find a busy region
	// without turning the check into a second pass over the buffer.
	autoFrostPoints = 5

	// autoFrostThreshold is the mean luma difference, 0..255, at or above which
	// the content behind a surface reads as busy. Below it the surface stays
	// clear, so a plain wallpaper does not acquire a frosted ground it never
	// needed.
	autoFrostThreshold = 24
)

// luma is the perceived brightness of one premultiplied BGRA pixel, 0..255.
// Captures are opaque by construction, so the premultiplied channels are the
// colour.
func luma(p []byte) int {
	return (299*int(p[2]) + 587*int(p[1]) + 114*int(p[0])) / 1000
}

// Materialize returns the ground a material surface composites beneath its root
// fill, or nil to leave the surface's backdrop untouched.
//
// sharp and blurred are the two halves of one reduced capture, as BlurPair
// returns them. ToneNone and ToneFrost resolve to the blurred backdrop;
// auto-frost compares the halves and chooses, so plain content behind the
// surface stays clear while a busy one frosts. Smoke and milk return a new
// transferred buffer; the other tones return one of the inputs, which callers
// treat as read-only.
func Materialize(sharp, blurred *ui.Image, m Material) *ui.Image {
	switch m.Tone {
	case ToneSmoke:
		return toneTransfer(blurred, m.Intensity, false)
	case ToneMilk:
		return toneTransfer(blurred, m.Intensity, true)
	case ToneFrost:
		return blurred
	case ToneAuto:
		if sharp == nil {
			return blurred
		}
		if Busyness(sharp, blurred, autoFrostPoints) >= autoFrostThreshold {
			return blurred
		}
		return sharp
	default:
		return nil
	}
}

// toneTransfer copies src and applies the tone curve to it. The transfer
// touches the colour channels only: a capture is opaque, so leaving alpha alone
// keeps the premultiplied copy premultiplied.
func toneTransfer(src *ui.Image, intensity uint8, lift bool) *ui.Image {
	if src == nil {
		return nil
	}
	if intensity > intensityMax {
		intensity = intensityMax
	}
	out := &ui.Image{Width: src.Width, Height: src.Height, Stride: src.Stride,
		Pix: append([]byte(nil), src.Pix...)}
	gain := float64(intensity) / 100
	if gain <= 0 {
		return out
	}
	for i := 0; i+3 < len(out.Pix); i += 4 {
		l := float64(luma(src.Pix[i:i+4])) / 255
		var t float64
		if lift {
			// Milk lifts most where the content is furthest from white, which
			// is what reads as a film over the surface.
			t = milkCap * gain * (1 - l)
		} else {
			// Smoke darkens most where the content is brightest, so detail in
			// a dark region is not crushed into black.
			t = smokeCap * gain * l
		}
		if t <= 0 {
			continue
		}
		for c := 0; c < 3; c++ {
			v := float64(out.Pix[i+c])
			if lift {
				v += (255 - v) * t
			} else {
				v *= 1 - t
			}
			out.Pix[i+c] = byte(v + 0.5)
		}
	}
	return out
}

// TransferLuma returns what a tone leaves of one input luma, 0..255. It is the
// scalar view of the same curves Materialize applies, so a caller can reason
// about what a material does to legibility without an image.
func TransferLuma(t Tone, intensity uint8, luma uint8) uint8 {
	if intensity > intensityMax {
		intensity = intensityMax
	}
	gain := float64(intensity) / 100
	v := float64(luma)
	switch t {
	case ToneSmoke:
		v *= 1 - smokeCap*gain*(v/255)
	case ToneMilk:
		lift := milkCap * gain * (1 - v/255)
		v += (255 - v) * lift
	}
	return uint8(v + 0.5)
}

// FrameDiffers reports whether two frames of the same geometry differ, sampling
// a bounded number of rows.
//
// A full comparison would cost more than the capture it exists to skip: the
// question is whether anything moved, not whether every byte matches. Frames of
// different geometry always differ, because the ground's geometry follows the
// surface.
func FrameDiffers(a, b *ui.Image, rows int) bool {
	if a == nil || b == nil {
		return true
	}
	if a.Width != b.Width || a.Height != b.Height || a.Stride != b.Stride {
		return true
	}
	if a.Height == 0 || a.Stride <= 0 {
		return false
	}
	if rows < 1 {
		rows = 1
	}
	step := a.Height / rows
	if step < 1 {
		step = 1
	}
	for y := 0; y < a.Height; y += step {
		off := y * a.Stride
		if off+a.Stride > len(a.Pix) || off+a.Stride > len(b.Pix) {
			return true
		}
		if !bytes.Equal(a.Pix[off:off+a.Stride], b.Pix[off:off+a.Stride]) {
			return true
		}
	}
	return false
}

// Busyness scores how much detail the blur destroyed, 0..255. Content with
// fine detail scores high, which is what makes auto-frost frost behind it and
// leave a plain wallpaper clear.
//
// The samples sit on a golden-angle disc inside the capture, so they cannot all
// land inside one glyph or one flat region.
func Busyness(sharp, frosted *ui.Image, points int) uint8 {
	if sharp == nil || frosted == nil || points < 1 {
		return 0
	}
	if sharp.Width != frosted.Width || sharp.Height != frosted.Height || sharp.Width < 2 || sharp.Height < 2 {
		return 0
	}
	sum, n := 0, 0
	for i := 0; i < points; i++ {
		x, y := goldenSample(i, points, sharp.Width, sharp.Height)
		o := y*sharp.Stride + x*4
		if o+3 >= len(sharp.Pix) || o+3 >= len(frosted.Pix) {
			continue
		}
		d := luma(sharp.Pix[o:o+4]) - luma(frosted.Pix[o:o+4])
		if d < 0 {
			d = -d
		}
		sum += d
		n++
	}
	if n == 0 {
		return 0
	}
	return uint8(sum / n)
}

// goldenSample spreads sample i of n over the middle of a w by h image. The
// golden angle keeps successive samples far apart for any count.
func goldenSample(i, n, w, h int) (int, int) {
	const goldenAngle = 2.399963229728653
	radius := math.Sqrt((float64(i)+0.5)/float64(n)) * 0.45
	theta := float64(i) * goldenAngle
	x := int(float64(w-1) * (0.5 + math.Cos(theta)*radius))
	y := int(float64(h-1) * (0.5 + math.Sin(theta)*radius))
	return clampInt(x, 0, w-1), clampInt(y, 0, h-1)
}
