package render

import "sync"

// The mono role promises an equal advance for every glyph; ASCII art and
// aligned columns shear apart when the face is proportional. Settings let a
// user point the role at any family, so the family is verified against its
// own metrics before text specs carry it.

var (
	monoMu      sync.Mutex
	monoVerdict = make(map[string]bool)
	monoProbed  bool
	monoMap     *FontMap
)

// systemMonospace reports whether family advances every glyph equally.
// Verdicts are cached per family because each probe walks the system font
// scanner.
func systemMonospace(family string) bool {
	if family == "" {
		return false
	}
	monoMu.Lock()
	defer monoMu.Unlock()
	if verdict, seen := monoVerdict[family]; seen {
		return verdict
	}
	if !monoProbed {
		monoProbed = true
		monoMap, _ = NewSystemFontMap("sans-serif", DefaultFontCacheDir())
	}
	mono := false
	if monoMap != nil {
		if face := monoMap.Face('M', FaceRequest{Family: family}); face != nil {
			mono = face.IsMonospace()
		}
	}
	monoVerdict[family] = mono
	return mono
}

// monospaceProbe is the seam tests swap so verdicts do not depend on the
// fonts installed on the machine.
var monospaceProbe = systemMonospace

// MonospaceFamily picks the family the mono role renders with. A configured
// family that really is monospace is kept. Otherwise the caller's default
// wins when it verifies, then the generic monospace family, and finally the
// configured family is returned unchanged, so a font environment the scanner
// cannot read never breaks the theme.
func MonospaceFamily(family, fallback string) string {
	if family != "" && monospaceProbe(family) {
		return family
	}
	if fallback != "" && fallback != family && monospaceProbe(fallback) {
		return fallback
	}
	if monospaceProbe("monospace") {
		return "monospace"
	}
	return family
}
