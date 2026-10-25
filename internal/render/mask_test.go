package render

import (
	"bytes"
	"image"
	"sync"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestRoundedMaskCornersTransparentCenterOpaque(t *testing.T) {
	m := RoundedMask(12, 100, 60)
	if m.AlphaAt(0, 0).A != 0 {
		t.Fatal("corner must be transparent")
	}
	if m.AlphaAt(50, 30).A != 255 {
		t.Fatal("center must be opaque")
	}
	if got := m.AlphaAt(3, 3).A; got == 0 || got == 255 {
		t.Fatalf("arc edge must have partial coverage, got %d", got)
	}
}

func TestRoundedMaskCacheReuses(t *testing.T) {
	a := RoundedMask(12, 100, 60)
	b := RoundedMask(12, 100, 60)
	if a != b {
		t.Fatal("same key must return same mask")
	}
}

func TestShadowTextureExtendsBeyondBounds(t *testing.T) {
	s := ShadowTexture(100, 60, 12, ElevPanel)
	if s.Bounds().Dx() <= 100 || s.Bounds().Dy() <= 60 {
		t.Fatal("shadow must spread beyond panel")
	}
	if s.AlphaAt(s.Bounds().Dx()/2, 12).A <= s.AlphaAt(0, 0).A {
		t.Fatal("shadow alpha must be stronger along the panel edge")
	}
}

func TestGeometryMaskCacheBoundsRetainedBytesAndRegeneratesSamePixels(t *testing.T) {
	type retained struct {
		name string
		get  func() *image.Alpha
		mask *image.Alpha
		pix  []byte
	}
	kept := []retained{
		{name: "rounded", get: func() *image.Alpha { return RoundedMask(11, 37, 25) }},
		{name: "corner", get: func() *image.Alpha { return CornerMask(11, 37, 25, SquareTR) }},
		{name: "ring", get: func() *image.Alpha { return RingMask(11, 37, 25, 2) }},
		{name: "shadow", get: func() *image.Alpha { return ShadowTexture(37, 25, 11, ElevMenu) }},
		{name: "search glyph", get: func() *image.Alpha { return SearchGlyphMask(18, 2) }},
		{name: "clear glyph", get: func() *image.Alpha { return ClearGlyphMask(18, 2) }},
	}
	for i := range kept {
		kept[i].mask = kept[i].get()
		kept[i].pix = append([]byte(nil), kept[i].mask.Pix...)
	}
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for i := 0; i < 16; i++ {
				w := 24 + (worker+i)%8
				RoundedMask(6, w, 20)
				RingMask(6, w, 20, 2)
				ShadowTexture(w, 20, 6, ElevMenu)
				SearchGlyphMask(w, 2)
				ClearGlyphMask(w, 2)
			}
		}(worker)
	}
	workers.Wait()

	first := RoundedMask(40, 3001, 80)
	for w := 3002; w <= 3144; w++ {
		RoundedMask(40, w, 80)
	}

	maskMu.Lock()
	retainedBytes := geometryCache.bytes
	maskMu.Unlock()
	if retainedBytes > geometryCacheBudget {
		t.Fatalf("geometry cache retains %d bytes, over %d bytes", retainedBytes, geometryCacheBudget)
	}

	if got := RoundedMask(40, 3001, 80); got == first {
		t.Fatal("oldest mask survived enough varied sizes to exceed the byte budget")
	}
	for _, tc := range kept {
		got := tc.get()
		if got == tc.mask {
			t.Errorf("%s mask survived cache pressure; expected regeneration", tc.name)
		}
		if !bytes.Equal(got.Pix, tc.pix) {
			t.Errorf("%s pixels changed after eviction and regeneration", tc.name)
		}
		if again := tc.get(); again != got {
			t.Errorf("%s repeated lookup did not reuse the regenerated mask", tc.name)
		}
	}

	for radius := 0; radius < geometryCacheMaxEntries+100; radius++ {
		CornerMask(radius, 0, 0, 0)
	}
	maskMu.Lock()
	entries := len(geometryCache.entries)
	maskMu.Unlock()
	if entries > geometryCacheMaxEntries {
		t.Fatalf("geometry cache retains %d entries, over %d", entries, geometryCacheMaxEntries)
	}
}

func TestCanvasFillRoundedMatchesMask(t *testing.T) {
	c, err := NewCanvas(make([]byte, 40*40*4), 40, 40, 40*4)
	if err != nil {
		t.Fatal(err)
	}
	c.FillRounded(ui.Rect{W: 40, H: 40}, 12, Color{R: 255, A: 255})
	if got := c.Pix[3]; got != 0 {
		t.Fatalf("corner alpha = %d, want untouched", got)
	}
	if got := c.Pix[20*c.Stride+20*4+3]; got != 255 {
		t.Fatalf("center alpha = %d, want opaque", got)
	}
}

func TestRippleMaskClipsTheDiscToTheBox(t *testing.T) {
	inside := RippleMask(20, 20, 5, 10, 10, 6)
	if inside.AlphaAt(10, 10).A != 255 {
		t.Fatal("the disc's centre is not opaque")
	}
	if inside.AlphaAt(0, 0).A != 0 {
		t.Fatal("the disc leaks past the rounded corner")
	}
	// The centre is seven pixels past the right edge, farther than its radius.
	outside := RippleMask(20, 20, 5, 27, 10, 6)
	for i := range outside.Pix {
		if outside.Pix[i] != 0 {
			t.Fatal("a disc beyond the box painted inside it")
		}
	}
}

func TestRoundedMaskVisibleGeneratesOnlyTheClippedSlice(t *testing.T) {
	const w, h = 200, 100
	c, err := NewCanvas(make([]byte, w*h*4), w, h, w*4)
	if err != nil {
		t.Fatal(err)
	}
	c.restrict = ui.Rect{X: 10, Y: 20, W: 30, H: 40}
	m, px, py := c.roundedMaskVisible(ui.Rect{X: 0, Y: 0, W: 1000, H: 50000}, 12, 0)
	if m == nil {
		t.Fatal("a box crossing the clip produced no mask")
	}
	if b := m.Bounds(); b.Dx() > 30 || b.Dy() > 40 {
		t.Fatalf("mask is %dx%d, larger than the 30x40 clip", b.Dx(), b.Dy())
	}
	if px != 10 || py != 20 {
		t.Fatalf("mask placed at %d,%d, want the clip origin 10,20", px, py)
	}
	if m, _, _ := c.roundedMaskVisible(ui.Rect{X: 300, Y: 0, W: 10, H: 10}, 12, 0); m != nil {
		t.Fatal("a box outside the clip generated a mask")
	}
}
