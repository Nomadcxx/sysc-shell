package ui

// Scale120 is a fractional render scale expressed as a numerator over 120, the
// unit wp_fractional_scale_v1.preferred_scale uses. It is never reduced to an
// integer: 1.25 is Scale120(150), not 1.
//
// It is the single owner of the logical-to-physical conversion. Layout and hit
// testing work in logical pixels; buffer allocation, painting, and damage work
// in the physical pixels this type produces.
type Scale120 int

// ScaleUnit is a scale of exactly 1.
const ScaleUnit Scale120 = 120

// Valid reports whether the scale is usable. The compositor never advertises a
// zero or negative preferred scale.
func (s Scale120) Valid() bool { return s > 0 }

// Physical converts a logical length to buffer pixels, rounding half up.
func (s Scale120) Physical(logical int) int {
	return (logical*int(s) + 60) / 120
}

// PhysicalRect maps a rectangle by converting its edges rather than its size,
// so rectangles that are adjacent in logical space stay adjacent in buffer
// pixels with no gap or overlap.
func (s Scale120) PhysicalRect(r Rect) Rect {
	x0, y0 := s.Physical(r.X), s.Physical(r.Y)
	return Rect{
		X: x0,
		Y: y0,
		W: s.Physical(r.X+r.W) - x0,
		H: s.Physical(r.Y+r.H) - y0,
	}
}
