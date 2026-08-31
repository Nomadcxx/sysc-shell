package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	if bar := barDiff(c.Bar, d.Bar); bar != nil {
		w.Bar = bar
	}
	if c.Theme.Radius != d.Theme.Radius {
		r := c.Theme.Radius
		w.Theme = &wireTheme{Radius: &r}
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
	if p := panelsDiff(c.Panels, d.Panels); p != nil {
		w.Panels = p
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
		if x != y {
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
