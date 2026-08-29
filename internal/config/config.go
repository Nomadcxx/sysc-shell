// Package config loads and validates the shell's JSON configuration.
//
// A candidate is validated in full before it can replace live state, and a
// failure names the exact field path. JSON comes from the standard library:
// both reference shells store JSON, so no parser dependency is needed.
package config

import (
	"fmt"
	"strings"
)

// Bar is the resolved policy for one bar.
type Bar struct {
	Enabled    bool
	Edge       string
	Height     int
	Gap        int
	Padding    int
	Spacing    int
	Radius     int
	FontFamily string
	FontSize   int
	Left       []string
	Center     []string
	Right      []string
}

// Theme is the resolved visual token set. Colours are #RRGGBB or #RRGGBBAA.
type Theme struct {
	Background string
	Foreground string
	Accent     string
	Muted      string
	Error      string
	Radius     int
}

// BackgroundOpaque reports whether the validated background colour has full
// alpha. Six-digit colours imply ff.
func (t Theme) BackgroundOpaque() bool {
	return len(t.Background) == 7 ||
		(len(t.Background) == 9 && strings.EqualFold(t.Background[7:], "ff"))
}

// OutputOverride adjusts the bar on one connector.
type OutputOverride struct {
	Connector string
	Bar       Bar
}

// Config is an immutable, fully resolved configuration.
type Config struct {
	Bar     Bar
	Theme   Theme
	Outputs []OutputOverride
}

// knownItems is the Milestone 2 item vocabulary.
//
// Unknown ids are rejected rather than ignored, so Milestone 3 defines the real
// vocabulary instead of inheriting an accidental one, and so a typo in a
// hand-edited file is visible rather than silently dropping a widget.
var knownItems = map[string]struct{}{
	"shell-name": {}, "workspace": {}, "meter": {}, "toggle": {},
}

// supportedEdges names every edge the model understands and whether this
// milestone implements it. An unimplemented edge is rejected with a named
// error rather than silently mis-rendering.
var supportedEdges = map[string]bool{
	"top": true, "bottom": false, "left": false, "right": false,
}

// Default is the built-in configuration, used when no file exists.
func Default() Config {
	return Config{
		Bar: Bar{
			Enabled: true, Edge: "top",
			Height: 48, Gap: 4, Padding: 8, Spacing: 6, Radius: 12,
			FontFamily: "sans-serif", FontSize: 14,
			Left:   []string{"shell-name"},
			Center: []string{"workspace"},
			Right:  []string{"meter", "toggle"},
		},
		Theme: Theme{
			Background: "#101418", Foreground: "#e8ecf0",
			Accent: "#0080ff", Muted: "#303438", Error: "#ff4040",
			Radius: 12,
		},
	}
}

// ForConnector resolves the bar policy for one connector. The first matching
// override wins; unset override fields keep the base value.
func (c Config) ForConnector(name string) Bar {
	for _, o := range c.Outputs {
		if o.Connector == name {
			return o.Bar
		}
	}
	return c.Bar
}

func pathErr(path, format string, args ...any) error {
	return fmt.Errorf("config: %s: %s", path, fmt.Sprintf(format, args...))
}
