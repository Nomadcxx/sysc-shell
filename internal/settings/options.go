package settings

import (
	"slices"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// CustomPalette is one saved palette as the settings pane needs it. The
// settings package never reads the palettes directory; the shell passes this in.
type CustomPalette struct {
	Slug string
	Name string
}

type options struct{ custom []CustomPalette }

// Option adjusts a registry at construction.
type Option func(*options)

// WithCustomPalettes supplies the saved palettes, which makes the custom source
// and the appearance.custom entry available.
func WithCustomPalettes(p []CustomPalette) Option {
	return func(o *options) { o.custom = slices.Clone(p) }
}

func (o options) slugs() []string {
	out := make([]string, len(o.custom))
	for i, p := range o.custom {
		out[i] = p.Slug
	}
	return out
}

// CustomPalettesFrom is the usable part of a store listing. A file that cannot
// be loaded is left out of the settings; the Palettes page shows it.
func CustomPalettesFrom(list []theme.PaletteInfo) []CustomPalette {
	var out []CustomPalette
	for _, p := range list {
		if p.Err == nil {
			out = append(out, CustomPalette{Slug: p.Slug, Name: p.Name})
		}
	}
	return out
}
