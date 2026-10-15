package wayland

import (
	"errors"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-wayland/client"
)

// A recapture is driven directly rather than through an open surface: the test
// harness has no compositor, so the surface creation path cannot run here. What
// these tests pin is the recapture itself — one capture of the surface's region,
// blurred at its radius, delivered once, and quiet on every failure.

func captureImage(w, h int) *ui.Image {
	img := &ui.Image{Width: w, Height: h, Stride: w * 4, Pix: make([]byte, w*h*4)}
	for i := 0; i+3 < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 0x30, 0x30, 0x30, 0xff
	}
	return img
}

func recaptureOwner(capture func(*client.Output, *ui.Rect) *ui.Image) (*owner, *OutputHost) {
	s := newHostSet()
	h := mappedHost(s, 7, "DP-1")
	o := &owner{hosts: s, rs: newRegistryState()}
	o.capture = capture
	return o, h
}

func TestAuxRecaptureDeliversTheSurfacesRegionAndRadius(t *testing.T) {
	t.Parallel()
	var asked *ui.Rect
	captures := 0
	o, h := recaptureOwner(func(_ *client.Output, r *ui.Rect) *ui.Image {
		captures++
		asked = r
		return captureImage(64, 64)
	})
	var got *ui.Image
	h.aux["panel:settings"] = &surfaceUnit{id: "panel:settings", recapture: &auxRecapture{
		region: ui.Rect{X: 4, Y: 8, W: 120, H: 80},
		radius: 24,
		frame:  func(img *ui.Image) { got = img },
	}}

	if err := o.recaptureAux(h, "panel:settings"); err != nil {
		t.Fatalf("recapture: %v", err)
	}
	if captures != 1 {
		t.Fatalf("captures = %d, want one", captures)
	}
	if asked == nil || *asked != (ui.Rect{X: 4, Y: 8, W: 120, H: 80}) {
		t.Fatalf("captured %v, want the surface's region", asked)
	}
	if got == nil {
		t.Fatal("a recapture delivered nothing")
	}
	// The frame is the reduced, blurred ground: the blur halves a 64-pixel
	// capture by the downsample factor.
	if got.Width != 16 || got.Height != 16 {
		t.Errorf("frame = %dx%d, want the reduced capture 16x16", got.Width, got.Height)
	}
}

func TestAuxRecaptureWithoutTheFlagIsANoOp(t *testing.T) {
	t.Parallel()
	captures := 0
	o, h := recaptureOwner(func(*client.Output, *ui.Rect) *ui.Image {
		captures++
		return captureImage(64, 64)
	})
	h.aux["panel:settings"] = &surfaceUnit{id: "panel:settings"}

	err := o.recaptureAux(h, "panel:settings")
	if !errors.Is(err, errAuxNotOpen) {
		t.Fatalf("recapture of a surface without the flag = %v, want errAuxNotOpen", err)
	}
	if captures != 0 {
		t.Errorf("captures = %d, want none", captures)
	}
}

func TestAuxRecaptureOnAClosedSurfaceIsANoOp(t *testing.T) {
	t.Parallel()
	captures := 0
	o, h := recaptureOwner(func(*client.Output, *ui.Rect) *ui.Image {
		captures++
		return captureImage(64, 64)
	})
	if err := o.recaptureAux(h, "panel:gone"); !errors.Is(err, errAuxNotOpen) {
		t.Fatalf("recapture of a closed surface = %v, want errAuxNotOpen", err)
	}
	if captures != 0 {
		t.Errorf("captures = %d, want none: a request that raced a close must not capture", captures)
	}
}

func TestAuxRecaptureSurvivesAFailedCapture(t *testing.T) {
	t.Parallel()
	o, h := recaptureOwner(func(*client.Output, *ui.Rect) *ui.Image { return nil })
	delivered := false
	h.aux["panel:settings"] = &surfaceUnit{id: "panel:settings", recapture: &auxRecapture{
		region: ui.Rect{W: 120, H: 80},
		radius: 24,
		frame:  func(*ui.Image) { delivered = true },
	}}

	if err := o.recaptureAux(h, "panel:settings"); err != nil {
		t.Fatalf("a failed capture reported %v, want nothing", err)
	}
	if delivered {
		t.Error("a failed capture still delivered a frame")
	}
	if o.fatal != nil {
		t.Fatalf("a failed recapture failed the owner: %v", o.fatal)
	}
}
