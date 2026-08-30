// Package config loads and validates the shell's JSON configuration.
//
// A candidate is validated in full before it can replace live state, and a
// failure names the exact field path. JSON comes from the standard library:
// both reference shells store JSON, so no parser dependency is needed.
package config

import (
	"fmt"
	"time"
)

// Item is one validated widget instance. Options live on the instance rather
// than the bar, so one bar can carry two clocks with different formats and two
// filesystem widgets watching different mounts.
type Item struct {
	ID string
	// Format is the Go layout string for a clock. Empty on other items.
	Format string
	// Boundary is how often this clock's text can change, derived from Format
	// at load. Zero on other items.
	Boundary time.Duration
	// MaxWidth caps a window title in logical pixels. Zero on other items.
	MaxWidth int

	// Display is "text", "meter" or "graph" on a metric item. Empty elsewhere.
	Display string
	// Interval is the sampling interval for a metric item. Zero elsewhere.
	Interval time.Duration
	// Path names the mount a filesystem item watches. Empty on other items.
	Path string
	// Device names the block device a block item watches.
	Device string
	// Interface names the network interface a network item watches.
	Interface string
	// Direction is "read"/"write" on block and "rx"/"tx" on network.
	Direction string
	// ShowCondition appends the condition word on a weather item.
	ShowCondition bool
	// Label is "percent", "time", "rate" or "none" on a battery item.
	Label string
	// WarnBelow is the percentage at or below which a discharging battery
	// warns. Zero on other items.
	WarnBelow int
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

// Theme is geometry the palette generator does not produce.
type Theme struct {
	Radius int
}

// ThemeConfig selects how the Material 3 palette is seeded.
type ThemeConfig struct {
	Source string // wallpaper | hex; 4B adds stock names with its catalog
	Seed   string // image path or #RRGGBB — meaning follows Source
	Scheme string // matugen scheme-*, default scheme-tonal-spot
	Mode   string // dark | light
}

type Accessibility struct {
	ReducedMotion bool
	HighContrast  bool
}

type Session struct {
	Locker string // external locker command; empty hides the lock action
}

type Panels struct {
	Gap     int // offset from the bar edge, logical px
	Padding int // output edge inset for clamping, logical px
}

// OutputOverride adjusts the bar on one connector.
type OutputOverride struct {
	Connector string
	Bar       Bar
}

// Weather is the process-wide weather source. Coordinates live here rather
// than on the item because one service serves every bar.
//
// Configured distinguishes a supplied block from the zero value, which is what
// lets a weather widget with no block fail with a useful message.
type Weather struct {
	Latitude   float64
	Longitude  float64
	Unit       string
	Interval   time.Duration
	Configured bool
}

// Config is an immutable, fully resolved configuration.
type Config struct {
	Bar           Bar
	Theme         Theme
	ThemeGen      ThemeConfig
	Accessibility Accessibility
	Session       Session
	Panels        Panels
	Weather       Weather
	Outputs       []OutputOverride
}

// knownItems is the Milestone 3 widget vocabulary through Tranche 3B. The
// Milestone 2 fixture ids are deliberately absent: there is no compatibility
// promise, so a stale configuration fails loudly instead of silently dropping
// a widget.
var knownItems = map[string]struct{}{
	"clock": {}, "workspace": {}, "window-title": {},
	"cpu": {}, "memory": {}, "filesystem": {}, "block": {}, "network": {},
	"weather": {}, "battery": {},
}

// fractionSources yield a value between zero and one, which a meter can fill.
// Rate sources yield bytes per second and have no full scale, so a meter is
// meaningless on them and rejected at load.
var fractionSources = map[string]bool{"cpu": true, "memory": true, "filesystem": true}

// rateSources yield bytes per second.
var rateSources = map[string]bool{"block": true, "network": true}

// blockDirections and networkDirections are deliberately separate vocabularies
// so "rx" on a block device fails rather than silently meaning "read".
var blockDirections = map[string]bool{"read": true, "write": true}
var networkDirections = map[string]bool{"rx": true, "tx": true}

// isMetric reports whether an id names a metric widget.
func isMetric(id string) bool { return fractionSources[id] || rateSources[id] }

const (
	// defaultClockFormat and defaultDateFormat are the two default clock
	// instances. There is no separate date widget: a date is a clock with a
	// coarser layout.
	defaultClockFormat = "15:04"
	defaultDateFormat  = "Mon 2 Jan"
	// defaultTitleMaxWidth matches the shipped default in the reference
	// shells, which cap the focused-window title at 250 to 260 logical pixels.
	defaultTitleMaxWidth = 260
	// defaultMetricInterval is the sampling period a metric item uses unless
	// it names its own.
	defaultMetricInterval = 2 * time.Second
	// defaultMetricDisplay renders a value as text.
	defaultMetricDisplay = "text"
	// defaultWeatherInterval matches the reference shell's fifteen minutes.
	defaultWeatherInterval  = 15 * time.Minute
	defaultWeatherUnit      = "celsius"
	defaultBatteryLabel     = "percent"
	defaultBatteryWarnBelow = 20
	// defaultBatteryInterval is coarser than the metric default: a battery
	// percentage moves a point an hour, so sampling it every two seconds would
	// cost wake-ups for nothing.
	defaultBatteryInterval = 30 * time.Second
)

// weatherUnits are the units the API accepts.
var weatherUnits = map[string]bool{"celsius": true, "fahrenheit": true}

// batteryLabels are the label modes a battery item accepts.
var batteryLabels = map[string]bool{
	"percent": true, "time": true, "rate": true, "none": true,
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
		Theme: Theme{Radius: 12},
		ThemeGen: ThemeConfig{
			Source: "wallpaper",
			Scheme: "scheme-tonal-spot",
			Mode:   "dark",
		},
		Panels: Panels{Gap: 8, Padding: 8},
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
