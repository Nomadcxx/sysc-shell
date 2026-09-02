package plugin

import "testing"

func TestCheckValuesAcceptsEachDeclaredType(t *testing.T) {
	t.Parallel()
	min, max := 1.0, 10.0
	schema := []Setting{
		{Key: "on", Type: SettingBool, Label: "On"},
		{Key: "count", Type: SettingInt, Label: "Count", Min: &min, Max: &max},
		{Key: "ratio", Type: SettingFloat, Label: "Ratio", Min: &min, Max: &max},
		{Key: "name", Type: SettingString, Label: "Name"},
		{Key: "mode", Type: SettingSelect, Label: "Mode", Options: []SettingOption{{Value: "a"}, {Value: "b"}}},
		{Key: "tint", Type: SettingColor, Label: "Tint"},
		{Key: "file", Type: SettingFile, Label: "File"},
		{Key: "dir", Type: SettingFolder, Label: "Folder"},
		{Key: "extra", Type: SettingString, Label: "Extra", VisibleWhen: &VisibleWhen{Key: "on", Equals: true}},
	}
	if err := CheckValues(schema, map[string]any{
		"on": true, "count": int64(3), "ratio": 2.5, "name": "tea",
		"mode": "a", "tint": "#aabbcc", "file": "/tmp/x", "dir": "/tmp", "extra": "hi",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSettingVisibleHonoursVisibleWhen(t *testing.T) {
	t.Parallel()
	s := Setting{
		Key: "extra", Type: SettingString, Label: "Extra",
		VisibleWhen: &VisibleWhen{Key: "on", Equals: true},
	}
	if SettingVisible(s, map[string]any{"on": false}) {
		t.Fatal("visible when parent is false")
	}
	if !SettingVisible(s, map[string]any{"on": true}) {
		t.Fatal("hidden when parent is true")
	}
	if SettingVisible(s, map[string]any{}) {
		t.Fatal("visible when parent is missing")
	}
	plain := Setting{Key: "name", Type: SettingString, Label: "Name"}
	if !SettingVisible(plain, nil) {
		t.Fatal("plain setting hidden")
	}
}

func TestCheckValuesRejectsBadCandidates(t *testing.T) {
	t.Parallel()
	min, max := 1.0, 5.0
	schema := []Setting{
		{Key: "on", Type: SettingBool, Label: "On"},
		{Key: "count", Type: SettingInt, Label: "Count", Min: &min, Max: &max},
		{Key: "mode", Type: SettingSelect, Label: "Mode", Options: []SettingOption{{Value: "a"}}},
		{Key: "tint", Type: SettingColor, Label: "Tint"},
	}
	cases := []struct {
		name   string
		values map[string]any
	}{
		{"unknown key", map[string]any{"ghost": true}},
		{"bool as string", map[string]any{"on": "yes"}},
		{"int out of range", map[string]any{"count": int64(9)}},
		{"select not listed", map[string]any{"mode": "z"}},
		{"bad colour", map[string]any{"tint": "blue"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := CheckValues(schema, c.values); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}
