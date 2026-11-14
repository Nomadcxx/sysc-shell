package displays

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// Policy is one output's saved native policy. A nil field means automatic: the
// node is absent from the managed file, so the compositor decides it and the
// shell records no policy it cannot honour. That distinction is the whole point
// of the type — a numeric observation of an automatic output is not a policy.
type Policy struct {
	Mode      *ModePolicy
	Scale     *float64
	Transform *TransformPolicy
	Position  *Position
	// Relative is a draft-only placement. The applier resolves it after reading
	// the actual post-mode/scale/transform dimensions, then clears it.
	Relative *RelativePosition
	VRR      *VRRPolicy
}

type RelativePosition struct{ To, Side string }

// Mode is one mode an output advertises, projected from the compositor's own
// answer. Custom mirrors the output's is_custom_mode flag rather than anything
// about the individual rate, which is how niri reports it.
type Mode struct {
	Width          uint16
	Height         uint16
	RefreshMilliHz uint32
	Preferred      bool
	Custom         bool
}

// ModePolicy is an exact advertised mode. RefreshMilliHz is integer millihertz
// because that is what niri advertises; the KDL string carries hertz.
type ModePolicy struct {
	Width          uint16
	Height         uint16
	RefreshMilliHz uint32
	Custom         bool
}

// Position is an explicit logical position in niri's own i32 coordinates.
type Position struct {
	X int32
	Y int32
}

// VRRPolicy is variable-refresh-rate. OnDemand false with a non-nil VRR is
// always-on; a nil VRR is always-off, which is niri's own encoding.
type VRRPolicy struct {
	OnDemand bool
}

// TransformPolicy is a niri transform name. It is stored as a string because
// internal/platform/niri already names the wire and native spellings
// identically, and duplicating the set here would let the two drift.
type TransformPolicy = string

// ScalePresets are the values the page offers. A saved value outside the set
// stays as saved: niri accepts any FloatOrInt<0,10> and snapping it would
// silently rewrite a policy the user chose elsewhere.
var ScalePresets = []float64{1, 1.25, 1.5, 1.75, 2}

// maxScale is niri's own upper bound for scale, FloatOrInt<0,10>.
const maxScale = 10.0

// ValidateScale reports whether f is a scale niri will accept. niri's bound is
// exclusive at 10, so the comparison is strict.
func ValidateScale(f float64) error {
	switch {
	case math.IsNaN(f):
		return fmt.Errorf("scale is not a number")
	case math.IsInf(f, 0):
		return fmt.Errorf("scale is not finite")
	case f <= 0:
		return fmt.Errorf("scale %v is not positive", f)
	case f >= maxScale:
		return fmt.Errorf("scale %v is outside niri's range (0, %v)", f, maxScale)
	}
	return nil
}

// IsPreset reports whether f is one of the offered values. It never rounds.
func IsPreset(f float64) bool { return slices.Contains(ScalePresets, f) }

// ValidateMode checks a mode against what the output advertises. Advertising is
// the only authority: a mode niri did not list would be sent as a custom mode
// and change hardware behaviour the user never saw.
func ValidateMode(advertised []Mode, want ModePolicy) error {
	for _, m := range advertised {
		if m.Width == want.Width && m.Height == want.Height && (want.RefreshMilliHz == 0 || m.RefreshMilliHz == want.RefreshMilliHz) {
			if want.Custom && !m.Custom {
				return fmt.Errorf("mode %dx%d@%d is not a custom mode", want.Width, want.Height, want.RefreshMilliHz)
			}
			return nil
		}
	}
	return fmt.Errorf("mode %dx%d@%d is not advertised by this output", want.Width, want.Height, want.RefreshMilliHz)
}

// Rect is an output's explicit logical footprint. Width and Height are the
// compositor's own logical size, which is what makes adjacency decidable.
type Rect struct {
	Position
	Width  int32
	Height int32
}

// ValidateArrangement rejects an explicit set that overlaps or leaves outputs
// floating. A preview that stacks two outputs on top of each other is
// unrecoverable by the user, so it is refused before any command is sent.
func ValidateArrangement(rects map[string]Rect) error {
	names := make([]string, 0, len(rects))
	for name := range rects {
		names = append(names, name)
	}
	slices.Sort(names)
	for i := 0; i < len(names); i++ {
		a := rects[names[i]]
		if err := validateRect(a); err != nil {
			return fmt.Errorf("%s: %w", names[i], err)
		}
		for j := i + 1; j < len(names); j++ {
			b := rects[names[j]]
			if err := validatePair(names[i], a, names[j], b); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateRect(r Rect) error {
	if r.Width <= 0 || r.Height <= 0 {
		return fmt.Errorf("logical size %dx%d is not positive", r.Width, r.Height)
	}
	return nil
}

func validatePair(nameA string, a Rect, nameB string, b Rect) error {
	xOverlap := overlap(a.X, a.X+a.Width, b.X, b.X+b.Width)
	yOverlap := overlap(a.Y, a.Y+a.Height, b.Y, b.Y+b.Height)
	switch {
	case xOverlap > 0 && yOverlap > 0:
		return fmt.Errorf("%s and %s overlap", nameA, nameB)
	case xOverlap > 0 || yOverlap > 0:
		return nil
	}
	// Disjoint on both axes. They still have to touch, or the arrangement
	// leaves a gap the pointer has to cross somewhere undefined.
	if sharesEdge(a, b) {
		return nil
	}
	return fmt.Errorf("%s and %s are not edge-adjacent", nameA, nameB)
}

func overlap(aLo, aHi, bLo, bHi int32) int32 {
	lo := max(aLo, bLo)
	hi := min(aHi, bHi)
	if hi <= lo {
		return 0
	}
	return hi - lo
}

// sharesEdge reports whether the rectangles touch along an edge segment of
// positive length. Corner contact is not adjacency: niri has no meaning for it
// and the pointer crossing it is undefined.
func sharesEdge(a, b Rect) bool {
	if a.X+a.Width == b.X || b.X+b.Width == a.X {
		return overlap(a.Y, a.Y+a.Height, b.Y, b.Y+b.Height) > 0
	}
	if a.Y+a.Height == b.Y || b.Y+b.Height == a.Y {
		return overlap(a.X, a.X+a.Width, b.X, b.X+b.Width) > 0
	}
	return false
}

// Equal reports whether two policies ask for the same thing. It is the test
// that decides whether a draft changed anything worth previewing.
func (p Policy) Equal(other Policy) bool {
	switch {
	case (p.Mode == nil) != (other.Mode == nil):
		return false
	case p.Mode != nil && *p.Mode != *other.Mode:
		return false
	case (p.Scale == nil) != (other.Scale == nil):
		return false
	case p.Scale != nil && *p.Scale != *other.Scale:
		return false
	case (p.Transform == nil) != (other.Transform == nil):
		return false
	case p.Transform != nil && *p.Transform != *other.Transform:
		return false
	case (p.Position == nil) != (other.Position == nil):
		return false
	case p.Position != nil && *p.Position != *other.Position:
		return false
	case (p.Relative == nil) != (other.Relative == nil):
		return false
	case p.Relative != nil && *p.Relative != *other.Relative:
		return false
	case !p.sameVRR(other):
		return false
	}
	return true
}

func (p Policy) sameVRR(other Policy) bool {
	if p.VRR == nil || other.VRR == nil {
		return p.VRR == other.VRR
	}
	return *p.VRR == *other.VRR
}

// String renders a policy for the ownership explanation. Automatic is spelled
// out rather than left blank, because "no value shown" and "automatic" are
// different claims to a user reading the file.
func (p Policy) String() string {
	var parts []string
	switch {
	case p.Mode == nil:
		parts = append(parts, "mode automatic")
	default:
		parts = append(parts, fmt.Sprintf("mode %s", p.Mode.String()))
	}
	switch {
	case p.Scale == nil:
		parts = append(parts, "scale automatic")
	default:
		parts = append(parts, fmt.Sprintf("scale %g", *p.Scale))
	}
	switch {
	case p.Transform == nil:
		parts = append(parts, "transform normal")
	default:
		parts = append(parts, "transform "+*p.Transform)
	}
	switch {
	case p.Position == nil:
		parts = append(parts, "position automatic")
	default:
		parts = append(parts, fmt.Sprintf("position %d,%d", p.Position.X, p.Position.Y))
	}
	switch {
	case p.VRR == nil:
		parts = append(parts, "vrr off")
	case p.VRR.OnDemand:
		parts = append(parts, "vrr on demand")
	default:
		parts = append(parts, "vrr always on")
	}
	return strings.Join(parts, ", ")
}

// String renders a mode the way niri's config spells it: hertz with three
// decimals, because millihertz values such as 120030 must not become 120.03.
func (m ModePolicy) String() string {
	if m.RefreshMilliHz == 0 {
		return fmt.Sprintf("%dx%d", m.Width, m.Height)
	}
	return fmt.Sprintf("%dx%d@%.3f", m.Width, m.Height, float64(m.RefreshMilliHz)/1000)
}
