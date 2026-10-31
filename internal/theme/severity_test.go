package theme

import (
	"math"
	"testing"
)

// The whole point of a fixed ramp is that a health band stays legible on a
// surface the theme chose, including one close to the ramp itself.
func TestSeverityColorMeetsBandContrastOnEverySurface(t *testing.T) {
	t.Parallel()

	surfaces := []Color{
		{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
		{R: 0xfa, G: 0xf8, B: 0xf4, A: 0xff},
		{R: 0x10, G: 0x14, B: 0x18, A: 0xff},
		{R: 0x1c, G: 0x1b, B: 0x1a, A: 0xff},
		{R: 0x80, G: 0x80, B: 0x80, A: 0xff},
		{R: 0x2e, G: 0x9e, B: 0x5b, A: 0xff}, // the good band itself
	}
	for _, surface := range surfaces {
		for _, s := range []Severity{SeverityGood, SeverityModerate, SeverityHigh} {
			painted := SeverityColor(s, surface)
			if got := ContrastRatio(painted, surface); got < severityBandRatio-0.05 {
				t.Errorf("severity %d on %#v = %.2f:1, want %.1f", s, surface, got, severityBandRatio)
			}
		}
	}
}

// The three bands must stay distinguishable from each other after adaptation,
// or the ramp collapses into one colour and the zones stop meaning anything.
func TestSeverityColorKeepsBandsDistinct(t *testing.T) {
	t.Parallel()

	for _, surface := range []Color{
		{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
		{R: 0x10, G: 0x14, B: 0x18, A: 0xff},
		{R: 0x80, G: 0x80, B: 0x80, A: 0xff},
	} {
		good := SeverityColor(SeverityGood, surface)
		moderate := SeverityColor(SeverityModerate, surface)
		high := SeverityColor(SeverityHigh, surface)
		if good == moderate || moderate == high || good == high {
			t.Errorf("on %#v the bands collapsed: %#v %#v %#v", surface, good, moderate, high)
		}
	}
}

// Red and green are the pair most colour vision deficiencies merge, so the two
// ends of the ramp have to differ in luminance as well as in hue.
func TestSeverityRampSeparatesByLuminance(t *testing.T) {
	t.Parallel()

	surface := Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	good := SeverityColor(SeverityGood, surface).Luminance()
	moderate := SeverityColor(SeverityModerate, surface).Luminance()
	high := SeverityColor(SeverityHigh, surface).Luminance()
	if math.Abs(good-high) < 0.02 {
		t.Fatalf("good and high luminance are %.3f and %.3f, too close to tell apart", good, high)
	}
	// The ramp is meant to get darker as it gets more severe, which is what
	// makes it survive a greyscale conversion.
	if !(good > moderate && moderate > high) {
		t.Fatalf("ramp luminance does not descend: %.3f %.3f %.3f", good, moderate, high)
	}
}

func TestSeverityInkCarriesBodyContrast(t *testing.T) {
	t.Parallel()

	surface := Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	for _, s := range []Severity{SeverityGood, SeverityModerate, SeverityHigh} {
		band := SeverityColor(s, surface)
		ink := SeverityInk(band)
		if got := ContrastRatio(ink, band); got < severityInkRatio-0.05 {
			t.Errorf("ink on severity %d = %.2f:1, want %.1f", s, got, severityInkRatio)
		}
	}
}

// A caller that hands over a nonsense severity must get the least alarming
// band, not a panic inside a paint loop.
func TestSeverityColorClampsUnknownSeverity(t *testing.T) {
	t.Parallel()

	surface := Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	if got, want := SeverityColor(Severity(9), surface), SeverityColor(SeverityHigh, surface); got != want {
		t.Fatalf("oversized severity = %#v, want the high band %#v", got, want)
	}
}
