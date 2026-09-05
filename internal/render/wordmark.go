package render

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/png"
	"sync"

	xdraw "golang.org/x/image/draw"
)

// wordmarkPNG is a single-channel alpha master of the SYSC brand mark, cut
// from sysc-greet's logo.png at author time. It is committed rather than
// generated during the build; SOURCE.md records the upstream commit, the
// trim, and the regeneration step.
//
// The mark is alpha only. It carries no colour of its own and is tinted at
// paint time, so it follows the theme like every other piece of chrome
// instead of pinning a brand colour into a palette that is derived from the
// user's wallpaper.
//
//go:embed icons/wordmark/sysc-mark.png
var wordmarkPNG []byte

// WordmarkAspect is the mark's width divided by its height. Callers size the
// mark by height and derive the width from this, so the asset owns its own
// proportions and the layout tree never hardcodes them.
const WordmarkAspect = 7.265625

// WordmarkWidth reports the logical width the mark occupies at this height.
func WordmarkWidth(height int) int {
	if height <= 0 {
		return 0
	}
	return int(float64(height)*WordmarkAspect + 0.5)
}

var (
	wordmarkOnce   sync.Once
	wordmarkMaster *image.Alpha
	wordmarkErr    error

	wordmarkMu    sync.Mutex
	wordmarkCache = map[[2]int]*image.Alpha{}
)

// wordmarkSource decodes the embedded master once. A broken embed latches its
// error rather than being retried every frame.
func wordmarkSource() (*image.Alpha, error) {
	wordmarkOnce.Do(func() {
		img, err := png.Decode(bytes.NewReader(wordmarkPNG))
		if err != nil {
			wordmarkErr = err
			return
		}
		b := img.Bounds()
		master := image.NewAlpha(image.Rect(0, 0, b.Dx(), b.Dy()))
		// The master is greyscale, so its luminance is the coverage we want.
		for y := range b.Dy() {
			for x := range b.Dx() {
				r, _, _, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				master.SetAlpha(x, y, color.Alpha{A: colorAlpha(r)})
			}
		}
		wordmarkMaster = master
	})
	return wordmarkMaster, wordmarkErr
}

// Wordmark returns the mark as an alpha mask at exactly this physical size,
// cached. The master is larger than any size the shell draws, so this only
// ever downsamples, which is where CatmullRom looks best -- the same filter
// and the same reasoning as the icon worker's raster path.
func Wordmark(w, h int) (*image.Alpha, error) {
	if w <= 0 || h <= 0 {
		return nil, nil
	}
	key := [2]int{w, h}
	wordmarkMu.Lock()
	if mask := wordmarkCache[key]; mask != nil {
		wordmarkMu.Unlock()
		return mask, nil
	}
	wordmarkMu.Unlock()

	master, err := wordmarkSource()
	if err != nil {
		return nil, err
	}
	scaled := image.NewAlpha(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), master, master.Bounds(), xdraw.Src, nil)

	wordmarkMu.Lock()
	wordmarkCache[key] = scaled
	wordmarkMu.Unlock()
	return scaled, nil
}

// colorAlpha narrows a 16-bit channel from RGBA() to the 8-bit coverage an
// image.Alpha stores.
func colorAlpha(v uint32) uint8 { return uint8(v >> 8) }
