package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// atomicReplace is os.Rename except in tests that force a failure after the
// temp file exists.
var atomicReplace = os.Rename

// Write encodes c as the sparse wire document (only fields that differ from
// Default) and replaces path atomically: unique temp in the destination
// directory, mode 0600, file and directory sync, then rename. A deferred
// cleanup removes the temp on every error. Rename replaces a symlink at path
// rather than following it.
func Write(path string, c Config) error {
	if path == "" {
		return fmt.Errorf("config: empty write path")
	}
	if _, err := applyTrayPreferences(wireTrayPreferences{
		Hidden: c.Tray.Hidden, Pinned: c.Tray.Pinned, Order: c.Tray.Order,
	}); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config: mkdir %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("config: create temp: %w", err)
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("config: chmod temp: %w", err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(toWire(c)); err != nil {
		_ = f.Close()
		return fmt.Errorf("config: encode: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("config: sync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("config: close temp: %w", err)
	}
	if err := atomicReplace(tmp, path); err != nil {
		return fmt.Errorf("config: replace %s: %w", path, err)
	}
	ok = true
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func toWire(c Config) wireConfig {
	d := Default()
	var w wireConfig
	// The base bar is written against the bar its own resolved theme derives,
	// not against the default bar: a compact preset moves the derived height
	// to 40, and recording that as an explicit override would pin the bar
	// there for every later preset change.
	if bar := barDiff(c.Bar, deriveBar(d.Bar, c.Theme)); bar != nil {
		w.Bar = bar
	}
	if t := themeDiff(c.Theme, d.Theme.Preset); t != nil {
		w.Theme = t
	}
	if tg := themeGenDiff(c.ThemeGen, d.ThemeGen); tg != nil {
		w.ThemeGen = tg
	}
	if acc := accessibilityDiff(c.Accessibility, d.Accessibility); acc != nil {
		w.Accessibility = acc
	}
	if c.Session.Locker != d.Session.Locker {
		v := c.Session.Locker
		w.Session = &wireSession{Locker: &v}
	}
	if p := wallpaperDiff(c.Wallpaper, d.Wallpaper); p != nil {
		w.Wallpaper = p
	}
	if p := panelsDiff(c.Panels, d.Panels); p != nil {
		w.Panels = p
	}
	if len(c.Tray.Hidden) > 0 || len(c.Tray.Pinned) > 0 || len(c.Tray.Order) > 0 {
		w.Tray = &wireTrayPreferences{
			Hidden: append([]string(nil), c.Tray.Hidden...),
			Pinned: append([]string(nil), c.Tray.Pinned...),
			Order:  append([]string(nil), c.Tray.Order...),
		}
	}
	if c.Weather.Configured {
		w.Weather = weatherWire(c.Weather)
	}
	for i := range c.Outputs {
		conn := c.Outputs[i].Connector
		out := wireOutput{Connector: &conn}
		if bar := barDiff(c.Outputs[i].Bar, c.Bar); bar != nil {
			out.Bar = bar
		}
		w.Outputs = append(w.Outputs, out)
	}
	if len(c.Templates) > 0 {
		w.Templates = c.Templates
	}
	w.Plugins = pluginsDiff(c.Plugins)
	return w
}

func barDiff(got, base Bar) *wireBar {
	var w wireBar
	set := false
	if got.Enabled != base.Enabled {
		v := got.Enabled
		w.Enabled = &v
		set = true
	}
	if got.Edge != base.Edge {
		v := got.Edge
		w.Edge = &v
		set = true
	}
	if got.Height != base.Height {
		v := got.Height
		w.Height = &v
		set = true
	}
	if got.Gap != base.Gap {
		v := got.Gap
		w.Gap = &v
		set = true
	}
	if got.Padding != base.Padding {
		v := got.Padding
		w.Padding = &v
		set = true
	}
	if got.Spacing != base.Spacing {
		v := got.Spacing
		w.Spacing = &v
		set = true
	}
	if got.FontFamily != base.FontFamily || got.FontSize != base.FontSize {
		f := wireFont{}
		if got.FontFamily != base.FontFamily {
			v := got.FontFamily
			f.Family = &v
		}
		if got.FontSize != base.FontSize {
			v := got.FontSize
			f.Size = &v
		}
		w.Font = &f
		set = true
	}
	if !itemsEqual(got.Left, base.Left) || !itemsEqual(got.Center, base.Center) || !itemsEqual(got.Right, base.Right) {
		left := encodeItems(got.Left)
		center := encodeItems(got.Center)
		right := encodeItems(got.Right)
		w.Items = &wireItems{Left: &left, Center: &center, Right: &right}
		set = true
	}
	if !set {
		return nil
	}
	return &w
}

func itemsEqual(a, b []Item) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		x.Boundary, y.Boundary = 0, 0
		// An Item carries group members, so it is no longer comparable with
		// ==. Members are compared recursively and then cleared so the
		// remaining scalar fields compare as before.
		if !itemsEqual(x.Items, y.Items) {
			return false
		}
		x.Items, y.Items = nil, nil
		if !reflect.DeepEqual(x, y) {
			return false
		}
	}
	return true
}

func encodeItems(items []Item) []wireItem {
	out := make([]wireItem, len(items))
	for i, it := range items {
		out[i] = encodeItem(it)
	}
	return out
}

func encodeItem(it Item) wireItem {
	w := wireItem{ID: it.ID}
	switch it.ID {
	case "plugin":
		plugin, entry, instance := it.Plugin, it.Entry, it.Instance
		w.Plugin, w.Entry, w.Instance = &plugin, &entry, &instance
	case "group":
		if len(it.Items) > 0 {
			nested := encodeItems(it.Items)
			w.Items = &nested
		}
	case "clock":
		if it.Format != "" {
			v := it.Format
			w.Format = &v
		}
	case "window-title", "weather":
		if it.MaxWidth > 0 {
			v := it.MaxWidth
			w.MaxWidth = &v
		}
		if it.ID == "weather" && it.ShowCondition {
			v := true
			w.ShowCondition = &v
		}
	case "cpu", "memory", "filesystem", "block", "network":
		if it.Display != "" {
			v := it.Display
			w.Display = &v
		}
		if it.Interval > 0 {
			v := it.Interval.String()
			w.Interval = &v
		}
		if it.Path != "" {
			v := it.Path
			w.Path = &v
		}
		if it.Device != "" {
			v := it.Device
			w.Device = &v
		}
		if it.Interface != "" {
			v := it.Interface
			w.Interface = &v
		}
		if it.Direction != "" {
			v := it.Direction
			w.Direction = &v
		}
	case "battery":
		if it.Label != "" {
			v := it.Label
			w.Label = &v
		}
		if it.WarnBelow > 0 {
			v := it.WarnBelow
			w.WarnBelow = &v
		}
		if it.Interval > 0 {
			v := it.Interval.String()
			w.Interval = &v
		}
	}
	return w
}

// pluginsDiff emits the plugin section only when it holds something, so a
// configuration that uses no plugins is written exactly as short as before.
func pluginsDiff(got Plugins) *wirePlugins {
	if len(got.Enabled) == 0 && len(got.Settings) == 0 && len(got.Instances) == 0 {
		return nil
	}
	c := got.clone()
	return &wirePlugins{Enabled: c.Enabled, Settings: c.Settings, Instances: c.Instances}
}

func themeGenDiff(got, base ThemeConfig) *wireThemeGen {
	var w wireThemeGen
	set := false
	if got.Source != base.Source {
		v := got.Source
		w.Source = &v
		set = true
	}
	if got.Seed != base.Seed {
		v := got.Seed
		w.Seed = &v
		set = true
	}
	if got.Scheme != base.Scheme {
		v := got.Scheme
		w.Scheme = &v
		set = true
	}
	if got.Mode != base.Mode {
		v := got.Mode
		w.Mode = &v
		set = true
	}
	if !set {
		return nil
	}
	return &w
}

func accessibilityDiff(got, base Accessibility) *wireAccessibility {
	if got == base {
		return nil
	}
	w := wireAccessibility{}
	if got.ReducedMotion != base.ReducedMotion {
		v := got.ReducedMotion
		w.ReducedMotion = &v
	}
	if got.HighContrast != base.HighContrast {
		v := got.HighContrast
		w.HighContrast = &v
	}
	return &w
}

func wallpaperDiff(got, base Wallpaper) *wireWallpaper {
	if got == base {
		return nil
	}
	w := wireWallpaper{}
	if got.ImageDirectory != base.ImageDirectory {
		v := got.ImageDirectory
		w.ImageDirectory = &v
	}
	if got.VideoDirectory != base.VideoDirectory {
		v := got.VideoDirectory
		w.VideoDirectory = &v
	}
	if got.Scale != base.Scale {
		v := got.Scale
		w.Scale = &v
	}
	if got.Loop != base.Loop {
		v := got.Loop
		w.Loop = &v
	}
	if got.FPS != base.FPS {
		v := got.FPS
		w.FPS = &v
	}
	if got.Fade != base.Fade {
		v := got.Fade
		w.Fade = &v
	}
	if got.FadeDuration != base.FadeDuration {
		v := got.FadeDuration
		w.FadeDuration = &v
	}
	if got.Hidden != base.Hidden {
		v := got.Hidden
		w.Hidden = &v
	}
	return &w
}

func panelsDiff(got, base Panels) *wirePanels {
	if got == base {
		return nil
	}
	w := wirePanels{}
	if got.Gap != base.Gap {
		v := got.Gap
		w.Gap = &v
	}
	if got.Padding != base.Padding {
		v := got.Padding
		w.Padding = &v
	}
	if got.OSD != base.OSD {
		v := got.OSD
		w.OSD = &v
	}
	return &w
}

func weatherWire(w Weather) *wireWeather {
	lat, lon := w.Latitude, w.Longitude
	out := &wireWeather{Latitude: &lat, Longitude: &lon}
	if w.Unit != "" {
		v := w.Unit
		out.Unit = &v
	}
	if w.Interval > 0 {
		v := w.Interval.String()
		out.Interval = &v
	}
	return out
}

// themeDiff records only the axes that deviate from the preset the theme
// selected, so a preset change moves everything the user never touched.
func themeDiff(got Theme, defaultPreset theme.Preset) *wireTheme {
	base, ok := theme.PresetComposition(got.Preset)
	if !ok {
		base = standardComposition()
	}
	var w wireTheme
	set := false
	if got.Preset != defaultPreset {
		v := string(got.Preset)
		w.Preset = &v
		set = true
	}
	if got.Density != base.Density {
		v := string(got.Density)
		w.Density = &v
		set = true
	}
	if got.Motion != base.Motion {
		v := string(got.Motion)
		w.Motion = &v
		set = true
	}
	if got.Elevation != base.Elevation {
		v := string(got.Elevation)
		w.Elevation = &v
		set = true
	}
	for _, f := range []struct {
		got, base string
		dest      **string
	}{
		{got.FontFamily, base.FontFamily, &w.FontFamily},
		{got.MonoFontFamily, base.MonoFontFamily, &w.MonoFontFamily},
	} {
		if f.got != f.base {
			v := f.got
			*f.dest = &v
			set = true
		}
	}
	for _, f := range []struct {
		got, base int
		dest      **int
	}{
		{got.FontScale, base.FontScale, &w.FontScale},
		{got.FontWeight, base.FontWeight, &w.FontWeight},
		{got.Radius, base.Radius, &w.Radius},
		{got.MotionSpeed, base.MotionSpeed, &w.MotionSpeed},
		{got.BarOpacity, base.BarOpacity, &w.BarOpacity},
		{got.PanelOpacity, base.PanelOpacity, &w.PanelOpacity},
		{got.OverlayOpacity, base.OverlayOpacity, &w.OverlayOpacity},
	} {
		if f.got != f.base {
			v := f.got
			*f.dest = &v
			set = true
		}
	}
	if !set {
		return nil
	}
	return &w
}
