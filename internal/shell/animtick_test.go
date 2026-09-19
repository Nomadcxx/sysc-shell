package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// vblank60 is one frame on the 60Hz panel both development machines drive.
const vblank60 = 16667 * time.Microsecond

// The ticker is what samples an animation; the compositor's frame callback is
// what presents it. When the two run at the same period they beat against each
// other: the tick drifts a whole frame every few seconds, so some vblanks get
// no new value and others get two, which reads as judder at any frame rate.
//
// Sampling at least twice per frame means every vblank has a value no older
// than half a frame, whatever the phase between them.
func TestAnimTickSamplesTwicePerFrame(t *testing.T) {
	t.Parallel()
	if animTick*2 > vblank60 {
		t.Errorf("anim tick %v samples less than twice per 60Hz frame (%v)", animTick, vblank60)
	}
}

// The frame cap paces publishes with `now.Sub(last) >= cap`. If the cap were
// at or above the tick period, ordinary ticker jitter would push a tick just
// under it and drop that sample, so the pacing meant to bound cost would
// instead reintroduce the irregular spacing the tick rate exists to remove.
func TestFrameCapStaysBelowTheTick(t *testing.T) {
	t.Parallel()
	if theme.BaseMotion.FrameCap >= animTick {
		t.Errorf("frame cap %v is not below the anim tick %v, so jitter can drop a sample",
			theme.BaseMotion.FrameCap, animTick)
	}
}
