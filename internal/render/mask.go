package render

import (
	"image"
	"image/color"
	"math"
	"sync"
)

type maskKey struct{ radius, w, h int }

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
	shadows = map[shadowKey]*image.Alpha{}
)

// RoundedMask returns a cached antialiased rounded-rectangle alpha mask.
func RoundedMask(radius, w, h int) *image.Alpha {
	key := maskKey{radius, w, h}
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
				mask.SetAlpha(x, y, color.Alpha{A: roundedCoverage(radius, w, h, x, y)})
			}
		}
	}
	masks[key] = mask
	return mask
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
