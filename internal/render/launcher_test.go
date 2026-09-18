package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/png"
	"testing"
)

// visibleBounds reports the smallest rectangle containing every pixel with
// non-zero alpha. The launcher asset's transparent margin is part of its
// contract, so the test pins the visible mark rather than the canvas.
func visibleBounds(img image.Image) image.Rectangle {
	b := img.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x+1 > maxX {
				maxX = x + 1
			}
			if y+1 > maxY {
				maxY = y + 1
			}
		}
	}
	if minX >= maxX || minY >= maxY {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX, maxY)
}

func TestLauncherMarkMask(t *testing.T) {
	t.Parallel()
	mask, err := LauncherMark(19, 19)
	if err != nil {
		t.Fatalf("launcher mark: %v", err)
	}
	if mask == nil {
		t.Fatal("launcher mark mask is nil")
	}
	if got := mask.Bounds(); got.Dx() != 19 || got.Dy() != 19 {
		t.Fatalf("mask bounds = %v, want 19x19", got)
	}
	covered := 0
	for y := 0; y < 19; y++ {
		for x := 0; x < 19; x++ {
			if mask.AlphaAt(x, y).A != 0 {
				covered++
			}
		}
	}
	if covered == 0 {
		t.Fatal("launcher mark mask is empty")
	}
	nilMask, err := LauncherMark(0, 19)
	if nilMask != nil || err != nil {
		t.Fatalf("degenerate size = (%v, %v), want (nil, nil)", nilMask, err)
	}
}

func TestMarkMaskDispatch(t *testing.T) {
	t.Parallel()
	if _, err := markMask("", 8, 8); err != nil {
		t.Fatalf("default mark: %v", err)
	}
	if _, err := markMask("launcher", 8, 8); err != nil {
		t.Fatalf("launcher mark: %v", err)
	}
	if _, err := markMask("bogus", 8, 8); err == nil {
		t.Fatal("unknown mark must error")
	}
}

func TestLauncherMarkAsset(t *testing.T) {
	t.Parallel()
	data := LauncherPNG()
	if len(data) == 0 {
		t.Fatal("launcher mark is empty")
	}
	sum := sha256.Sum256(data)
	const want = "02f3a6246c193b06701e8d99d7cfbcb5b57136db943d2a67aca8740891827d3f"
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("launcher mark hash = %s, want %s", got, want)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.Width != 1024 || cfg.Height != 1024 {
		t.Fatalf("launcher mark size = %dx%d, want 1024x1024", cfg.Width, cfg.Height)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := visibleBounds(img); got != image.Rect(128, 128, 896, 896) {
		t.Fatalf("visible bounds = %v, want (128,128)-(896,896)", got)
	}
}
