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

	// Items are the members of a group item, rendered inside one shared
	// capsule. Empty on every other id, and a group may not nest.
	Items []Item

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

	// Plugin, Entry and Instance address an external plugin widget on a
	// "plugin" item and are empty on every built-in one. Instance namespaces
	// this placement's settings, so two copies of one widget can differ.
	Plugin   string
	Entry    string
	Instance string
}

// Plugins is the shell's record of external plugins: which are turned on and
// what the user has configured them with.
//
// Nothing here is checked against what is installed. Configuration cannot know
// that, and a plugin that is temporarily absent must not cost the user their
// settings or their bar layout, so an entry naming an uninstalled plugin is
// preserved and the host shows a placeholder in its place.
type Plugins struct {
	// Enabled lists the plugin ids the user has turned on.
	Enabled []string
	// Settings holds plugin-scoped values, keyed by plugin id.
	Settings map[string]map[string]any
	// Instances holds widget-instance-scoped values, keyed by the placement
	// instance id.
	Instances map[string]map[string]any
}

// Clone is a deep copy, so a candidate cannot alias live configuration.
func (p Plugins) Clone() Plugins { return p.clone() }

func (p Plugins) clone() Plugins {
	out := Plugins{}
	if p.Enabled != nil {
		out.Enabled = append([]string(nil), p.Enabled...)
	}
	out.Settings = cloneValues(p.Settings)
	out.Instances = cloneValues(p.Instances)
	return out
}

func cloneValues(in map[string]map[string]any) map[string]map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]map[string]any, len(in))
	for k, v := range in {
		inner := make(map[string]any, len(v))
		for ik, iv := range v {
			inner[ik] = iv
		}
		out[k] = inner
	}
	return out
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
	Source string // wallpaper | hex | stock
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
	OSD     string
}

// TrayPreferences are stable service-independent item tokens. Service
// generations deliberately never appear here: reconnecting the tray service
// must not discard a user's placement choices.
type TrayPreferences struct {
	Hidden []string
	Pinned []string
	Order  []string
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
// Wallpaper is the picker's two library roots plus the gSlapper playback
// settings. Assignments are not here: what is on which output is state, not
// configuration, and lives under $XDG_STATE_HOME (D19).
//
// The directories keep a literal leading tilde. Default() must not read the
// environment, so expansion belongs to whoever opens the directory.
type Wallpaper struct {
	ImageDirectory string
	VideoDirectory string
	// Scale is fill, stretch, original, or panscan, forwarded to GStreamer.
	Scale string
	Loop  bool
	// FPS is the frame cap: 30, 60, or 100.
	FPS          int
	Fade         bool
	FadeDuration float64
	// Hidden is none, auto-pause, or auto-stop: what gSlapper does when the
	// wallpaper is occluded. It also decides whether a video-to-video apply can
	// use IPC `change`, because gSlapper requires --auto-stop for that.
	Hidden string
}

// wallpaperScales, wallpaperFPS, and wallpaperHidden are closed vocabularies:
// an unknown value fails the load rather than silently falling back, so a typo
// is visible instead of quietly changing what the engine does.
var (
	wallpaperScales = map[string]bool{"fill": true, "stretch": true, "original": true, "panscan": true}
	wallpaperFPS    = map[int]bool{30: true, 60: true, 100: true}
	wallpaperHidden = map[string]bool{"none": true, "auto-pause": true, "auto-stop": true}
)

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
	Tray          TrayPreferences
	Weather       Weather
	Wallpaper     Wallpaper
	Outputs       []OutputOverride
	Templates     map[string]bool
	Plugins       Plugins
}

// knownItems is the Milestone 3 widget vocabulary through Tranche 3B. The
// Milestone 2 fixture ids are deliberately absent: there is no compatibility
// promise, so a stale configuration fails loudly instead of silently dropping
// a widget.
var knownItems = map[string]struct{}{
	"clock": {}, "workspace": {}, "window-title": {},
	"cpu": {}, "memory": {}, "filesystem": {}, "block": {}, "network": {},
	"weather": {}, "battery": {}, "notifications": {},
	// group holds other items inside one capsule. It carries no options of
	// its own; every option belongs to a nested item.
	"group": {},
	// "plugin" is a placement rather than a widget of its own: the item names
	// which external plugin widget fills the slot.
	"plugin": {},
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
			// Time and date sit together, which is what each reference shell
			// does; the right section carries status widgets. Weather is not
			// here: it requires configured coordinates, so a default bar
			// carrying it would fail validation out of the box.
			Center: []Item{
				{ID: "clock", Format: defaultClockFormat, Boundary: time.Minute},
				{ID: "clock", Format: defaultDateFormat, Boundary: time.Minute},
			},
			Right: []Item{
				{ID: "group", Items: []Item{
					{ID: "cpu", Display: "text", Interval: defaultMetricInterval},
					{ID: "memory", Display: "text", Interval: defaultMetricInterval},
				}},
				{ID: "battery", Interval: defaultMetricInterval},
				{ID: "notifications"},
			},
		},
		Theme: Theme{Radius: 12},
		ThemeGen: ThemeConfig{
			Source: "wallpaper",
			Scheme: "scheme-tonal-spot",
			Mode:   "dark",
		},
		Panels: Panels{Gap: 0, Padding: 8, OSD: "bottom-center"},
		Wallpaper: Wallpaper{
			ImageDirectory: "~/Pictures/Wallpapers",
			VideoDirectory: "~/Videos/Wallpapers",
			Scale:          "fill",
			Loop:           true,
			FPS:            30,
			FadeDuration:   0.5,
			Hidden:         "none",
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

func (c Config) TemplateEnabled(name string) bool {
	if c.Templates != nil {
		if v, ok := c.Templates[name]; ok {
			return v
		}
	}
	return name == "niri"
}

func pathErr(path, format string, args ...any) error {
	return fmt.Errorf("config: %s: %s", path, fmt.Sprintf(format, args...))
}
