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
var Fallback = Tokens{
	Surface: "#101214", SurfaceContainer: "#181a1d",
	OnSurface: "#e6e6e6", OnSurfaceVariant: "#9aa0a6",
	Primary: "#0080ff", OnPrimary: "#ffffff",
	PrimaryContainer: "#003a75", OnPrimaryContainer: "#d6e3ff",
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
