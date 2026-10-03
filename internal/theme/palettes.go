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

const (
	dmsDarkError  = "#f2b8b5"
	dmsLightError = "#b3261e"
)

// palettes holds anchors from Noctalia, DMS, Eldritch, and sysc-greet. Roles
// each source does not publish are completed by derive() and use the same
// contrast validation as generated palettes. Noctalia's surfaceVariant is the
// preferred High anchor; entries that need another value name its source or
// local tuning. The four newly added Noctalia schemes use Surface for Low,
// while the six earlier schemes retain their existing Low anchors. DMS maps
// surfaceContainerHighest to High and uses authored Low endpoints because it
// publishes no surfaceContainerLowest.
var palettes = map[string]struct{ Dark, Light anchors }{
	"catppuccin": {
		// Mocha and Latte.
		Dark: anchors{
			Surface: "#1e1e2e", OnSurface: "#cdd6f4",
			Low: "#11111b", High: "#313244",
			Primary: "#cba6f7", Secondary: "#fab387", Tertiary: "#94e2d5", Error: "#f38ba8",
		},
		Light: anchors{
			Surface: "#eff1f5", OnSurface: "#4c4f69",
			Low: "#dce0e8", High: "#ccd0da",
			Primary: "#8839ef", Secondary: "#fe640b", Tertiary: "#40a02b", Error: "#d20f39",
		},
	},
	"gruvbox": {
		Dark: anchors{
			Surface: "#282828", OnSurface: "#fbf1c7",
			Low: "#1d2021", High: "#3c3836",
			Primary: "#b8bb26", Secondary: "#fabd2f", Tertiary: "#83a598", Error: "#fb4934",
		},
		Light: anchors{
			Surface: "#fbf1c7", OnSurface: "#3c3836",
			Low: "#f9f5d7", High: "#ebdbb2",
			Primary: "#98971a", Secondary: "#d79921", Tertiary: "#458588", Error: "#cc241d",
		},
	},
	"nord": {
		// Polar Night and Snow Storm.
		Dark: anchors{
			Surface: "#2e3440", OnSurface: "#eceff4",
			Low: "#272c36", High: "#3b4252",
			Primary: "#8fbcbb", Secondary: "#88c0d0", Tertiary: "#5e81ac", Error: "#bf616a",
		},
		Light: anchors{
			Surface: "#eceff4", OnSurface: "#2e3440",
			Low: "#e5e9f0", High: "#c8d0dc", // Retained anchor; surfaceVariant collapses the shell's visible container ladder.
			Primary: "#5e81ac", Secondary: "#64adc2", Tertiary: "#6fa9a8", Error: "#bf616a",
		},
	},
	"dracula": {
		Dark: anchors{
			Surface: "#282a36", OnSurface: "#f8f8f2",
			Low: "#21222c", High: "#44475a",
			Primary: "#bd93f9", Secondary: "#ff79c6", Tertiary: "#8be9fd", Error: "#ff5555",
		},
		Light: anchors{
			Surface: "#f8f8f2", OnSurface: "#282a36",
			Low: "#f5f1e0", High: "#e6e6ea",
			Primary: "#8332f4", Secondary: "#ff1399", Tertiary: "#0398b9", Error: "#ff5555",
		},
	},
	"tokyo-night": {
		Dark: anchors{
			Surface: "#1a1b26", OnSurface: "#c0caf5",
			Low: "#16161e", High: "#24283b",
			Primary: "#7aa2f7", Secondary: "#bb9af7", Tertiary: "#9ece6a", Error: "#f7768e",
		},
		Light: anchors{
			// Noctalia's #3760bf is an accent and leaves too little contrast
			// against raised light surfaces; use Tokyo Night's dark ink instead.
			Surface: "#e1e2e7", OnSurface: "#343b58",
			Low: "#d5d6db", High: "#a8aecb", // Noctalia shadow supplies sufficient container separation.
			Primary: "#2e7de9", Secondary: "#9854f1", Tertiary: "#587539", Error: "#f52a65",
		},
	},
	"rose-pine": {
		// Main and Dawn.
		Dark: anchors{
			Surface: "#191724", OnSurface: "#e0def4",
			Low: "#16141f", High: "#26233a",
			Primary: "#ebbcba", Secondary: "#9ccfd8", Tertiary: "#31748f", Error: "#eb6f92",
		},
		Light: anchors{
			Surface: "#fffaf3", OnSurface: "#575279",
			Low: "#fffaf3", High: "#f2e9e1",
			Primary: "#d7827e", Secondary: "#56949f", Tertiary: "#286983", Error: "#b4637a",
		},
	},
	// Noctalia's other built-ins publish surfaceVariant but no container ladder.
	// Surface supplies Low; surfaceVariant supplies High unless an entry notes
	// an alternate anchor needed to keep the shell's container ladder visible.
	"ayu": {
		Dark: anchors{
			Surface: "#0b0e14", OnSurface: "#d1d1c7",
			Low: "#0b0e14", High: "#1e222a",
			Primary: "#e6b450", Secondary: "#aad94c", Tertiary: "#39bae6", Error: "#d95757",
		},
		Light: anchors{
			Surface: "#f8f9fa", OnSurface: "#42474c",
			Low: "#f8f9fa", High: "#e4e6e9",
			Primary: "#ff8f40", Secondary: "#86b300", Tertiary: "#55b4d4", Error: "#e65050",
		},
	},
	"kanagawa": {
		Dark: anchors{
			Surface: "#1f1f28", OnSurface: "#c8c093",
			Low: "#1f1f28", High: "#2a2a37",
			Primary: "#76946a", Secondary: "#c0a36e", Tertiary: "#7e9cd8", Error: "#c34043",
		},
		Light: anchors{
			Surface: "#f2ecbc", OnSurface: "#545464",
			Low: "#f2ecbc", High: "#e5ddb0",
			Primary: "#6f894e", Secondary: "#77713f", Tertiary: "#4d699b", Error: "#c84053",
		},
	},
	"noctalia": {
		Dark: anchors{
			Surface: "#070722", OnSurface: "#f3edf7",
			Low: "#070722", High: "#21215f", // Noctalia outline; variant collapses the shell's visible container ladder.
			Primary: "#fff59b", Secondary: "#a9aefe", Tertiary: "#9bfece", Error: "#fd4663",
		},
		Light: anchors{
			Surface: "#e6e8fa", OnSurface: "#0e0e43",
			Low: "#e6e8fa", High: "#c2c3d9", // Local anchor; surfaceVariant is too close to Surface for a visible ladder.
			Primary: "#5d65f5", Secondary: "#8e93d8", Tertiary: "#0e0e43", Error: "#fd4663",
		},
	},
	// Cthulhu and Dusk use the upstream Eldritch dark/light palettes.
	// Abyss remains separately selectable and uses its published colors in both modes.
	"eldritch": {
		Dark: anchors{
			Surface: "#212337", OnSurface: "#ebfafa",
			Low: "#212337", High: "#292e42",
			Primary: "#37f499", Secondary: "#04d1f9", Tertiary: "#a48cf2", Error: "#f16c75",
		},
		Light: anchors{
			Surface: "#f0f3f4", OnSurface: "#1e2029",
			Low: "#f0f3f4", High: "#d5d9db",
			Primary: "#fb5bb6", Secondary: "#0ad6ff", Tertiary: "#8a69f7", Error: "#fb5b66",
		},
	},
	// Published by https://github.com/eldritch-theme/eldritch at b1cf2bf7fae65a32974eababb4b86926a0ffebef.
	"eldritch-abyss": {
		Dark: anchors{
			Surface: "#171928", OnSurface: "#d8e6e6",
			Low: "#252738", High: "#474852",
			Primary: "#2dcc82", Secondary: "#0396b3", Tertiary: "#8b75d9", Error: "#cc5860",
		},
		Light: anchors{
			Surface: "#171928", OnSurface: "#d8e6e6",
			Low: "#252738", High: "#474852",
			Primary: "#2dcc82", Secondary: "#0396b3", Tertiary: "#8b75d9", Error: "#cc5860",
		},
	},
	// RAMA has no light definition in sysc-greet; preserve its source colors in both modes.
	"rama": {
		Dark: anchors{
			Surface: "#2b2d42", OnSurface: "#edf2f4",
			Low: "#2b2d42", High: "#3b3d52",
			Primary: "#ef233c", Secondary: "#d90429", Tertiary: "#f59e0b", Error: "#ef233c",
		},
		Light: anchors{
			Surface: "#2b2d42", OnSurface: "#edf2f4",
			Low: "#2b2d42", High: "#3b3d52",
			Primary: "#ef233c", Secondary: "#d90429", Tertiary: "#f59e0b", Error: "#ef233c",
		},
	},
	// Void has only a dark sysc-greet source palette; retain it in either mode.
	"void": {
		Dark: anchors{
			Surface: "#000000", OnSurface: "#ffffff",
			Low: "#000000", High: "#1a1a1a",
			Primary: "#ffffff", Secondary: "#ffffff", Tertiary: "#808080", Error: "#999999",
		},
		Light: anchors{
			Surface: "#000000", OnSurface: "#ffffff",
			Low: "#000000", High: "#1a1a1a",
			Primary: "#ffffff", Secondary: "#ffffff", Tertiary: "#808080", Error: "#999999",
		},
	},
	// DMS stock themes publish surfaceContainerHighest but not
	// surfaceContainerLowest. High maps to the published highest; Low is authored
	// below Surface for dark themes and above it for light themes. These entries
	// also lack tertiary and error, so surfaceTint and Material 3 defaults fill them.
	"amber": {
		Dark: anchors{
			Surface: "#17130b", OnSurface: "#ebe1d4",
			Low: "#130f07", High: "#39342b",
			Primary: "#ffc107", Secondary: "#ffd54f", Tertiary: "#ffd54f", Error: dmsDarkError,
		},
		Light: anchors{
			Surface: "#fff8f2", OnSurface: "#1f1b13",
			Low: "#fffaf4", High: "#ebe1d4",
			Primary: "#ff8f00", Secondary: "#ffc107", Tertiary: "#ff8f00", Error: dmsLightError,
		},
	},
	"blue": {
		Dark: anchors{
			Surface: "#101418", OnSurface: "#e0e2e8",
			Low: "#0c1014", High: "#32353a",
			Primary: "#42a5f5", Secondary: "#8ab4f8", Tertiary: "#8ab4f8", Error: dmsDarkError,
		},
		Light: anchors{
			Surface: "#f7f9ff", OnSurface: "#181c20",
			Low: "#f9fbff", High: "#e0e2e8",
			Primary: "#1976d2", Secondary: "#42a5f5", Tertiary: "#1976d2", Error: dmsLightError,
		},
	},
	"purple": {
		Dark: anchors{
			Surface: "#141218", OnSurface: "#e6e0e9",
			Low: "#100e14", High: "#36343a",
			Primary: "#d0bcff", Secondary: "#ccc2dc", Tertiary: "#d0bcff", Error: dmsDarkError,
		},
		Light: anchors{
			Surface: "#fef7ff", OnSurface: "#1d1b20",
			Low: "#fff9ff", High: "#e6e0e9",
			Primary: "#6750a4", Secondary: "#625b71", Tertiary: "#6750a4", Error: dmsLightError,
		},
	},
	"green": {
		Dark: anchors{
			Surface: "#10140f", OnSurface: "#e0e4db",
			Low: "#0c100b", High: "#323630",
			Primary: "#4caf50", Secondary: "#81c995", Tertiary: "#81c995", Error: dmsDarkError,
		},
		Light: anchors{
			Surface: "#f7fbf1", OnSurface: "#191d17",
			Low: "#f9fdf3", High: "#e0e4db",
			Primary: "#2e7d32", Secondary: "#4caf50", Tertiary: "#2e7d32", Error: dmsLightError,
		},
	},
	"orange": {
		Dark: anchors{
			Surface: "#1a120e", OnSurface: "#f0dfd8",
			Low: "#160e0a", High: "#3d332e",
			Primary: "#ff6d00", Secondary: "#ffb74d", Tertiary: "#ffb74d", Error: dmsDarkError,
		},
		Light: anchors{
			Surface: "#fff8f6", OnSurface: "#221a16",
			Low: "#fffaf8", High: "#f0dfd8",
			Primary: "#e65100", Secondary: "#ff9800", Tertiary: "#e65100", Error: dmsLightError,
		},
	},
	"red": {
		Dark: anchors{
			Surface: "#1a1110", OnSurface: "#f1dedc",
			Low: "#160d0c", High: "#3d3231",
			Primary: "#f44336", Secondary: "#f28b82", Tertiary: "#f28b82", Error: dmsDarkError,
		},
		Light: anchors{
			Surface: "#fff8f7", OnSurface: "#231918",
			Low: "#fffaf9", High: "#f1dedc",
			Primary: "#d32f2f", Secondary: "#f44336", Tertiary: "#d32f2f", Error: dmsLightError,
		},
	},
	"cyan": {
		Dark: anchors{
			Surface: "#0e1416", OnSurface: "#dee3e5",
			Low: "#0a1012", High: "#303637",
			Primary: "#00bcd4", Secondary: "#4dd0e1", Tertiary: "#4dd0e1", Error: dmsDarkError,
		},
		Light: anchors{
			Surface: "#f5fafc", OnSurface: "#171d1e",
			Low: "#f7fcfe", High: "#dee3e5",
			Primary: "#0097a7", Secondary: "#00bcd4", Tertiary: "#0097a7", Error: dmsLightError,
		},
	},
	"coral": {
		Dark: anchors{
			Surface: "#1a1110", OnSurface: "#f1dedc",
			Low: "#160d0c", High: "#3d3231",
			Primary: "#ffb4ab", Secondary: "#f9dedc", Tertiary: "#ffb4ab", Error: dmsDarkError,
		},
		Light: anchors{
			Surface: "#fff8f7", OnSurface: "#231918",
			Low: "#fffaf9", High: "#f1dedc",
			Primary: "#8c1d18", Secondary: "#ff5449", Tertiary: "#8c1d18", Error: dmsLightError,
		},
	},
	"pink": {
		Dark: anchors{
			Surface: "#191112", OnSurface: "#f0dee0",
			Low: "#150d0e", High: "#3c3233",
			Primary: "#e91e63", Secondary: "#f8bbd9", Tertiary: "#f8bbd9", Error: dmsDarkError,
		},
		Light: anchors{
			Surface: "#fff8f7", OnSurface: "#22191a",
			Low: "#fffaf9", High: "#f0dee0",
			Primary: "#c2185b", Secondary: "#e91e63", Tertiary: "#c2185b", Error: dmsLightError,
		},
	},
	"monochrome": {
		Dark: anchors{
			Surface: "#2a2a2a", OnSurface: "#e4e2e3",
			Low: "#252525", High: "#505050",
			Primary: "#ffffff", Secondary: "#c4c6d0", Tertiary: "#c2c6d6", Error: "#ffb4ab",
		},
		Light: anchors{
			Surface: "#f5f5f6", OnSurface: "#2a2a2a",
			Low: "#fafafb", High: "#d0d0d2",
			Primary: "#2b303c", Secondary: "#4a4d56", Tertiary: "#5a5f6e", Error: "#ba1a1a",
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
	// Pale named accents need this lift to keep On*Fixed above the 4.5:1 floor.
	fixedLift, dimLift := 0.62, 0.45
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
