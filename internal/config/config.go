// Package config loads and validates the shell's JSON configuration.
//
// A candidate is validated in full before it can replace live state, and a
// failure names the exact field path. JSON comes from the standard library:
// both reference shells store JSON, so no parser dependency is needed.
package config

import (
	"fmt"
	"strings"
	"time"
)

// Item is one validated widget instance. Options live on the instance rather
// than the bar, so one bar can carry two clocks with different formats.
type Item struct {
	ID string
	// Format is the Go layout string for a clock. Empty on other items.
	Format string
	// Boundary is how often this clock's text can change, derived from Format
	// at load. Zero on other items.
	Boundary time.Duration
	// MaxWidth caps a window title in logical pixels. Zero on other items.
	MaxWidth int
}

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
	Left       []Item
	Center     []Item
	Right      []Item
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

// knownItems is the Tranche 3A widget vocabulary. The Milestone 2 fixture ids
// are deliberately absent: there is no compatibility promise, so a stale
// configuration fails loudly instead of silently dropping a widget.
var knownItems = map[string]struct{}{
	"clock": {}, "workspace": {}, "window-title": {},
}

const (
	// defaultClockFormat and defaultDateFormat are the two default clock
	// instances. There is no separate date widget: a date is a clock with a
	// coarser layout.
	defaultClockFormat = "15:04"
	defaultDateFormat  = "Mon 2 Jan"
	// defaultTitleMaxWidth matches the shipped default in the reference
	// shells, which cap the focused-window title at 250 to 260 logical pixels.
	defaultTitleMaxWidth = 260
)

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
			Height: 48, Gap: 4, Padding: 6, Spacing: 4, Radius: 12,
			FontFamily: "sans-serif", FontSize: 14,
			Left: []Item{
				{ID: "workspace"},
				{ID: "window-title", MaxWidth: defaultTitleMaxWidth},
			},
			Center: []Item{
				{ID: "clock", Format: defaultClockFormat, Boundary: time.Minute},
			},
			Right: []Item{
				{ID: "clock", Format: defaultDateFormat, Boundary: time.Minute},
			},
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
