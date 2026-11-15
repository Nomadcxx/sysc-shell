package displays

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/sblinch/kdl-go/document"
)

// savedPolicy reads only the fields the editor owns from the first matching
// native block. Other children remain with the full block cloned on Keep.
func savedPolicy(graph *Graph, name Name) (Policy, string, error) {
	var p Policy
	source, ok := graph.Find(name)
	if !ok {
		return p, "", nil
	}
	unavailable := ""
	for _, child := range source.Node.Children {
		key := text(child.Name)
		switch key {
		case "off", "modeline":
			unavailable = "This output uses " + key + "; it cannot be edited here."
		case "mode":
			if len(child.Arguments) != 1 {
				return p, "", fmt.Errorf("%s: mode needs one value", source.File)
			}
			m, err := parseNativeMode(text(child.Arguments[0]))
			if err != nil {
				return p, "", err
			}
			if v, ok := child.Properties.Get("custom"); ok && v.Value == true {
				m.Custom = true
				unavailable = "Custom display modes cannot be edited here."
			}
			p.Mode = &m
		case "scale":
			if len(child.Arguments) != 1 {
				return p, "", fmt.Errorf("%s: scale needs one value", source.File)
			}
			v, err := numeric(child.Arguments[0])
			if err != nil {
				return p, "", err
			}
			if err := ValidateScale(v); err != nil {
				return p, "", err
			}
			p.Scale = &v
		case "transform":
			if len(child.Arguments) != 1 {
				return p, "", fmt.Errorf("%s: transform needs one value", source.File)
			}
			i := slices.Index(nativeTransforms, text(child.Arguments[0]))
			if i < 0 {
				return p, "", fmt.Errorf("%s: unknown native transform", source.File)
			}
			value := Transforms[i]
			p.Transform = &value
		case "position":
			pos := Position{}
			for _, field := range []struct {
				key string
				dst *int32
			}{{"x", &pos.X}, {"y", &pos.Y}} {
				v, ok := child.Properties.Get(field.key)
				if !ok {
					continue
				}
				n, err := numeric(v)
				if err != nil || n != math.Trunc(n) || n < math.MinInt32 || n > math.MaxInt32 {
					return p, "", fmt.Errorf("%s: invalid position %s", source.File, field.key)
				}
				*field.dst = int32(n)
			}
			p.Position = &pos
		case "variable-refresh-rate":
			p.VRR = &VRRPolicy{}
			if v, ok := child.Properties.Get("on-demand"); ok {
				b, valid := v.Value.(bool)
				if !valid {
					return p, "", fmt.Errorf("%s: on-demand must be boolean", source.File)
				}
				p.VRR.OnDemand = b
			}
		}
	}
	return p, unavailable, nil
}

func numeric(v *document.Value) (float64, error) {
	if v == nil {
		return 0, fmt.Errorf("missing number")
	}
	switch v.Value.(type) {
	case string, bool:
		return 0, fmt.Errorf("expected a number")
	}
	n, err := strconv.ParseFloat(fmt.Sprint(v.Value), 64)
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
		return 0, fmt.Errorf("invalid number")
	}
	return n, nil
}

func parseNativeMode(raw string) (ModePolicy, error) {
	var m ModePolicy
	resolution, rate, exact := strings.Cut(raw, "@")
	w, h, ok := strings.Cut(resolution, "x")
	if !ok {
		return m, fmt.Errorf("invalid native mode %q", raw)
	}
	width, e1 := strconv.ParseUint(w, 10, 16)
	height, e2 := strconv.ParseUint(h, 10, 16)
	if e1 != nil || e2 != nil || width == 0 || height == 0 {
		return m, fmt.Errorf("invalid native mode %q", raw)
	}
	m.Width, m.Height = uint16(width), uint16(height)
	if exact {
		hz, err := strconv.ParseFloat(rate, 64)
		if err != nil || math.IsInf(hz, 0) || math.IsNaN(hz) || hz <= 0 || hz*1000 > math.MaxUint32 {
			return m, fmt.Errorf("invalid refresh in %q", raw)
		}
		m.RefreshMilliHz = uint32(math.Round(hz * 1000))
	}
	return m, nil
}
