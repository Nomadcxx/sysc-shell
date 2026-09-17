package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// Sub-project B gives every bar widget a stable identity, not just plugin
// placements. Until now resolveItem refused `instance` on anything but a
// plugin, so two clocks on one bar were indistinguishable and a per-widget
// option written through the settings surface reached every widget of that
// type.

func TestInstanceIsAcceptedOnABuiltInItem(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"bar": {"items": {"left": [
		{"id": "clock", "instance": "clock-1", "format": "15:04"},
		{"id": "clock", "instance": "clock-2", "format": "Mon 2 Jan"}
	]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Bar.Left) != 2 {
		t.Fatalf("left = %d items, want 2", len(cfg.Bar.Left))
	}
	if got := cfg.Bar.Left[0].Instance; got != "clock-1" {
		t.Errorf("first clock instance = %q, want clock-1", got)
	}
	if got := cfg.Bar.Left[1].Instance; got != "clock-2" {
		t.Errorf("second clock instance = %q, want clock-2", got)
	}
	// Identity is added beside the typed options, not in place of them: D4
	// keeps resolveItem's per-id validation, which an instance-keyed map would
	// have discarded.
	if got := cfg.Bar.Left[0].Format; got != "15:04" {
		t.Errorf("first clock format = %q, want 15:04", got)
	}
}

// The misplaced-field loop keeps two members. Narrowing it must not stop it
// rejecting them, or a plugin field on a built-in item would be silently
// ignored rather than named.
func TestPluginAndEntryAreStillRefusedOnABuiltInItem(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, doc, field string }{
		{"plugin", `{"id": "clock", "plugin": "org.sysc.timer"}`, "plugin"},
		{"entry", `{"id": "clock", "entry": "bar"}`, "entry"},
	} {
		_, err := Parse([]byte(`{"bar": {"items": {"left": [` + tc.doc + `]}}}`))
		if err == nil {
			t.Fatalf("%s: Parse succeeded, want a refusal", tc.name)
		}
		if !strings.Contains(err.Error(), "only on a plugin placement") {
			t.Errorf("%s: error %q does not name the plugin-placement rule", tc.name, err)
		}
		if !strings.Contains(err.Error(), tc.field) {
			t.Errorf("%s: error %q does not name the field", tc.name, err)
		}
	}
}

// A group is draggable, inspectable and addressable under D2, and a
// structurally anonymous group cannot be any of those.
func TestInstanceIsAcceptedOnAGroup(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"bar": {"items": {"right": [
		{"id": "group", "instance": "group-1", "items": [{"id": "cpu"}, {"id": "memory"}]}
	]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Bar.Right) != 1 {
		t.Fatalf("right = %d items, want 1", len(cfg.Bar.Right))
	}
	g := cfg.Bar.Right[0]
	if g.ID != "group" || g.Instance != "group-1" {
		t.Errorf("group = %+v, want id group with instance group-1", g)
	}
	if len(g.Items) != 2 {
		t.Errorf("group holds %d members, want 2", len(g.Items))
	}
}

// D3 mints ids lazily, so a document that never named one still has to load.
func TestAGroupWithoutAnIdStillLoads(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"bar": {"items": {"right": [
		{"id": "group", "items": [{"id": "cpu"}]}
	]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := cfg.Bar.Right[0].Instance; got != "" {
		t.Errorf("instance = %q, want empty on a group that never named one", got)
	}
}

// Both group invariants D5 relies on are enforced by the loader today. The
// editor must refuse at the drop rather than lean on these, but they are the
// backstop and widening the schema must not weaken them.
func TestGroupInvariantsSurviveTheSchemaChange(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, doc, want string }{
		{
			"a group may not nest",
			`{"id": "group", "instance": "g1", "items": [{"id": "group", "items": [{"id": "cpu"}]}]}`,
			"a group may not contain a group",
		},
		{
			"a group needs a member",
			`{"id": "group", "instance": "g1", "items": []}`,
			"a group needs at least one item",
		},
	} {
		_, err := Parse([]byte(`{"bar": {"items": {"right": [` + tc.doc + `]}}}`))
		if err == nil {
			t.Fatalf("%s: Parse succeeded, want a refusal", tc.name)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %q, want it to say %q", tc.name, err, tc.want)
		}
	}
}

// An instance id is validated by the same rule a plugin placement's is, so
// the vocabulary stays one vocabulary.
func TestABuiltInInstanceIdIsValidated(t *testing.T) {
	t.Parallel()
	_, err := Parse([]byte(`{"bar": {"items": {"left": [{"id": "clock", "instance": "not a valid id"}]}}}`))
	if err == nil {
		t.Fatal("Parse accepted a malformed instance id, want a refusal")
	}
	if !strings.Contains(err.Error(), "instance") {
		t.Errorf("error %q does not name the instance field", err)
	}
}

// An id that does not survive a write is inert: the editor mints one, the file
// does not carry it, and the next load hands back anonymous widgets. This is
// the round-trip guard the settings foundation's enum work proved the value of,
// applied to the thing B actually changes.
func TestInstanceIdsSurviveAWriteAndReload(t *testing.T) {
	t.Parallel()
	src, err := Parse([]byte(`{"bar": {"items": {"left": [
		{"id": "clock", "instance": "clock-1", "format": "15:04"},
		{"id": "group", "instance": "group-1", "items": [
			{"id": "cpu", "instance": "cpu-1"},
			{"id": "memory"}
		]}
	]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, src); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(got.Bar.Left) != 2 {
		t.Fatalf("left = %d items, want 2", len(got.Bar.Left))
	}
	if id := got.Bar.Left[0].Instance; id != "clock-1" {
		t.Errorf("clock instance = %q after a round trip, want clock-1", id)
	}
	group := got.Bar.Left[1]
	if group.Instance != "group-1" {
		t.Errorf("group instance = %q after a round trip, want group-1", group.Instance)
	}
	if len(group.Items) != 2 {
		t.Fatalf("group holds %d members after a round trip, want 2", len(group.Items))
	}
	if id := group.Items[0].Instance; id != "cpu-1" {
		t.Errorf("nested cpu instance = %q after a round trip, want cpu-1", id)
	}
	// D3: a widget nobody addressed stays anonymous. Writing must not invent
	// an id for it, or every file grows ids for widgets the user never touched.
	if id := group.Items[1].Instance; id != "" {
		t.Errorf("unaddressed memory widget gained instance %q, want none", id)
	}
}
