// Package theme generates the Material 3 token set the shell renders from.
package theme

import "fmt"

// Tokens is the subset of Material 3 tokens the shell consumes. Dark and
// light variants are generated together; Mode selects which is active.
type Tokens struct {
	Surface                 string
	SurfaceContainer        string
	SurfaceContainerHigh    string
	SurfaceContainerHighest string
	OnSurface               string
	OnSurfaceVariant        string
	Primary                 string
	OnPrimary               string
	PrimaryContainer        string
	OnPrimaryContainer      string
	Outline                 string
	OutlineVariant          string
	Error                   string
	OnError                 string
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
	SurfaceContainerHigh: "#3a4149", SurfaceContainerHighest: "#464e58",
	OnSurface: "#e6e6e6", OnSurfaceVariant: "#9aa0a6",
	Primary: "#0080ff", OnPrimary: "#0b1016",
	PrimaryContainer: "#1f7ab5", OnPrimaryContainer: "#0b1016",
	Outline: "#737d89", OutlineVariant: "#59616b",
	Error: "#ff5449", OnError: "#ffffff",
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

// hexColor reports whether s is a colour the shell can parse: #RRGGBB or
// #RRGGBBAA.
func hexColor(s string) bool {
	if len(s) != 7 && len(s) != 9 {
		return false
	}
	if s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// Complete reports whether every role the shell paints with carries a parseable
// colour. A partial palette must never be published: with half the roles
// updated a surface paints in two themes at once, which is worse than keeping
// the old one, so a generator that returns an incomplete set is rejected whole.
func (t Tokens) Complete() error {
	for _, role := range []struct {
		name, value string
	}{
		{"surface", t.Surface},
		{"surface_container", t.SurfaceContainer},
		{"surface_container_high", t.SurfaceContainerHigh},
		{"surface_container_highest", t.SurfaceContainerHighest},
		{"on_surface", t.OnSurface},
		{"on_surface_variant", t.OnSurfaceVariant},
		{"primary", t.Primary},
		{"on_primary", t.OnPrimary},
		{"primary_container", t.PrimaryContainer},
		{"on_primary_container", t.OnPrimaryContainer},
		{"outline", t.Outline},
		{"outline_variant", t.OutlineVariant},
		{"error", t.Error},
		{"on_error", t.OnError},
	} {
		if !hexColor(role.value) {
			return fmt.Errorf("theme: role %s is %q, not a #RRGGBB colour", role.name, role.value)
		}
	}
	return nil
}
