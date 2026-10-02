package render

import (
	"bytes"
	"math"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func paintSpinnerAt(t *testing.T, phase float64, tone ui.Tone) *Canvas {
	t.Helper()
	c := newTestCanvas(t, 32, 32)
	n := &ui.Node{Kind: ui.KindSpinner, Key: "s", Width: 32, Value: phase, Tone: tone, Bounds: ui.Rect{W: 32, H: 32}}
	if err := paintNode(c, n, NewTextRenderer(mustTestFace(t)), testStyle, testStyle.Size); err != nil {
		t.Fatal(err)
	}
	return c
}

// ringPoint is the pixel on the spinner's band at a clockwise angle from the
// top, in turns.
func ringPoint(turn float64) (int, int) {
	r := 16 - 0.5 - float64(32/10)/2
	a := turn * 2 * math.Pi
	return int(16 + math.Sin(a)*r), int(16 - math.Cos(a)*r)
}

func TestSpinnerArcTurnsWithItsPhase(t *testing.T) {
	t.Parallel()
	rest := paintSpinnerAt(t, 0, ui.ToneNormal)
	half := paintSpinnerAt(t, 0.5, ui.ToneNormal)
	if bytes.Equal(rest.Pix, half.Pix) {
		t.Fatal("phase 0 and 0.5 painted the same raster")
	}
	accent := testStyle.accent()
	x, y := ringPoint(spinnerSweep / 2)
	if got := pixelAt(t, rest, x, y); got != accent {
		t.Fatalf("mid-arc pixel at phase 0 = %+v, want accent %+v", got, accent)
	}
	if got := pixelAt(t, half, x, y); got == accent {
		t.Fatal("the arc did not move away at phase 0.5")
	}
	x, y = ringPoint(0.5 + spinnerSweep/2)
	if got := pixelAt(t, half, x, y); got != accent {
		t.Fatalf("mid-arc pixel at phase 0.5 = %+v, want accent", got)
	}
}

func TestSpinnerTakesItsTone(t *testing.T) {
	t.Parallel()
	c := paintSpinnerAt(t, 0, ui.ToneError)
	x, y := ringPoint(spinnerSweep / 2)
	if got := pixelAt(t, c, x, y); got != testStyle.Error {
		t.Fatalf("error-toned arc = %+v, want %+v", got, testStyle.Error)
	}
}
