package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// Wire types use pointers so an absent field is distinguishable from its zero
// value and inherits the default instead of overwriting it with zero.
type wireFont struct {
	Family *string `json:"family"`
	Size   *int    `json:"size"`
}

type wireItems struct {
	Left   *[]string `json:"left"`
	Center *[]string `json:"center"`
	Right  *[]string `json:"right"`
}

type wireBar struct {
	Enabled *bool      `json:"enabled"`
	Edge    *string    `json:"edge"`
	Height  *int       `json:"height"`
	Gap     *int       `json:"gap"`
	Padding *int       `json:"padding"`
	Spacing *int       `json:"spacing"`
	Font    *wireFont  `json:"font"`
	Items   *wireItems `json:"items"`
}

type wireTheme struct {
	Background *string `json:"background"`
	Foreground *string `json:"foreground"`
	Accent     *string `json:"accent"`
	Muted      *string `json:"muted"`
	Error      *string `json:"error"`
	Radius     *int    `json:"radius"`
}

type wireOutput struct {
	Connector *string  `json:"connector"`
	Bar       *wireBar `json:"bar"`
}

type wireConfig struct {
	Bar     *wireBar     `json:"bar"`
	Theme   *wireTheme   `json:"theme"`
	Outputs []wireOutput `json:"outputs"`
}

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}([0-9a-fA-F]{2})?$`)

// DefaultPath is $XDG_CONFIG_HOME/sysc-shell/config.json.
func DefaultPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "sysc-shell", "config.json")
}

// Load reads and validates a configuration file. A missing file is not an
// error: the built-in defaults apply.
func Load(path string) (Config, error) {
	if path == "" {
		return Default(), nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	return Parse(data)
}

// Parse decodes and validates a complete candidate, returning a resolved
// Config only when every field passes.
func Parse(data []byte) (Config, error) {
	var wire wireConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("more than one JSON value")
		}
		return Config{}, fmt.Errorf("config: %w", err)
	}

	cfg := Default()
	if wire.Theme != nil {
		theme, err := applyTheme(cfg.Theme, *wire.Theme, "theme")
		if err != nil {
			return Config{}, err
		}
		cfg.Theme = theme
	}
	// The bar's radius mirrors the theme's, so the opaque region and the
	// painted body agree without a second token.
	cfg.Bar.Radius = cfg.Theme.Radius
	if wire.Bar != nil {
		bar, err := applyBar(cfg.Bar, *wire.Bar, "bar")
		if err != nil {
			return Config{}, err
		}
		cfg.Bar = bar
	}

	seen := make(map[string]struct{}, len(wire.Outputs))
	for i, o := range wire.Outputs {
		path := fmt.Sprintf("outputs[%d]", i)
		if o.Connector == nil || *o.Connector == "" {
			return Config{}, pathErr(path+".connector", "must be a non-empty connector name")
		}
		if _, dup := seen[*o.Connector]; dup {
			return Config{}, pathErr(path+".connector", "%q appears more than once", *o.Connector)
		}
		seen[*o.Connector] = struct{}{}

		bar := cfg.Bar
		if o.Bar != nil {
			var err error
			if bar, err = applyBar(cfg.Bar, *o.Bar, path+".bar"); err != nil {
				return Config{}, err
			}
		}
		cfg.Outputs = append(cfg.Outputs, OutputOverride{Connector: *o.Connector, Bar: bar})
	}
	return cfg, nil
}

func applyBar(base Bar, w wireBar, path string) (Bar, error) {
	out := base
	if w.Enabled != nil {
		out.Enabled = *w.Enabled
	}
	if w.Edge != nil {
		supported, known := supportedEdges[*w.Edge]
		if !known {
			return Bar{}, pathErr(path+".edge", "%q is not one of top, bottom, left, right", *w.Edge)
		}
		if !supported {
			return Bar{}, pathErr(path+".edge",
				"%q is not supported in this milestone; use top", *w.Edge)
		}
		out.Edge = *w.Edge
	}
	if w.Height != nil {
		out.Height = *w.Height
	}
	if w.Gap != nil {
		out.Gap = *w.Gap
	}
	if w.Padding != nil {
		out.Padding = *w.Padding
	}
	if w.Spacing != nil {
		out.Spacing = *w.Spacing
	}
	if w.Font != nil {
		if w.Font.Family != nil {
			out.FontFamily = *w.Font.Family
		}
		if w.Font.Size != nil {
			out.FontSize = *w.Font.Size
		}
	}
	if w.Items != nil {
		var err error
		if out.Left, err = items(w.Items.Left, out.Left, path+".items.left"); err != nil {
			return Bar{}, err
		}
		if out.Center, err = items(w.Items.Center, out.Center, path+".items.center"); err != nil {
			return Bar{}, err
		}
		if out.Right, err = items(w.Items.Right, out.Right, path+".items.right"); err != nil {
			return Bar{}, err
		}
	}
	return out, validateBar(out, path)
}

// validateBar enforces every range rule, reporting the field path that failed.
func validateBar(b Bar, path string) error {
	if b.Gap < 0 {
		return pathErr(path+".gap", "%d is negative", b.Gap)
	}
	if body := b.Height - 2*b.Gap; body <= 0 {
		return pathErr(path+".height",
			"%d with gap %d leaves a body of %d; the minimum is %d",
			b.Height, b.Gap, body, 2*b.Gap+1)
	}
	if b.Padding < 0 {
		return pathErr(path+".padding", "%d is negative", b.Padding)
	}
	if b.Spacing < 0 {
		return pathErr(path+".spacing", "%d is negative", b.Spacing)
	}
	if b.FontSize <= 0 {
		return pathErr(path+".font.size", "%d is not positive", b.FontSize)
	}
	return nil
}

func items(supplied *[]string, base []string, path string) ([]string, error) {
	if supplied == nil {
		return base, nil
	}
	for i, id := range *supplied {
		if _, ok := knownItems[id]; !ok {
			return nil, pathErr(fmt.Sprintf("%s[%d]", path, i), "%q is not a known item", id)
		}
	}
	return append([]string(nil), *supplied...), nil
}

func applyTheme(base Theme, w wireTheme, path string) (Theme, error) {
	out := base
	fields := []struct {
		name  string
		value *string
		dest  *string
	}{
		{"background", w.Background, &out.Background},
		{"foreground", w.Foreground, &out.Foreground},
		{"accent", w.Accent, &out.Accent},
		{"muted", w.Muted, &out.Muted},
		{"error", w.Error, &out.Error},
	}
	for _, f := range fields {
		if f.value == nil {
			continue
		}
		if !colorPattern.MatchString(*f.value) {
			return Theme{}, pathErr(path+"."+f.name, "%q is not #RRGGBB or #RRGGBBAA", *f.value)
		}
		*f.dest = *f.value
	}
	if w.Radius != nil {
		if *w.Radius < 0 {
			return Theme{}, pathErr(path+".radius", "%d is negative", *w.Radius)
		}
		out.Radius = *w.Radius
	}
	return out, nil
}
