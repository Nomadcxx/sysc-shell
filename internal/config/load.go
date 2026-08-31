package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// Wire types use pointers so an absent field is distinguishable from its zero
// value and inherits the default instead of overwriting it with zero.
type wireFont struct {
	Family *string `json:"family,omitempty"`
	Size   *int    `json:"size,omitempty"`
}

// wireItem decodes either a bare id string or an object carrying that id plus
// its options. Both reference shells attach options per instance, and a
// max-width has nowhere else to live.
type wireItem struct {
	ID       string  `json:"id"`
	Format   *string `json:"format,omitempty"`
	MaxWidth *int    `json:"max-width,omitempty"`

	Display   *string `json:"display,omitempty"`
	Interval  *string `json:"interval,omitempty"`
	Path      *string `json:"path,omitempty"`
	Device    *string `json:"device,omitempty"`
	Interface *string `json:"interface,omitempty"`
	Direction *string `json:"direction,omitempty"`

	ShowCondition *bool `json:"show-condition,omitempty"`

	Label     *string `json:"label,omitempty"`
	WarnBelow *int    `json:"warn-below,omitempty"`
}

func (i *wireItem) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var id string
		if err := json.Unmarshal(trimmed, &id); err != nil {
			return err
		}
		*i = wireItem{ID: id}
		return nil
	}
	// A local type without this method, so decoding the object form does not
	// recurse into UnmarshalJSON.
	type plain wireItem
	var p plain
	if err := json.Unmarshal(trimmed, &p); err != nil {
		return err
	}
	*i = wireItem(p)
	return nil
}

type wireItems struct {
	Left   *[]wireItem `json:"left,omitempty"`
	Center *[]wireItem `json:"center,omitempty"`
	Right  *[]wireItem `json:"right,omitempty"`
}

type wireBar struct {
	Enabled *bool      `json:"enabled,omitempty"`
	Edge    *string    `json:"edge,omitempty"`
	Height  *int       `json:"height,omitempty"`
	Gap     *int       `json:"gap,omitempty"`
	Padding *int       `json:"padding,omitempty"`
	Spacing *int       `json:"spacing,omitempty"`
	Font    *wireFont  `json:"font,omitempty"`
	Items   *wireItems `json:"items,omitempty"`
}

type wireTheme struct {
	Radius *int `json:"radius,omitempty"`
}

type wireThemeGen struct {
	Source *string `json:"source,omitempty"`
	Seed   *string `json:"seed,omitempty"`
	Scheme *string `json:"scheme,omitempty"`
	Mode   *string `json:"mode,omitempty"`
}

type wireAccessibility struct {
	ReducedMotion *bool `json:"reduced-motion,omitempty"`
	HighContrast  *bool `json:"high-contrast,omitempty"`
}

type wireSession struct {
	Locker *string `json:"locker,omitempty"`
}

type wirePanels struct {
	Gap     *int    `json:"gap,omitempty"`
	Padding *int    `json:"padding,omitempty"`
	OSD     *string `json:"osd,omitempty"`
}

type wireOutput struct {
	Connector *string  `json:"connector,omitempty"`
	Bar       *wireBar `json:"bar,omitempty"`
}

type wireWeather struct {
	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`
	Unit      *string  `json:"unit,omitempty"`
	Interval  *string  `json:"interval,omitempty"`
}

type wireConfig struct {
	Bar           *wireBar           `json:"bar,omitempty"`
	Theme         *wireTheme         `json:"theme,omitempty"`
	ThemeGen      *wireThemeGen      `json:"theme-gen,omitempty"`
	Accessibility *wireAccessibility `json:"accessibility,omitempty"`
	Session       *wireSession       `json:"session,omitempty"`
	Panels        *wirePanels        `json:"panels,omitempty"`
	Weather       *wireWeather       `json:"weather,omitempty"`
	Outputs       []wireOutput       `json:"outputs,omitempty"`
	Templates     map[string]bool    `json:"templates,omitempty"`
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
	if wire.ThemeGen != nil {
		gen, err := applyThemeGen(cfg.ThemeGen, *wire.ThemeGen, "theme-gen")
		if err != nil {
			return Config{}, err
		}
		cfg.ThemeGen = gen
	}
	if wire.Accessibility != nil {
		cfg.Accessibility = applyAccessibility(cfg.Accessibility, *wire.Accessibility)
	}
	if wire.Session != nil {
		cfg.Session = applySession(cfg.Session, *wire.Session)
	}
	if wire.Panels != nil {
		panels, err := applyPanels(cfg.Panels, *wire.Panels, "panels")
		if err != nil {
			return Config{}, err
		}
		cfg.Panels = panels
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

	if wire.Weather != nil {
		weather, err := applyWeather(*wire.Weather, "weather")
		if err != nil {
			return Config{}, err
		}
		cfg.Weather = weather
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
	if err := requireWeatherWhenUsed(cfg); err != nil {
		return Config{}, err
	}
	if len(wire.Templates) > 0 {
		cfg.Templates = wire.Templates
	}
	return cfg, nil
}

// applyWeather resolves and validates the weather block.
func applyWeather(w wireWeather, path string) (Weather, error) {
	out := Weather{
		Unit:       defaultWeatherUnit,
		Interval:   defaultWeatherInterval,
		Configured: true,
	}

	if w.Latitude == nil {
		return Weather{}, pathErr(path+".latitude", "is required")
	}
	if *w.Latitude < -90 || *w.Latitude > 90 {
		return Weather{}, pathErr(path+".latitude", "%v is outside -90 through 90", *w.Latitude)
	}
	out.Latitude = *w.Latitude

	if w.Longitude == nil {
		return Weather{}, pathErr(path+".longitude", "is required")
	}
	if *w.Longitude < -180 || *w.Longitude > 180 {
		return Weather{}, pathErr(path+".longitude", "%v is outside -180 through 180", *w.Longitude)
	}
	out.Longitude = *w.Longitude

	if w.Unit != nil {
		if !weatherUnits[*w.Unit] {
			return Weather{}, pathErr(path+".unit", "%q is not celsius or fahrenheit", *w.Unit)
		}
		out.Unit = *w.Unit
	}
	if w.Interval != nil {
		interval, err := time.ParseDuration(*w.Interval)
		if err != nil {
			return Weather{}, pathErr(path+".interval", "%q is not a duration such as 15m", *w.Interval)
		}
		if interval <= 0 {
			return Weather{}, pathErr(path+".interval", "%v is not positive", interval)
		}
		out.Interval = interval
	}
	return out, nil
}

// requireWeatherWhenUsed is the configuration's one cross-section rule: a
// weather widget needs coordinates, and they live in a different block.
//
// Without this the widget would render an error forever because of a block the
// user was never told to write. Every override is checked too, because an
// override can introduce the widget on one output alone.
func requireWeatherWhenUsed(cfg Config) error {
	if cfg.Weather.Configured {
		return nil
	}
	bars := []Bar{cfg.Bar}
	for _, o := range cfg.Outputs {
		bars = append(bars, o.Bar)
	}
	for _, bar := range bars {
		for _, section := range [][]Item{bar.Left, bar.Center, bar.Right} {
			for _, item := range section {
				if item.ID == "weather" {
					return pathErr("weather.latitude",
						"is required because a weather widget is configured")
				}
			}
		}
	}
	return nil
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

// items resolves one section, rejecting the whole candidate on the first
// failure and naming its exact field path.
func items(supplied *[]wireItem, base []Item, path string) ([]Item, error) {
	if supplied == nil {
		return base, nil
	}
	out := make([]Item, 0, len(*supplied))
	for i, w := range *supplied {
		item, err := resolveItem(w, fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

// resolveItem validates one item and fills in its defaults. An option supplied
// on an item that does not accept it is an error rather than a silently
// ignored field, so a misplaced setting is visible.
func resolveItem(w wireItem, path string) (Item, error) {
	if _, ok := knownItems[w.ID]; !ok {
		return Item{}, pathErr(path, "%q is not a known item", w.ID)
	}
	item := Item{ID: w.ID}

	if w.Format != nil && w.ID != "clock" {
		return Item{}, pathErr(path+".format", "is accepted only on a clock, not on %q", w.ID)
	}
	if w.MaxWidth != nil && w.ID != "window-title" && w.ID != "weather" {
		return Item{}, pathErr(path+".max-width", "is accepted only on a window-title, not on %q", w.ID)
	}
	if w.ShowCondition != nil && w.ID != "weather" {
		return Item{}, pathErr(path+".show-condition",
			"is accepted only on a weather widget, not on %q", w.ID)
	}
	if w.Label != nil && w.ID != "battery" {
		return Item{}, pathErr(path+".label",
			"is accepted only on a battery widget, not on %q", w.ID)
	}
	if w.WarnBelow != nil && w.ID != "battery" {
		return Item{}, pathErr(path+".warn-below",
			"is accepted only on a battery widget, not on %q", w.ID)
	}
	if !isMetric(w.ID) {
		for _, unwanted := range []struct {
			name string
			set  bool
		}{
			{"display", w.Display != nil},
			{"path", w.Path != nil},
			{"device", w.Device != nil},
			{"interface", w.Interface != nil},
			{"direction", w.Direction != nil},
		} {
			if unwanted.set {
				return Item{}, pathErr(path+"."+unwanted.name,
					"is accepted only on a metric item, not on %q", w.ID)
			}
		}
	}
	// interval is accepted on the metric items and on battery, which is also
	// sampled. The selectors below stay rejected on battery.
	if w.Interval != nil && !isMetric(w.ID) && w.ID != "battery" {
		return Item{}, pathErr(path+".interval",
			"is accepted only on a sampled item, not on %q", w.ID)
	}

	switch w.ID {
	case "clock":
		item.Format = defaultClockFormat
		if w.Format != nil {
			item.Format = *w.Format
		}
		boundary, err := clockBoundary(item.Format)
		if err != nil {
			return Item{}, pathErr(path+".format", "%s", err)
		}
		item.Boundary = boundary
	case "window-title":
		item.MaxWidth = defaultTitleMaxWidth
		if w.MaxWidth != nil {
			if *w.MaxWidth <= 0 {
				return Item{}, pathErr(path+".max-width", "%d is not positive", *w.MaxWidth)
			}
			item.MaxWidth = *w.MaxWidth
		}
	case "cpu", "memory", "filesystem", "block", "network":
		resolved, err := resolveMetric(w, path)
		if err != nil {
			return Item{}, err
		}
		item = resolved
	case "weather":
		if w.ShowCondition != nil {
			item.ShowCondition = *w.ShowCondition
		}
		if w.MaxWidth != nil {
			if *w.MaxWidth <= 0 {
				return Item{}, pathErr(path+".max-width", "%d is not positive", *w.MaxWidth)
			}
			item.MaxWidth = *w.MaxWidth
		}
	case "battery":
		item.Label = defaultBatteryLabel
		item.WarnBelow = defaultBatteryWarnBelow
		// A battery is a sampled source, so it carries an interval like the
		// metric items do. Without this the lease in Task 5 would be acquired
		// at zero and rejected as non-positive, and the bar would fail to
		// build.
		item.Interval = defaultBatteryInterval
		if w.Interval != nil {
			interval, err := time.ParseDuration(*w.Interval)
			if err != nil {
				return Item{}, pathErr(path+".interval",
					"%q is not a duration such as 30s", *w.Interval)
			}
			if interval <= 0 {
				return Item{}, pathErr(path+".interval", "%v is not positive", interval)
			}
			item.Interval = interval
		}
		if w.Label != nil {
			if !batteryLabels[*w.Label] {
				return Item{}, pathErr(path+".label",
					"%q is not one of percent, time, rate, none", *w.Label)
			}
			item.Label = *w.Label
		}
		if w.WarnBelow != nil {
			if *w.WarnBelow < 1 || *w.WarnBelow > 99 {
				return Item{}, pathErr(path+".warn-below",
					"%d is outside 1 through 99", *w.WarnBelow)
			}
			item.WarnBelow = *w.WarnBelow
		}
	}
	return item, nil
}

// resolveMetric validates one metric item and fills in its defaults.
//
// A selector naming an absent mount, device or interface is deliberately not
// an error here: devices are hot-plugged and interfaces come and go, so the
// widget validates as well-formed and renders the unavailable placeholder
// until its subject appears.
func resolveMetric(w wireItem, path string) (Item, error) {
	item := Item{
		ID:       w.ID,
		Display:  defaultMetricDisplay,
		Interval: defaultMetricInterval,
	}

	if w.Display != nil {
		switch *w.Display {
		case "text", "graph":
		case "meter":
			if rateSources[w.ID] {
				return Item{}, pathErr(path+".display",
					"a meter needs a full scale, which the rate source %q has none", w.ID)
			}
		default:
			return Item{}, pathErr(path+".display",
				"%q is not one of text, meter, graph", *w.Display)
		}
		item.Display = *w.Display
	}

	if w.Interval != nil {
		interval, err := time.ParseDuration(*w.Interval)
		if err != nil {
			return Item{}, pathErr(path+".interval", "%q is not a duration such as 2s", *w.Interval)
		}
		if interval <= 0 {
			return Item{}, pathErr(path+".interval", "%v is not positive", interval)
		}
		item.Interval = interval
	}

	// Each selector is required on exactly one id and rejected on the others,
	// so a path on a CPU widget cannot be silently ignored.
	if err := selector(w.Path, w.ID, "filesystem", path+".path", &item.Path); err != nil {
		return Item{}, err
	}
	if err := selector(w.Device, w.ID, "block", path+".device", &item.Device); err != nil {
		return Item{}, err
	}
	if err := selector(w.Interface, w.ID, "network", path+".interface", &item.Interface); err != nil {
		return Item{}, err
	}

	switch w.ID {
	case "block":
		item.Direction = "read"
		if w.Direction != nil {
			if !blockDirections[*w.Direction] {
				return Item{}, pathErr(path+".direction",
					"%q is not one of read, write", *w.Direction)
			}
			item.Direction = *w.Direction
		}
	case "network":
		item.Direction = "rx"
		if w.Direction != nil {
			if !networkDirections[*w.Direction] {
				return Item{}, pathErr(path+".direction", "%q is not one of rx, tx", *w.Direction)
			}
			item.Direction = *w.Direction
		}
	default:
		if w.Direction != nil {
			return Item{}, pathErr(path+".direction",
				"is accepted only on block and network, not on %q", w.ID)
		}
	}
	return item, nil
}

// selector requires an option on the one id that needs it and rejects it
// elsewhere. A machine has many mounts, devices and interfaces, so there is no
// defensible default and the option cannot be optional.
func selector(supplied *string, id, wantID, path string, dest *string) error {
	if id != wantID {
		if supplied != nil {
			return pathErr(path, "is accepted only on %q, not on %q", wantID, id)
		}
		return nil
	}
	// Trimmed before the emptiness check: " " is a typo, not a mount point, and
	// accepting it would load a widget that waits forever for a subject that
	// cannot exist.
	if supplied == nil || strings.TrimSpace(*supplied) == "" {
		return pathErr(path, "is required on %q", wantID)
	}
	*dest = strings.TrimSpace(*supplied)
	return nil
}

// clockProbeBase is a fixed instant in UTC, so boundary derivation is
// deterministic and independent of the machine's clock and zone.
var clockProbeBase = time.Date(2026, 8, 30, 15, 4, 5, 0, time.UTC)

// clockProbes ascend, so the first one that changes the rendered text names
// the resolution the layout displays. The long tail exists to distinguish a
// legitimately coarse layout such as "January 2006" from a typo.
var clockProbes = []time.Duration{
	time.Second, time.Minute, time.Hour,
	24 * time.Hour, 32 * 24 * time.Hour, 400 * 24 * time.Hour,
}

// clockBoundary reports how often a layout's text can change.
//
// time.Format never returns an error, so a layout cannot be validated by
// parsing it; it can be validated by observing it. A layout that renders
// identically at every probe does not depend on the time at all, which is what
// a non-layout such as "HH:MM" does.
//
// The result is capped at one minute even for a daily layout. Truncating to a
// day would truncate to UTC midnight rather than local midnight, so a date
// would flip at the wrong moment and break across a daylight-saving change.
// Re-rendering once a minute and detecting no change is correct and needs no
// calendar arithmetic.
func clockBoundary(layout string) (time.Duration, error) {
	base := clockProbeBase.Format(layout)
	for _, probe := range clockProbes {
		if clockProbeBase.Add(probe).Format(layout) == base {
			continue
		}
		if probe == time.Second {
			return time.Second, nil
		}
		return time.Minute, nil
	}
	return 0, fmt.Errorf(
		"%q does not change with time; a Go layout uses a reference instant such as 15:04", layout)
}

func applyTheme(base Theme, w wireTheme, path string) (Theme, error) {
	out := base
	if w.Radius != nil {
		if *w.Radius < 0 {
			return Theme{}, pathErr(path+".radius", "%d is negative", *w.Radius)
		}
		out.Radius = *w.Radius
	}
	return out, nil
}

var themeSources = map[string]bool{"wallpaper": true, "hex": true, "stock": true}
var themeModes = map[string]bool{"dark": true, "light": true}
var osdPositions = map[string]bool{
	"top-left": true, "top-center": true, "top-right": true,
	"center-left": true, "center": true, "center-right": true,
	"bottom-left": true, "bottom-center": true, "bottom-right": true,
}

func applyThemeGen(base ThemeConfig, w wireThemeGen, path string) (ThemeConfig, error) {
	out := base
	if w.Source != nil {
		if !themeSources[*w.Source] {
			return ThemeConfig{}, pathErr(path+".source", "%q is not one of wallpaper, hex, stock", *w.Source)
		}
		out.Source = *w.Source
	}
	if w.Seed != nil {
		out.Seed = *w.Seed
	}
	if w.Scheme != nil {
		out.Scheme = *w.Scheme
	}
	if w.Mode != nil {
		if !themeModes[*w.Mode] {
			return ThemeConfig{}, pathErr(path+".mode", "%q is not one of dark, light", *w.Mode)
		}
		out.Mode = *w.Mode
	}
	if out.Source == "hex" && !colorPattern.MatchString(out.Seed) {
		return ThemeConfig{}, pathErr(path+".seed", "%q is not #RRGGBB or #RRGGBBAA", out.Seed)
	}
	if out.Source == "stock" {
		if _, ok := theme.StockSeed(out.Seed); !ok {
			return ThemeConfig{}, pathErr(path+".seed", "%q is not a known stock theme", out.Seed)
		}
	}
	return out, nil
}

func applyAccessibility(base Accessibility, w wireAccessibility) Accessibility {
	if w.ReducedMotion != nil {
		base.ReducedMotion = *w.ReducedMotion
	}
	if w.HighContrast != nil {
		base.HighContrast = *w.HighContrast
	}
	return base
}

func applySession(base Session, w wireSession) Session {
	if w.Locker != nil {
		base.Locker = *w.Locker
	}
	return base
}

func applyPanels(base Panels, w wirePanels, path string) (Panels, error) {
	out := base
	if w.Gap != nil {
		out.Gap = *w.Gap
	}
	if w.Padding != nil {
		out.Padding = *w.Padding
	}
	if w.OSD != nil {
		if !osdPositions[*w.OSD] {
			return Panels{}, pathErr(path+".osd", "%q is not a known position", *w.OSD)
		}
		out.OSD = *w.OSD
	}
	if out.Gap < 0 {
		return Panels{}, pathErr(path+".gap", "%d is negative", out.Gap)
	}
	if out.Padding < 0 {
		return Panels{}, pathErr(path+".padding", "%d is negative", out.Padding)
	}
	return out, nil
}
