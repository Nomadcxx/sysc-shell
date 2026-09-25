package settings

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

type Registry struct{ entries []Entry }

func Default() *Registry {
	return DefaultFor(config.Default())
}

func DefaultFor(cfg config.Config) *Registry {
	r := &Registry{entries: []Entry{
		{
			Path: "bar.enabled", Label: "Enabled", Section: "Bar", Group: "Surface",
			Describe: "Draw the bar on every output.",
			Kind:     KindBool,
			Get:      getBool(func(c config.Config) bool { return c.Bar.Enabled }),
			Set:      setBool("bar.enabled", func(c *config.Config, b bool) { c.Bar.Enabled = b }),
		},
		{
			Path: "bar.edge", Label: "Edge", Section: "Bar", Group: "Surface", Kind: KindEnum,
			Describe: "Which screen edge the bar anchors to.",
			Options:  barEdges,
			Get:      func(c config.Config) string { return c.Bar.Edge },
			Set:      setEnum("bar.edge", barEdges, func(c *config.Config, v string) { c.Bar.Edge = v }),
		},
		{
			Path: "bar.height", Label: "Height", Section: "Bar", Group: "Geometry",
			Describe: "Bar height in logical pixels. It follows the density ladder unless set here.",
			Kind:     KindInt, Min: 24, Max: 64,
			Get: getInt(func(c config.Config) int { return c.Bar.Height }),
			Set: setInt("bar.height", 24, 64, func(c *config.Config, n int) { c.Bar.Height = n }),
		},
		{
			Path: "bar.gap", Label: "Gap", Section: "Bar", Group: "Geometry",
			Describe: "Space between the bar and the screen edge.",
			Kind:     KindInt, Min: 0, Max: 32,
			Get: getInt(func(c config.Config) int { return c.Bar.Gap }),
			Set: setInt("bar.gap", 0, 32, func(c *config.Config, n int) { c.Bar.Gap = n }),
		},
		{
			Path: "bar.padding", Label: "Padding", Section: "Bar", Group: "Geometry",
			Describe: "Space inside the bar, before its first widget.",
			Kind:     KindInt, Min: 0, Max: 32,
			Get: getInt(func(c config.Config) int { return c.Bar.Padding }),
			Set: setInt("bar.padding", 0, 32, func(c *config.Config, n int) { c.Bar.Padding = n }),
		},
		{
			Path: "bar.spacing", Label: "Spacing", Section: "Bar", Group: "Geometry",
			Describe: "Space between neighbouring widgets.",
			Kind:     KindInt, Min: 0, Max: 32,
			Get: getInt(func(c config.Config) int { return c.Bar.Spacing }),
			Set: setInt("bar.spacing", 0, 32, func(c *config.Config, n int) { c.Bar.Spacing = n }),
		},
		{
			Path: "bar.font-family", Label: "Font family", Section: "Bar", Group: "Typography",
			Describe:   "Font the bar's widgets use. Empty follows the appearance font.",
			Kind:       KindFont,
			EmptyLabel: "Follow the appearance font",
			Get:        func(c config.Config) string { return c.Bar.FontFamily },
			Set:        setString(func(c *config.Config, v string) { c.Bar.FontFamily = v }),
		},
		{
			Path: "bar.font-size", Label: "Font size", Section: "Bar", Group: "Typography",
			Describe: "Text size in the bar.",
			Kind:     KindInt, Min: 8, Max: 32,
			Get: getInt(func(c config.Config) int { return c.Bar.FontSize }),
			Set: setInt("bar.font-size", 8, 32, func(c *config.Config, n int) { c.Bar.FontSize = n }),
		},
		{
			Path: "appearance.source", Label: "Theme source", Section: "Appearance", Group: "Palette",
			Describe: "Where the palette is seeded from.",
			Kind:     KindEnum, Options: themeSources,
			Get: func(c config.Config) string { return c.ThemeGen.Source },
			Set: setEnum("appearance.source", themeSources, func(c *config.Config, v string) {
				c.ThemeGen.Source = v
				c.ThemeGen.Seed = seedFor(v, c.ThemeGen.Seed)
			}),
		},
		seedEntry(cfg),
		// The palette entry writes the same field the seed does: with source
		// set to palette the seed names a scheme, and an enum is a kinder way
		// to pick one than typing it.
		{
			Path: "appearance.palette", Label: "Palette", Section: "Appearance", Group: "Palette",
			Describe: "A bundled palette. Choosing one also sets the source to palette.",
			Kind:     KindEnum,
			Options:  theme.PaletteNames(),
			Get: func(c config.Config) string {
				if c.ThemeGen.Source == "palette" {
					return c.ThemeGen.Seed
				}
				return ""
			},
			// Choosing a palette is choosing the source too; leaving the
			// source on wallpaper would silently ignore the choice.
			Set: setEnum("appearance.palette", theme.PaletteNames(), func(c *config.Config, v string) {
				c.ThemeGen.Source = "palette"
				c.ThemeGen.Seed = v
			}),
		},
		{
			Path: "appearance.scheme", Label: "Scheme", Section: "Appearance", Group: "Palette",
			Describe: "The Material scheme the palette is generated through.",
			Kind:     KindString,
			Get:      func(c config.Config) string { return c.ThemeGen.Scheme },
			Set:      setString(func(c *config.Config, v string) { c.ThemeGen.Scheme = v }),
		},
		{
			Path: "appearance.mode", Label: "Mode", Section: "Appearance", Group: "Palette",
			Describe: "Light or dark resolution of the same palette.",
			Kind:     KindEnum,
			Options:  themeModes,
			Get:      func(c config.Config) string { return c.ThemeGen.Mode },
			Set:      setEnum("appearance.mode", themeModes, func(c *config.Config, v string) { c.ThemeGen.Mode = v }),
		},
		// The D3 composition axes. Percent and weight fields go through the
		// integer control, so there is no float setting kind.
		{
			Path: "appearance.preset", Label: "Preset", Section: "Appearance", Group: "Composition",
			Describe: "The bundled composition the theme starts from. Every axis stays overridable.",
			Kind:     KindEnum,
			Options:  presetNames,
			Get:      func(c config.Config) string { return string(c.Theme.Preset) },
			// A preset reseeds every axis, so it goes through the rebase
			// helper rather than being written as one more field.
			Set: setEnum("appearance.preset", presetNames, func(c *config.Config, v string) {
				next := theme.Preset(v)
				from, okFrom := theme.PresetComposition(c.Theme.Preset)
				to, okTo := theme.PresetComposition(next)
				if okFrom && okTo {
					// Rebase carries the axes the user actually changed and
					// reseeds the ones still sitting on the old preset's value.
					c.Theme.Composition = theme.Rebase(c.Theme.Composition, from, to)
				}
				c.Theme.Preset = next
			}),
		},
		{
			Path: "appearance.density", Label: "Density", Section: "Appearance", Group: "Composition",
			Describe: "How much room controls take. Every surface derives from it.",
			Kind:     KindEnum,
			Options:  densityNames,
			Get:      func(c config.Config) string { return string(c.Theme.Density) },
			Set: setEnum("appearance.density", densityNames,
				func(c *config.Config, v string) { c.Theme.Density = theme.Density(v) }),
		},
		{
			Path: "appearance.font-family", Label: "Font family", Section: "Appearance", Group: "Typography",
			Describe: "Font for interface text.",
			Kind:     KindFont,
			Get:      func(c config.Config) string { return c.Theme.FontFamily },
			Set:      setString(func(c *config.Config, v string) { c.Theme.FontFamily = v }),
		},
		{
			Path: "appearance.mono-font-family", Label: "Mono font family", Section: "Appearance", Group: "Typography",
			Describe: "Font for fixed-width text.",
			Kind:     KindFont,
			Get:      func(c config.Config) string { return c.Theme.MonoFontFamily },
			Set:      setString(func(c *config.Config, v string) { c.Theme.MonoFontFamily = v }),
		},
		{
			Path: "appearance.font-scale", Label: "Font scale", Section: "Appearance", Group: "Typography",
			Describe: "Text size as a percentage of the preset's.",
			Kind:     KindInt,
			Min:      theme.FontScaleMin, Max: theme.FontScaleMax,
			Get: getInt(func(c config.Config) int { return c.Theme.FontScale }),
			Set: setInt("appearance.font-scale", theme.FontScaleMin, theme.FontScaleMax,
				func(c *config.Config, n int) { c.Theme.FontScale = n }),
		},
		{
			Path: "appearance.font-weight", Label: "Font weight", Section: "Appearance", Group: "Typography",
			Describe: "Stroke weight for interface text.",
			Kind:     KindInt,
			Min:      theme.FontWeightMin, Max: theme.FontWeightMax,
			Get: getInt(func(c config.Config) int { return c.Theme.FontWeight }),
			Set: setInt("appearance.font-weight", theme.FontWeightMin, theme.FontWeightMax,
				func(c *config.Config, n int) { c.Theme.FontWeight = n }),
		},
		{
			Path: "appearance.radius", Label: "Radius", Section: "Appearance", Group: "Shape",
			Describe: "Corner radius for panels and cards.",
			Kind:     KindInt,
			Min:      theme.RadiusMin, Max: theme.RadiusMax,
			Get: getInt(func(c config.Config) int { return c.Theme.Radius }),
			// The bar keeps its own radius as a local override; this is the
			// composition axis every other surface derives from.
			Set: setInt("appearance.radius", theme.RadiusMin, theme.RadiusMax,
				func(c *config.Config, n int) { c.Theme.Radius = n }),
		},
		{
			Path: "appearance.input-radius", Label: "Input radius", Section: "Appearance", Group: "Shape",
			Describe: "Corner radius for interactive elements: fields, switches and buttons.",
			Kind:     KindInt,
			Min:      theme.RadiusMin, Max: theme.RadiusMax,
			Get: getInt(func(c config.Config) int { return c.Theme.InputRadius }),
			// The parallel ladder to appearance.radius. Every preset seeds it
			// with that preset's Radius, so inputs keep their shape until
			// someone moves this axis on its own.
			Set: setInt("appearance.input-radius", theme.RadiusMin, theme.RadiusMax,
				func(c *config.Config, n int) { c.Theme.InputRadius = n }),
		},
		{
			Path: "appearance.motion", Label: "Motion", Section: "Appearance", Group: "Motion",
			Describe: "How animations move.",
			Kind:     KindEnum,
			Options:  motionNames,
			Get:      func(c config.Config) string { return string(c.Theme.Motion) },
			Set: setEnum("appearance.motion", motionNames,
				func(c *config.Config, v string) { c.Theme.Motion = theme.MotionStyle(v) }),
		},
		{
			Path: "appearance.motion-speed", Label: "Motion speed", Section: "Appearance", Group: "Motion",
			Describe: "Animation duration as a percentage of the preset's.",
			Kind:     KindInt,
			Min:      theme.SpeedMin, Max: theme.SpeedMax,
			Get: getInt(func(c config.Config) int { return c.Theme.MotionSpeed }),
			Set: setInt("appearance.motion-speed", theme.SpeedMin, theme.SpeedMax,
				func(c *config.Config, n int) { c.Theme.MotionSpeed = n }),
		},
		{
			Path: "appearance.bar-opacity", Label: "Bar opacity", Section: "Appearance", Group: "Opacity",
			Describe: "How opaque the bar is.",
			Kind:     KindInt,
			Min:      theme.OpacityMin, Max: theme.OpacityMax,
			Get: getInt(func(c config.Config) int { return c.Theme.BarOpacity }),
			Set: setInt("appearance.bar-opacity", theme.OpacityMin, theme.OpacityMax,
				func(c *config.Config, n int) { c.Theme.BarOpacity = n }),
		},
		// Panels take the blurred floor so the axis can reach it at all; the
		// effective floor is still 80 unless a backdrop is present.
		{
			Path: "appearance.panel-opacity", Label: "Panel opacity", Section: "Appearance", Group: "Opacity",
			Describe: "How opaque panels are. The lower floor applies only behind a blur.",
			Kind:     KindInt,
			Min:      theme.OpacityMinBlurred, Max: theme.OpacityMax,
			Get: getInt(func(c config.Config) int { return c.Theme.PanelOpacity }),
			Set: setInt("appearance.panel-opacity", theme.OpacityMinBlurred, theme.OpacityMax,
				func(c *config.Config, n int) { c.Theme.PanelOpacity = n }),
		},
		{
			Path: "appearance.overlay-opacity", Label: "Overlay opacity", Section: "Appearance", Group: "Opacity",
			Describe: "How opaque overlays and dialogues are.",
			Kind:     KindInt,
			Min:      theme.OpacityMin, Max: theme.OpacityMax,
			Get: getInt(func(c config.Config) int { return c.Theme.OverlayOpacity }),
			Set: setInt("appearance.overlay-opacity", theme.OpacityMin, theme.OpacityMax,
				func(c *config.Config, n int) { c.Theme.OverlayOpacity = n }),
		},
		{
			Path: "appearance.blur-behind", Label: "Blur behind panels", Section: "Appearance", Group: "Depth",
			Describe: "Blur what is behind a panel.",
			Kind:     KindBool,
			Get:      getBool(func(c config.Config) bool { return c.Theme.BlurBehind }),
			Set:      setBool("appearance.blur-behind", func(c *config.Config, b bool) { c.Theme.BlurBehind = b }),
		},
		{
			Path: "appearance.blur-radius", Label: "Blur radius", Section: "Appearance", Group: "Depth",
			Describe: "How strong that blur is.",
			Kind:     KindInt,
			Min:      theme.BlurRadiusMin, Max: theme.BlurRadiusMax,
			Get: getInt(func(c config.Config) int { return c.Theme.BlurRadius }),
			Set: setInt("appearance.blur-radius", theme.BlurRadiusMin, theme.BlurRadiusMax,
				func(c *config.Config, n int) { c.Theme.BlurRadius = n }),
		},
		{
			Path: "appearance.elevation", Label: "Elevation", Section: "Appearance", Group: "Depth",
			Describe: "How much shadow separates a surface from what is under it.",
			Kind:     KindEnum,
			Options:  elevationNames,
			Get:      func(c config.Config) string { return string(c.Theme.Elevation) },
			Set: setEnum("appearance.elevation", elevationNames,
				func(c *config.Config, v string) { c.Theme.Elevation = theme.Elevation(v) }),
		},
		{
			Path: "panels.gap", Label: "Panel gap", Section: "Panels", Group: "Placement",
			Describe: "Offset from the bar edge.",
			Kind:     KindInt, Min: 0, Max: 64,
			Get: getInt(func(c config.Config) int { return c.Panels.Gap }),
			Set: setInt("panels.gap", 0, 64, func(c *config.Config, n int) { c.Panels.Gap = n }),
		},
		{
			Path: "panels.padding", Label: "Panel padding", Section: "Panels", Group: "Placement",
			Describe: "Inset from the output edge, used when a panel is clamped.",
			Kind:     KindInt, Min: 0, Max: 64,
			Get: getInt(func(c config.Config) int { return c.Panels.Padding }),
			Set: setInt("panels.padding", 0, 64, func(c *config.Config, n int) { c.Panels.Padding = n }),
		},
		{
			Path: "panels.osd", Label: "OSD position", Section: "Panels", Group: "Placement",
			Describe: "Where the on-screen display appears.",
			Kind:     KindEnum,
			Options:  osdPositions,
			Get:      func(c config.Config) string { return c.Panels.OSD },
			Set:      setEnum("panels.osd", osdPositions, func(c *config.Config, v string) { c.Panels.OSD = v }),
		},
		{
			Path: "monitor.refresh", Label: "Refresh every", Section: "Monitor", Group: "Sampling",
			Describe: "Seconds between process and metric samples while the panel is open.",
			Kind:     KindInt, Min: 1, Max: 10,
			Get: getInt(func(c config.Config) int { return c.Monitor.Refresh }),
			Set: setInt("monitor.refresh", 1, 10, func(c *config.Config, n int) { c.Monitor.Refresh = n }),
		},
		monitorRoleEntry("monitor.sort-column-background", "Sort column background", "Fill behind the sorted column.",
			func(c *config.Config) *string { return &c.Monitor.SortBackground }),
		monitorRoleEntry("monitor.sort-column-color", "Sort column text", "Text colour in the sorted column.",
			func(c *config.Config) *string { return &c.Monitor.SortColor }),
		monitorRoleEntry("monitor.hover-background", "Hover background", "Fill behind the hovered row.",
			func(c *config.Config) *string { return &c.Monitor.HoverBackground }),
		monitorRoleEntry("monitor.hover-color", "Hover text", "Text colour in the hovered row.",
			func(c *config.Config) *string { return &c.Monitor.HoverColor }),
		{
			Path: "monitor.show-apps", Label: "Show applications", Section: "Monitor", Group: "Sections",
			Describe: "Group processes under their open application windows.",
			Kind:     KindBool,
			Get:      getBool(func(c config.Config) bool { return c.Monitor.ShowApps }),
			Set:      setBool("monitor.show-apps", func(c *config.Config, v bool) { c.Monitor.ShowApps = v }),
		},
		{
			Path: "monitor.show-processes", Label: "Show processes", Section: "Monitor", Group: "Sections",
			Describe: "List every process grouped by executable.",
			Kind:     KindBool,
			Get:      getBool(func(c config.Config) bool { return c.Monitor.ShowProcesses }),
			Set:      setBool("monitor.show-processes", func(c *config.Config, v bool) { c.Monitor.ShowProcesses = v }),
		},
		{
			Path: "session.locker", Label: "Locker", Section: "Session", Group: "Lock",
			Describe: "Command that locks the session. Empty hides the lock action.",
			Kind:     KindString,
			Get:      func(c config.Config) string { return c.Session.Locker },
			Set:      setString(func(c *config.Config, v string) { c.Session.Locker = v }),
		},
		{
			Path: "accessibility.reduced-motion", Label: "Reduced motion", Section: "Accessibility", Group: "Assistance",
			Describe: "Shorten or remove animation.",
			Kind:     KindBool,
			Get:      getBool(func(c config.Config) bool { return c.Accessibility.ReducedMotion }),
			Set: setBool("accessibility.reduced-motion",
				func(c *config.Config, b bool) { c.Accessibility.ReducedMotion = b }),
		},
		{
			Path: "accessibility.high-contrast", Label: "High contrast", Section: "Accessibility", Group: "Assistance",
			Describe: "Raise contrast between text and its background.",
			Kind:     KindBool,
			Get:      getBool(func(c config.Config) bool { return c.Accessibility.HighContrast }),
			Set: setBool("accessibility.high-contrast",
				func(c *config.Config, b bool) { c.Accessibility.HighContrast = b }),
		},
		{
			Path: "weather.city", Label: "City", Section: "Weather", Group: "Place", Kind: KindString,
			Describe: "Place name the forecast service geocodes. Setting it clears the coordinates.",
			Get:      func(c config.Config) string { return c.Weather.City },
			Set: setPlaceLabel("weather.city", func(c *config.Config, v string) {
				c.Weather.City = v
				if v != "" {
					// The writer emits a city or coordinates, never both, and
					// the loader refuses a block carrying the two. Leaving
					// stale coordinates behind would make this choice silently
					// lose to them on the next write.
					c.Weather.Latitude, c.Weather.Longitude = 0, 0
					c.Weather.Configured = true
				}
			}),
		},
		{
			Path: "weather.latitude", Label: "Latitude", Section: "Weather", Group: "Place", Kind: KindString,
			Describe: "Degrees north, -90 through 90. Setting it clears the city.",
			Get:      getFloat(func(c config.Config) float64 { return c.Weather.Latitude }),
			Set: setFloat("weather.latitude", -90, 90, func(c *config.Config, f float64) {
				c.Weather.Latitude = f
				c.Weather.City = ""
				c.Weather.Configured = true
			}),
		},
		{
			Path: "weather.longitude", Label: "Longitude", Section: "Weather", Group: "Place", Kind: KindString,
			Describe: "Degrees east, -180 through 180. Setting it clears the city.",
			Get:      getFloat(func(c config.Config) float64 { return c.Weather.Longitude }),
			Set: setFloat("weather.longitude", -180, 180, func(c *config.Config, f float64) {
				c.Weather.Longitude = f
				c.Weather.City = ""
				c.Weather.Configured = true
			}),
		},
		{
			Path: "weather.location", Label: "Label", Section: "Weather", Group: "Place", Kind: KindString,
			Describe: "Display name for the place. It never feeds the request; the coordinates do that.",
			Get:      func(c config.Config) string { return c.Weather.Location },
			Set:      setPlaceLabel("weather.location", func(c *config.Config, v string) { c.Weather.Location = v }),
		},
		{
			Path: "weather.unit", Label: "Unit", Section: "Weather", Group: "Forecast", Kind: KindEnum,
			Describe: "Temperature scale.",
			Options:  weatherUnits,
			// Weather is written only once a place is set, so an unconfigured
			// block reports the value the loader would supply rather than the
			// empty zero value, which no option matches.
			Get: func(c config.Config) string {
				if c.Weather.Unit == "" {
					return defaultWeatherUnit
				}
				return c.Weather.Unit
			},
			Set: setEnum("weather.unit", weatherUnits, func(c *config.Config, v string) { c.Weather.Unit = v }),
		},
		{
			Path: "weather.interval", Label: "Refresh every", Section: "Weather", Group: "Forecast", Kind: KindString,
			Describe: "How often the forecast is fetched, as a duration such as 15m.",
			Get: func(c config.Config) string {
				if c.Weather.Interval <= 0 {
					return defaultWeatherInterval.String()
				}
				return c.Weather.Interval.String()
			},
			Set: setDuration("weather.interval", func(c *config.Config, d time.Duration) { c.Weather.Interval = d }),
		},
		{
			Path: "wallpaper.image-directory", Label: "Image library", Section: "Wallpaper", Group: "Library",
			Describe: "Directory the picker scans for stills. A leading tilde is expanded when it is opened.",
			Kind:     KindPath,
			Get:      func(c config.Config) string { return c.Wallpaper.ImageDirectory },
			Set: setFilledString("wallpaper.image-directory",
				func(c *config.Config, v string) { c.Wallpaper.ImageDirectory = v }),
		},
		{
			Path: "wallpaper.video-directory", Label: "Video library", Section: "Wallpaper", Group: "Library",
			Describe: "Directory the picker scans for video wallpapers.",
			Kind:     KindPath,
			Get:      func(c config.Config) string { return c.Wallpaper.VideoDirectory },
			Set: setFilledString("wallpaper.video-directory",
				func(c *config.Config, v string) { c.Wallpaper.VideoDirectory = v }),
		},
		{
			Path: "wallpaper.scale", Label: "Scaling", Section: "Wallpaper", Group: "Playback", Kind: KindEnum,
			Describe: "How a wallpaper fills an output.",
			Options:  wallpaperScales,
			Get:      func(c config.Config) string { return c.Wallpaper.Scale },
			Set:      setEnum("wallpaper.scale", wallpaperScales, func(c *config.Config, v string) { c.Wallpaper.Scale = v }),
		},
		{
			Path: "wallpaper.loop", Label: "Loop video", Section: "Wallpaper", Group: "Playback", Kind: KindBool,
			Describe: "Restart a video wallpaper when it reaches its end.",
			Get:      getBool(func(c config.Config) bool { return c.Wallpaper.Loop }),
			Set:      setBool("wallpaper.loop", func(c *config.Config, b bool) { c.Wallpaper.Loop = b }),
		},
		{
			Path: "wallpaper.fps", Label: "Frame cap", Section: "Wallpaper", Group: "Playback", Kind: KindEnum,
			Describe: "Frames per second the playback engine is held to.",
			Options:  wallpaperFPS,
			Get:      getInt(func(c config.Config) int { return c.Wallpaper.FPS }),
			Set: setEnum("wallpaper.fps", wallpaperFPS, func(c *config.Config, v string) {
				n, _ := strconv.Atoi(v)
				c.Wallpaper.FPS = n
			}),
		},
		{
			Path: "wallpaper.fade", Label: "Cross-fade", Section: "Wallpaper", Group: "Transition", Kind: KindBool,
			Describe: "Fade between wallpapers instead of cutting.",
			Get:      getBool(func(c config.Config) bool { return c.Wallpaper.Fade }),
			Set:      setBool("wallpaper.fade", func(c *config.Config, b bool) { c.Wallpaper.Fade = b }),
		},
		{
			Path: "wallpaper.fade-duration", Label: "Fade seconds", Section: "Wallpaper", Group: "Transition",
			Describe: "How long a cross-fade takes, in seconds.",
			Kind:     KindString,
			Get:      getFloat(func(c config.Config) float64 { return c.Wallpaper.FadeDuration }),
			Set: setFloat("wallpaper.fade-duration", 0, maxFadeSeconds,
				func(c *config.Config, f float64) { c.Wallpaper.FadeDuration = f }),
		},
		{
			Path: "wallpaper.hidden", Label: "When occluded", Section: "Wallpaper", Group: "Playback", Kind: KindEnum,
			Describe: "What the playback engine does while the wallpaper cannot be seen.",
			Options:  wallpaperHidden,
			Get:      func(c config.Config) string { return c.Wallpaper.Hidden },
			Set:      setEnum("wallpaper.hidden", wallpaperHidden, func(c *config.Config, v string) { c.Wallpaper.Hidden = v }),
		},
	}}
	r.addWidgetEntries(cfg)
	r.addTemplateEntries()
	r.addTrayEntries(cfg)
	r.addOutputEntries(cfg)
	r.resolveDefaults()
	return r
}

// presetAxisPaths names the entries that write a theme.Composition axis.
//
// They are called out because config.Write treats them differently from every
// other setting: themeDiff bases each one against the selected preset, while
// barDiff, panelsDiff, accessibilityDiff and wallpaperDiff base against
// Default(). Reset follows the writer, so an axis missing from this set would
// reset to a value the writer then records as a deviation.
func presetAxisPaths() []string {
	return []string{
		"appearance.density", "appearance.font-family", "appearance.mono-font-family",
		"appearance.font-scale", "appearance.font-weight", "appearance.radius",
		"appearance.motion", "appearance.motion-speed", "appearance.bar-opacity",
		"appearance.panel-opacity", "appearance.overlay-opacity", "appearance.blur-behind",
		"appearance.blur-radius", "appearance.elevation",
	}
}

// resolveDefaults gives every entry the rule that resolves its default. Both
// rules read through the entry's own Get against a configuration standing in
// for "untouched", so a setting still declares its field exactly once.
func (r *Registry) resolveDefaults() {
	axis := make(map[string]bool, len(presetAxisPaths()))
	for _, path := range presetAxisPaths() {
		axis[path] = true
	}
	for i := range r.entries {
		e := &r.entries[i]
		if e.Default != nil || e.Get == nil {
			continue
		}
		get := e.Get
		if axis[e.Path] {
			e.Default = func(c config.Config) string { return get(presetBaseline(c)) }
			continue
		}
		e.Default = func(config.Config) string { return get(config.Default()) }
	}
}

// presetBaseline is the configuration an axis would hold had the user never
// touched it: the selected preset's composition, falling back to the standard
// one for a preset the theme package does not know, which is what themeDiff
// does when it decides whether to record an axis at all.
func presetBaseline(c config.Config) config.Config {
	base := config.Default()
	base.Theme.Preset = c.Theme.Preset
	comp, ok := theme.PresetComposition(c.Theme.Preset)
	if !ok {
		comp, _ = theme.PresetComposition(theme.PresetStandard)
	}
	base.Theme.Composition = comp
	return base
}

// SectionNames is the information architecture in rail order. The pane walks
// it, so a section absent here is a section the user cannot reach however many
// entries name it.
func SectionNames() []string {
	return []string{
		"Appearance", "Templates", "Bar", "Widgets", "Panels", "Monitor", "Wallpaper",
		"Weather", "Displays", "Tray", "Plugins", "Session", "Accessibility",
	}
}

// The closed vocabularies the enum entries offer. Each is named once because
// an entry's Options and its setter's membership check must not drift apart:
// a value the surface offers and the setter rejects is unreachable, and one
// the setter accepts and the surface hides is undiscoverable.
var (
	// The loader accepts one edge today and names the rest unsupported, so
	// offering them would hand the user a control that writes a file the shell
	// then declines to start from. The remaining edges arrive with the bar
	// geometry work, which is what makes them real.
	barEdges     = []string{"top"}
	themeSources = []string{"wallpaper", "hex", "stock", "palette"}
	themeModes   = []string{"dark", "light"}
	presetNames  = []string{
		string(theme.PresetStandard), string(theme.PresetCompact), string(theme.PresetExpressive),
	}
	densityNames = []string{
		string(theme.DensityMini), string(theme.DensityCompact), string(theme.DensityDefault),
		string(theme.DensityComfortable), string(theme.DensitySpacious),
	}
	motionNames    = []string{string(theme.MotionStandard), string(theme.MotionExpressive)}
	elevationNames = []string{
		string(theme.ElevationNone), string(theme.ElevationSubtle), string(theme.ElevationStandard),
	}
	weatherUnits    = []string{"celsius", "fahrenheit"}
	wallpaperScales = []string{"fill", "stretch", "original", "panscan"}
	wallpaperFPS    = []string{"30", "60", "100"}
	wallpaperHidden = []string{"none", "auto-pause", "auto-stop"}
	osdPositions    = []string{
		"top-left", "top-center", "top-right",
		"center-left", "center", "center-right",
		"bottom-left", "bottom-center", "bottom-right",
	}
)

const (
	// These mirror rules the loader owns and does not export. A copy that
	// drifts writes a file the shell then refuses to load, which is why
	// TestSettingsWriteBackThroughTheLoader round-trips every one of them.
	maxPlaceLabelBytes     = 80
	defaultWeatherUnit     = "celsius"
	defaultWeatherInterval = 15 * time.Minute
	// maxFadeSeconds is this package's own ceiling. The loader refuses only a
	// negative fade, but an unbounded field on a slider has no travel and a
	// minute-long cross-fade is not a setting anyone wants by accident.
	maxFadeSeconds = 10
)

// validHex asks the loader's own rule rather than restating it. A setter that
// writes a colour and the field that marks one valid as it is typed must agree
// with what config.Load will accept, or a value marks itself good and is then
// refused by the very write it was typed for.
func validHex(v string) bool { return config.ValidColor(v) }

// seedFor keeps the seed readable by the source about to read it.
//
// The loader validates the seed through the source, so changing one without
// the other wrote a file the shell then refused to load. That is why a stock
// theme could not be chosen from settings at all: the picker was not missing,
// the choice it made was unloadable.
func seedFor(source, seed string) string {
	switch source {
	case "stock":
		if _, ok := theme.StockSeed(seed); ok {
			return seed
		}
		if names := theme.StockNames(); len(names) > 0 {
			return names[0]
		}
	case "hex":
		if validHex(seed) {
			return seed
		}
		// A stock name stands for a hex, so coming from a stock theme keeps
		// the colour the user was already looking at.
		if hex, ok := theme.StockSeed(seed); ok {
			return hex
		}
		if names := theme.StockNames(); len(names) > 0 {
			if hex, ok := theme.StockSeed(names[0]); ok {
				return hex
			}
		}
	case "palette":
		if slices.Contains(theme.PaletteNames(), seed) {
			return seed
		}
		if names := theme.PaletteNames(); len(names) > 0 {
			return names[0]
		}
	case "wallpaper":
		// The seed is an image path here, which configuration does not
		// validate, and an empty one means the wallpaper in use. A leftover
		// stock name, palette name or colour would be read as a filename.
		if _, ok := theme.StockSeed(seed); ok {
			return ""
		}
		if slices.Contains(theme.PaletteNames(), seed) || validHex(seed) {
			return ""
		}
	}
	return seed
}

// seedEntry is built from the supplied configuration because what the seed
// means follows the source: under "stock" it names one of a closed set of
// bundled themes, so it is a picker rather than a free-text field.
func seedEntry(cfg config.Config) Entry {
	e := Entry{
		Path: "appearance.seed", Label: "Seed", Section: "Appearance", Group: "Palette",
		Describe: "What the source reads: an image path, a colour, a stock theme, or a palette name.",
		Kind:     KindString,
		Get:      func(c config.Config) string { return c.ThemeGen.Seed },
		Set:      setString(func(c *config.Config, v string) { c.ThemeGen.Seed = v }),
	}
	if cfg.ThemeGen.Source == "hex" {
		e.Kind = KindHex
		e.Set = write(func(c *config.Config, v string) error {
			if !validHex(strings.TrimSpace(v)) {
				return fmt.Errorf("settings: appearance.seed: %q is not #RRGGBB or #RRGGBBAA", v)
			}
			c.ThemeGen.Seed = strings.TrimSpace(v)
			return nil
		})
		return e
	}
	if cfg.ThemeGen.Source == "stock" {
		names := theme.StockNames()
		e.Kind = KindEnum
		e.Options = names
		e.Set = setEnum("appearance.seed", names, func(c *config.Config, v string) { c.ThemeGen.Seed = v })
	}
	return e
}

func (r *Registry) addTemplateEntries() {
	for _, name := range []string{
		"alacritty", "foot", "ghostty", "kitty", "wezterm", "niri",
		"gtk3", "gtk4", "qt", "kcolorscheme", "emacs", "helix",
		"btop", "cava", "starship", "scroll",
	} {
		r.entries = append(r.entries, Entry{
			Path: "theme.templates." + name, Label: name, Section: "Templates", Group: "Applications", Kind: KindBool,
			Describe: "Write this application's colours when the theme changes.",
			Get:      getBool(func(c config.Config) bool { return c.TemplateEnabled(name) }),
			Set: setBool("theme.templates."+name, func(c *config.Config, b bool) {
				if c.Templates == nil {
					c.Templates = map[string]bool{}
				}
				c.Templates[name] = b
			}),
		})
	}
}

// addWidgetEntries gives every option-bearing widget on the bar its own rows,
// addressed by position rather than by widget id.
//
// The entries used to be one per type, reading the first widget of that type
// and writing through eachItem to all of them, so two clocks could not take
// different formats from any interface (D4). An entry now names one item, and
// writing through it mints that item's instance id, because an option change
// is one of the three things D3 counts as addressing a widget.
func (r *Registry) addWidgetEntries(cfg config.Config) {
	for _, ref := range config.BarItemRefs(cfg.Bar) {
		it := cfg.Bar.ItemAt(ref)
		if it == nil {
			continue
		}
		r.entries = append(r.entries, widgetEntries(cfg, ref, *it)...)
	}
}

// widgetEntryGroup names the rows of one widget in the pane. Two widgets of a
// type have to be told apart, so the group carries the lane the widget sits in
// and, once it has one, its instance id.
func widgetEntryGroup(ref config.ItemRef, it config.Item) string {
	name := WidgetName(it)
	if it.Instance != "" {
		return name + " (" + it.Instance + ")"
	}
	return name + " (" + ref.Lane + " " + strconv.Itoa(ref.Path.Index+1) + ")"
}

// widgetEntryPath addresses the item rather than the widget type, so two
// clocks produce two distinct paths.
func widgetEntryPath(ref config.ItemRef, option string) string {
	path := "widgets." + ref.Lane + "." + strconv.Itoa(ref.Path.Index)
	if ref.Path.Member >= 0 {
		path += "." + strconv.Itoa(ref.Path.Member)
	}
	return path + "." + option
}

// writeItem resolves the reference against the configuration being written,
// mints the item's id, and hands it to the assignment. A reference that no
// longer resolves is an error rather than a silent no-op: the item was removed
// underneath an open pane, and pretending the write happened would leave the
// surface showing a value nothing holds.
func writeItem(ref config.ItemRef, assign func(*config.Item) error) Setter {
	return write(func(c *config.Config, _ string) error {
		it := c.Bar.ItemAt(ref)
		if it == nil {
			return fmt.Errorf("settings: %s no longer names a widget", ref.Lane)
		}
		config.NewMinter(*c).Ensure(it)
		return assign(it)
	})
}

// WidgetEntriesFor synthesises the option rows for one specific item at
// runtime. It is the seam sub-project A was built for: entries carry their own
// typed accessors, so a setting can be constructed over an arbitrary
// config.Item, which the retired Get/Set switch pair could never express --
// a switch can only name a setting by a path known at compile time.
func WidgetEntriesFor(cfg config.Config, ref config.ItemRef, it config.Item) []Entry {
	return widgetEntries(cfg, ref, it)
}

func widgetEntries(cfg config.Config, ref config.ItemRef, it config.Item) []Entry {
	group := widgetEntryGroup(ref, it)
	read := func(get func(config.Item) string) Getter {
		return func(c config.Config) string {
			if cur := c.Bar.ItemAt(ref); cur != nil {
				return get(*cur)
			}
			return ""
		}
	}
	switch it.ID {
	case "clock":
		return []Entry{{
			Path: widgetEntryPath(ref, "format"), Label: "Format",
			Section: "Widgets", Group: group,
			Describe: "Go layout string, such as 15:04 for a 24-hour clock.",
			Kind:     KindString,
			Get:      read(func(i config.Item) string { return i.Format }),
			Set: func(c *config.Config, v string) error {
				return writeItem(ref, func(i *config.Item) error {
					i.Format = v
					return nil
				})(c, v)
			},
		}}
	case "window-title":
		return []Entry{{
			Path: widgetEntryPath(ref, "max-width"), Label: "Maximum width",
			Section: "Widgets", Group: group,
			Describe: "Longest title in logical pixels before it is shortened.",
			Kind:     KindInt, Min: 40, Max: 800,
			Get: read(func(i config.Item) string { return strconv.Itoa(i.MaxWidth) }),
			Set: func(c *config.Config, v string) error {
				n, err := strconv.Atoi(strings.TrimSpace(v))
				if err != nil {
					return fmt.Errorf("settings: %s: %q is not a number", widgetEntryPath(ref, "max-width"), v)
				}
				if n < 40 || n > 800 {
					return fmt.Errorf("settings: %s: %d is outside 40 through 800",
						widgetEntryPath(ref, "max-width"), n)
				}
				return writeItem(ref, func(i *config.Item) error {
					i.MaxWidth = n
					return nil
				})(c, v)
			},
		}}
	}
	return nil
}

// write adapts a field assignment into a Setter. Every setter carries the same
// two obligations — refuse a nil configuration, and rebase the derived bar
// against the theme it had on entry — and they live here rather than in each
// entry so a new entry cannot forget either.
func write(assign func(*config.Config, string) error) Setter {
	return func(c *config.Config, v string) error {
		if c == nil {
			return fmt.Errorf("settings: nil config")
		}
		from := c.Theme
		if err := assign(c, v); err != nil {
			return err
		}
		config.RebaseDerivedBar(c, from)
		return nil
	}
}

// monitorRoleEntry is one monitor colour: a menu over the theme's role
// names, so the panel follows the palette rather than a fixed hex value.
func monitorRoleEntry(path, label, describe string, field func(*config.Config) *string) Entry {
	roles := theme.ColorRoleNames()
	return Entry{
		Path: path, Label: label, Describe: describe, Section: "Monitor", Group: "Colours",
		Kind: KindEnum, Options: roles,
		Get: func(c config.Config) string { return *field(&c) },
		Set: setEnum(path, roles, func(c *config.Config, v string) { *field(c) = v }),
	}
}

func getBool(read func(config.Config) bool) Getter {
	return func(c config.Config) string { return strconv.FormatBool(read(c)) }
}

func getInt(read func(config.Config) int) Getter {
	return func(c config.Config) string { return strconv.Itoa(read(c)) }
}

func setBool(path string, assign func(*config.Config, bool)) Setter {
	return write(func(c *config.Config, v string) error {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("settings: %s: %q is not a boolean", path, v)
		}
		assign(c, b)
		return nil
	})
}

func setInt(path string, min, max int, assign func(*config.Config, int)) Setter {
	return write(func(c *config.Config, v string) error {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("settings: %s: %q is not an integer", path, v)
		}
		if (min != 0 || max != 0) && (n < min || n > max) {
			return fmt.Errorf("settings: %s: %d is outside %d..%d", path, n, min, max)
		}
		assign(c, n)
		return nil
	})
}

func setEnum(path string, options []string, assign func(*config.Config, string)) Setter {
	return write(func(c *config.Config, v string) error {
		for _, o := range options {
			if o == v {
				assign(c, v)
				return nil
			}
		}
		return fmt.Errorf("settings: %s: %q is not a valid option", path, v)
	})
}

func setString(assign func(*config.Config, string)) Setter {
	return write(func(c *config.Config, v string) error {
		assign(c, v)
		return nil
	})
}

func (r *Registry) Register(entries ...Entry) {
	r.entries = append(r.entries, entries...)
}

func (r *Registry) Section(name string) []Entry {
	var out []Entry
	for _, e := range r.entries {
		if e.Section == name {
			out = append(out, e)
		}
	}
	return out
}

func (r *Registry) ByPath(path string) *Entry {
	for i := range r.entries {
		if r.entries[i].Path == path {
			return &r.entries[i]
		}
	}
	return nil
}

func (r *Registry) Search(q string) []Entry {
	q = strings.ToLower(q)
	if q == "" {
		return nil
	}
	var out []Entry
	for _, e := range r.entries {
		// The description is searched too. The word a user knows is often in
		// the explanation rather than in the label, so matching labels alone
		// hides the setting that would have answered them.
		if strings.Contains(strings.ToLower(e.Label), q) ||
			strings.Contains(strings.ToLower(e.Describe), q) {
			out = append(out, e)
		}
	}
	return out
}

// addTrayEntries renders one group per tray token the configuration already
// names, carrying the two toggles D9 ships. The tokens are not a free-form
// list editor: each is keyed by an item the tray host enumerates, and a token
// whose item is absent is kept and shown rather than quietly dropped.
func (r *Registry) addTrayEntries(cfg config.Config) {
	for _, token := range unionOf(cfg.Tray.Hidden, cfg.Tray.Pinned, cfg.Tray.Order) {
		r.entries = append(r.entries,
			Entry{
				Path: "tray." + token + ".hidden", Label: "Hidden", Section: "Tray", Group: token,
				Describe: "Keep this item out of the tray.", Kind: KindBool,
				Get: getBool(func(c config.Config) bool { return slices.Contains(c.Tray.Hidden, token) }),
				Set: setBool("tray."+token+".hidden", func(c *config.Config, b bool) {
					c.Tray.Hidden = toggleToken(c.Tray.Hidden, token, b)
				}),
			},
			Entry{
				Path: "tray." + token + ".pinned", Label: "Pinned", Section: "Tray", Group: token,
				Describe: "Hold this item in the bar rather than the drawer.", Kind: KindBool,
				Get: getBool(func(c config.Config) bool { return slices.Contains(c.Tray.Pinned, token) }),
				Set: setBool("tray."+token+".pinned", func(c *config.Config, b bool) {
					c.Tray.Pinned = toggleToken(c.Tray.Pinned, token, b)
				}),
			},
		)
	}
}

// addOutputEntries exposes the per-connector bar overrides that have been
// modelled since the reload work and reachable from nothing. It reads the
// overrides the configuration already carries: creating one for a connector
// that has none is per-output geometry, which sub-project C owns.
func (r *Registry) addOutputEntries(cfg config.Config) {
	for i := range cfg.Outputs {
		conn := cfg.Outputs[i].Connector
		override := func(c *config.Config) *config.Bar {
			for j := range c.Outputs {
				if c.Outputs[j].Connector == conn {
					return &c.Outputs[j].Bar
				}
			}
			return nil
		}
		read := func(c config.Config, get func(config.Bar) int) int {
			for _, o := range c.Outputs {
				if o.Connector == conn {
					return get(o.Bar)
				}
			}
			return 0
		}
		r.entries = append(r.entries,
			Entry{
				Path: "outputs." + conn + ".enabled", Label: "Enabled", Section: "Displays", Group: conn,
				Describe: "Draw the bar on this output.", Kind: KindBool,
				Get: getBool(func(c config.Config) bool {
					return read(c, func(b config.Bar) int {
						if b.Enabled {
							return 1
						}
						return 0
					}) == 1
				}),
				Set: setBool("outputs."+conn+".enabled", func(c *config.Config, v bool) {
					if b := override(c); b != nil {
						b.Enabled = v
					}
				}),
			},
			Entry{
				Path: "outputs." + conn + ".height", Label: "Height", Section: "Displays", Group: conn,
				Describe: "Bar height on this output.", Kind: KindInt, Min: 24, Max: 64,
				Get: getInt(func(c config.Config) int { return read(c, func(b config.Bar) int { return b.Height }) }),
				Set: setInt("outputs."+conn+".height", 24, 64, func(c *config.Config, n int) {
					if b := override(c); b != nil {
						b.Height = n
					}
				}),
			},
			Entry{
				Path: "outputs." + conn + ".font-size", Label: "Font size", Section: "Displays", Group: conn,
				Describe: "Bar text size on this output.", Kind: KindInt, Min: 8, Max: 32,
				Get: getInt(func(c config.Config) int { return read(c, func(b config.Bar) int { return b.FontSize }) }),
				Set: setInt("outputs."+conn+".font-size", 8, 32, func(c *config.Config, n int) {
					if b := override(c); b != nil {
						b.FontSize = n
					}
				}),
			},
		)
	}
}

// unionOf is the deduplicated, ordered merge the discovered-item sections are
// keyed on. Order is alphabetical because the lists it reads are ordered by
// how the user happened to add things, and a row that moves under the cursor
// is worse than one in an arbitrary but stable place.
func unionOf(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range lists {
		for _, v := range list {
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// toggleToken adds or removes one token, preserving the order of the rest.
// The loader refuses a repeated token, so adding is conditional.
func toggleToken(list []string, token string, on bool) []string {
	if on {
		if slices.Contains(list, token) {
			return list
		}
		return append(append([]string(nil), list...), token)
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		if v != token {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func getFloat(read func(config.Config) float64) Getter {
	return func(c config.Config) string { return strconv.FormatFloat(read(c), 'f', -1, 64) }
}

func setFloat(path string, min, max float64, assign func(*config.Config, float64)) Setter {
	return write(func(c *config.Config, v string) error {
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return fmt.Errorf("settings: %s: %q is not a number", path, v)
		}
		if f < min || f > max {
			return fmt.Errorf("settings: %s: %v is outside %v..%v", path, f, min, max)
		}
		assign(c, f)
		return nil
	})
}

func setDuration(path string, assign func(*config.Config, time.Duration)) Setter {
	return write(func(c *config.Config, v string) error {
		d, err := time.ParseDuration(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("settings: %s: %q is not a duration such as 15m", path, v)
		}
		if d <= 0 {
			return fmt.Errorf("settings: %s: %v is not positive", path, d)
		}
		assign(c, d)
		return nil
	})
}

// setFilledString refuses the empty string, for the fields the loader requires
// to carry something.
func setFilledString(path string, assign func(*config.Config, string)) Setter {
	return write(func(c *config.Config, v string) error {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("settings: %s: cannot be empty", path)
		}
		assign(c, v)
		return nil
	})
}

// setPlaceLabel mirrors the loader's rule for a free-text place name: bounded,
// and free of control characters so a stray newline cannot reach a surface.
func setPlaceLabel(path string, assign func(*config.Config, string)) Setter {
	return write(func(c *config.Config, v string) error {
		if len(v) > maxPlaceLabelBytes {
			return fmt.Errorf("settings: %s: is %d bytes, over the %d-byte limit", path, len(v), maxPlaceLabelBytes)
		}
		for _, r := range v {
			if unicode.IsControl(r) {
				return fmt.Errorf("settings: %s: carries a control character", path)
			}
		}
		assign(c, v)
		return nil
	})
}
