package settings

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

type Kind uint8

const (
	KindBool Kind = iota
	KindInt
	KindEnum
	KindString
)

type Entry struct {
	Path     string
	Label    string
	Section  string
	Kind     Kind
	Options  []string
	Min, Max int
}

type Registry struct{ entries []Entry }

func Default() *Registry {
	r := &Registry{entries: []Entry{
		{Path: "bar.enabled", Label: "Enabled", Section: "Bar", Kind: KindBool},
		{Path: "bar.edge", Label: "Edge", Section: "Bar", Kind: KindEnum, Options: []string{"top", "bottom"}},
		{Path: "bar.height", Label: "Height", Section: "Bar", Kind: KindInt, Min: 24, Max: 64},
		{Path: "bar.gap", Label: "Gap", Section: "Bar", Kind: KindInt, Min: 0, Max: 32},
		{Path: "bar.padding", Label: "Padding", Section: "Bar", Kind: KindInt, Min: 0, Max: 32},
		{Path: "bar.spacing", Label: "Spacing", Section: "Bar", Kind: KindInt, Min: 0, Max: 32},
		{Path: "bar.radius", Label: "Radius", Section: "Bar", Kind: KindInt, Min: 0, Max: 32},
		{Path: "bar.font-family", Label: "Font family", Section: "Bar", Kind: KindString},
		{Path: "bar.font-size", Label: "Font size", Section: "Bar", Kind: KindInt, Min: 8, Max: 32},
		{Path: "appearance.source", Label: "Theme source", Section: "Appearance", Kind: KindEnum, Options: []string{"wallpaper", "hex", "stock"}},
		{Path: "appearance.seed", Label: "Seed", Section: "Appearance", Kind: KindString},
		{Path: "appearance.scheme", Label: "Scheme", Section: "Appearance", Kind: KindString},
		{Path: "appearance.mode", Label: "Mode", Section: "Appearance", Kind: KindEnum, Options: []string{"dark", "light"}},
		{Path: "panels.gap", Label: "Panel gap", Section: "Panels", Kind: KindInt, Min: 0, Max: 64},
		{Path: "panels.padding", Label: "Panel padding", Section: "Panels", Kind: KindInt, Min: 0, Max: 64},
		{Path: "panels.osd", Label: "OSD position", Section: "Panels", Kind: KindEnum, Options: []string{
			"top-left", "top-center", "top-right",
			"center-left", "center", "center-right",
			"bottom-left", "bottom-center", "bottom-right",
		}},
		{Path: "session.locker", Label: "Locker", Section: "Session", Kind: KindString},
		{Path: "accessibility.reduced-motion", Label: "Reduced motion", Section: "Accessibility", Kind: KindBool},
		{Path: "accessibility.high-contrast", Label: "High contrast", Section: "Accessibility", Kind: KindBool},
	}}
	r.addWidgetEntries(config.Default())
	return r
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
			})
		case "window-title":
			if seen["window-title.max-width"] {
				return
			}
			seen["window-title.max-width"] = true
			r.entries = append(r.entries, Entry{
				Path: "widgets.window-title.max-width", Label: "Title max width", Section: "Widgets",
				Kind: KindInt, Min: 40, Max: 800,
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

func (e Entry) Get(c config.Config) string {
	switch e.Path {
	case "bar.enabled":
		return strconv.FormatBool(c.Bar.Enabled)
	case "bar.edge":
		return c.Bar.Edge
	case "bar.height":
		return strconv.Itoa(c.Bar.Height)
	case "bar.gap":
		return strconv.Itoa(c.Bar.Gap)
	case "bar.padding":
		return strconv.Itoa(c.Bar.Padding)
	case "bar.spacing":
		return strconv.Itoa(c.Bar.Spacing)
	case "bar.radius":
		return strconv.Itoa(c.Bar.Radius)
	case "bar.font-family":
		return c.Bar.FontFamily
	case "bar.font-size":
		return strconv.Itoa(c.Bar.FontSize)
	case "appearance.source":
		return c.ThemeGen.Source
	case "appearance.seed":
		return c.ThemeGen.Seed
	case "appearance.scheme":
		return c.ThemeGen.Scheme
	case "appearance.mode":
		return c.ThemeGen.Mode
	case "panels.gap":
		return strconv.Itoa(c.Panels.Gap)
	case "panels.padding":
		return strconv.Itoa(c.Panels.Padding)
	case "panels.osd":
		return c.Panels.OSD
	case "session.locker":
		return c.Session.Locker
	case "accessibility.reduced-motion":
		return strconv.FormatBool(c.Accessibility.ReducedMotion)
	case "accessibility.high-contrast":
		return strconv.FormatBool(c.Accessibility.HighContrast)
	case "widgets.clock.format":
		if it := firstItem(c, "clock"); it != nil {
			return it.Format
		}
	case "widgets.window-title.max-width":
		if it := firstItem(c, "window-title"); it != nil {
			return strconv.Itoa(it.MaxWidth)
		}
	}
	return ""
}

func (e Entry) Set(c *config.Config, v string) error {
	if c == nil {
		return fmt.Errorf("settings: nil config")
	}
	switch e.Kind {
	case KindBool:
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("settings: %s: %q is not a boolean", e.Path, v)
		}
		return e.setBool(c, b)
	case KindInt:
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("settings: %s: %q is not an integer", e.Path, v)
		}
		if (e.Min != 0 || e.Max != 0) && (n < e.Min || n > e.Max) {
			return fmt.Errorf("settings: %s: %d is outside %d..%d", e.Path, n, e.Min, e.Max)
		}
		return e.setInt(c, n)
	case KindEnum:
		ok := false
		for _, o := range e.Options {
			if o == v {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("settings: %s: %q is not a valid option", e.Path, v)
		}
		return e.setString(c, v)
	case KindString:
		return e.setString(c, v)
	}
	return fmt.Errorf("settings: %s: unknown kind", e.Path)
}

func (e Entry) setBool(c *config.Config, b bool) error {
	switch e.Path {
	case "bar.enabled":
		c.Bar.Enabled = b
	case "accessibility.reduced-motion":
		c.Accessibility.ReducedMotion = b
	case "accessibility.high-contrast":
		c.Accessibility.HighContrast = b
	default:
		return fmt.Errorf("settings: %s: not a boolean", e.Path)
	}
	return nil
}

func (e Entry) setInt(c *config.Config, n int) error {
	switch e.Path {
	case "bar.height":
		c.Bar.Height = n
	case "bar.gap":
		c.Bar.Gap = n
	case "bar.padding":
		c.Bar.Padding = n
	case "bar.spacing":
		c.Bar.Spacing = n
	case "bar.radius":
		c.Bar.Radius = n
	case "bar.font-size":
		c.Bar.FontSize = n
	case "panels.gap":
		c.Panels.Gap = n
	case "panels.padding":
		c.Panels.Padding = n
	case "widgets.window-title.max-width":
		eachItem(c, "window-title", func(it *config.Item) { it.MaxWidth = n })
	default:
		return fmt.Errorf("settings: %s: not an integer", e.Path)
	}
	return nil
}

func (e Entry) setString(c *config.Config, v string) error {
	switch e.Path {
	case "bar.edge":
		c.Bar.Edge = v
	case "bar.font-family":
		c.Bar.FontFamily = v
	case "appearance.source":
		c.ThemeGen.Source = v
	case "appearance.seed":
		c.ThemeGen.Seed = v
	case "appearance.scheme":
		c.ThemeGen.Scheme = v
	case "appearance.mode":
		c.ThemeGen.Mode = v
	case "panels.osd":
		c.Panels.OSD = v
	case "session.locker":
		c.Session.Locker = v
	case "widgets.clock.format":
		eachItem(c, "clock", func(it *config.Item) { it.Format = v })
	default:
		return fmt.Errorf("settings: %s: not a string", e.Path)
	}
	return nil
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
