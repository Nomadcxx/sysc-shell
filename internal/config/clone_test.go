package config

import (
	"fmt"
	"reflect"
	"testing"
)

// populatedConfig carries a non-nil instance of every mutable field the
// clone has to copy.
func populatedConfig() Config {
	reserve := 48
	other := 64
	cfg := Default()
	cfg.Bar.Reserve = &reserve
	cfg.Outputs = []OutputOverride{{
		Connector: "DP-1",
		Bar:       Bar{Height: 64, Left: []Item{{ID: "clock"}}, Reserve: &other},
	}}
	cfg.Tray.Hidden = []string{"id:hidden"}
	cfg.Tray.Pinned = []string{"id:pinned"}
	cfg.Tray.Order = []string{"id:order"}
	cfg.Media.Blacklist = []string{"org.mpris.MediaPlayer2.blacklisted"}
	cfg.Applications.Favourites = []string{"org.gnome.Nautilus"}
	cfg.Applications.Hidden = []string{"old-editor"}
	cfg.Applications.DefaultTerminal = "foot"
	cfg.Templates = map[string]bool{"foot": true}
	cfg.Plugins = Plugins{
		Enabled: []string{"timer"},
		Settings: map[string]map[string]any{"timer": {
			"sound":       true,
			"preferences": map[string]any{"items": []any{map[string]any{"name": "quiet"}}},
			"empty_map":   map[string]any(nil),
			"empty_list":  []any(nil),
		}, "empty": nil},
		Instances: map[string]map[string]any{"timer.1": {
			"sound":       false,
			"preferences": map[string]any{"items": []any{map[string]any{"name": "compact"}}},
		}},
		Sources: []PluginSource{{Name: "extra", URL: "https://example.invalid/plugins"}},
	}
	return cfg
}

func TestCloneIsDeep(t *testing.T) {
	cfg := populatedConfig()
	clone := cfg.Clone()
	if !reflect.DeepEqual(clone, cfg) {
		t.Fatalf("Clone changed the value:\n got %+v\nwant %+v", clone, cfg)
	}

	clone.Bar.Left[0].ID = "changed"
	clone.Bar.Center[0].Items[0].ID = "changed"
	*clone.Bar.Reserve = 1
	clone.Outputs[0].Bar.Left[0].ID = "changed"
	*clone.Outputs[0].Bar.Reserve = 1
	clone.Tray.Hidden[0] = "changed"
	clone.Tray.Pinned[0] = "changed"
	clone.Tray.Order[0] = "changed"
	clone.Media.Blacklist[0] = "changed"
	clone.Applications.Favourites[0] = "changed"
	clone.Applications.Hidden[0] = "changed"
	clone.Templates["foot"] = false
	clone.Plugins.Enabled[0] = "changed"
	clone.Plugins.Settings["timer"]["sound"] = false
	clone.Plugins.Instances["timer.1"]["sound"] = true
	clone.Plugins.Settings["timer"]["preferences"].(map[string]any)["items"].([]any)[0].(map[string]any)["name"] = "changed"
	clone.Plugins.Instances["timer.1"]["preferences"].(map[string]any)["items"].([]any)[0].(map[string]any)["name"] = "changed"
	clone.Plugins.Sources[0].Name = "changed"

	if fresh := populatedConfig(); !reflect.DeepEqual(cfg, fresh) {
		t.Fatalf("editing the clone changed the original:\n got %+v\nwant %+v", cfg, fresh)
	}
}

// TestCloneCopiesEveryMutableField is the guard: it fails when a new slice,
// map, or pointer field is added to Config without a matching copy in Clone.
func TestCloneCopiesEveryMutableField(t *testing.T) {
	cfg := populatedConfig()
	clone := cfg.Clone()

	original := map[string]uintptr{}
	collectMutable(reflect.ValueOf(cfg), "Config", original)
	copied := map[string]uintptr{}
	collectMutable(reflect.ValueOf(clone), "Config", copied)

	for path, ptr := range original {
		other, ok := copied[path]
		if !ok {
			t.Errorf("%s: missing from the copy", path)
			continue
		}
		if ptr == other {
			t.Errorf("%s: the copy still shares storage with the original", path)
		}
	}
	for path := range copied {
		if _, ok := original[path]; !ok {
			t.Errorf("%s: present only in the copy", path)
		}
	}
}

// collectMutable records the address of every non-empty slice, non-nil map,
// and non-nil pointer reachable from v, keyed by field path.
func collectMutable(v reflect.Value, path string, out map[string]uintptr) {
	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if !t.Field(i).IsExported() {
				continue
			}
			collectMutable(v.Field(i), path+"."+t.Field(i).Name, out)
		}
	case reflect.Slice:
		if v.IsNil() {
			return
		}
		if v.Len() > 0 {
			out[path] = v.Pointer()
		}
		for i := 0; i < v.Len(); i++ {
			collectMutable(v.Index(i), fmt.Sprintf("%s[%d]", path, i), out)
		}
	case reflect.Interface:
		if !v.IsNil() {
			collectMutable(v.Elem(), path, out)
		}
	case reflect.Map:
		if !v.IsNil() {
			out[path] = v.Pointer()
			iter := v.MapRange()
			for iter.Next() {
				collectMutable(iter.Value(), fmt.Sprintf("%s[%v]", path, iter.Key()), out)
			}
		}
	case reflect.Ptr:
		if !v.IsNil() {
			out[path] = v.Pointer()
		}
	}
}
