package theme

// Fallback is the compiled-in palette used when matugen is absent or fails.
// It is a complete role family so a shell running without a generator has the
// same contract as one running with it.
//
// The surface ladder is measured, not chosen by eye: a capsule separates from
// its bar by roughly 1.15:1 here and in the reference bar the values were read
// from. An earlier ladder held the same ratio but sat so near black that the
// difference was imperceptible, so capsules did not read at all.
//
// The accent and error foregrounds are the output of EnsureContrast against
// this palette's own backgrounds, so Fallback satisfies Valid rather than
// merely defining every field.
var Fallback = Tokens{
	Primary:                 "#0080ff",
	OnPrimary:               "#0b1016",
	PrimaryContainer:        "#1f7ab5",
	OnPrimaryContainer:      "#ffffff",
	Secondary:               "#bec6dc",
	OnSecondary:             "#283041",
	SecondaryContainer:      "#3e4759",
	OnSecondaryContainer:    "#dae2f9",
	Tertiary:                "#ddbce0",
	OnTertiary:              "#3f2844",
	TertiaryContainer:       "#573e5c",
	OnTertiaryContainer:     "#fad8fd",
	Error:                   "#ff5449",
	OnError:                 "#1a1a1a",
	ErrorContainer:          "#93000a",
	OnErrorContainer:        "#ffdad6",
	Surface:                 "#1d2025",
	OnSurface:               "#e6e6e6",
	SurfaceVariant:          "#2e333a",
	OnSurfaceVariant:        "#9aa0a6",
	SurfaceDim:              "#15171b",
	SurfaceBright:           "#3b3f45",
	SurfaceContainerLowest:  "#121419",
	SurfaceContainerLow:     "#1f2229",
	SurfaceContainer:        "#282c33",
	SurfaceContainerHigh:    "#3a4149",
	SurfaceContainerHighest: "#464e58",
	Background:              "#1d2025",
	OnBackground:            "#e6e6e6",
	Outline:                 "#737d89",
	OutlineVariant:          "#59616b",
	InverseSurface:          "#e2e2e9",
	InverseOnSurface:        "#2e3036",
	InversePrimary:          "#425e91",
	Shadow:                  "#000000",
	Scrim:                   "#000000",
	SurfaceTint:             "#abc7ff",
	PrimaryFixed:            "#d7e3ff",
	PrimaryFixedDim:         "#abc7ff",
	OnPrimaryFixed:          "#001b3f",
	OnPrimaryFixedVariant:   "#284677",
	SecondaryFixed:          "#dae2f9",
	SecondaryFixedDim:       "#bec6dc",
	OnSecondaryFixed:        "#131c2b",
	OnSecondaryFixedVariant: "#3e4759",
	TertiaryFixed:           "#fad8fd",
	TertiaryFixedDim:        "#ddbce0",
	OnTertiaryFixed:         "#29132e",
	OnTertiaryFixedVariant:  "#573e5c",
}

// FallbackHighContrast is the compiled palette for a cold start with high
// contrast enabled.
//
// A second table rather than a repair of Fallback: 7:1 is unreachable against
// a saturated mid-tone fill, so no foreground repair can rescue a bright
// accent -- pure black on Fallback's #0080ff reaches only 5.53:1. matugen
// solves this at --contrast 1 by moving the accent backgrounds themselves,
// which a fixed table cannot do at runtime without rewriting the user's hues.
// These values are that generator's output for the same seed, so FallbackFor
// satisfies Valid in either mode and the cold-start path needs no special
// case.
var FallbackHighContrast = Tokens{
	Primary:                 "#ebf0ff",
	OnPrimary:               "#000000",
	PrimaryContainer:        "#a6c3fc",
	OnPrimaryContainer:      "#000000",
	Secondary:               "#ebf0ff",
	OnSecondary:             "#000000",
	SecondaryContainer:      "#bac3d8",
	OnSecondaryContainer:    "#000000",
	Tertiary:                "#ffeafe",
	OnTertiary:              "#000000",
	TertiaryContainer:       "#d9b8dc",
	OnTertiaryContainer:     "#000000",
	Error:                   "#ffece9",
	OnError:                 "#000000",
	ErrorContainer:          "#ffaea4",
	OnErrorContainer:        "#000000",
	Surface:                 "#111318",
	OnSurface:               "#ffffff",
	SurfaceVariant:          "#44474e",
	OnSurfaceVariant:        "#ffffff",
	SurfaceDim:              "#111318",
	SurfaceBright:           "#4e5056",
	SurfaceContainerLowest:  "#000000",
	SurfaceContainerLow:     "#1e2025",
	SurfaceContainer:        "#2e3036",
	SurfaceContainerHigh:    "#393b41",
	SurfaceContainerHighest: "#45474c",
	Background:              "#111318",
	OnBackground:            "#ffffff",
	Outline:                 "#eeeff9",
	OutlineVariant:          "#c0c2cc",
	InverseSurface:          "#e2e2e9",
	InverseOnSurface:        "#000000",
	InversePrimary:          "#2a4879",
	Shadow:                  "#000000",
	Scrim:                   "#000000",
	SurfaceTint:             "#abc7ff",
	PrimaryFixed:            "#d7e3ff",
	PrimaryFixedDim:         "#abc7ff",
	OnPrimaryFixed:          "#000000",
	OnPrimaryFixedVariant:   "#00102b",
	SecondaryFixed:          "#dae2f9",
	SecondaryFixedDim:       "#bec6dc",
	OnSecondaryFixed:        "#000000",
	OnSecondaryFixedVariant: "#081121",
	TertiaryFixed:           "#fad8fd",
	TertiaryFixedDim:        "#ddbce0",
	OnTertiaryFixed:         "#000000",
	OnTertiaryFixedVariant:  "#1d0823",
}

// FallbackFor returns the compiled palette for the accessibility mode in
// force. The result satisfies Valid for that same mode.
func FallbackFor(highContrast bool) Tokens {
	if highContrast {
		return FallbackHighContrast
	}
	return Fallback
}
