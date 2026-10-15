package render

import (
	"bytes"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// sharedBacking reports whether two images hand out the same pixel array.
func sharedBacking(a, b *ui.Image) bool {
	if len(a.Pix) == 0 || len(b.Pix) == 0 {
		return false
	}
	return &a.Pix[0] == &b.Pix[0]
}

// solid builds a uniform premultiplied image.
func solid(w, h int, r, g, b, a byte) *ui.Image {
	img := &ui.Image{Width: w, Height: h, Stride: w * 4, Pix: make([]byte, w*h*4)}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = b, g, r, a
	}
	return img
}

func TestBlurLeavesAUniformFieldUnchanged(t *testing.T) {
	t.Parallel()
	// A blur of a constant is that constant. If the edge handling wraps or
	// reads outside the buffer, this is where it shows.
	src := solid(64, 64, 0x20, 0x30, 0x40, 0xff)
	got := Blur(src, 4, 24)
	if got == nil {
		t.Fatal("Blur returned nil for a valid source")
	}
	for i := 0; i < len(got.Pix); i += 4 {
		if got.Pix[i] != 0x40 || got.Pix[i+1] != 0x30 || got.Pix[i+2] != 0x20 || got.Pix[i+3] != 0xff {
			t.Fatalf("pixel %d = %v, want the source colour", i/4, got.Pix[i:i+4])
		}
	}
}

func TestBlurReducesByTheFactor(t *testing.T) {
	t.Parallel()
	got := Blur(solid(64, 32, 1, 2, 3, 0xff), 4, 24)
	if got.Width != 16 || got.Height != 8 {
		t.Fatalf("size = %dx%d, want 16x8", got.Width, got.Height)
	}
	if got.Stride != got.Width*4 {
		t.Errorf("stride = %d, want %d", got.Stride, got.Width*4)
	}
}

func TestBlurSpreadsASinglePixel(t *testing.T) {
	t.Parallel()
	// One bright pixel in a dark field must leak into its neighbours, and the
	// result must stay inside the source's range.
	src := solid(32, 32, 0, 0, 0, 0xff)
	o := (16*32 + 16) * 4
	src.Pix[o], src.Pix[o+1], src.Pix[o+2] = 0xff, 0xff, 0xff
	got := Blur(src, 1, 3)
	centre := (16*32 + 16) * 4
	near := (16*32 + 17) * 4
	if got.Pix[near] == 0 {
		t.Error("the neighbour stayed black; nothing spread")
	}
	if got.Pix[near] > got.Pix[centre] {
		t.Error("the neighbour is brighter than the centre")
	}
}

func TestBlurIsIndependentOfRadius(t *testing.T) {
	t.Parallel()
	// The sliding window means cost does not grow with radius. Correctness
	// must not either: a uniform field is uniform at any radius.
	for _, r := range []int{1, 8, 64} {
		got := Blur(solid(32, 32, 9, 9, 9, 0xff), 1, r)
		if got.Pix[0] != 9 {
			t.Errorf("radius %d changed a uniform field to %d", r, got.Pix[0])
		}
	}
}

func TestBlurRejectsDegenerateInput(t *testing.T) {
	t.Parallel()
	if Blur(nil, 4, 8) != nil {
		t.Error("nil source should return nil")
	}
	if Blur(solid(2, 2, 0, 0, 0, 0xff), 4, 8) != nil {
		t.Error("a source smaller than the factor should return nil, not a zero-sized image")
	}
	short := &ui.Image{Width: 8, Height: 8, Stride: 32, Pix: make([]byte, 16)}
	if Blur(short, 1, 2) != nil {
		t.Error("a buffer shorter than its declared geometry should return nil")
	}
}

// TestBlurPairReturnsTheSharpCopyBesideTheBlur pins the contract the material's
// auto-frost needs: the reduced sharp capture and the blurred one from one
// downsample, at the same geometry, as separate buffers.
func TestBlurPairReturnsTheSharpCopyBesideTheBlur(t *testing.T) {
	t.Parallel()
	src := solid(64, 32, 0x40, 0x30, 0x20, 0xff)
	sharp, blurred := BlurPair(src, 4, 24)
	if sharp == nil || blurred == nil {
		t.Fatal("BlurPair returned nil for a valid source")
	}
	want := Blur(src, 4, 24)
	if blurred.Width != want.Width || blurred.Height != want.Height {
		t.Fatalf("blurred = %dx%d, Blur gives %dx%d", blurred.Width, blurred.Height, want.Width, want.Height)
	}
	if !bytes.Equal(blurred.Pix, want.Pix) {
		t.Error("BlurPair's blurred half differs from Blur")
	}
	if sharp.Width != want.Width || sharp.Height != want.Height {
		t.Fatalf("sharp = %dx%d, want the reduced %dx%d", sharp.Width, sharp.Height, want.Width, want.Height)
	}
	if sharedBacking(sharp, src) || sharedBacking(blurred, src) {
		t.Error("a returned image aliases the source's pixels")
	}
	if sharedBacking(sharp, blurred) {
		t.Error("the sharp copy aliases the blurred image; the busy check needs both")
	}

	// The sharp half must be the reduced capture, not a second blurred copy.
	spiked := solid(32, 32, 0, 0, 0, 0xff)
	o := (16*32 + 16) * 4
	spiked.Pix[o], spiked.Pix[o+1], spiked.Pix[o+2] = 0xff, 0xff, 0xff
	sharpSpike, blurredSpike := BlurPair(spiked, 1, 3)
	centre := (16*32 + 16) * 4
	if sharpSpike.Pix[centre] != 0xff {
		t.Errorf("the sharp half lost the bright pixel: %d", sharpSpike.Pix[centre])
	}
	if blurredSpike.Pix[centre] >= sharpSpike.Pix[centre] {
		t.Error("the blurred half is not lower than the sharp one")
	}
}

func TestBlurPairRejectsDegenerateInput(t *testing.T) {
	t.Parallel()
	if sharp, blurred := BlurPair(nil, 4, 8); sharp != nil || blurred != nil {
		t.Error("nil source should return nils")
	}
	if sharp, blurred := BlurPair(solid(2, 2, 0, 0, 0, 0xff), 4, 8); sharp != nil || blurred != nil {
		t.Error("a source smaller than the factor should return nils")
	}
	short := &ui.Image{Width: 8, Height: 8, Stride: 32, Pix: make([]byte, 16)}
	if sharp, blurred := BlurPair(short, 1, 2); sharp != nil || blurred != nil {
		t.Error("a buffer shorter than its declared geometry should return nils")
	}
	// With no radius there is nothing to blur, so both halves are the same
	// reduced capture.
	sharp, blurred := BlurPair(solid(16, 16, 7, 7, 7, 0xff), 1, 0)
	if sharp == nil || !bytes.Equal(sharp.Pix, blurred.Pix) {
		t.Error("a zero radius did not return one reduced capture for both halves")
	}
}
