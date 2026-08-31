// Package theme generates the Material 3 token set the shell renders from.
package theme

// Tokens is the subset of Material 3 tokens the shell consumes. Dark and
// light variants are generated together; Mode selects which is active.
type Tokens struct {
	Surface            string
	SurfaceContainer   string
	OnSurface          string
	OnSurfaceVariant   string
	Primary            string
	OnPrimary          string
	PrimaryContainer   string
	OnPrimaryContainer string
	Outline            string
	Error              string
	OnError            string
}

// Fallback is the compiled-in palette used when matugen is absent or fails.
// Seeded from the Milestone 2 ProofStyle colors so the shell never renders
// without a theme.
// Levels are chosen against a live reference rather than by eye. A capsule
// separates from its bar by roughly 1.15:1 in both this palette and the DMS
// bar it was measured from; the earlier values held the same ratio but sat so
// near black that the difference was imperceptible, so capsules did not read
// at all. Surface luminance now matches the reference.
var Fallback = Tokens{
	Surface: "#1d2025", SurfaceContainer: "#282c33",
	OnSurface: "#e6e6e6", OnSurfaceVariant: "#9aa0a6",
	Primary: "#0080ff", OnPrimary: "#0b1016",
	PrimaryContainer: "#1f7ab5", OnPrimaryContainer: "#0b1016",
	Outline: "#4a4f55", Error: "#ff5449", OnError: "#ffffff",
}

type Source struct {
	Kind string // wallpaper | hex | stock
	Seed string
}

type Options struct {
	Mode         string // dark | light
	Scheme       string
	HighContrast bool
}
