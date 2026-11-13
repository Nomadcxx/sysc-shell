package config

import (
	"fmt"
	"maps"
)

// OutputBarOverridden distinguishes an explicit value from an inherited one,
// even when the two currently happen to be equal.
func (c Config) OutputBarOverridden(connector, key string) bool {
	for _, o := range c.Outputs {
		if o.Connector == connector {
			if o.Set != nil {
				return o.Set[key]
			}
			// Old in-process callers construct full resolved bars without a
			// presence map. Preserve their existing difference-based behavior.
			if w := barDiff(o.Bar, c.Bar); w != nil {
				return outputFields(*w)[key]
			}
		}
	}
	return false
}

// SetOutputBar copies only key from bar. Callers can reuse a shared setting's
// setter on a candidate Bar; all other fields retain their current ownership.
func (c *Config) SetOutputBar(connector, key string, bar Bar) error {
	if connector == "" || !outputFieldKnown(key) {
		return fmt.Errorf("config: invalid output bar field %q on %q", key, connector)
	}
	if key == "reserve" && bar.Reserve == nil {
		return fmt.Errorf("config: use ResetOutputBar to inherit reserve on %q", connector)
	}
	index := -1
	o := OutputOverride{Connector: connector, Bar: c.Bar.Clone(), Set: map[string]bool{}}
	for i, existing := range c.Outputs {
		if existing.Connector == connector {
			index = i
			o = existing
			o.Bar = existing.resolve(c.Bar).Clone()
			o.Set = existing.fields(c.Bar)
			break
		}
	}
	// Merge only the requested field into the resolved candidate. Reusing
	// applyBar keeps enum, range and item validation at the loader's boundary.
	one := OutputOverride{Bar: bar, Set: map[string]bool{key: true}}
	w := one.wire()
	if key == "gap" && o.Set["height"] {
		w.Height = &o.Bar.Height
	}
	candidate, err := applyBar(o.Bar, w, "outputs."+connector+".bar")
	if err != nil {
		return err
	}
	o.Bar = candidate.Clone()
	o.Set[key] = true
	if index < 0 {
		c.Outputs = append(c.Outputs, o)
	} else {
		c.Outputs[index] = o
	}
	return nil
}

// ResetOutputBar removes only the named overrides. It never removes a lane
// or advanced field merely because the current page does not expose it.
func (c *Config) ResetOutputBar(connector string, keys ...string) {
	for i := range c.Outputs {
		o := &c.Outputs[i]
		if o.Connector != connector {
			continue
		}
		o.Set = o.fields(c.Bar)
		for _, key := range keys {
			delete(o.Set, key)
		}
		o.Bar = o.resolve(c.Bar).Clone()
		return
	}
}

func (o OutputOverride) fields(base Bar) map[string]bool {
	if o.Set != nil {
		return maps.Clone(o.Set)
	}
	if w := barDiff(o.Bar, base); w != nil {
		return outputFields(*w)
	}
	return map[string]bool{}
}

func outputFieldKnown(key string) bool {
	switch key {
	case "enabled", "edge", "height", "gap", "reserve", "padding", "spacing", "style", "shape", "frost-opacity", "pill-opacity", "font.family", "font.size", "items.left", "items.center", "items.right":
		return true
	}
	return false
}

func outputFields(w wireBar) map[string]bool {
	set := map[string]bool{}
	for _, field := range []struct {
		key     string
		present bool
	}{
		{"enabled", w.Enabled != nil}, {"edge", w.Edge != nil}, {"height", w.Height != nil},
		{"gap", w.Gap != nil}, {"reserve", w.Reserve != nil}, {"padding", w.Padding != nil},
		{"spacing", w.Spacing != nil}, {"style", w.Style != nil}, {"shape", w.Shape != nil},
		{"frost-opacity", w.Frost != nil}, {"pill-opacity", w.Pill != nil},
	} {
		if field.present {
			set[field.key] = true
		}
	}
	if w.Font != nil {
		if w.Font.Family != nil {
			set["font.family"] = true
		}
		if w.Font.Size != nil {
			set["font.size"] = true
		}
	}
	if w.Items != nil {
		if w.Items.Left != nil {
			set["items.left"] = true
		}
		if w.Items.Center != nil {
			set["items.center"] = true
		}
		if w.Items.Right != nil {
			set["items.right"] = true
		}
	}
	return set
}

// wire preserves field presence instead of inferring it from today's defaults.
func (o OutputOverride) wire() wireBar {
	b, set := o.Bar, o.Set
	w := wireBar{}
	if set["enabled"] {
		w.Enabled = &b.Enabled
	}
	if set["edge"] {
		w.Edge = &b.Edge
	}
	if set["height"] {
		w.Height = &b.Height
	}
	if set["gap"] {
		w.Gap = &b.Gap
	}
	if set["reserve"] {
		w.Reserve = b.Reserve
	}
	if set["padding"] {
		w.Padding = &b.Padding
	}
	if set["spacing"] {
		w.Spacing = &b.Spacing
	}
	if set["style"] {
		w.Style = &b.Style
	}
	if set["shape"] {
		w.Shape = &b.Shape
	}
	if set["frost-opacity"] {
		w.Frost = &b.FrostOpacity
	}
	if set["pill-opacity"] {
		w.Pill = &b.PillOpacity
	}
	if set["font.family"] || set["font.size"] {
		w.Font = &wireFont{}
		if set["font.family"] {
			w.Font.Family = &b.FontFamily
		}
		if set["font.size"] {
			w.Font.Size = &b.FontSize
		}
	}
	if set["items.left"] || set["items.center"] || set["items.right"] {
		w.Items = &wireItems{}
		if set["items.left"] {
			lane := encodeItems(b.Left)
			w.Items.Left = &lane
		}
		if set["items.center"] {
			lane := encodeItems(b.Center)
			w.Items.Center = &lane
		}
		if set["items.right"] {
			lane := encodeItems(b.Right)
			w.Items.Right = &lane
		}
	}
	return w
}

// resolve overlays already validated values without re-decoding lanes or
// hiding a temporarily invalid combination; ResolveOutputs validates the result.
func (o OutputOverride) resolve(base Bar) Bar {
	if o.Set == nil {
		return o.Bar
	}
	b, out := o.Bar, base
	for key, stated := range o.Set {
		if !stated {
			continue
		}
		switch key {
		case "enabled":
			out.Enabled = b.Enabled
		case "edge":
			out.Edge = b.Edge
		case "height":
			out.Height = b.Height
		case "gap":
			out.Gap = b.Gap
		case "reserve":
			out.Reserve = b.Reserve
		case "padding":
			out.Padding = b.Padding
		case "spacing":
			out.Spacing = b.Spacing
		case "style":
			out.Style = b.Style
		case "shape":
			out.Shape = b.Shape
		case "frost-opacity":
			out.FrostOpacity = b.FrostOpacity
		case "pill-opacity":
			out.PillOpacity = b.PillOpacity
		case "font.family":
			out.FontFamily = b.FontFamily
		case "font.size":
			out.FontSize = b.FontSize
		case "items.left":
			out.Left = b.Left
		case "items.center":
			out.Center = b.Center
		case "items.right":
			out.Right = b.Right
		}
	}
	if o.Set["gap"] && !o.Set["height"] {
		out.Height = base.Height - 2*base.Gap + 2*out.Gap
	}
	return out
}
