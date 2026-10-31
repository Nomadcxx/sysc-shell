package theme

// Severity is a health or status threshold: a reading is good, worth noticing,
// or high enough to act on.
//
// These are deliberately not theme roles. A palette that has never defined a
// warning colour still has to paint a pollen warning, and a theme that defines
// one badly must not be able to turn a health signal into decoration. So the
// three base colours below are fixed and the surface they land on only decides
// how far each one is lifted to stay legible.
type Severity uint8

const (
	SeverityGood Severity = iota
	SeverityModerate
	SeverityHigh
)

// The base severity ramp. These are chosen to read as green/amber/red to a
// sighted reader, to separate from each other for the common forms of colour
// vision deficiency, and to survive being lifted or darkened by EnsureContrast
// without changing which one they are.
//
// The amber is darker than a decorative amber on purpose: #FFC107 on a light
// surface measures about 1.6:1, which fails as a band even though it looks
// right in isolation.
var severityBase = [3]Color{
	{R: 0x2E, G: 0x9E, B: 0x5B, A: 0xff}, // good
	{R: 0xB8, G: 0x7A, B: 0x0A, A: 0xff}, // moderate
	{R: 0xC4, G: 0x36, B: 0x2F, A: 0xff}, // high
}

// severityInk is the text colour a band carries when its own colour is too
// light to hold dark text. Both are checked against the band in SeverityInk.
var (
	severityDarkInk  = Color{R: 0x1A, G: 0x1A, B: 0x1A, A: 0xff}
	severityLightInk = Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
)

// severityBandRatio is the contrast a band needs against the surface behind it.
// It is the non-text threshold: a band is a shape, not a word. Text drawn on
// the band is held to the 4.5:1 body-text threshold separately.
const (
	severityBandRatio = 3.0
	severityInkRatio  = 4.5
)

// SeverityColor returns the painted colour for a severity band on the given
// surface, lifted or darkened just enough to meet the non-text contrast
// threshold.
//
// The surface matters because the same green is legible on a light card and
// muddy on a dark one. Lifting the fixed base rather than substituting a theme
// role is what keeps the meaning stable across palettes: the band a reader
// learned as "amber means moderate" stays amber.
func SeverityColor(severity Severity, surface Color) Color {
	base := severityBase[clampSeverity(severity)]
	return EnsureContrast(base, surface, severityBandRatio)
}

// SeverityInk returns the text colour to draw on top of a band painted with
// SeverityColor. It picks whichever of near-black and white carries more
// contrast, and repairs toward the better endpoint when neither reaches the
// body-text threshold on its own.
func SeverityInk(band Color) Color {
	dark := ContrastRatio(severityDarkInk, band)
	light := ContrastRatio(severityLightInk, band)
	if dark >= light {
		if dark >= severityInkRatio {
			return severityDarkInk
		}
		return EnsureContrast(severityDarkInk, band, severityInkRatio)
	}
	if light >= severityInkRatio {
		return severityLightInk
	}
	return EnsureContrast(severityLightInk, band, severityInkRatio)
}

func clampSeverity(s Severity) int {
	switch {
	case s >= SeverityHigh:
		return 2
	case s == SeverityModerate:
		return 1
	default:
		return 0
	}
}
