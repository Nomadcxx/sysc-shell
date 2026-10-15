package render

import (
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Unmix recovers the background from a capture that contains the shell's own
// pixels.
//
// The compositor blends source-over, so a captured pixel is
// ours + (1 - our alpha) * background and the background is therefore
// (captured - ours) / (1 - our alpha), per channel. ours carries the shell's own
// surface as it was last committed, in the same premultiplied BGRA layout, with
// its alpha saying how much of the pixel it owns.
//
// What this can recover is bounded and the bound is the point: content the shell
// cannot account for — another client's window inside the region — is inside the
// capture and is not removed. The lens's ground is therefore "the composite
// minus ours", never a claim about the wallpaper. Where the shell owns the whole
// pixel there is nothing left to recover, and the result is transparent there.
func Unmix(captured, ours *ui.Image) *ui.Image {
	if captured == nil {
		return nil
	}
	if ours == nil || ours.Width != captured.Width || ours.Height != captured.Height || ours.Stride != captured.Stride {
		// Nothing to subtract: the capture is the ground.
		return captured
	}
	out := &ui.Image{Width: captured.Width, Height: captured.Height, Stride: captured.Stride,
		Pix: make([]byte, len(captured.Pix))}
	for i := 0; i+3 < len(captured.Pix) && i+3 < len(ours.Pix); i += 4 {
		a := float64(ours.Pix[i+3]) / 255
		if a >= 1 {
			// The shell owns this pixel outright; the background is not in it.
			continue
		}
		rest := 1 - a
		for ch := 0; ch < 3; ch++ {
			v := (float64(captured.Pix[i+ch]) - float64(ours.Pix[i+ch])) / rest
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			out.Pix[i+ch] = byte(v + 0.5)
		}
		out.Pix[i+3] = 0xff
	}
	return out
}

// LensRung is how a material surface's ground is being produced.
type LensRung uint8

const (
	// LensStatic is the capture taken when the surface opened: no recapture is
	// running, which is yesterday's behaviour and always available.
	LensStatic LensRung = iota
	// LensLive is a recaptured ground, kept current.
	LensLive
	// LensStale is the last frame, no longer refreshing: the surface paints
	// what it has rather than asking again.
	LensStale
)

const (
	// MaxLensAge is how old a live frame may be before the surface admits it is
	// no longer live. A lens showing content from a second ago is a visible
	// artefact; naming the rung is how it stops being a hidden one.
	MaxLensAge = 500 * time.Millisecond
	// MaxLensFailures is how many consecutive captures may fail before the
	// surface stops asking. A machine that cannot capture should not pay for
	// the attempt every cadence.
	MaxLensFailures = 3
)

// LensState is what the ladder needs to know about a surface's ground.
type LensState struct {
	// FrameAge is how long since a frame arrived; zero means none has.
	FrameAge time.Duration
	// Failures counts consecutive failed captures.
	Failures int
	// OursComplete reports whether the shell can account for its own pixels in
	// the captured region. Where it cannot, the unmix would subtract the wrong
	// thing and the lens must not run at all.
	OursComplete bool
}

// LensRungFor answers which rung applies.
//
// The ladder only ever degrades towards the static capture, never towards a
// guess: an incomplete unmix, a run of failures, or an aged frame each take the
// surface back to a ground it can trust. That is what makes the feature's
// failure mode "the panel looks like it did before" rather than "the panel shows
// something stale".
func LensRungFor(s LensState) LensRung {
	if !s.OursComplete || s.Failures >= MaxLensFailures {
		return LensStatic
	}
	if s.FrameAge <= 0 || s.FrameAge > MaxLensAge {
		return LensStatic
	}
	return LensLive
}

// LensKeepsLastFrame reports whether a surface with this state should keep
// painting the frame it has rather than dropping back to the capture. It is
// separate from the rung because the two are different questions: the rung says
// whether the ground is current, this says whether what is on screen is worth
// keeping while it is not.
func LensKeepsLastFrame(s LensState) bool {
	return s.Failures < MaxLensFailures && s.FrameAge > 0
}
