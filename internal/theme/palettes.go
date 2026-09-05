package theme

import (
	"math"
	"sort"
)

// A named palette is a published colour scheme rather than a generated one.
// The wallpaper and hex sources ask matugen for a tonal palette around a seed;
// a named palette instead carries the scheme's own colours, which is the whole
// reason someone asks for Gruvbox by name rather than for a brown seed.
//
// Only the anchors are authored. The remaining Material roles are derived from
// them here, so a scheme is a dozen values to check against its published
// reference rather than forty-nine to keep in step by hand.
type anchors struct {
	// Surface is the scheme's background and OnSurface its body text.
	Surface, OnSurface string
	// Low and High bracket the container ladder: Low sits under the surface
	// and High is the scheme's most raised panel colour. Every container level
	// is interpolated between them.
	Low, High string
	// The three accents and the error colour, named as the scheme names them.
	Primary, Secondary, Tertiary, Error string
}

// palettes holds each scheme's dark and light anchors, taken from the
// scheme's own published palette rather than sampled from a screenshot.
var palettes = map[string]struct{ Dark, Light anchors }{
	"catppuccin": {
		// Mocha and Latte.
		Dark: anchors{
			Surface: "#1e1e2e", OnSurface: "#cdd6f4",
			Low: "#11111b", High: "#585b70",
			Primary: "#89b4fa", Secondary: "#cba6f7", Tertiary: "#f5c2e7", Error: "#f38ba8",
		},
		Light: anchors{
			Surface: "#eff1f5", OnSurface: "#4c4f69",
			Low: "#dce0e8", High: "#acb0be",
			Primary: "#1e66f5", Secondary: "#8839ef", Tertiary: "#ea76cb", Error: "#d20f39",
		},
	},
	"gruvbox": {
		Dark: anchors{
			Surface: "#282828", OnSurface: "#ebdbb2",
			Low: "#1d2021", High: "#665c54",
			Primary: "#83a598", Secondary: "#8ec07c", Tertiary: "#fabd2f", Error: "#fb4934",
		},
		Light: anchors{
			Surface: "#fbf1c7", OnSurface: "#3c3836",
			Low: "#f9f5d7", High: "#bdae93",
			Primary: "#076678", Secondary: "#427b58", Tertiary: "#b57614", Error: "#9d0006",
		},
	},
	"nord": {
		// Polar Night and Snow Storm.
		Dark: anchors{
			Surface: "#2e3440", OnSurface: "#eceff4",
			Low: "#272c36", High: "#4c566a",
			Primary: "#88c0d0", Secondary: "#81a1c1", Tertiary: "#a3be8c", Error: "#bf616a",
		},
		Light: anchors{
			Surface: "#eceff4", OnSurface: "#2e3440",
			Low: "#e5e9f0", High: "#c8d0dc",
			Primary: "#5e81ac", Secondary: "#4c566a", Tertiary: "#5d7a4a", Error: "#a3454f",
		},
	},
	"dracula": {
		// Dracula and its light counterpart, Alucard.
		Dark: anchors{
			Surface: "#282a36", OnSurface: "#f8f8f2",
			Low: "#21222c", High: "#44475a",
			Primary: "#bd93f9", Secondary: "#ff79c6", Tertiary: "#8be9fd", Error: "#ff5555",
		},
		Light: anchors{
			Surface: "#fffbeb", OnSurface: "#1f1f1f",
			Low: "#f5f1e0", High: "#cfcbb8",
			Primary: "#644ac9", Secondary: "#a3144d", Tertiary: "#036a96", Error: "#cb3a2a",
		},
	},
	"tokyo-night": {
		// Night and Day.
		Dark: anchors{
			Surface: "#1a1b26", OnSurface: "#c0caf5",
			Low: "#16161e", High: "#3b4261",
			Primary: "#7aa2f7", Secondary: "#bb9af7", Tertiary: "#7dcfff", Error: "#f7768e",
		},
		Light: anchors{
			// The scheme publishes #3760bf as fg, but that blue is an accent and
			// leaves no room for a ladder above it; #343b58 is its dark ink.
			Surface: "#e1e2e7", OnSurface: "#343b58",
			Low: "#d5d6db", High: "#a8aecb",
			Primary: "#2e7de9", Secondary: "#9854f1", Tertiary: "#007197", Error: "#f52a65",
		},
	},
	"rose-pine": {
		// Main and Dawn.
		Dark: anchors{
			Surface: "#191724", OnSurface: "#e0def4",
			Low: "#16141f", High: "#403d52",
			Primary: "#c4a7e7", Secondary: "#9ccfd8", Tertiary: "#f6c177", Error: "#eb6f92",
		},
		Light: anchors{
			Surface: "#faf4ed", OnSurface: "#575279",
			Low: "#fffaf3", High: "#dfdad9",
			Primary: "#907aa9", Secondary: "#56949f", Tertiary: "#ea9d34", Error: "#b4637a",
		},
	},
}

// PaletteNames lists every named palette, sorted, so a settings enum and a
// configuration error message read the same order.
func PaletteNames() []string {
	out := make([]string, 0, len(palettes))
	for name := range palettes {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// HasPalette reports whether a name is a known scheme.
func HasPalette(name string) bool {
	_, ok := palettes[name]
	return ok
}

// NamedPalette expands a scheme's anchors into a complete role family for one
// mode. The second result is false for an unknown name.
//
// The result is validated the same way generated output is: the caller still
// runs Valid, and this returns something Valid accepts rather than assuming it.
func NamedPalette(name, mode string, highContrast bool) (Tokens, bool) {
	p, ok := palettes[name]
	if !ok {
		return Tokens{}, false
	}
	a := p.Dark
	if mode == "light" {
		a = p.Light
	}
	return derive(a, highContrast), true
}

// mix blends from toward to by t, in sRGB. The palettes are authored in sRGB
// and the surface ladders are short steps, so a linear blend here matches what
// the schemes themselves publish better than a perceptual space would.
func mix(from, to Color, t float64) Color {
	if t <= 0 {
		return from
	}
	if t >= 1 {
		return to
	}
	f := func(x, y uint8) uint8 {
		return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t))
	}
	return Color{R: f(from.R, to.R), G: f(from.G, to.G), B: f(from.B, to.B), A: 0xff}
}

// on picks the legible foreground for a fill: the scheme's own text colour
// when it already clears the floor, and otherwise that colour walked toward
// whichever extreme reaches it.
func on(bg, preferred Color, ratio float64) Color {
	if ContrastRatio(preferred, bg) >= ratio {
		return preferred
	}
	return EnsureContrast(preferred, bg, ratio)
}

// onWorst resolves one foreground that has to clear its floor against several
// backgrounds. The validator checks on_surface against all eight surface
// levels and each on_*_fixed against both the fixed colour and its dim
// variant, so resolving against the single hardest one is what makes the whole
// set pass rather than the one that happened to be checked first.
func onWorst(preferred Color, ratio float64, bgs ...Color) Color {
	fg := preferred
	// Two passes: moving the foreground for the hardest background can leave
	// it short on another, and the second pass settles that.
	for range 2 {
		worst, worstRatio := bgs[0], math.Inf(1)
		for _, b := range bgs {
			if r := ContrastRatio(fg, b); r < worstRatio {
				worst, worstRatio = b, r
			}
		}
		if worstRatio >= ratio {
			return fg
		}
		fg = EnsureContrast(fg, worst, ratio)
	}
	return fg
}

// hcAccent pushes an accent toward the text extreme until a foreground can
// actually reach the high-contrast floor on it. Seven to one is not reachable
// against a saturated mid-tone by moving the foreground alone, so the fill is
// what moves, which is also what matugen does at its highest contrast level.
func hcAccent(c, onSurface Color, ratio float64) Color {
	for i := 0; i <= 20; i++ {
		cand := mix(c, onSurface, float64(i)/20)
		if ContrastRatio(EnsureContrast(onSurface, cand, ratio), cand) >= ratio {
			return cand
		}
	}
	return onSurface
}

// ladderTop caps the container ladder at the highest step body text still
// clears. A scheme's own top panel colour can sit too near its text -- gruvbox
// bg3 does -- and the validator checks on_surface against every step, so the
// ladder yields rather than the text moving off the scheme's own value.
func ladderTop(surface, onSurface, want Color, ratio float64) Color {
	if ContrastRatio(onSurface, want) >= ratio {
		return want
	}
	lo, hi := 0.0, 1.0
	for range 24 {
		mid := (lo + hi) / 2
		if ContrastRatio(onSurface, mix(surface, want, mid)) >= ratio {
			lo = mid
		} else {
			hi = mid
		}
	}
	return mix(surface, want, lo)
}

// derive expands anchors into all forty-nine roles.
//
// High contrast pushes each accent toward the surface's opposite end before
// the foregrounds are resolved. A 7:1 floor is not reachable against a
// saturated mid-tone by moving the foreground alone -- that is the same wall
// the compiled fallback hit -- so the fill has to move, which is also what
// matugen does at its highest contrast level.
func derive(a anchors, highContrast bool) Tokens {
	text := 4.5
	nonText := 3.0
	if highContrast {
		text, nonText = 7.0, 4.5
	}

	surface := mustColor(a.Surface)
	onSurface := mustColor(a.OnSurface)
	low := mustColor(a.Low)
	high := mustColor(a.High)

	accent := func(hex string) Color {
		c := mustColor(hex)
		if highContrast {
			c = hcAccent(c, onSurface, text)
		}
		return c
	}
	primary := accent(a.Primary)
	secondary := accent(a.Secondary)
	tertiary := accent(a.Tertiary)
	errCol := accent(a.Error)

	// The container ladder runs from the scheme's own low to its own high, so
	// a capsule separates by the amount the scheme intends rather than by a
	// fixed percentage of an arbitrary grey. The top is capped where body text
	// would stop clearing its floor.
	top := ladderTop(surface, onSurface, high, text)
	step := func(t float64) Color { return mix(surface, top, t) }
	containerLowest := mix(surface, low, 1.0)
	containerLow := mix(surface, low, 0.5)
	container := step(0.45)
	containerHigh := step(0.72)
	containerHighest := step(1.0)
	surfaceDim := containerLowest
	surfaceBright := containerHighest
	surfaceVariant := containerHigh

	// A tonal container is the accent pulled most of the way to the surface,
	// which keeps the scheme's hue while staying a background. Under high
	// contrast it is pulled further, until a foreground can reach the floor on
	// it -- a container is a background, so it yields rather than the text.
	tonal := func(c Color) Color {
		base := mix(surface, c, 0.28)
		if !highContrast {
			return base
		}
		for i := 0; i <= 20; i++ {
			cand := mix(base, surface, float64(i)/20)
			if ContrastRatio(EnsureContrast(onSurface, cand, text), cand) >= text {
				return cand
			}
		}
		return surface
	}

	// A fixed accent is the light end of that accent and keeps its value in
	// either mode, which is what the role means.
	white := Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	fixedLift, dimLift := 0.62, 0.28
	if highContrast {
		fixedLift, dimLift = 0.88, 0.7
	}
	fixed := func(c Color) Color { return mix(c, white, fixedLift) }
	fixedDim := func(c Color) Color { return mix(c, white, dimLift) }

	outline := on(surface, mix(onSurface, surface, 0.45), nonText)
	outlineVariant := mix(onSurface, surface, 0.72)

	primaryContainer := tonal(primary)
	secondaryContainer := tonal(secondary)
	tertiaryContainer := tonal(tertiary)
	errorContainer := tonal(errCol)

	primaryFixed, primaryFixedDim := fixed(primary), fixedDim(primary)
	secondaryFixed, secondaryFixedDim := fixed(secondary), fixedDim(secondary)
	tertiaryFixed, tertiaryFixedDim := fixed(tertiary), fixedDim(tertiary)

	// Body text has to clear its floor on every surface level, not just the
	// base one.
	body := onWorst(onSurface, text, surface, surfaceDim, surfaceBright,
		containerLowest, containerLow, container, containerHigh, containerHighest)
	// The variant is checked against the surface and the variant surface.
	variant := onWorst(mix(onSurface, surface, 0.25), text, surface, surfaceVariant)

	inverseSurface := onSurface
	inverseOnSurface := on(inverseSurface, surface, text)

	return Tokens{
		Primary:              primary.Hex(),
		OnPrimary:            on(primary, surface, text).Hex(),
		PrimaryContainer:     primaryContainer.Hex(),
		OnPrimaryContainer:   on(primaryContainer, onSurface, text).Hex(),
		Secondary:            secondary.Hex(),
		OnSecondary:          on(secondary, surface, text).Hex(),
		SecondaryContainer:   secondaryContainer.Hex(),
		OnSecondaryContainer: on(secondaryContainer, onSurface, text).Hex(),
		Tertiary:             tertiary.Hex(),
		OnTertiary:           on(tertiary, surface, text).Hex(),
		TertiaryContainer:    tertiaryContainer.Hex(),
		OnTertiaryContainer:  on(tertiaryContainer, onSurface, text).Hex(),

		Error:            errCol.Hex(),
		OnError:          on(errCol, surface, text).Hex(),
		ErrorContainer:   errorContainer.Hex(),
		OnErrorContainer: on(errorContainer, onSurface, text).Hex(),

		Surface:                 surface.Hex(),
		OnSurface:               body.Hex(),
		SurfaceVariant:          surfaceVariant.Hex(),
		OnSurfaceVariant:        variant.Hex(),
		SurfaceDim:              surfaceDim.Hex(),
		SurfaceBright:           surfaceBright.Hex(),
		SurfaceContainerLowest:  containerLowest.Hex(),
		SurfaceContainerLow:     containerLow.Hex(),
		SurfaceContainer:        container.Hex(),
		SurfaceContainerHigh:    containerHigh.Hex(),
		SurfaceContainerHighest: containerHighest.Hex(),

		Background:   surface.Hex(),
		OnBackground: body.Hex(),

		Outline:        outline.Hex(),
		OutlineVariant: outlineVariant.Hex(),

		InverseSurface:   inverseSurface.Hex(),
		InverseOnSurface: inverseOnSurface.Hex(),
		InversePrimary:   on(inverseSurface, primary, nonText).Hex(),

		Shadow:      "#000000",
		Scrim:       "#000000",
		SurfaceTint: primary.Hex(),

		PrimaryFixed:            primaryFixed.Hex(),
		PrimaryFixedDim:         primaryFixedDim.Hex(),
		OnPrimaryFixed:          onWorst(onSurface, text, primaryFixed, primaryFixedDim).Hex(),
		OnPrimaryFixedVariant:   on(primaryFixed, onSurface, text).Hex(),
		SecondaryFixed:          secondaryFixed.Hex(),
		SecondaryFixedDim:       secondaryFixedDim.Hex(),
		OnSecondaryFixed:        onWorst(onSurface, text, secondaryFixed, secondaryFixedDim).Hex(),
		OnSecondaryFixedVariant: on(secondaryFixed, onSurface, text).Hex(),
		TertiaryFixed:           tertiaryFixed.Hex(),
		TertiaryFixedDim:        tertiaryFixedDim.Hex(),
		OnTertiaryFixed:         onWorst(onSurface, text, tertiaryFixed, tertiaryFixedDim).Hex(),
		OnTertiaryFixedVariant:  on(tertiaryFixed, onSurface, text).Hex(),
	}
}

// mustColor parses an authored anchor. The anchors are compiled constants, so
// a parse failure is a typo in this file rather than anything a user can cause,
// and a black stand-in makes it obvious in the palette test rather than at a
// call site far away.
func mustColor(hex string) Color {
	c, err := ParseColor(hex)
	if err != nil {
		return Color{A: 0xff}
	}
	return c
}
