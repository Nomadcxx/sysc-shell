package settings

import "slices"

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
