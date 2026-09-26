// Package config loads and validates the shell's JSON configuration.
//
// A candidate is validated in full before it can replace live state, and a
// failure names the exact field path. JSON comes from the standard library:
// both reference shells store JSON, so no parser dependency is needed.
package config

import (
	"fmt"
	"sort"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
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
	// Sources lists the sources the user added, plus an entry named for the
	// built-in source when the user disabled it. EffectiveSources merges in
	// the built-in one.
	Sources []PluginSource
}

// PluginSource is one git repository the plugin store reads a catalog from.
type PluginSource struct {
	Name    string
	URL     string
	Enabled bool
}

// BuiltinPluginSource is the first-party catalog. It is always present: a
// configuration may disable it but not remove or repoint it.
var BuiltinPluginSource = PluginSource{Name: "sysc", URL: "https://github.com/Nomadcxx/sysc-plugins", Enabled: true}

// EffectiveSources is every source in order, the built-in one first.
func (p Plugins) EffectiveSources() []PluginSource {
	out := []PluginSource{BuiltinPluginSource}
	for _, s := range p.Sources {
		if s.Name == BuiltinPluginSource.Name {
			out[0].Enabled = s.Enabled
			continue
		}
		out = append(out, s)
	}
	return out
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
	if p.Sources != nil {
		out.Sources = append([]PluginSource(nil), p.Sources...)
	}
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
	Enabled bool
	Edge    string
	Height  int
	Gap     int
	// Reserve overrides how much of the output the compositor keeps clear for
	// this bar. Nil follows the extent, which is what every bar did before the
	// field existed; a stated zero lets windows tile beneath the bar. The
	// pointer is what separates "unset" from "zero", and it is also what keeps
	// Write from emitting a reserve into a document that never asked for one.
	Reserve    *int
	Padding    int
	Spacing    int
	Radius     int
	FontFamily string
	FontSize   int
	Left       []Item
	Center     []Item
	Right      []Item
}

// Body is the drawn height of the bar: the surface extent less the gap that
// keeps the screen edge clickable.
func (b Bar) Body() int { return b.Height - 2*b.Gap }

// Extent is how much of the cross axis the layer surface occupies. The gap
// lives inside the surface with a zero layer margin, so the extent carries one
// gap, not two.
//
// This is the one derivation. The platform reads it for the surface size and
// the shell reads it through Theme.Geometry; they computed it separately until
// Milestone 9, agreeing only because each encoded the same assumption.
func (b Bar) Extent() int { return b.Gap + b.Body() }

// ExclusiveZone is how much of the output the compositor keeps clear. It
// follows the extent unless the document states a reserve, which may be zero.
func (b Bar) ExclusiveZone() int {
	if b.Reserve == nil {
		return b.Extent()
	}
	return *b.Reserve
}

// Theme is the composition the palette generator does not produce: the
// independent density, typography, shape, opacity, elevation, and motion axes,
// plus the preset that seeded them.
//
// The composition is embedded rather than nested so every existing
// cfg.Theme.Radius reader keeps working, and so the resolver and the settings
// registry read one type rather than a configuration-shaped copy of it.
type Theme struct {
	// Preset is the bundled composition this theme started from. It is not a
	// mode: every axis below stays independently overridable, and the sparse
	// writer records only the axes that deviate from it.
	Preset theme.Preset
	theme.Composition
}

// ThemeConfig selects how the Material 3 palette is seeded.
type ThemeConfig struct {
	Source string // wallpaper | hex | stock | palette
	Seed   string // image path, #RRGGBB, stock name, or palette name — follows Source
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
	// Enabled controls whether the shell projects tray items into bars. The
	// tray service remains available so the projection can be re-enabled.
	Enabled bool
	Hidden  []string
	Pinned  []string
	Order   []string
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
	Latitude  float64
	Longitude float64
	Unit      string
	Interval  time.Duration
	// City is an optional place name the shell resolves to coordinates
	// through the forecast provider's geocoding. Exactly one of city or
	// latitude+longitude is accepted.
	City string
	// Location is an optional display label for the place the coordinates
	// name. It never feeds the request; the coordinates do that alone.
	Location   string
	Configured bool
}

// Media selects the active MPRIS player. Names are full well-known bus names,
// so a browser tab can be blacklisted without hiding an unrelated player.
type Media struct {
	Preferred string
	Blacklist []string
}

// Monitor is the system monitor panel. Colours are theme role names, not
// hex, so they follow the wallpaper palette; theme.ColorRoleNames is the
// vocabulary.
type Monitor struct {
	Refresh         int // seconds, 1..10
	SortBackground  string
	SortColor       string
	HoverBackground string
	HoverColor      string
	ShowApps        bool
	ShowProcesses   bool
}

func defaultMonitor() Monitor {
	return Monitor{Refresh: 1, SortBackground: "surface_variant", SortColor: "on_surface_variant",
		HoverBackground: "surface_variant", HoverColor: "on_surface_variant", ShowApps: true, ShowProcesses: true}
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
	Media         Media
	Monitor       Monitor
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
	"cpu": {}, "memory": {}, "temperature": {}, "gpu": {}, "filesystem": {}, "block": {}, "network": {},
	"weather": {}, "battery": {}, "notifications": {},
	"running-apps": {}, "wordmark": {},
	"launcher": {},
	// "wallpaper" opens the picker. It is deliberately not in Default(): a
	// user who wants the glyph adds it, and an existing bar does not change.
	"wallpaper": {},
	"volume":    {},
	// "wifi" is connectivity: the signal glyph that opens the network panel.
	// It is deliberately not "network", which is already bound above as the
	// throughput rate source with rx/tx directions. Naming it "network" would
	// have meant a configuration migration later.
	"wifi": {},
	// Bluetooth is opt-in: its bar glyph is useful when requested, but adding
	// it to the default would change existing layouts.
	"bluetooth": {},
	// Media is opt-in like Bluetooth: the glyph and title appear only while a
	// player is on the session bus, and the design carries no player picker
	// here — the control centre's Media page is the one picker.
	"media": {},
	// Clipboard is a daemon-backed metadata projection. Its bar glyph is
	// useful even when the per-user daemon is unavailable, so it ships in the
	// default layout and reports that state through its tooltip.
	"clipboard": {},
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
var fractionSources = map[string]bool{
	"cpu": true, "memory": true, "temperature": true, "gpu": true, "filesystem": true,
}

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
	maxWeatherLocationBytes = 80
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
	"top": true, "bottom": true, "left": false, "right": false,
}

// Default is the built-in configuration, used when no file exists.
func Default() Config {
	c := Config{
		Bar: Bar{
			Enabled: true, Edge: "top", Gap: 4,
			Left: []Item{
				{ID: "launcher"},
				{ID: "workspace"},
				{ID: "window-title", MaxWidth: defaultTitleMaxWidth},
			},
			// Time and date sit together, which is what each reference shell
			// does; the right section carries status widgets. Weather is not
			// here: it requires configured coordinates, so a default bar
			// carrying it would fail validation out of the box.
			Center: []Item{
				{ID: "group", Items: []Item{
					{ID: "clock", Format: defaultClockFormat, Boundary: time.Minute},
					{ID: "clock", Format: defaultDateFormat, Boundary: time.Minute},
				}},
				{ID: "wordmark"},
				{ID: "media"},
			},
			Right: []Item{
				{ID: "running-apps"},
				{ID: "group", Items: []Item{
					{ID: "cpu", Display: "radial", Interval: defaultMetricInterval},
					{ID: "memory", Display: "radial", Interval: defaultMetricInterval},
					{ID: "temperature", Display: "radial", Interval: defaultMetricInterval},
					{ID: "gpu", Display: "radial", Interval: defaultMetricInterval},
				}},
				{ID: "clipboard"},
				{ID: "notifications"},
			},
		},
		Theme: Theme{Preset: theme.PresetStandard, Composition: standardComposition()},
		ThemeGen: ThemeConfig{
			Source: "wallpaper",
			Scheme: "scheme-tonal-spot",
			Mode:   "dark",
		},
		Panels:  Panels{Gap: 0, Padding: 8, OSD: "bottom-center"},
		Monitor: defaultMonitor(),
		Wallpaper: Wallpaper{
			// Stills and video share one directory by default, which D9
			// allows: that is how the library on this machine is laid out, and
			// a split default would hide every video behind a second root.
			ImageDirectory: "~/Pictures/wallpapers",
			VideoDirectory: "~/Pictures/wallpapers",
			Scale:          "fill",
			Loop:           true,
			FPS:            30,
			FadeDuration:   0.5,
			Hidden:         "none",
		},
	}
	// The bar's geometry is derived, not written twice: height, padding,
	// spacing, radius, and text size all follow the resolved composition, and
	// an explicit bar block overrides them afterwards.
	c.Bar = deriveBar(c.Bar, c.Theme)
	return c
}

// standardComposition is the standard preset, which is also the composition a
// file with no preset key resolves to.
func standardComposition() theme.Composition {
	c, ok := theme.PresetComposition(theme.PresetStandard)
	if !ok {
		panic("config: the standard preset is missing")
	}
	return c
}

// deriveBar fills the bar geometry the composition owns. It leaves the bar's
// own identity -- whether it is enabled, its edge, its gap, and its items --
// untouched, because those are not theme axes.
func deriveBar(b Bar, t Theme) Bar {
	m := t.Metrics()
	b.Height = m.BarHeight
	b.Padding = m.BarPadding
	b.Spacing = m.BarSpacing
	b.Radius = t.Radius
	b.FontFamily = t.FontFamily
	b.FontSize = t.TextSize(theme.RoleBody)
	return b
}

// RebaseDerivedBar updates only bar fields that still followed the previous
// theme. Explicit bar overrides remain untouched when a settings edit changes
// the theme axes that own those fields.
func RebaseDerivedBar(c *Config, from Theme) {
	if c == nil {
		return
	}
	before := deriveBar(c.Bar, from)
	after := deriveBar(c.Bar, c.Theme)
	if c.Bar.Height == before.Height {
		c.Bar.Height = after.Height
	}
	if c.Bar.Padding == before.Padding {
		c.Bar.Padding = after.Padding
	}
	if c.Bar.Spacing == before.Spacing {
		c.Bar.Spacing = after.Spacing
	}
	if c.Bar.Radius == before.Radius {
		c.Bar.Radius = after.Radius
	}
	if c.Bar.FontFamily == before.FontFamily {
		c.Bar.FontFamily = after.FontFamily
	}
	if c.Bar.FontSize == before.FontSize {
		c.Bar.FontSize = after.FontSize
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

// KnownItemIDs lists the bar widget vocabulary in a stable order. The config
// vocabulary and the shell's widget builder are two lists that have to agree;
// exporting this lets a test in the shell assert they do, rather than the two
// drifting until an accepted item silently draws nothing.
func KnownItemIDs() []string {
	out := make([]string, 0, len(knownItems))
	for id := range knownItems {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
