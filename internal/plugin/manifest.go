// Package plugin is the shell's host for external plugin processes. It owns
// discovery, manifest validation, supervision, settings, persistent state, and
// the conversion of wire views into shell-owned trees.
//
// The trust model is deliberate and narrow. A plugin runs as the shell user
// with the shell user's privileges, so capabilities negotiate which host calls
// a plugin may make; they are not a security boundary and this package adds no
// sandbox that the operating system is not already enforcing. What the package
// does guarantee is that a misbehaving plugin cannot make the shell
// unresponsive, unbounded, or wrong: every input is bounded before it is
// parsed and validated before it reaches presentation.
package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// ManifestName is the file every plugin directory must contain.
const ManifestName = "manifest.json"

// The manifest ceilings. They bound what a directory can ask the shell to hold
// in memory and render before anything has been started.
const (
	// MaxManifestBytes bounds one manifest file.
	MaxManifestBytes = 256 << 10
	// MaxEntries bounds each of the services, widgets, and panels lists.
	MaxEntries = 32
	// MaxSettings bounds one settings schema, whether plugin or instance
	// scoped.
	MaxSettings = 64
	// MaxSelectOptions bounds one select setting's choices.
	MaxSelectOptions = 64
	// MaxRequires bounds the declared command dependencies.
	MaxRequires = 16
	// MinPanelExtent and MaxPanelExtent bound a declared panel in logical
	// pixels. A panel smaller than the minimum could not show its own error
	// content; one larger than the maximum is not a panel.
	MinPanelExtent = 64
	MaxPanelExtent = 4096

	maxIDBytes    = 128
	maxLabelBytes = 256
	maxTextBytes  = 4096
)

// Capability names a class of host call a plugin may request. The manifest
// declares what it wants; the host grants the intersection with what it
// supports, and the plugin is told what it actually received.
type Capability string

const (
	// CapNotifications lets a plugin send notifications through the shell's
	// own client rather than needing session-bus access of its own.
	CapNotifications Capability = "notifications"
	// CapPanels lets a plugin ask the host to open a panel it declared.
	CapPanels Capability = "panels"
	// CapSettings lets a plugin read its committed settings.
	CapSettings Capability = "settings"
	// CapState lets a plugin read and write its namespaced persistent store.
	CapState Capability = "state"
)

var knownCapabilities = map[Capability]bool{
	CapNotifications: true, CapPanels: true, CapSettings: true, CapState: true,
}

// Placement says how a declared panel is positioned. Version one attaches a
// panel to the widget that opened it.
type Placement string

const PlacementAttached Placement = "attached"

// SettingType names a value the shell can generate a control for. A plugin
// declares its settings and the shell renders them; a plugin cannot supply a
// settings interface of its own.
type SettingType string

const (
	SettingBool   SettingType = "bool"
	SettingInt    SettingType = "int"
	SettingFloat  SettingType = "float"
	SettingString SettingType = "string"
	SettingSelect SettingType = "select"
	SettingColor  SettingType = "color"
	SettingFile   SettingType = "file"
	SettingFolder SettingType = "folder"
)

var knownSettingTypes = map[SettingType]bool{
	SettingBool: true, SettingInt: true, SettingFloat: true, SettingString: true,
	SettingSelect: true, SettingColor: true, SettingFile: true, SettingFolder: true,
}

// numeric reports whether a type carries bounds.
func (t SettingType) numeric() bool { return t == SettingInt || t == SettingFloat }

// Manifest is one validated plugin declaration.
type Manifest struct {
	Schema      int
	ID          string
	Name        string
	Version     string
	Author      string
	Description string
	Protocol    v1.Version
	// Exec is the declared relative path; ExecPath is the resolved absolute
	// one, proven to be a regular executable inside the plugin directory.
	Exec     string
	ExecPath string
	Dir      string

	Capabilities []Capability
	Requires     []string
	Services     []Service
	Widgets      []Widget
	Panels       []Panel
	Settings     []Setting
}

// Service is one background behaviour the plugin owns. It is metadata for the
// manager: the process supplies the behaviour itself.
type Service struct {
	ID    string
	Label string
}

// Widget is one bar entry a user can place. Its settings are instance scoped,
// so two placements of one widget hold separate values.
type Widget struct {
	ID       string
	Label    string
	Settings []Setting
}

// Panel is one panel the plugin may ask the host to open.
type Panel struct {
	ID        string
	Label     string
	Width     int
	Height    int
	Placement Placement
}

// SettingOption is one choice in a select setting.
type SettingOption struct {
	Value string
	Label string
}

// VisibleWhen hides a setting until another setting in the same schema holds a
// given value.
type VisibleWhen struct {
	Key    string
	Equals any
}

// Setting is one generated control.
type Setting struct {
	Key         string
	Type        SettingType
	Label       string
	Description string
	Default     any
	Min         *float64
	Max         *float64
	Options     []SettingOption
	VisibleWhen *VisibleWhen
}

// Grants reports whether the manifest requested a capability.
func (m Manifest) Grants(c Capability) bool {
	for _, have := range m.Capabilities {
		if have == c {
			return true
		}
	}
	return false
}

// MissingCommands returns the declared dependencies that are not on PATH.
//
// It is looked up on demand rather than cached at load, because a user can
// install a missing dependency without touching the plugin, and the manager
// must be able to say so without a rescan.
func (m Manifest) MissingCommands() []string {
	var missing []string
	for _, cmd := range m.Requires {
		if _, err := exec.LookPath(cmd); err != nil {
			missing = append(missing, cmd)
		}
	}
	return missing
}

// Panel returns the declared panel with the given entry ID.
func (m Manifest) Panel(id string) (Panel, bool) {
	for _, p := range m.Panels {
		if p.ID == id {
			return p, true
		}
	}
	return Panel{}, false
}

// Widget returns the declared widget with the given entry ID.
func (m Manifest) Widget(id string) (Widget, bool) {
	for _, w := range m.Widgets {
		if w.ID == id {
			return w, true
		}
	}
	return Widget{}, false
}

// The wire shapes. Pointers distinguish an absent field from its zero value,
// which matters for settings bounds and defaults.
type wireManifest struct {
	Schema      int          `json:"schema"`
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Version     string       `json:"version"`
	Author      string       `json:"author,omitempty"`
	Description string       `json:"description,omitempty"`
	Protocol    wireProtocol `json:"protocol"`
	Exec        string       `json:"exec"`

	Capabilities []string      `json:"capabilities,omitempty"`
	Requires     *wireRequires `json:"requires,omitempty"`
	Services     []wireEntry   `json:"services,omitempty"`
	Widgets      []wireWidget  `json:"widgets,omitempty"`
	Panels       []wirePanel   `json:"panels,omitempty"`
	Settings     []wireSetting `json:"settings,omitempty"`
}

type wireProtocol struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
}

type wireRequires struct {
	Commands []string `json:"commands,omitempty"`
}

type wireEntry struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}

type wireWidget struct {
	ID       string        `json:"id"`
	Label    string        `json:"label,omitempty"`
	Settings []wireSetting `json:"settings,omitempty"`
}

type wirePanel struct {
	ID        string `json:"id"`
	Label     string `json:"label,omitempty"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Placement string `json:"placement"`
}

type wireSetting struct {
	Key         string           `json:"key"`
	Type        string           `json:"type"`
	Label       string           `json:"label"`
	Description string           `json:"description,omitempty"`
	Default     json.RawMessage  `json:"default,omitempty"`
	Min         *float64         `json:"min,omitempty"`
	Max         *float64         `json:"max,omitempty"`
	Options     []wireOption     `json:"options,omitempty"`
	VisibleWhen *wireVisibleWhen `json:"visible_when,omitempty"`
}

type wireOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type wireVisibleWhen struct {
	Key    string          `json:"key"`
	Equals json.RawMessage `json:"equals"`
}

var (
	// commandPattern is a bare command name; a dependency is looked up on
	// PATH, never executed from a path the manifest chose.
	commandPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	colorPattern   = regexp.MustCompile(`^#[0-9a-fA-F]{6}([0-9a-fA-F]{2})?$`)
)

// LoadManifest reads and validates the manifest in one plugin directory.
//
// It returns a manifest only when the declaration is well formed and the
// declared executable is a regular executable file inside that directory. A
// declared dependency that is missing from PATH is not a rejection: the
// manager keeps such a plugin visible and stopped so a user can see what to
// install.
func LoadManifest(dir string) (Manifest, error) {
	path := filepath.Join(dir, ManifestName)
	f, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("plugin: open %s: %w", path, err)
	}
	defer f.Close()

	// Read one byte past the ceiling so an oversized file is a diagnosable
	// rejection rather than a truncated parse.
	data, err := io.ReadAll(io.LimitReader(f, MaxManifestBytes+1))
	if err != nil {
		return Manifest{}, fmt.Errorf("plugin: read %s: %w", path, err)
	}
	if len(data) > MaxManifestBytes {
		return Manifest{}, fmt.Errorf("plugin: %s is larger than the %d byte limit", path, MaxManifestBytes)
	}

	m, err := ParseManifest(data)
	if err != nil {
		return Manifest{}, fmt.Errorf("plugin: %s: %w", path, err)
	}
	m.Dir = dir
	if m.ExecPath, err = resolveExec(dir, m.Exec); err != nil {
		return Manifest{}, fmt.Errorf("plugin: %s: %w", path, err)
	}
	return m, nil
}

// ParseManifest validates manifest bytes without touching the filesystem. It
// is the decoder the committed JSON fixtures run through, so a compatibility
// test proves the wire rather than a Go struct round trip.
func ParseManifest(data []byte) (Manifest, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var w wireManifest
	if err := dec.Decode(&w); err != nil {
		return Manifest{}, err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("more than one JSON value")
		}
		return Manifest{}, err
	}
	return validateManifest(w)
}

func validateManifest(w wireManifest) (Manifest, error) {
	if w.Schema != 1 {
		return Manifest{}, fmt.Errorf("schema %d is not supported; this shell reads schema 1", w.Schema)
	}
	if !v1.ValidPluginID(w.ID) {
		return Manifest{}, fmt.Errorf("id %q is not a reverse-domain identifier of at most %d bytes", w.ID, v1.MaxPluginIDBytes)
	}
	if err := text("name", w.Name, maxLabelBytes, true); err != nil {
		return Manifest{}, err
	}
	if err := text("version", w.Version, maxLabelBytes, true); err != nil {
		return Manifest{}, err
	}
	if err := text("author", w.Author, maxLabelBytes, false); err != nil {
		return Manifest{}, err
	}
	if err := text("description", w.Description, maxTextBytes, false); err != nil {
		return Manifest{}, err
	}
	if w.Protocol.Major != 1 {
		return Manifest{}, fmt.Errorf("protocol major %d is not supported; this shell speaks major 1", w.Protocol.Major)
	}
	if w.Protocol.Minor < 0 {
		return Manifest{}, fmt.Errorf("protocol minor %d is negative", w.Protocol.Minor)
	}

	m := Manifest{
		Schema:      w.Schema,
		ID:          w.ID,
		Name:        w.Name,
		Version:     w.Version,
		Author:      w.Author,
		Description: w.Description,
		Protocol:    v1.Version{Major: w.Protocol.Major, Minor: w.Protocol.Minor},
		Exec:        w.Exec,
	}

	var err error
	if m.Capabilities, err = capabilities(w.Capabilities); err != nil {
		return Manifest{}, err
	}
	if m.Requires, err = requires(w.Requires); err != nil {
		return Manifest{}, err
	}
	if m.Services, err = services(w.Services); err != nil {
		return Manifest{}, err
	}
	if m.Widgets, err = widgets(w.Widgets); err != nil {
		return Manifest{}, err
	}
	if m.Panels, err = panels(w.Panels); err != nil {
		return Manifest{}, err
	}
	if m.Settings, err = settings(w.Settings, "settings"); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// text checks a free-form label or paragraph.
func text(field, value string, max int, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is empty", field)
	}
	if len(value) > max {
		return fmt.Errorf("%s is %d bytes, more than the %d allowed", field, len(value), max)
	}
	return nil
}

func capabilities(names []string) ([]Capability, error) {
	out := make([]Capability, 0, len(names))
	seen := make(map[Capability]bool, len(names))
	for _, name := range names {
		c := Capability(name)
		if !knownCapabilities[c] {
			return nil, fmt.Errorf("capability %q is not one this shell offers", name)
		}
		if seen[c] {
			return nil, fmt.Errorf("capability %q is declared twice", name)
		}
		seen[c] = true
		out = append(out, c)
	}
	return out, nil
}

func requires(w *wireRequires) ([]string, error) {
	if w == nil {
		return nil, nil
	}
	if len(w.Commands) > MaxRequires {
		return nil, fmt.Errorf("requires.commands lists %d entries, more than the %d allowed", len(w.Commands), MaxRequires)
	}
	seen := make(map[string]bool, len(w.Commands))
	out := make([]string, 0, len(w.Commands))
	for _, cmd := range w.Commands {
		if !commandPattern.MatchString(cmd) || len(cmd) > maxIDBytes {
			return nil, fmt.Errorf("requires.commands: %q is not a bare command name", cmd)
		}
		if seen[cmd] {
			return nil, fmt.Errorf("requires.commands: %q is declared twice", cmd)
		}
		seen[cmd] = true
		out = append(out, cmd)
	}
	return out, nil
}

// entryIDs checks one list's identifiers and count in a single place, since
// services, widgets, and panels share those rules.
func entryIDs(field string, count int, id func(int) string) error {
	if count > MaxEntries {
		return fmt.Errorf("%s lists %d entries, more than the %d allowed", field, count, MaxEntries)
	}
	seen := make(map[string]bool, count)
	for i := 0; i < count; i++ {
		v := id(i)
		if !v1.ValidEntryID(v) {
			return fmt.Errorf("%s[%d]: id %q is not an identifier of at most %d bytes", field, i, v, v1.MaxEntryIDBytes)
		}
		if seen[v] {
			return fmt.Errorf("%s: id %q is declared twice", field, v)
		}
		seen[v] = true
	}
	return nil
}

func services(w []wireEntry) ([]Service, error) {
	if err := entryIDs("services", len(w), func(i int) string { return w[i].ID }); err != nil {
		return nil, err
	}
	out := make([]Service, len(w))
	for i, e := range w {
		if err := text(fmt.Sprintf("services[%d].label", i), e.Label, maxLabelBytes, false); err != nil {
			return nil, err
		}
		out[i] = Service{ID: e.ID, Label: e.Label}
	}
	return out, nil
}

func widgets(w []wireWidget) ([]Widget, error) {
	if err := entryIDs("widgets", len(w), func(i int) string { return w[i].ID }); err != nil {
		return nil, err
	}
	out := make([]Widget, len(w))
	for i, e := range w {
		if err := text(fmt.Sprintf("widgets[%d].label", i), e.Label, maxLabelBytes, false); err != nil {
			return nil, err
		}
		s, err := settings(e.Settings, fmt.Sprintf("widgets[%d].settings", i))
		if err != nil {
			return nil, err
		}
		out[i] = Widget{ID: e.ID, Label: e.Label, Settings: s}
	}
	return out, nil
}

func panels(w []wirePanel) ([]Panel, error) {
	if err := entryIDs("panels", len(w), func(i int) string { return w[i].ID }); err != nil {
		return nil, err
	}
	out := make([]Panel, len(w))
	for i, e := range w {
		if err := text(fmt.Sprintf("panels[%d].label", i), e.Label, maxLabelBytes, false); err != nil {
			return nil, err
		}
		if Placement(e.Placement) != PlacementAttached {
			return nil, fmt.Errorf("panels[%d]: placement %q is not one this shell supports", i, e.Placement)
		}
		for _, d := range []struct {
			what string
			v    int
		}{{"width", e.Width}, {"height", e.Height}} {
			if d.v < MinPanelExtent || d.v > MaxPanelExtent {
				return nil, fmt.Errorf("panels[%d]: %s %d is outside %d through %d",
					i, d.what, d.v, MinPanelExtent, MaxPanelExtent)
			}
		}
		out[i] = Panel{ID: e.ID, Label: e.Label, Width: e.Width, Height: e.Height, Placement: PlacementAttached}
	}
	return out, nil
}

func settings(w []wireSetting, field string) ([]Setting, error) {
	if len(w) > MaxSettings {
		return nil, fmt.Errorf("%s lists %d entries, more than the %d allowed", field, len(w), MaxSettings)
	}
	keys := make(map[string]SettingType, len(w))
	out := make([]Setting, 0, len(w))
	for i, e := range w {
		path := fmt.Sprintf("%s[%d]", field, i)
		s, err := setting(e, path)
		if err != nil {
			return nil, err
		}
		if _, dup := keys[s.Key]; dup {
			return nil, fmt.Errorf("%s: key %q is declared twice", field, s.Key)
		}
		keys[s.Key] = s.Type
		out = append(out, s)
	}
	// visible_when is resolved after the whole schema is known, so a setting
	// may depend on one declared later. The comparison value is re-read from
	// the wire because it has to be checked against the type of the setting it
	// names, which was not known when this setting was decoded.
	for i, s := range out {
		if s.VisibleWhen == nil {
			continue
		}
		if s.VisibleWhen.Key == s.Key {
			return nil, fmt.Errorf("%s: %q is visible_when itself", field, s.Key)
		}
		dep, ok := keys[s.VisibleWhen.Key]
		if !ok {
			return nil, fmt.Errorf("%s: %q is visible_when %q, which is not declared", field, s.Key, s.VisibleWhen.Key)
		}
		if err := valueOfType(w[i].VisibleWhen.Equals, dep); err != nil {
			return nil, fmt.Errorf("%s: %q visible_when %q: %w", field, s.Key, s.VisibleWhen.Key, err)
		}
	}
	return out, nil
}

func setting(e wireSetting, path string) (Setting, error) {
	if !v1.ValidEntryID(e.Key) {
		return Setting{}, fmt.Errorf("%s: key %q is not an identifier", path, e.Key)
	}
	t := SettingType(e.Type)
	if !knownSettingTypes[t] {
		return Setting{}, fmt.Errorf("%s: type %q is not one this shell can render", path, e.Type)
	}
	if err := text(path+".label", e.Label, maxLabelBytes, true); err != nil {
		return Setting{}, err
	}
	if err := text(path+".description", e.Description, maxTextBytes, false); err != nil {
		return Setting{}, err
	}

	s := Setting{Key: e.Key, Type: t, Label: e.Label, Description: e.Description, Min: e.Min, Max: e.Max}

	if !t.numeric() && (e.Min != nil || e.Max != nil) {
		return Setting{}, fmt.Errorf("%s: a %s setting has no numeric bounds", path, t)
	}
	if e.Min != nil && e.Max != nil && *e.Min > *e.Max {
		return Setting{}, fmt.Errorf("%s: min %v is above max %v", path, *e.Min, *e.Max)
	}

	if t != SettingSelect && len(e.Options) > 0 {
		return Setting{}, fmt.Errorf("%s: a %s setting has no options", path, t)
	}
	if t == SettingSelect {
		if len(e.Options) == 0 {
			return Setting{}, fmt.Errorf("%s: a select setting needs options", path)
		}
		if len(e.Options) > MaxSelectOptions {
			return Setting{}, fmt.Errorf("%s: %d options, more than the %d allowed", path, len(e.Options), MaxSelectOptions)
		}
		seen := make(map[string]bool, len(e.Options))
		for j, o := range e.Options {
			if o.Value == "" || len(o.Value) > maxLabelBytes {
				return Setting{}, fmt.Errorf("%s.options[%d]: value %q is empty or too long", path, j, o.Value)
			}
			if err := text(fmt.Sprintf("%s.options[%d].label", path, j), o.Label, maxLabelBytes, true); err != nil {
				return Setting{}, err
			}
			if seen[o.Value] {
				return Setting{}, fmt.Errorf("%s.options: value %q is declared twice", path, o.Value)
			}
			seen[o.Value] = true
			s.Options = append(s.Options, SettingOption{Value: o.Value, Label: o.Label})
		}
	}

	if len(e.Default) > 0 {
		if err := valueOfType(e.Default, t); err != nil {
			return Setting{}, fmt.Errorf("%s.default: %w", path, err)
		}
		def, err := decodeValue(e.Default, t)
		if err != nil {
			return Setting{}, fmt.Errorf("%s.default: %w", path, err)
		}
		if err := s.inRange(def); err != nil {
			return Setting{}, fmt.Errorf("%s.default: %w", path, err)
		}
		if t == SettingSelect && !s.hasOption(def.(string)) {
			return Setting{}, fmt.Errorf("%s.default: %q is not one of the options", path, def)
		}
		s.Default = def
	}

	if e.VisibleWhen != nil {
		if !v1.ValidEntryID(e.VisibleWhen.Key) {
			return Setting{}, fmt.Errorf("%s.visible_when.key %q is not an identifier", path, e.VisibleWhen.Key)
		}
		s.VisibleWhen = &VisibleWhen{Key: e.VisibleWhen.Key}
		var equals any
		if err := json.Unmarshal(e.VisibleWhen.Equals, &equals); err != nil {
			return Setting{}, fmt.Errorf("%s.visible_when.equals: %w", path, err)
		}
		s.VisibleWhen.Equals = equals
	}
	return s, nil
}

// inRange checks a value against the setting's declared bounds.
func (s Setting) inRange(v any) error {
	f, ok := numberOf(v)
	if !ok {
		return nil
	}
	if s.Min != nil && f < *s.Min {
		return fmt.Errorf("%v is below the declared minimum %v", v, *s.Min)
	}
	if s.Max != nil && f > *s.Max {
		return fmt.Errorf("%v is above the declared maximum %v", v, *s.Max)
	}
	return nil
}

func (s Setting) hasOption(value string) bool {
	for _, o := range s.Options {
		if o.Value == value {
			return true
		}
	}
	return false
}

func numberOf(v any) (float64, bool) {
	switch n := v.(type) {
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// valueOfType reports whether raw JSON matches a declared setting type.
func valueOfType(raw json.RawMessage, t SettingType) error {
	_, err := decodeValue(raw, t)
	return err
}

// decodeValue converts raw JSON into the Go value the shell stores for a
// setting of this type. Integers decode as int64 rather than float64 so that a
// count never round-trips through a fraction.
func decodeValue(raw json.RawMessage, t SettingType) (any, error) {
	if len(raw) == 0 {
		return nil, errors.New("no value")
	}
	switch t {
	case SettingBool:
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, fmt.Errorf("expected a boolean")
		}
		return b, nil
	case SettingInt:
		var n int64
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&n); err != nil {
			return nil, fmt.Errorf("expected a whole number")
		}
		return n, nil
	case SettingFloat:
		var f float64
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("expected a number")
		}
		return f, nil
	case SettingString, SettingSelect, SettingFile, SettingFolder:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("expected a string")
		}
		if len(s) > maxTextBytes {
			return nil, fmt.Errorf("string is longer than %d bytes", maxTextBytes)
		}
		return s, nil
	case SettingColor:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("expected a colour string")
		}
		if !colorPattern.MatchString(s) {
			return nil, fmt.Errorf("%q is not #RRGGBB or #RRGGBBAA", s)
		}
		return s, nil
	}
	return nil, fmt.Errorf("unknown setting type %q", t)
}

// resolveExec proves the declared entry point is a regular executable inside
// the plugin directory.
//
// Containment is checked after symbolic links are resolved, not only on the
// path text: a plugin directory is user-writable, so a relative name that
// looks contained can still point at an executable the packager never shipped.
func resolveExec(dir, rel string) (string, error) {
	if rel == "" {
		return "", errors.New("exec is empty")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("exec %q is absolute; it must name a file inside the plugin directory", rel)
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("exec %q leaves the plugin directory", rel)
	}

	full := filepath.Join(dir, clean)
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolve plugin directory: %w", err)
	}
	realExec, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", fmt.Errorf("exec %q: %w", rel, err)
	}
	within, err := filepath.Rel(realDir, realExec)
	if err != nil {
		return "", fmt.Errorf("exec %q: %w", rel, err)
	}
	if within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("exec %q resolves to %s, outside the plugin directory", rel, realExec)
	}

	info, err := os.Stat(realExec)
	if err != nil {
		return "", fmt.Errorf("exec %q: %w", rel, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("exec %q is not a regular file", rel)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("exec %q is not executable", rel)
	}
	return full, nil
}
