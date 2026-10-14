package render

import (
	"bytes"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// meanLuma averages the perceived brightness of an image's pixels, so a test
// can say "darker" or "lighter" without asserting a specific curve.
func meanLuma(img *ui.Image) int {
	if img == nil || len(img.Pix) == 0 {
		return 0
	}
	sum, n := 0, 0
	for i := 0; i+3 < len(img.Pix); i += 4 {
		sum += luma(img.Pix[i : i+4])
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

// meanCanvasLuma averages a painted canvas's brightness, so a test can say
// "darker" without asserting a specific curve.
func meanCanvasLuma(c *Canvas) int {
	if c == nil || c.Width <= 0 || c.Height <= 0 || c.Stride <= 0 {
		return 0
	}
	sum, n := 0, 0
	for y := 0; y < c.Height; y++ {
		for x := 0; x < c.Width; x++ {
			o := y*c.Stride + x*4
			if o+3 >= len(c.Pix) {
				continue
			}
			sum += luma(c.Pix[o : o+4])
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

// chequer builds a high-detail image: the content auto-frost must recognise as
// busy.
func chequer(w, h, cell int) *ui.Image {
	img := &ui.Image{Width: w, Height: h, Stride: w * 4, Pix: make([]byte, w*h*4)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := byte(0x10)
			if (x/cell+y/cell)%2 == 0 {
				v = 0xf0
			}
			o := y*img.Stride + x*4
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = v, v, v, 0xff
		}
	}
	return img
}

func TestMaterializeSmokeLowersLumaWithinItsCeiling(t *testing.T) {
	t.Parallel()
	src := solid(16, 16, 0x40, 0x40, 0x40, 0xff)
	identity := Materialize(nil, src, Material{Tone: ToneSmoke, Intensity: 0})
	if identity == nil || !bytes.Equal(identity.Pix, src.Pix) {
		t.Fatal("zero intensity is not the identity")
	}
	got := Materialize(nil, src, Material{Tone: ToneSmoke, Intensity: 100})
	if got == nil {
		t.Fatal("smoke returned nil")
	}
	before, after := meanLuma(src), meanLuma(got)
	if after >= before {
		t.Fatalf("smoke did not lower luma: %d -> %d", before, after)
	}
	// The cap: at full intensity a pixel keeps at least 1-0.58 of its value.
	if after*100 < before*40 {
		t.Errorf("smoke fell to %d from %d, past its ceiling", after, before)
	}
	if sharedBacking(got, src) {
		t.Error("a transfer must not return the input buffer")
	}
	// The axis cannot be driven past its design range from code either.
	over := Materialize(nil, src, Material{Tone: ToneSmoke, Intensity: 255})
	full := Materialize(nil, src, Material{Tone: ToneSmoke, Intensity: 100})
	if !bytes.Equal(over.Pix, full.Pix) {
		t.Error("an intensity above the ceiling was not clamped")
	}
}

func TestMaterializeMilkLiftsTowardWhite(t *testing.T) {
	t.Parallel()
	dark := solid(16, 16, 0x10, 0x10, 0x10, 0xff)
	identity := Materialize(nil, dark, Material{Tone: ToneMilk, Intensity: 0})
	if !bytes.Equal(identity.Pix, dark.Pix) {
		t.Fatal("zero intensity is not the identity")
	}
	lifted := Materialize(nil, dark, Material{Tone: ToneMilk, Intensity: 100})
	if meanLuma(lifted) <= meanLuma(dark) {
		t.Fatal("milk did not lift luma")
	}
	for i := 0; i+3 < len(lifted.Pix); i += 4 {
		if lifted.Pix[i+3] != 0xff {
			t.Fatalf("milk changed the alpha channel: %v", lifted.Pix[i:i+4])
		}
		if lifted.Pix[i] > 255 || lifted.Pix[i+1] > 255 || lifted.Pix[i+2] > 255 {
			t.Fatal("milk overshot the channel range")
		}
	}
}

func TestMaterializeChoosesTheGroundPerTone(t *testing.T) {
	t.Parallel()
	if got := Materialize(nil, solid(8, 8, 1, 1, 1, 0xff), Material{Tone: ToneNone}); got != nil {
		t.Error("ToneNone should leave the surface's backdrop untouched")
	}
	plain := solid(32, 32, 0x20, 0x20, 0x20, 0xff)
	sharpPlain, blurredPlain := BlurPair(plain, 4, 12)
	if got := Materialize(sharpPlain, blurredPlain, Material{Tone: ToneFrost}); !sharedBacking(got, blurredPlain) {
		t.Error("frost should resolve to the blurred capture")
	}
	if got := Materialize(sharpPlain, blurredPlain, Material{Tone: ToneAuto}); !sharedBacking(got, sharpPlain) {
		t.Error("auto-frost should leave plain content clear")
	}
	// The cells are wider than the downsample block on purpose: a pattern the
	// reduction itself destroys is not content the blur can be blamed for, and
	// would score zero.
	busy := chequer(48, 48, 8)
	sharpBusy, blurredBusy := BlurPair(busy, 4, 12)
	if got := Materialize(sharpBusy, blurredBusy, Material{Tone: ToneAuto}); !sharedBacking(got, blurredBusy) {
		t.Error("auto-frost should frost busy content")
	}
}

// TestMaterialGroundIsWhatThePainterComposites is the offscreen half of the
// phase gate: the ground the shell resolves is the ground that reaches the
// framebuffer, through the real painter.
//
// The live half cannot run here — a second shell refuses to start while another
// owns the session socket (ipc.ErrSingleInstance), and taking over the owner's
// session is not something a test may do — so the live check is recorded unrun
// rather than asserted.
func TestMaterialGroundIsWhatThePainterComposites(t *testing.T) {
	t.Parallel()
	// A blur preserves a constant, so a plain capture's two halves are equal;
	// detail is what separates them.
	plain := solid(32, 32, 0x40, 0x40, 0x40, 0xff)
	busySharp := chequer(32, 32, 8)
	busyBlurred := solid(32, 32, 0x80, 0x80, 0x80, 0xff)

	base := testStyle
	base.SurfaceOpacity = 0x80
	base.Radius = 12

	blurredOnly := base
	blurredOnly.Backdrop = plain

	smoke := base
	smoke.Backdrop = Materialize(plain, plain, Material{Tone: ToneSmoke, Intensity: 100})

	frosted := base
	frosted.Backdrop = Materialize(busySharp, busyBlurred, Material{Tone: ToneAuto, Intensity: 100})

	frostedReference := base
	frostedReference.Backdrop = busyBlurred

	plainCanvas := newTestCanvas(t, canvasW, canvasH)
	paintTree(t, plainCanvas, base)
	blurCanvas := newTestCanvas(t, canvasW, canvasH)
	paintTree(t, blurCanvas, blurredOnly)
	smokeCanvas := newTestCanvas(t, canvasW, canvasH)
	paintTree(t, smokeCanvas, smoke)
	frostedCanvas := newTestCanvas(t, canvasW, canvasH)
	paintTree(t, frostedCanvas, frosted)
	referenceCanvas := newTestCanvas(t, canvasW, canvasH)
	paintTree(t, referenceCanvas, frostedReference)

	if bytes.Equal(plainCanvas.Pix, blurCanvas.Pix) {
		t.Fatal("the backdrop chain changed nothing; the fixture is inert")
	}
	if bytes.Equal(blurCanvas.Pix, smokeCanvas.Pix) {
		t.Error("the smoke ground reached the framebuffer unchanged")
	}
	// The ground replaces the capture rather than being added beside it: the
	// interior is darker under smoke than under the plain capture.
	if meanCanvasLuma(smokeCanvas) >= meanCanvasLuma(blurCanvas) {
		t.Errorf("smoke interior luma %d is not below the plain capture's %d",
			meanCanvasLuma(smokeCanvas), meanCanvasLuma(blurCanvas))
	}
	// Auto-frost over busy content is the blurred capture, so its painted
	// result matches a canvas given that ground directly.
	if !bytes.Equal(frostedCanvas.Pix, referenceCanvas.Pix) {
		t.Error("auto-frost over busy content did not paint the blurred capture")
	}
	// The rounded silhouette still holds: the corner never fills.
	if a := smokeCanvas.Pix[3]; a != 0 {
		t.Errorf("corner alpha = %d, want the mask to leave it clear", a)
	}
}

func TestBusynessSeparatesBusyFromPlain(t *testing.T) {
	t.Parallel()
	plainSharp, plainBlur := BlurPair(solid(32, 32, 0x20, 0x20, 0x20, 0xff), 4, 12)
	busySharp, busyBlur := BlurPair(chequer(48, 48, 8), 4, 12)
	plain := Busyness(plainSharp, plainBlur, autoFrostPoints)
	busy := Busyness(busySharp, busyBlur, autoFrostPoints)
	if plain >= busy {
		t.Fatalf("plain content scored %d against busy content's %d", plain, busy)
	}
	if busy < autoFrostThreshold {
		t.Errorf("busy content scored %d, below the frost threshold %d", busy, autoFrostThreshold)
	}
	if plain >= autoFrostThreshold {
		t.Errorf("plain content scored %d, at or above the frost threshold %d", plain, autoFrostThreshold)
	}
	if Busyness(nil, nil, autoFrostPoints) != 0 {
		t.Error("nil inputs should score zero rather than fail")
	}
	if Busyness(plainSharp, plainBlur, 0) != 0 {
		t.Error("zero sample points should score zero")
	}
}
