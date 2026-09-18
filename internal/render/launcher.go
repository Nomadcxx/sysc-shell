package render

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"sync"

	xdraw "golang.org/x/image/draw"
)

// launcherPNG is the supplied nested-gates launcher mark, copied byte-for-byte
// from the owner's artwork. It is committed rather than generated during the
// build; SOURCE.md records the source path, hash, and copy step.
//
// The asset keeps its transparent margin: the visible mark is smaller than its
// 1024x1024 canvas, and callers size the box, not the mark.
//
// Like the wordmark master, the mark is consumed as an alpha-only mask and
// tinted at paint time, so it follows the theme instead of pinning the
// artwork's own colour into a palette derived from the user's wallpaper.
//
//go:embed icons/launcher/sysc-aperture.png
var launcherPNG []byte

// LauncherPNG returns the embedded launcher mark bytes. The asset test decodes
// them to pin the hash, dimensions, and alpha bounds the shell relies on.
func LauncherPNG() []byte {
	return launcherPNG
}

var (
	launcherOnce   sync.Once
	launcherMaster *image.Alpha
	launcherErr    error

	launcherMu    sync.Mutex
	launcherCache = map[[2]int]*image.Alpha{}
)

// launcherSource decodes the embedded master once, reading the artwork's alpha
// channel as coverage. A broken embed latches its error rather than being
// retried every frame.
func launcherSource() (*image.Alpha, error) {
	launcherOnce.Do(func() {
		img, err := png.Decode(bytes.NewReader(launcherPNG))
		if err != nil {
			launcherErr = err
			return
		}
		b := img.Bounds()
		master := image.NewAlpha(image.Rect(0, 0, b.Dx(), b.Dy()))
		for y := range b.Dy() {
			for x := range b.Dx() {
				_, _, _, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				master.SetAlpha(x, y, color.Alpha{A: colorAlpha(a)})
			}
		}
		launcherMaster = master
	})
	return launcherMaster, launcherErr
}

// LauncherMark returns the launcher mark as an alpha mask at exactly this
// physical size, cached. The master is larger than any size the shell draws,
// so this only ever downsamples, which is where CatmullRom looks best -- the
// same filter and the same reasoning as the wordmark path.
func LauncherMark(w, h int) (*image.Alpha, error) {
	if w <= 0 || h <= 0 {
		return nil, nil
	}
	key := [2]int{w, h}
	launcherMu.Lock()
	if mask := launcherCache[key]; mask != nil {
		launcherMu.Unlock()
		return mask, nil
	}
	launcherMu.Unlock()

	master, err := launcherSource()
	if err != nil {
		return nil, err
	}
	scaled := image.NewAlpha(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), master, master.Bounds(), xdraw.Src, nil)

	launcherMu.Lock()
	launcherCache[key] = scaled
	launcherMu.Unlock()
	return scaled, nil
}

// markMask resolves the alpha master a KindWordmark node paints. An unknown
// name is a composition error, not a silent fallback onto the brand mark.
func markMask(name string, w, h int) (*image.Alpha, error) {
	switch name {
	case "":
		return Wordmark(w, h)
	case "launcher":
		return LauncherMark(w, h)
	default:
		return nil, fmt.Errorf("render: unknown mark %q", name)
	}
}
