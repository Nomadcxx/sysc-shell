package settings

import (
	"fmt"
	"strconv"
	"strings"

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
			Path: "bar.enabled", Label: "Enabled", Section: "Bar", Kind: KindBool,
			Get: getBool(func(c config.Config) bool { return c.Bar.Enabled }),
			Set: setBool("bar.enabled", func(c *config.Config, b bool) { c.Bar.Enabled = b }),
		},
		{
			Path: "bar.edge", Label: "Edge", Section: "Bar", Kind: KindEnum,
			Options: barEdges,
			Get:     func(c config.Config) string { return c.Bar.Edge },
			Set:     setEnum("bar.edge", barEdges, func(c *config.Config, v string) { c.Bar.Edge = v }),
		},
		{
			Path: "bar.height", Label: "Height", Section: "Bar", Kind: KindInt, Min: 24, Max: 64,
			Get: getInt(func(c config.Config) int { return c.Bar.Height }),
			Set: setInt("bar.height", 24, 64, func(c *config.Config, n int) { c.Bar.Height = n }),
		},
		{
			Path: "bar.gap", Label: "Gap", Section: "Bar", Kind: KindInt, Min: 0, Max: 32,
			Get: getInt(func(c config.Config) int { return c.Bar.Gap }),
			Set: setInt("bar.gap", 0, 32, func(c *config.Config, n int) { c.Bar.Gap = n }),
		},
		{
			Path: "bar.padding", Label: "Padding", Section: "Bar", Kind: KindInt, Min: 0, Max: 32,
			Get: getInt(func(c config.Config) int { return c.Bar.Padding }),
			Set: setInt("bar.padding", 0, 32, func(c *config.Config, n int) { c.Bar.Padding = n }),
		},
		{
			Path: "bar.spacing", Label: "Spacing", Section: "Bar", Kind: KindInt, Min: 0, Max: 32,
			Get: getInt(func(c config.Config) int { return c.Bar.Spacing }),
			Set: setInt("bar.spacing", 0, 32, func(c *config.Config, n int) { c.Bar.Spacing = n }),
		},
		{
			Path: "bar.font-family", Label: "Font family", Section: "Bar", Kind: KindString,
			Get: func(c config.Config) string { return c.Bar.FontFamily },
			Set: setString(func(c *config.Config, v string) { c.Bar.FontFamily = v }),
		},
		{
			Path: "bar.font-size", Label: "Font size", Section: "Bar", Kind: KindInt, Min: 8, Max: 32,
			Get: getInt(func(c config.Config) int { return c.Bar.FontSize }),
			Set: setInt("bar.font-size", 8, 32, func(c *config.Config, n int) { c.Bar.FontSize = n }),
		},
		{
			Path: "bar.items.left", Label: "Left items", Section: "Bar", Kind: KindString,
			Get: func(c config.Config) string { return formatItemIDs(c.Bar.Left) },
			Set: setString(func(c *config.Config, v string) { c.Bar.Left = parseItemIDs(v, c.Bar.Left) }),
		},
		{
			Path: "bar.items.center", Label: "Center items", Section: "Bar", Kind: KindString,
			Get: func(c config.Config) string { return formatItemIDs(c.Bar.Center) },
			Set: setString(func(c *config.Config, v string) { c.Bar.Center = parseItemIDs(v, c.Bar.Center) }),
		},
		{
			Path: "bar.items.right", Label: "Right items", Section: "Bar", Kind: KindString,
			Get: func(c config.Config) string { return formatItemIDs(c.Bar.Right) },
			Set: setString(func(c *config.Config, v string) { c.Bar.Right = parseItemIDs(v, c.Bar.Right) }),
		},
		{
			Path: "appearance.source", Label: "Theme source", Section: "Appearance", Kind: KindEnum,
			Options: themeSources,
			Get:     func(c config.Config) string { return c.ThemeGen.Source },
			Set:     setEnum("appearance.source", themeSources, func(c *config.Config, v string) { c.ThemeGen.Source = v }),
		},
		seedEntry(cfg),
		// The palette entry writes the same field the seed does: with source
		// set to palette the seed names a scheme, and an enum is a kinder way
		// to pick one than typing it.
		{
			Path: "appearance.palette", Label: "Palette", Section: "Appearance", Kind: KindEnum,
			Options: theme.PaletteNames(),
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
			Path: "appearance.scheme", Label: "Scheme", Section: "Appearance", Kind: KindString,
			Get: func(c config.Config) string { return c.ThemeGen.Scheme },
			Set: setString(func(c *config.Config, v string) { c.ThemeGen.Scheme = v }),
		},
		{
			Path: "appearance.mode", Label: "Mode", Section: "Appearance", Kind: KindEnum,
			Options: themeModes,
			Get:     func(c config.Config) string { return c.ThemeGen.Mode },
			Set:     setEnum("appearance.mode", themeModes, func(c *config.Config, v string) { c.ThemeGen.Mode = v }),
		},
		// The D3 composition axes. Percent and weight fields go through the
		// integer control, so there is no float setting kind.
		{
			Path: "appearance.preset", Label: "Preset", Section: "Appearance", Kind: KindEnum,
			Options: presetNames,
			Get:     func(c config.Config) string { return string(c.Theme.Preset) },
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
			Path: "appearance.density", Label: "Density", Section: "Appearance", Kind: KindEnum,
			Options: densityNames,
			Get:     func(c config.Config) string { return string(c.Theme.Density) },
			Set: setEnum("appearance.density", densityNames,
				func(c *config.Config, v string) { c.Theme.Density = theme.Density(v) }),
		},
		{
			Path: "appearance.font-family", Label: "Font family", Section: "Appearance", Kind: KindString,
			Get: func(c config.Config) string { return c.Theme.FontFamily },
			Set: setString(func(c *config.Config, v string) { c.Theme.FontFamily = v }),
		},
		{
			Path: "appearance.mono-font-family", Label: "Mono font family", Section: "Appearance", Kind: KindString,
			Get: func(c config.Config) string { return c.Theme.MonoFontFamily },
			Set: setString(func(c *config.Config, v string) { c.Theme.MonoFontFamily = v }),
		},
		{
			Path: "appearance.font-scale", Label: "Font scale", Section: "Appearance", Kind: KindInt,
			Min: theme.FontScaleMin, Max: theme.FontScaleMax,
			Get: getInt(func(c config.Config) int { return c.Theme.FontScale }),
			Set: setInt("appearance.font-scale", theme.FontScaleMin, theme.FontScaleMax,
				func(c *config.Config, n int) { c.Theme.FontScale = n }),
		},
		{
			Path: "appearance.font-weight", Label: "Font weight", Section: "Appearance", Kind: KindInt,
			Min: theme.FontWeightMin, Max: theme.FontWeightMax,
			Get: getInt(func(c config.Config) int { return c.Theme.FontWeight }),
			Set: setInt("appearance.font-weight", theme.FontWeightMin, theme.FontWeightMax,
				func(c *config.Config, n int) { c.Theme.FontWeight = n }),
		},
		{
			Path: "appearance.radius", Label: "Radius", Section: "Appearance", Kind: KindInt,
			Min: theme.RadiusMin, Max: theme.RadiusMax,
			Get: getInt(func(c config.Config) int { return c.Theme.Radius }),
			// The bar keeps its own radius as a local override; this is the
			// composition axis every other surface derives from.
			Set: setInt("appearance.radius", theme.RadiusMin, theme.RadiusMax,
				func(c *config.Config, n int) { c.Theme.Radius = n }),
		},
		{
			Path: "appearance.motion", Label: "Motion", Section: "Appearance", Kind: KindEnum,
			Options: motionNames,
			Get:     func(c config.Config) string { return string(c.Theme.Motion) },
			Set: setEnum("appearance.motion", motionNames,
				func(c *config.Config, v string) { c.Theme.Motion = theme.MotionStyle(v) }),
		},
		{
			Path: "appearance.motion-speed", Label: "Motion speed", Section: "Appearance", Kind: KindInt,
			Min: theme.SpeedMin, Max: theme.SpeedMax,
			Get: getInt(func(c config.Config) int { return c.Theme.MotionSpeed }),
			Set: setInt("appearance.motion-speed", theme.SpeedMin, theme.SpeedMax,
				func(c *config.Config, n int) { c.Theme.MotionSpeed = n }),
		},
		{
			Path: "appearance.bar-opacity", Label: "Bar opacity", Section: "Appearance", Kind: KindInt,
			Min: theme.OpacityMin, Max: theme.OpacityMax,
			Get: getInt(func(c config.Config) int { return c.Theme.BarOpacity }),
			Set: setInt("appearance.bar-opacity", theme.OpacityMin, theme.OpacityMax,
				func(c *config.Config, n int) { c.Theme.BarOpacity = n }),
		},
		// Panels take the blurred floor so the axis can reach it at all; the
		// effective floor is still 80 unless a backdrop is present.
		{
			Path: "appearance.panel-opacity", Label: "Panel opacity", Section: "Appearance", Kind: KindInt,
			Min: theme.OpacityMinBlurred, Max: theme.OpacityMax,
			Get: getInt(func(c config.Config) int { return c.Theme.PanelOpacity }),
			Set: setInt("appearance.panel-opacity", theme.OpacityMinBlurred, theme.OpacityMax,
				func(c *config.Config, n int) { c.Theme.PanelOpacity = n }),
		},
		{
			Path: "appearance.overlay-opacity", Label: "Overlay opacity", Section: "Appearance", Kind: KindInt,
			Min: theme.OpacityMin, Max: theme.OpacityMax,
			Get: getInt(func(c config.Config) int { return c.Theme.OverlayOpacity }),
			Set: setInt("appearance.overlay-opacity", theme.OpacityMin, theme.OpacityMax,
				func(c *config.Config, n int) { c.Theme.OverlayOpacity = n }),
		},
		{
			Path: "appearance.blur-behind", Label: "Blur behind panels", Section: "Appearance", Kind: KindBool,
			Get: getBool(func(c config.Config) bool { return c.Theme.BlurBehind }),
			Set: setBool("appearance.blur-behind", func(c *config.Config, b bool) { c.Theme.BlurBehind = b }),
		},
		{
			Path: "appearance.blur-radius", Label: "Blur radius", Section: "Appearance", Kind: KindInt,
			Min: theme.BlurRadiusMin, Max: theme.BlurRadiusMax,
			Get: getInt(func(c config.Config) int { return c.Theme.BlurRadius }),
			Set: setInt("appearance.blur-radius", theme.BlurRadiusMin, theme.BlurRadiusMax,
				func(c *config.Config, n int) { c.Theme.BlurRadius = n }),
		},
		{
			Path: "appearance.elevation", Label: "Elevation", Section: "Appearance", Kind: KindEnum,
			Options: elevationNames,
			Get:     func(c config.Config) string { return string(c.Theme.Elevation) },
			Set: setEnum("appearance.elevation", elevationNames,
				func(c *config.Config, v string) { c.Theme.Elevation = theme.Elevation(v) }),
		},
		{
			Path: "panels.gap", Label: "Panel gap", Section: "Panels", Kind: KindInt, Min: 0, Max: 64,
			Get: getInt(func(c config.Config) int { return c.Panels.Gap }),
			Set: setInt("panels.gap", 0, 64, func(c *config.Config, n int) { c.Panels.Gap = n }),
		},
		{
			Path: "panels.padding", Label: "Panel padding", Section: "Panels", Kind: KindInt, Min: 0, Max: 64,
			Get: getInt(func(c config.Config) int { return c.Panels.Padding }),
			Set: setInt("panels.padding", 0, 64, func(c *config.Config, n int) { c.Panels.Padding = n }),
		},
		{
			Path: "panels.osd", Label: "OSD position", Section: "Panels", Kind: KindEnum,
			Options: osdPositions,
			Get:     func(c config.Config) string { return c.Panels.OSD },
			Set:     setEnum("panels.osd", osdPositions, func(c *config.Config, v string) { c.Panels.OSD = v }),
		},
		{
			Path: "session.locker", Label: "Locker", Section: "Session", Kind: KindString,
			Get: func(c config.Config) string { return c.Session.Locker },
			Set: setString(func(c *config.Config, v string) { c.Session.Locker = v }),
		},
		{
			Path: "accessibility.reduced-motion", Label: "Reduced motion", Section: "Accessibility", Kind: KindBool,
			Get: getBool(func(c config.Config) bool { return c.Accessibility.ReducedMotion }),
			Set: setBool("accessibility.reduced-motion",
				func(c *config.Config, b bool) { c.Accessibility.ReducedMotion = b }),
		},
		{
			Path: "accessibility.high-contrast", Label: "High contrast", Section: "Accessibility", Kind: KindBool,
			Get: getBool(func(c config.Config) bool { return c.Accessibility.HighContrast }),
			Set: setBool("accessibility.high-contrast",
				func(c *config.Config, b bool) { c.Accessibility.HighContrast = b }),
		},
	}}
	r.addWidgetEntries(cfg)
	r.addTemplateEntries()
	return r
}

// The closed vocabularies the enum entries offer. Each is named once because
// an entry's Options and its setter's membership check must not drift apart:
// a value the surface offers and the setter rejects is unreachable, and one
// the setter accepts and the surface hides is undiscoverable.
var (
	barEdges     = []string{"top", "bottom"}
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
	osdPositions = []string{
		"top-left", "top-center", "top-right",
		"center-left", "center", "center-right",
		"bottom-left", "bottom-center", "bottom-right",
	}
)

// seedEntry is built from the supplied configuration because what the seed
// means follows the source: under "stock" it names one of a closed set of
// bundled themes, so it is a picker rather than a free-text field.
func seedEntry(cfg config.Config) Entry {
	e := Entry{
		Path: "appearance.seed", Label: "Seed", Section: "Appearance", Kind: KindString,
		Get: func(c config.Config) string { return c.ThemeGen.Seed },
		Set: setString(func(c *config.Config, v string) { c.ThemeGen.Seed = v }),
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
			Path: "theme.templates." + name, Label: name + " theme", Section: "Appearance", Kind: KindBool,
			Get: getBool(func(c config.Config) bool { return c.TemplateEnabled(name) }),
			Set: setBool("theme.templates."+name, func(c *config.Config, b bool) {
				if c.Templates == nil {
					c.Templates = map[string]bool{}
				}
				c.Templates[name] = b
			}),
		})
	}
}

func (r *Registry) addWidgetEntries(cfg config.Config) {
	seen := map[string]bool{}
	add := func(it config.Item) {
		switch it.ID {
		case "clock":
			if seen["clock.format"] {
				return
			}
			seen["clock.format"] = true
			r.entries = append(r.entries, Entry{
				Path: "widgets.clock.format", Label: "Clock format", Section: "Widgets", Kind: KindString,
				Get: func(c config.Config) string {
					if it := firstItem(c, "clock"); it != nil {
						return it.Format
					}
					return ""
				},
				Set: setString(func(c *config.Config, v string) {
					eachItem(c, "clock", func(it *config.Item) { it.Format = v })
				}),
			})
		case "window-title":
			if seen["window-title.max-width"] {
				return
			}
			seen["window-title.max-width"] = true
			r.entries = append(r.entries, Entry{
				Path: "widgets.window-title.max-width", Label: "Title max width", Section: "Widgets",
				Kind: KindInt, Min: 40, Max: 800,
				Get: func(c config.Config) string {
					if it := firstItem(c, "window-title"); it != nil {
						return strconv.Itoa(it.MaxWidth)
					}
					return ""
				},
				Set: setInt("widgets.window-title.max-width", 40, 800, func(c *config.Config, n int) {
					eachItem(c, "window-title", func(it *config.Item) { it.MaxWidth = n })
				}),
			})
		}
	}
	for _, it := range cfg.Bar.Left {
		add(it)
	}
	for _, it := range cfg.Bar.Center {
		add(it)
	}
	for _, it := range cfg.Bar.Right {
		add(it)
	}
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
		if strings.Contains(strings.ToLower(e.Label), q) {
			out = append(out, e)
		}
	}
	return out
}

func formatItemIDs(items []config.Item) string {
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	return strings.Join(ids, ",")
}

func parseItemIDs(s string, prev []config.Item) []config.Item {
	used := make([]bool, len(prev))
	var out []config.Item
	for _, id := range strings.Split(s, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		it := config.Item{ID: id}
		for i, p := range prev {
			if used[i] || p.ID != id {
				continue
			}
			it = p
			used[i] = true
			break
		}
		out = append(out, it)
	}
	return out
}

func firstItem(c config.Config, id string) *config.Item {
	for _, it := range append(append(append([]config.Item{}, c.Bar.Left...), c.Bar.Center...), c.Bar.Right...) {
		if it.ID == id {
			item := it
			return &item
		}
	}
	return nil
}

func eachItem(c *config.Config, id string, fn func(*config.Item)) {
	walk := func(items []config.Item) {
		for i := range items {
			if items[i].ID == id {
				fn(&items[i])
			}
		}
	}
	walk(c.Bar.Left)
	walk(c.Bar.Center)
	walk(c.Bar.Right)
}
