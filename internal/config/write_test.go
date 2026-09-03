package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestWriteAtomicRoundTrip(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "config.json")
	c := Default()
	c.Bar.Height = 42
	if err := Write(p, c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bar.Height != 42 {
		t.Fatalf("height = %d, want 42", got.Bar.Height)
	}
	if got.Bar.Gap != Default().Bar.Gap {
		t.Fatalf("gap = %d, want the default to survive", got.Bar.Gap)
	}
}

func TestWriteLeavesNoTempOnSuccess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := Write(p, Default()); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if matched, _ := filepath.Match(".config-*.tmp", e.Name()); matched {
			t.Fatalf("left temp %s", e.Name())
		}
	}
}

func TestWriteUsesPrivatePermissions(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := Write(p, Default()); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if mode := st.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode = %o, want 0600", mode)
	}
}

func TestWriteFailureKeepsOriginalAndCleansTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	orig := Default()
	orig.Bar.Height = 48
	if err := Write(p, orig); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}

	old := atomicReplace
	t.Cleanup(func() { atomicReplace = old })
	atomicReplace = func(string, string) error {
		return os.ErrPermission
	}

	next := Default()
	next.Bar.Height = 42
	if err := Write(p, next); err == nil {
		t.Fatal("Write succeeded, want failure")
	}
	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("failure replaced the original")
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if matched, _ := filepath.Match(".config-*.tmp", e.Name()); matched {
			t.Fatalf("left temp %s", e.Name())
		}
	}
}

func TestWriteOmitsDefaultFields(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "config.json")
	c := Default()
	c.Bar.Height = 42
	if err := Write(p, c); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["weather"]; ok {
		t.Fatal("wrote default weather")
	}
	var bar map[string]any
	if err := json.Unmarshal(m["bar"], &bar); err != nil {
		t.Fatal(err)
	}
	if _, ok := bar["gap"]; ok {
		t.Fatal("wrote default gap")
	}
	if bar["height"] != float64(42) {
		t.Fatalf("height field = %v", bar["height"])
	}
}

func TestWriteRoundTripsGroupMembers(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "config.json")
	c := Default()
	c.Bar.Right = []Item{
		{ID: "plugin", Plugin: "org.sysc.screen-recorder", Entry: "bar", Instance: "rec-1"},
		{ID: "group", Items: []Item{
			{ID: "cpu", Display: "text"},
			{ID: "memory", Display: "text"},
		}},
	}
	if err := Write(p, c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bar.Right) != 2 || got.Bar.Right[1].ID != "group" {
		t.Fatalf("right = %#v", got.Bar.Right)
	}
	if n := len(got.Bar.Right[1].Items); n != 2 {
		t.Fatalf("group members = %d, want 2 (empty group is unloadable)", n)
	}
	if got.Bar.Right[1].Items[0].ID != "cpu" || got.Bar.Right[1].Items[1].ID != "memory" {
		t.Fatalf("group members = %#v", got.Bar.Right[1].Items)
	}
}

func TestThemeRoundTripIsStable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		doc  string
	}{
		{"defaults", `{}`},
		{"preset only", `{"theme":{"preset":"expressive"}}`},
		{"preset and deviations", `{"theme":{"preset":"compact","radius":20,"font-scale":125}}`},
		{"axes without a preset", `{"theme":{"density":"comfortable","elevation":"none"}}`},
		{"bar override", `{"theme":{"density":"compact"},"bar":{"height":52}}`},
		{"old file", `{"theme":{"radius":16},"bar":{"padding":10}}`},
		{"output override", `{"bar":{"height":52},"outputs":[{"connector":"DP-1","bar":{"height":60}}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			first, err := Parse([]byte(tc.doc))
			if err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(t.TempDir(), "config.json")
			if err := Write(p, first); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			second, err := Parse(data)
			if err != nil {
				t.Fatalf("re-parsing what we wrote failed: %v\n%s", err, data)
			}
			if second.Theme != first.Theme {
				t.Errorf("theme changed across a round trip:\n got %+v\nwant %+v",
					second.Theme, first.Theme)
			}
			if !barsEquivalent(second.Bar, first.Bar) {
				t.Errorf("bar changed across a round trip:\n got %+v\nwant %+v",
					second.Bar, first.Bar)
			}
			// A second write must produce the same bytes as the first.
			q := filepath.Join(t.TempDir(), "config.json")
			if err := Write(q, second); err != nil {
				t.Fatal(err)
			}
			again, err := os.ReadFile(q)
			if err != nil {
				t.Fatal(err)
			}
			if string(again) != string(data) {
				t.Errorf("writing twice produced different documents:\n%s\n---\n%s", data, again)
			}
		})
	}
}

func barsEquivalent(a, b Bar) bool {
	return a.Enabled == b.Enabled && a.Edge == b.Edge && a.Height == b.Height &&
		a.Gap == b.Gap && a.Padding == b.Padding && a.Spacing == b.Spacing &&
		a.Radius == b.Radius && a.FontFamily == b.FontFamily && a.FontSize == b.FontSize
}

func TestThemeSparseWriteRecordsOnlyDeviations(t *testing.T) {
	t.Parallel()
	// A preset with no deviations writes the preset and nothing else.
	c, err := Parse([]byte(`{"theme":{"preset":"expressive"}}`))
	if err != nil {
		t.Fatal(err)
	}
	w := toWire(c)
	if w.Theme == nil || w.Theme.Preset == nil || *w.Theme.Preset != "expressive" {
		t.Fatalf("theme block = %+v, want the preset recorded", w.Theme)
	}
	if w.Theme.Radius != nil {
		t.Errorf("radius %d was written even though it is the preset's own value",
			*w.Theme.Radius)
	}
	if w.Theme.Motion != nil {
		t.Errorf("motion %q was written even though it is the preset's own value",
			*w.Theme.Motion)
	}
	if w.Theme.PanelOpacity != nil {
		t.Errorf("panel opacity %d was written even though it is the preset's own value",
			*w.Theme.PanelOpacity)
	}
	// The bar it derives is not an override either.
	if w.Bar != nil {
		t.Errorf("bar block = %+v, want nothing: every value is derived", w.Bar)
	}
}

func TestThemeSparseWriteRecordsADeviation(t *testing.T) {
	t.Parallel()
	c, err := Parse([]byte(`{"theme":{"preset":"compact","radius":20}}`))
	if err != nil {
		t.Fatal(err)
	}
	w := toWire(c)
	if w.Theme == nil || w.Theme.Radius == nil || *w.Theme.Radius != 20 {
		t.Fatalf("radius deviation was not recorded: %+v", w.Theme)
	}
	if w.Theme.Density != nil {
		t.Errorf("density %q was written even though compact supplies it", *w.Theme.Density)
	}
}

func TestThemeDefaultWritesNoThemeBlock(t *testing.T) {
	t.Parallel()
	if w := toWire(Default()); w.Theme != nil {
		t.Errorf("theme block = %+v, want nothing for the default composition", w.Theme)
	}
}

func TestThemePresetChangeDoesNotPinTheDerivedBar(t *testing.T) {
	t.Parallel()
	// Switching to compact moves the derived bar to 40 px. That derived value
	// must not be recorded as an explicit override, or a later preset change
	// would leave the bar stuck at the old height.
	c, err := Parse([]byte(`{"theme":{"preset":"compact"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Bar.Height != 40 {
		t.Fatalf("derived height = %d, want compact's 40", c.Bar.Height)
	}
	if w := toWire(c); w.Bar != nil {
		t.Errorf("bar block = %+v, want nothing: the height is derived", w.Bar)
	}
}

func TestThemeRebasePreservesAnExplicitDeviation(t *testing.T) {
	t.Parallel()
	// Standard with a user-chosen comfortable density and font scale.
	c, err := Parse([]byte(`{"theme":{"density":"comfortable","font-scale":125}}`))
	if err != nil {
		t.Fatal(err)
	}
	from, _ := theme.PresetComposition(c.Theme.Preset)
	to, _ := theme.PresetComposition(theme.PresetExpressive)
	c.Theme.Composition = theme.Rebase(c.Theme.Composition, from, to)
	c.Theme.Preset = theme.PresetExpressive
	c.Bar = deriveBar(Default().Bar, c.Theme)

	if c.Theme.Density != theme.DensityComfortable {
		t.Errorf("density = %q, want the deviation to survive", c.Theme.Density)
	}
	if c.Theme.FontScale != 125 {
		t.Errorf("font scale = %d, want the deviation to survive", c.Theme.FontScale)
	}
	if c.Theme.Radius != to.Radius {
		t.Errorf("radius = %d, want expressive's %d", c.Theme.Radius, to.Radius)
	}

	// The rewritten document keeps exactly those two deviations.
	w := toWire(c)
	if w.Theme == nil || w.Theme.Density == nil || w.Theme.FontScale == nil {
		t.Fatalf("theme block = %+v, want the two deviations", w.Theme)
	}
	if w.Theme.Radius != nil {
		t.Errorf("radius was recorded as a deviation from its own preset")
	}
}
