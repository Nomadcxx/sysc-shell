package wayland

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestCaptureWithoutTheManagerReturnsNil(t *testing.T) {
	t.Parallel()
	// A compositor without screencopy must produce an opaque panel, not a
	// failure. This is the whole of D12.
	o := &owner{}
	if got := o.captureRegion(nil, ui.Rect{W: 100, H: 100}); got != nil {
		t.Error("capture without a manager returned an image")
	}
}

func TestCaptureRejectsADegenerateRegion(t *testing.T) {
	t.Parallel()
	o := &owner{}
	for _, r := range []ui.Rect{{}, {W: 0, H: 10}, {W: 10, H: 0}, {W: -1, H: 5}} {
		if got := o.captureRegion(nil, r); got != nil {
			t.Errorf("capture of %+v returned an image", r)
		}
	}
}

func TestCaptureBufferFitsOnlyWhatWeCanAllocate(t *testing.T) {
	t.Parallel()
	// newGeneration always allocates stride = width*4, so an offer with any
	// other stride cannot be satisfied. Handing the compositor a buffer whose
	// geometry disagrees with the offer earns zwlr_screencopy_frame_v1.error
	// invalid_buffer, which is a protocol error and kills the connection: the
	// whole shell dies for a decoration. Declining the offer costs a backdrop.
	const (
		argb = formatARGB8888
		xrgb = formatXRGB8888
		// RGB332 is one byte per pixel, so it is not the canvas's layout at any
		// stride.
		rgb332 = uint32(0x38424752)
	)
	tests := []struct {
		name                  string
		format                uint32
		width, height, stride uint32
		want                  bool
	}{
		{"the natural argb offer", argb, 1120, 960, 1120 * 4, true},
		// What this Niri actually offers for a region capture, measured
		// 2026-09-12: xrgb8888 and nothing else. Declining it means never
		// capturing a backdrop at all.
		{"the xrgb offer a real compositor sends", xrgb, 1120, 960, 1120 * 4, true},
		{"a padded stride we cannot express", argb, 1120, 960, 1152 * 4, false},
		{"a stride narrower than the row", argb, 1120, 960, 1000 * 4, false},
		{"a format that is not our byte order", rgb332, 1120, 960, 1120 * 4, false},
		{"zero width", argb, 0, 960, 0, false},
		{"zero height", argb, 1120, 0, 1120 * 4, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := captureBufferFits(tc.format, tc.width, tc.height, tc.stride); got != tc.want {
				t.Errorf("captureBufferFits(%d, %d, %d, %d) = %v, want %v",
					tc.format, tc.width, tc.height, tc.stride, got, tc.want)
			}
		})
	}
}

func TestNormaliseCaptureHonoursYInvert(t *testing.T) {
	t.Parallel()
	// Two rows of one pixel, told apart by their first byte. A compositor that
	// reports y_invert hands back the bottom row first.
	src := []byte{
		0x11, 0, 0, 0,
		0x22, 0, 0, 0,
	}

	flipped := make([]byte, len(src))
	normaliseCapture(flipped, src, 2, 4, true, false)
	if flipped[0] != 0x22 || flipped[4] != 0x11 {
		t.Errorf("rows were not flipped: %#v", flipped)
	}

	asIs := make([]byte, len(src))
	normaliseCapture(asIs, src, 2, 4, false, false)
	if asIs[0] != 0x11 || asIs[4] != 0x22 {
		t.Errorf("rows were flipped when the compositor did not ask: %#v", asIs)
	}
}

func TestNormaliseCaptureFillsAlphaOnlyForAnXFormat(t *testing.T) {
	t.Parallel()
	// An x-format's fourth byte is undefined. Left alone it reaches the painter
	// as a per-pixel opacity, and a zero there is how a captured backdrop
	// silently disappears.
	src := []byte{1, 2, 3, 0x00, 4, 5, 6, 0x7f}

	opaque := make([]byte, len(src))
	normaliseCapture(opaque, src, 1, 8, false, true)
	if opaque[3] != 0xff || opaque[7] != 0xff {
		t.Errorf("alpha was not forced opaque: %#v", opaque)
	}
	// Colour bytes must be untouched: 0xff is premultiplication by one, so no
	// channel needs rescaling.
	if opaque[0] != 1 || opaque[1] != 2 || opaque[2] != 3 ||
		opaque[4] != 4 || opaque[5] != 5 || opaque[6] != 6 {
		t.Errorf("colour channels changed: %#v", opaque)
	}

	kept := make([]byte, len(src))
	normaliseCapture(kept, src, 1, 8, false, false)
	if kept[3] != 0x00 || kept[7] != 0x7f {
		t.Errorf("alpha was rewritten for a buffer that carries real alpha: %#v", kept)
	}
}
