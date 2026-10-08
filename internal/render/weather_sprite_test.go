package render

import (
	"image"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestWeatherFBMIsDeterministicAndBounded(t *testing.T) {
	for i := 0; i < 500; i++ {
		x, y := float64(i)*.37, float64(i)*.11
		a, b := weatherFBM(x, y, 9, 5), weatherFBM(x, y, 9, 5)
		if a != b || a < 0 || a >= 1 {
			t.Fatalf("fbm(%v,%v) = %v / %v", x, y, a, b)
		}
	}
	if weatherFBM(1.5, 2.5, 1, 4) == weatherFBM(1.5, 2.5, 2, 4) {
		t.Error("seed does not change the field")
	}
}

func TestWeatherSpriteBakeAndBlitStayInsideTheMask(t *testing.T) {
	s := bakeWeatherSprite(40, 20, func(u, v float64) Color { return Color{R: 255, G: 255, B: 255, A: 255} })
	if s.w != 40 || s.h != 20 || len(s.pix) != 40*20*4 {
		t.Fatalf("sprite = %dx%d len %d", s.w, s.h, len(s.pix))
	}
	c := newTestCanvas(t, 64, 64)
	box := ui.Rect{X: 8, Y: 8, W: 32, H: 32}
	mask := fullMask(box.W, box.H)
	mask.Pix[0] = 0 // the box's top-left corner is outside the shape
	blitWeatherSprite(c, box, mask, s, -10, -5, 1)
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			inBox := x >= box.X && x < box.X+box.W && y >= box.Y && y < box.Y+box.H
			painted := pixelAt(t, c, x, y).A != 0
			if painted && !inBox {
				t.Fatalf("pixel (%d,%d) painted outside the box", x, y)
			}
		}
	}
	if pixelAt(t, c, box.X, box.Y).A != 0 {
		t.Error("masked corner was painted")
	}
	if pixelAt(t, c, box.X+5, box.Y+5).A == 0 {
		t.Error("sprite did not land inside the box")
	}
	// Degenerate inputs are no-ops, never panics.
	blitWeatherSprite(c, ui.Rect{}, mask, s, 0, 0, 1)
	blitWeatherSprite(c, box, mask, nil, 0, 0, 1)
	blitWeatherSprite(c, box, mask, s, 1000, -1000, 1)
	_ = bakeWeatherSprite(0, 0, func(float64, float64) Color { return Color{} })
	_ = bakeWeatherSprite(1, 1, func(float64, float64) Color { return Color{A: 255} })
}

func TestWeatherSpriteCacheIsBoundedAndReuses(t *testing.T) {
	weatherSpriteReset()
	bakes := 0
	bake := func() *weatherSprite {
		bakes++
		return bakeWeatherSprite(2, 2, func(float64, float64) Color { return Color{A: 255} })
	}
	first := weatherSpriteFor(weatherSpriteKey{kind: "t", seed: 1, w: 2, h: 2}, bake)
	again := weatherSpriteFor(weatherSpriteKey{kind: "t", seed: 1, w: 2, h: 2}, bake)
	if first != again || bakes != 1 {
		t.Fatalf("cache baked %d times for one key", bakes)
	}
	for i := uint64(0); i < 3*weatherSpriteCap; i++ {
		weatherSpriteFor(weatherSpriteKey{kind: "t", seed: 100 + i, w: 2, h: 2}, bake)
	}
	if n := weatherSpriteLen(); n > weatherSpriteCap {
		t.Fatalf("cache holds %d sprites, cap %d", n, weatherSpriteCap)
	}
}

func fullMask(w, h int) *image.Alpha {
	m := image.NewAlpha(image.Rect(0, 0, w, h))
	for i := range m.Pix {
		m.Pix[i] = 255
	}
	return m
}
