package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// timerManifest is the manifest printed in the milestone design. It is the
// compatibility fixture: if this stops loading, version one has moved.
const timerManifest = `{
  "schema": 1,
  "id": "org.sysc.timer",
  "name": "Timer",
  "version": "1.0.0",
  "protocol": {"major": 1, "minor": 0},
  "exec": "bin/sysc-plugin-timer",
  "capabilities": ["notifications", "panels", "settings", "state"],
  "requires": {"commands": []},
  "services": [{"id": "timer"}],
  "widgets": [{"id": "bar", "settings": []}],
  "panels": [{"id": "panel", "width": 320, "height": 280,
               "placement": "attached"}],
  "settings": []
}`

// writePlugin lays out a plugin directory holding the given manifest and an
// executable at the given relative path. An empty exec path writes no file.
func writePlugin(t *testing.T, manifest, execRel string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if execRel != "" {
		writeExec(t, filepath.Join(dir, execRel), 0o755)
	}
	return dir
}

func writeExec(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
}

// edit returns the design manifest with one top-level field replaced.
func edit(t *testing.T, key string, value any) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(timerManifest), &m); err != nil {
		t.Fatal(err)
	}
	if value == nil {
		delete(m, key)
	} else {
		m[key] = value
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestLoadManifestAcceptsTheDesignFixture(t *testing.T) {
	t.Parallel()

	dir := writePlugin(t, timerManifest, "bin/sysc-plugin-timer")
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.ID != "org.sysc.timer" || m.Name != "Timer" || m.Version != "1.0.0" {
		t.Errorf("identity = %q %q %q", m.ID, m.Name, m.Version)
	}
	if m.Protocol.Major != 1 || m.Protocol.Minor != 0 {
		t.Errorf("protocol = %+v, want 1.0", m.Protocol)
	}
	if want := filepath.Join(dir, "bin/sysc-plugin-timer"); m.ExecPath != want {
		t.Errorf("exec = %q, want %q", m.ExecPath, want)
	}
	if len(m.Widgets) != 1 || m.Widgets[0].ID != "bar" {
		t.Errorf("widgets = %+v", m.Widgets)
	}
	if len(m.Panels) != 1 || m.Panels[0].Width != 320 || m.Panels[0].Height != 280 {
		t.Errorf("panels = %+v", m.Panels)
	}
	if !m.Grants(CapNotifications) || !m.Grants(CapState) {
		t.Errorf("capabilities = %v", m.Capabilities)
	}
}

// loadErr is LoadManifest with the manifest discarded, for the many cases that
// only assert on the rejection.
func loadErr(t *testing.T, dir string) error {
	t.Helper()
	_, err := LoadManifest(dir)
	return err
}

func TestLoadManifestRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	dir := writePlugin(t, edit(t, "sandbox", true), "bin/sysc-plugin-timer")
	err := loadErr(t, dir)
	if err == nil {
		t.Fatal("LoadManifest accepted an unknown field")
	}
	if !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("err = %v, want it to name the field", err)
	}
}

func TestLoadManifestRejectsAnUnsupportedSchema(t *testing.T) {
	t.Parallel()

	for _, schema := range []any{0, 2, "1"} {
		dir := writePlugin(t, edit(t, "schema", schema), "bin/sysc-plugin-timer")
		if err := loadErr(t, dir); err == nil {
			t.Errorf("schema %v accepted", schema)
		}
	}
}

func TestLoadManifestRejectsBadIDs(t *testing.T) {
	t.Parallel()

	bad := []string{
		"", "a", "UPPER.CASE", "org.sysc..timer", ".leading", "trailing.",
		"has space", "has/slash", "../escape", strings.Repeat("a", 129),
	}
	for _, id := range bad {
		dir := writePlugin(t, edit(t, "id", id), "bin/sysc-plugin-timer")
		if err := loadErr(t, dir); err == nil {
			t.Errorf("id %q accepted", id)
		}
	}
}

func TestLoadManifestRejectsDuplicateEntryIDs(t *testing.T) {
	t.Parallel()

	dup := edit(t, "widgets", []any{
		map[string]any{"id": "bar"},
		map[string]any{"id": "bar"},
	})
	dir := writePlugin(t, dup, "bin/sysc-plugin-timer")
	err := loadErr(t, dir)
	if err == nil {
		t.Fatal("two widgets with one id accepted")
	}
	if !strings.Contains(err.Error(), "bar") {
		t.Fatalf("err = %v, want it to name the duplicate", err)
	}
}

func TestLoadManifestRejectsAnExecutablePathThatEscapesTheDirectory(t *testing.T) {
	t.Parallel()

	for _, exec := range []string{"../outside", "/bin/sh", "bin/../../outside", "./../x"} {
		dir := writePlugin(t, edit(t, "exec", exec), "")
		if err := loadErr(t, dir); err == nil {
			t.Errorf("exec %q accepted", exec)
		}
	}
}

func TestLoadManifestRejectsASymlinkOutOfTheDirectory(t *testing.T) {
	t.Parallel()

	// Containment has to survive the filesystem, not only the path string: a
	// relative name can still resolve to an executable the packager never
	// shipped.
	outside := t.TempDir()
	target := filepath.Join(outside, "real")
	writeExec(t, target, 0o755)

	dir := writePlugin(t, timerManifest, "")
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "bin", "sysc-plugin-timer")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := loadErr(t, dir); err == nil {
		t.Fatal("a symlink out of the plugin directory was accepted")
	}
}

func TestLoadManifestRejectsANonExecutableEntry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(timerManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExec(t, filepath.Join(dir, "bin", "sysc-plugin-timer"), 0o644)
	if err := loadErr(t, dir); err == nil {
		t.Fatal("a non-executable entry was accepted")
	}
}

func TestLoadManifestRejectsAMissingEntry(t *testing.T) {
	t.Parallel()

	dir := writePlugin(t, timerManifest, "")
	if err := loadErr(t, dir); err == nil {
		t.Fatal("a missing entry was accepted")
	}
}

func TestLoadManifestRejectsADirectoryAsTheEntry(t *testing.T) {
	t.Parallel()

	dir := writePlugin(t, timerManifest, "")
	if err := os.MkdirAll(filepath.Join(dir, "bin", "sysc-plugin-timer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := loadErr(t, dir); err == nil {
		t.Fatal("a directory was accepted as the entry")
	}
}

func TestLoadManifestRejectsUnknownCapabilities(t *testing.T) {
	t.Parallel()

	dir := writePlugin(t, edit(t, "capabilities", []string{"notifications", "root"}), "bin/sysc-plugin-timer")
	err := loadErr(t, dir)
	if err == nil {
		t.Fatal("an unknown capability was accepted")
	}
	if !strings.Contains(err.Error(), "root") {
		t.Fatalf("err = %v, want it to name the capability", err)
	}
}

func TestLoadManifestRejectsDuplicateCapabilities(t *testing.T) {
	t.Parallel()

	dir := writePlugin(t, edit(t, "capabilities", []string{"state", "state"}), "bin/sysc-plugin-timer")
	if err := loadErr(t, dir); err == nil {
		t.Fatal("a duplicate capability was accepted")
	}
}

func TestLoadManifestValidPanelIncludeSettings(t *testing.T) {
	t.Parallel()

	withSettings := edit(t, "panels", []any{
		map[string]any{
			"id": "panel", "width": 320, "height": 280,
			"placement": "attached", "include_settings": true,
		},
	})
	dir := writePlugin(t, withSettings, "bin/sysc-plugin-timer")
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m.Panels) != 1 || !m.Panels[0].IncludeSettings {
		t.Errorf("panels = %+v, want IncludeSettings true", m.Panels)
	}

	// Omitted include_settings stays false (Weather and the design fixture).
	dir = writePlugin(t, timerManifest, "bin/sysc-plugin-timer")
	m, err = LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m.Panels) != 1 || m.Panels[0].IncludeSettings {
		t.Errorf("panels = %+v, want IncludeSettings false", m.Panels)
	}
}

func TestLoadReferenceRecorderManifest(t *testing.T) {
	t.Parallel()

	manifest, err := os.ReadFile(filepath.Join("..", "..", "plugins", "reference", "recorder", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := writePlugin(t, string(manifest), "bin/sysc-plugin-screen-recorder")
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m.Panels) != 1 || !m.Panels[0].IncludeSettings {
		t.Errorf("panels = %+v, want one panel with IncludeSettings true", m.Panels)
	}
	if !m.Grants(CapPanels) {
		t.Errorf("capabilities = %v, want panels", m.Capabilities)
	}
}

func TestLoadManifestRejectsInvalidPanelGeometry(t *testing.T) {
	t.Parallel()

	cases := []map[string]any{
		{"id": "p", "width": 0, "height": 280, "placement": "attached"},
		{"id": "p", "width": 320, "height": -1, "placement": "attached"},
		{"id": "p", "width": 100000, "height": 280, "placement": "attached"},
		{"id": "p", "width": 320, "height": 280, "placement": "floating"},
		{"id": "p", "width": 320, "height": 280},
	}
	for i, panel := range cases {
		dir := writePlugin(t, edit(t, "panels", []any{panel}), "bin/sysc-plugin-timer")
		if err := loadErr(t, dir); err == nil {
			t.Errorf("case %d: panel %+v accepted", i, panel)
		}
	}
}

func TestLoadManifestValidatesSettingSchemas(t *testing.T) {
	t.Parallel()

	good := []any{
		map[string]any{"key": "sound", "type": "bool", "label": "Play a sound", "default": true},
		map[string]any{"key": "minutes", "type": "int", "label": "Minutes", "default": 5, "min": 1, "max": 240},
		map[string]any{"key": "scale", "type": "float", "label": "Scale", "default": 1.5, "min": 0.5, "max": 4},
		map[string]any{"key": "label", "type": "string", "label": "Label", "default": "Timer"},
		map[string]any{"key": "units", "type": "select", "label": "Units", "default": "metric",
			"options": []any{
				map[string]any{"value": "metric", "label": "Metric"},
				map[string]any{"value": "imperial", "label": "Imperial"},
			}},
		map[string]any{"key": "tint", "type": "color", "label": "Tint", "default": "#ff8800"},
		map[string]any{"key": "sound_file", "type": "file", "label": "Sound"},
		map[string]any{"key": "notes_dir", "type": "folder", "label": "Notes"},
		map[string]any{"key": "warn_at", "type": "int", "label": "Warn at", "default": 30,
			"visible_when": map[string]any{"key": "sound", "equals": true}},
	}
	dir := writePlugin(t, edit(t, "settings", good), "bin/sysc-plugin-timer")
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	if len(m.Settings) != len(good) {
		t.Fatalf("settings = %d, want %d", len(m.Settings), len(good))
	}
}

func TestLoadManifestRejectsInvalidSettingSchemas(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		setting map[string]any
	}{
		{"unknown type", map[string]any{"key": "x", "type": "matrix", "label": "X"}},
		{"no key", map[string]any{"type": "bool", "label": "X"}},
		{"bad key", map[string]any{"key": "Has Space", "type": "bool", "label": "X"}},
		{"no label", map[string]any{"key": "x", "type": "bool"}},
		{"default of the wrong type", map[string]any{"key": "x", "type": "int", "label": "X", "default": "five"}},
		{"select with no options", map[string]any{"key": "x", "type": "select", "label": "X", "default": "a"}},
		{"select default outside options", map[string]any{"key": "x", "type": "select", "label": "X", "default": "c",
			"options": []any{map[string]any{"value": "a", "label": "A"}}}},
		{"bounds inverted", map[string]any{"key": "x", "type": "int", "label": "X", "default": 5, "min": 10, "max": 1}},
		{"default below min", map[string]any{"key": "x", "type": "int", "label": "X", "default": 0, "min": 1, "max": 10}},
		{"bounds on a string", map[string]any{"key": "x", "type": "string", "label": "X", "min": 1}},
		{"options on a bool", map[string]any{"key": "x", "type": "bool", "label": "X",
			"options": []any{map[string]any{"value": "a", "label": "A"}}}},
		{"bad colour default", map[string]any{"key": "x", "type": "color", "label": "X", "default": "orange"}},
		{"visible_when names nothing", map[string]any{"key": "x", "type": "bool", "label": "X",
			"visible_when": map[string]any{"key": "absent", "equals": true}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := writePlugin(t, edit(t, "settings", []any{c.setting}), "bin/sysc-plugin-timer")
			if err := loadErr(t, dir); err == nil {
				t.Fatalf("accepted a setting with %s", c.name)
			}
		})
	}
}

func TestLoadManifestRejectsDuplicateSettingKeys(t *testing.T) {
	t.Parallel()

	dup := []any{
		map[string]any{"key": "x", "type": "bool", "label": "One"},
		map[string]any{"key": "x", "type": "bool", "label": "Two"},
	}
	dir := writePlugin(t, edit(t, "settings", dup), "bin/sysc-plugin-timer")
	if err := loadErr(t, dir); err == nil {
		t.Fatal("a duplicate setting key was accepted")
	}
}

func TestLoadManifestRejectsAnOversizedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	padded := edit(t, "description", strings.Repeat("a", MaxManifestBytes))
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(padded), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExec(t, filepath.Join(dir, "bin", "sysc-plugin-timer"), 0o755)
	if err := loadErr(t, dir); err == nil {
		t.Fatal("an oversized manifest was accepted")
	}
}

func TestLoadManifestRejectsAnUnsupportedProtocolMajor(t *testing.T) {
	t.Parallel()

	dir := writePlugin(t, edit(t, "protocol", map[string]any{"major": 2, "minor": 0}), "bin/sysc-plugin-timer")
	if err := loadErr(t, dir); err == nil {
		t.Fatal("protocol major 2 accepted")
	}
}

func TestLoadManifestBoundsEntryCounts(t *testing.T) {
	t.Parallel()

	many := make([]any, MaxEntries+1)
	for i := range many {
		many[i] = map[string]any{"id": "w" + strings.Repeat("x", i%3) + itoa(i)}
	}
	dir := writePlugin(t, edit(t, "widgets", many), "bin/sysc-plugin-timer")
	if err := loadErr(t, dir); err == nil {
		t.Fatalf("more than %d widgets accepted", MaxEntries)
	}
}

// Not parallel: it replaces PATH for the process.
func TestMissingCommandsReportsAnAbsentDependency(t *testing.T) {
	empty := t.TempDir()
	t.Setenv("PATH", empty)

	withDep := edit(t, "requires", map[string]any{"commands": []string{"gpu-screen-recorder"}})
	dir := writePlugin(t, withDep, "bin/sysc-plugin-timer")
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("a missing dependency must not reject the manifest: %v", err)
	}
	missing := m.MissingCommands()
	if len(missing) != 1 || missing[0] != "gpu-screen-recorder" {
		t.Fatalf("missing = %v, want the one dependency", missing)
	}

	writeExec(t, filepath.Join(empty, "gpu-screen-recorder"), 0o755)
	if got := m.MissingCommands(); len(got) != 0 {
		t.Fatalf("missing = %v after the command appeared, want none", got)
	}
}

func TestLoadManifestRejectsBadDependencyNames(t *testing.T) {
	t.Parallel()

	for _, cmd := range []string{"", "/bin/sh", "../sh", "rm -rf", strings.Repeat("a", 300)} {
		dir := writePlugin(t, edit(t, "requires", map[string]any{"commands": []string{cmd}}), "bin/sysc-plugin-timer")
		if err := loadErr(t, dir); err == nil {
			t.Errorf("dependency %q accepted", cmd)
		}
	}
}

// itoa avoids importing strconv for one call in a fixture builder.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
