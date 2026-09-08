package render

import (
	"bytes"
	"image"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

var blackToWhite = []gradientStop{
	{at: 0, c: Color{A: 255}},
	{at: 1, c: Color{R: 255, G: 255, B: 255, A: 255}},
}

func TestPeriodicGradientWrapsWithoutAColourJump(t *testing.T) {
	t.Parallel()
	primary := Color{R: 0x20, G: 0x70, B: 0xe0, A: 0xff}
	stops := []gradientStop{
		{at: 0, c: primary},
		{at: 0.33, c: Color{R: 0xb0, G: 0x50, B: 0xd0, A: 0xff}},
		{at: 0.66, c: Color{R: 0x30, G: 0xc0, B: 0xb0, A: 0xff}},
		{at: 1, c: primary},
	}
	axis := gradientAxisForDegrees(0)
	atZero := sampleGradientPeriodic(stops, axis, 0, 0.25, 0)
	atOne := sampleGradientPeriodic(stops, axis, 1, 0.25, 0)
	if atZero != atOne {
		t.Fatalf("phase 0 = %+v, phase 1 = %+v", atZero, atOne)
	}
	before := sampleGradientPeriodic(stops, axis, -0.001, 0, 0)
	after := sampleGradientPeriodic(stops, axis, 0.001, 0, 0)
	if colourDistance(before, after) > 4 {
		t.Fatalf("wrap jumps from %+v to %+v", before, after)
	}
}

func colourDistance(a, b Color) int {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) + d(a.G, b.G) + d(a.B, b.B) + d(a.A, b.A)
}

func TestLoopingWordmarkRepeatsAtOnePhase(t *testing.T) {
	t.Parallel()
	g := ui.GradientPaint{
		Stops: [4]ui.GradientStop{
			{At: 0, Role: ui.PaintPrimary},
			{At: 0.33, Role: ui.PaintSecondary},
			{At: 0.66, Role: ui.PaintTertiary},
			{At: 1, Role: ui.PaintPrimary},
		},
		Count: 4, Motion: ui.GradientLoop, From: 0, To: 1,
	}
	if a, b := paintWordmarkAt(t, g, 0), paintWordmarkAt(t, g, 1); !bytes.Equal(a.Pix, b.Pix) {
		t.Fatal("loop phase 0 and 1 painted different rasters")
	}
}

func TestGradientAxisHorizontal(t *testing.T) {
	t.Parallel()
	a := gradientAxisForDegrees(0)
	if a.x != 1 || a.y != 0 {
		t.Fatalf("axis = %+v, want x=1 y=0", a)
	}
}

func TestGradientAxisVerticalSnaps(t *testing.T) {
	t.Parallel()
	a := gradientAxisForDegrees(90)
	if a.x != 0 || a.y != 1 {
		t.Fatalf("axis = %+v, want x=0 y=1", a)
	}
}

func TestSampleStopsPinsEndpoints(t *testing.T) {
	t.Parallel()
	stops := []gradientStop{
		{at: 0, c: Color{R: 0, A: 255}},
		{at: 1, c: Color{R: 255, A: 255}},
	}
	if got := sampleStops(stops, 0); got.R != 0 {
		t.Fatalf("t=0: R=%d, want 0", got.R)
	}
	if got := sampleStops(stops, 1); got.R != 255 {
		t.Fatalf("t=1: R=%d, want 255", got.R)
	}
	mid := sampleStops(stops, 0.5)
	if mid.R < 120 || mid.R > 135 {
		t.Fatalf("t=0.5: R=%d, want ~127", mid.R)
	}
}

func TestFillRectGradientHorizontalRamp(t *testing.T) {
	t.Parallel()
	c := newTestCanvas(t, 4, 1)
	fillRectGradient(c, ui.Rect{W: 4, H: 1}, blackToWhite, 0, 0, false)
	left, right := pixelAt(t, c, 0, 0), pixelAt(t, c, 3, 0)
	if left.R >= right.R {
		t.Fatalf("x=0 R=%d, x=3 R=%d, want dark→light", left.R, right.R)
	}
}

func TestFillRectGradient180DoesNotCollapse(t *testing.T) {
	t.Parallel()
	c := newTestCanvas(t, 2, 1)
	fillRectGradient(c, ui.Rect{W: 2, H: 1}, blackToWhite, 180, 0, false)
	a, b := pixelAt(t, c, 0, 0), pixelAt(t, c, 1, 0)
	if a == b {
		t.Fatalf("180° 2×1 collapsed to %+v", a)
	}
}

func TestBlendMaskGradientHorizontalRamp(t *testing.T) {
	t.Parallel()
	c := newTestCanvas(t, 4, 1)
	mask := image.NewAlpha(image.Rect(0, 0, 4, 1))
	for i := range mask.Pix {
		mask.Pix[i] = 255
	}
	blendMaskGradient(c, mask, 0, 0, blackToWhite, 0, 0, false)
	left, right := pixelAt(t, c, 0, 0), pixelAt(t, c, 3, 0)
	if left.R >= right.R {
		t.Fatalf("x=0 R=%d, x=3 R=%d, want dark→light", left.R, right.R)
	}
}

func TestBlendMaskGradientZeroCoverageLeavesCanvasUntouched(t *testing.T) {
	t.Parallel()
	c := newTestCanvas(t, 4, 1)
	for i := range c.Pix {
		c.Pix[i] = 0x42
	}
	before := append([]byte(nil), c.Pix...)
	mask := image.NewAlpha(image.Rect(0, 0, 4, 1))
	blendMaskGradient(c, mask, 0, 0, blackToWhite, 0, 0, false)
	for i := range before {
		if c.Pix[i] != before[i] {
			t.Fatal("zero-coverage mask changed the canvas")
		}
	}
}
