package wayland

import (
	"errors"
	"fmt"
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// surfaceState holds the pure configure and scale state of one layer surface.
//
// Four values stay distinct here: the connector identity lives on the output
// host, the configure size is logical, the scale is a numerator over 120, and
// the buffer size is physical pixels.
type surfaceState struct {
	// logicalWidth and logicalHeight come from zwlr_layer_surface_v1.configure.
	// They are what remains after other layer surfaces' exclusive zones, and
	// are never derived from the output mode or from Niri IPC.
	logicalWidth  int
	logicalHeight int
	// scale120 comes from wp_fractional_scale_v1.preferred_scale. It defaults
	// to 1 until the first event arrives, which is not ordered against
	// configure.
	scale120     ui.Scale120
	acknowledged bool
}

func newSurfaceState() *surfaceState {
	return &surfaceState{scale120: ui.ScaleUnit}
}

// configure records a configure event and reports whether the logical size
// changed. Acknowledgement is a separate step.
func (s *surfaceState) configure(width, height int) bool {
	changed := width != s.logicalWidth || height != s.logicalHeight
	s.logicalWidth, s.logicalHeight = width, height
	if changed {
		s.acknowledged = false
	}
	return changed
}

// acknowledge records that the configure was acked, which is what makes the
// surface eligible for a buffer.
func (s *surfaceState) acknowledge() { s.acknowledged = true }

// preferredScale records a fractional scale and reports whether it changed. A
// scale-only event at an unchanged logical size still retires the old physical
// buffer generation, so a change here is a reconfigure.
func (s *surfaceState) preferredScale(scale ui.Scale120) bool {
	if scale == s.scale120 {
		return false
	}
	s.scale120 = scale
	return true
}

// eligible reports whether a buffer may be attached.
func (s *surfaceState) eligible() bool {
	return s.acknowledged && s.logicalWidth > 0 && s.logicalHeight > 0
}

var errBufferSize = errors.New("wayland: unusable buffer size")

// bufferSize converts the logical configure size to physical buffer pixels,
// validating the multiplication and the int32 conversion the wire requires.
func (s *surfaceState) bufferSize() (int32, int32, error) {
	if s.logicalWidth <= 0 || s.logicalHeight <= 0 {
		return 0, 0, fmt.Errorf("%w: logical size is %dx%d", errBufferSize, s.logicalWidth, s.logicalHeight)
	}
	if !s.scale120.Valid() {
		return 0, 0, fmt.Errorf("%w: scale120 is %d", errBufferSize, s.scale120)
	}
	if s.logicalWidth > math.MaxInt32/int(s.scale120) || s.logicalHeight > math.MaxInt32/int(s.scale120) {
		return 0, 0, fmt.Errorf("%w: %dx%d at scale120 %d overflows", errBufferSize, s.logicalWidth, s.logicalHeight, s.scale120)
	}

	width := s.scale120.Physical(s.logicalWidth)
	height := s.scale120.Physical(s.logicalHeight)
	if width <= 0 || height <= 0 || width > math.MaxInt32 || height > math.MaxInt32 {
		return 0, 0, fmt.Errorf("%w: physical size is %dx%d", errBufferSize, width, height)
	}
	return int32(width), int32(height), nil
}

// retirement tracks whether a superseded buffer generation may be unmapped. A
// generation owns its memfd, mapping, pool and buffers together, and storage
// the compositor still reads must never be written or unmapped.
type retirement struct {
	outstanding int
	destroyed   bool
}

// attached records a buffer submitted to the compositor.
func (r *retirement) attached() { r.outstanding++ }

// released records wl_buffer.release.
func (r *retirement) released() error {
	if r.outstanding == 0 {
		return fmt.Errorf("wayland: buffer release with no buffer attached")
	}
	r.outstanding--
	return nil
}

// destroy records that the surface went away, which frees the generation
// regardless of outstanding buffers.
func (r *retirement) destroy() { r.destroyed = true }

// freeable reports whether the generation's storage may be unmapped.
func (r *retirement) freeable() bool { return r.destroyed || r.outstanding == 0 }
