package render

import (
	"bytes"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func premulPixel(w, h int, b, g, r, a byte) *ui.Image {
	img := &ui.Image{Width: w, Height: h, Stride: w * 4, Pix: make([]byte, w*h*4)}
	for i := 0; i+3 < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = b, g, r, a
	}
	return img
}

func TestUnmixRecoversTheBackgroundAtBothExtremes(t *testing.T) {
	t.Parallel()
	// Nothing of ours: the capture is the background, untouched.
	captured := premulPixel(4, 4, 0x40, 0x40, 0x40, 0xff)
	empty := premulPixel(4, 4, 0, 0, 0, 0x00)
	got := Unmix(captured, empty)
	if !bytes.Equal(got.Pix, captured.Pix) {
		t.Error("an empty contribution changed the capture")
	}

	// Fully ours: the background is not in the pixel at all, so nothing is
	// recovered and the result is transparent.
	ours := premulPixel(4, 4, 0x80, 0x80, 0x80, 0xff)
	full := Unmix(captured, ours)
	if full.Pix[3] != 0x00 {
		t.Errorf("a fully owned pixel left alpha %d, want it cleared", full.Pix[3])
	}

	// Half ours: background = (captured - ours) / (1 - alpha), exactly.
	captured = premulPixel(4, 4, 0xc0, 0xc0, 0xc0, 0xff)
	ours = premulPixel(4, 4, 0x40, 0x40, 0x40, 0x80)
	half := Unmix(captured, ours)
	for ch := 0; ch < 3; ch++ {
		if half.Pix[ch] != 0xff {
			t.Fatalf("channel %d = %#x, want the recovered 0xff", ch, half.Pix[ch])
		}
	}
	if half.Pix[3] != 0xff {
		t.Errorf("a recovered pixel has alpha %d, want opaque", half.Pix[3])
	}
}

func TestUnmixClampsAndPassesThroughWhatItCannotUse(t *testing.T) {
	t.Parallel()
	// Ours brighter than the capture would recover below zero: it clamps rather
	// than wrapping into a bright artefact.
	captured := premulPixel(2, 2, 0x10, 0x10, 0x10, 0xff)
	ours := premulPixel(2, 2, 0x80, 0x80, 0x80, 0x40)
	got := Unmix(captured, ours)
	if got.Pix[0] != 0 {
		t.Errorf("recovered %#x from an impossible subtraction, want 0", got.Pix[0])
	}
	// A nil or mismatched contribution is no contribution: the capture passes
	// through, because subtracting a different geometry would be worse.
	if Unmix(captured, nil) != captured {
		t.Error("a nil contribution did not pass the capture through")
	}
	if other := premulPixel(3, 3, 0, 0, 0, 0x80); Unmix(captured, other) != captured {
		t.Error("a mismatched contribution did not pass the capture through")
	}
	if Unmix(nil, ours) != nil {
		t.Error("a nil capture returned a buffer")
	}
}

func TestLensRungDegradesToTheStaticCapture(t *testing.T) {
	t.Parallel()
	live := LensState{FrameAge: 50 * time.Millisecond, OursComplete: true}
	if got := LensRungFor(live); got != LensLive {
		t.Errorf("a fresh, accounted frame resolved %v, want live", got)
	}
	for _, tc := range []struct {
		name string
		s    LensState
	}{
		{"no frame yet", LensState{OursComplete: true}},
		{"aged frame", LensState{FrameAge: MaxLensAge + time.Millisecond, OursComplete: true}},
		{"unaccounted region", LensState{FrameAge: 10 * time.Millisecond}},
		{"failed past the cap", LensState{FrameAge: 10 * time.Millisecond, OursComplete: true, Failures: MaxLensFailures}},
	} {
		if got := LensRungFor(tc.s); got != LensStatic {
			t.Errorf("%s resolved %v, want static", tc.name, got)
		}
	}
	// One failure still runs: the ladder degrades on a run, not on a hiccup.
	nearly := LensState{FrameAge: 10 * time.Millisecond, OursComplete: true, Failures: MaxLensFailures - 1}
	if got := LensRungFor(nearly); got != LensLive {
		t.Errorf("one failure below the cap resolved %v, want live", got)
	}
	if !LensKeepsLastFrame(LensState{FrameAge: time.Second}) {
		t.Error("an aged frame is not reported as worth keeping")
	}
	if LensKeepsLastFrame(LensState{Failures: MaxLensFailures, FrameAge: time.Second}) {
		t.Error("a surface that stopped asking still claims to keep a frame")
	}
}
