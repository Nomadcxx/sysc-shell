package store

import (
	"encoding/json"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
)

func validAsset() Asset {
	return Asset{URL: "https://example.com/t.tar.gz", SHA256: sum([]byte("x")), Size: 10}
}

func validEntry() Entry {
	return entryFor(Release{Version: "1.4.0", Protocol: v1Version(1, 3),
		Capabilities: []string{"panels"}, Assets: map[string]Asset{"linux-amd64": validAsset()}})
}

func decodeOne(t *testing.T, mutate func(map[string]any)) (Catalog, error) {
	t.Helper()
	raw, _ := json.Marshal(validEntry())
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	mutate(m)
	body, _ := json.Marshal(map[string]any{"schema": 1, "plugins": []any{m}})
	return Decode(body)
}

func TestDecodeReadsAValidRow(t *testing.T) {
	t.Parallel()
	cat, err := decodeOne(t, func(m map[string]any) {
		m["unknown_future_field"] = 7
		m["homepage"] = "https://example.com"
		m["updated_at"] = "2026-09-24T00:00:00Z"
	})
	if err != nil || len(cat.Entries) != 1 || len(cat.Rejected) != 0 {
		t.Fatalf("Decode = %+v, %v", cat, err)
	}
	e := cat.Entries[0]
	if e.ID != fixtureID || e.Version != "1.4.0" || e.Homepage != "https://example.com" || e.UpdatedAt.IsZero() {
		t.Errorf("entry = %+v", e)
	}
}

func TestDecodeRejectsAnUnknownSchema(t *testing.T) {
	t.Parallel()
	_, err := Decode([]byte(`{"schema": 2, "plugins": []}`))
	if KindOf(err) != KindSchema {
		t.Fatalf("err = %v, want %s", err, KindSchema)
	}
}

func TestDecodeRejectsBadRowsAndKeepsTheRest(t *testing.T) {
	t.Parallel()
	cases := map[string]func(map[string]any){
		"bad id":             func(m map[string]any) { m["id"] = "Timer" },
		"missing name":       func(m map[string]any) { delete(m, "name") },
		"missing category":   func(m map[string]any) { delete(m, "category") },
		"bad version":        func(m map[string]any) { m["version"] = "1.4" },
		"http homepage":      func(m map[string]any) { m["homepage"] = "http://example.com" },
		"http release notes": func(m map[string]any) { m["release_notes"] = "http://example.com" },
		"no assets":          func(m map[string]any) { m["assets"] = map[string]any{} },
		"remote http asset": func(m map[string]any) {
			m["assets"] = map[string]any{"linux-amd64": map[string]any{"url": "http://example.com/a", "sha256": sum(nil), "size": 1}}
		},
		"short sha": func(m map[string]any) {
			m["assets"] = map[string]any{"linux-amd64": map[string]any{"url": "https://e.com/a", "sha256": "abc", "size": 1}}
		},
		"oversize asset": func(m map[string]any) {
			m["assets"] = map[string]any{"linux-amd64": map[string]any{"url": "https://e.com/a", "sha256": sum(nil), "size": MaxAssetBytes + 1}}
		},
		"bad asset key": func(m map[string]any) {
			m["assets"] = map[string]any{"darwin": map[string]any{"url": "https://e.com/a", "sha256": sum(nil), "size": 1}}
		},
		"bad timestamp": func(m map[string]any) { m["added_at"] = "yesterday" },
	}
	for name, mutate := range cases {
		cat, err := decodeOne(t, mutate)
		if err != nil {
			t.Errorf("%s: whole catalog failed: %v", name, err)
			continue
		}
		if len(cat.Entries) != 0 || len(cat.Rejected) != 1 {
			t.Errorf("%s: entries %d rejected %d, want 0 and 1", name, len(cat.Entries), len(cat.Rejected))
		}
	}
}

func TestDecodeAdmitsLoopbackHTTPAssets(t *testing.T) {
	t.Parallel()
	cat, err := decodeOne(t, func(m map[string]any) {
		m["assets"] = map[string]any{"linux-amd64": map[string]any{"url": "http://127.0.0.1:9/a", "sha256": sum(nil), "size": 1}}
	})
	if err != nil || len(cat.Entries) != 1 {
		t.Fatalf("Decode = %+v, %v", cat, err)
	}
}

func TestDecodeMapsAnUnknownCategoryToOther(t *testing.T) {
	t.Parallel()
	cat, err := decodeOne(t, func(m map[string]any) { m["category"] = "Stock" })
	if err != nil || len(cat.Entries) != 1 || cat.Entries[0].Category != CategoryOther {
		t.Fatalf("Decode = %+v, %v", cat, err)
	}
}

func TestDecodeRejectsEveryRowOfADuplicatedID(t *testing.T) {
	t.Parallel()
	e := validEntry()
	body, _ := json.Marshal(map[string]any{"schema": 1, "plugins": []Entry{e, e}})
	cat, err := Decode(body)
	if err != nil || len(cat.Entries) != 0 || len(cat.Rejected) != 2 {
		t.Fatalf("Decode = %+v, %v", cat, err)
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()
	asset := map[string]Asset{"linux-amd64": validAsset()}
	tooNew := v1Version(1, plugin.HostProtocolMinor+1)
	cases := []struct {
		name    string
		entry   Entry
		arch    string
		compat  Compat
		version string
		noAsset bool
	}{
		{"tip fits", entryFor(Release{Version: "1.4.0", Protocol: v1Version(1, 0), Assets: asset}), "amd64", Compatible, "1.4.0", false},
		{"tip too new, older fits", entryFor(
			Release{Version: "2.0.0", Protocol: tooNew, Assets: asset},
			Release{Version: "1.3.2", Protocol: v1Version(1, 0), Assets: asset},
			Release{Version: "1.2.0", Protocol: v1Version(1, 0), Assets: asset},
		), "amd64", HeldBack, "1.3.2", false},
		{"nothing fits", entryFor(Release{Version: "2.0.0", Protocol: v1Version(2, 0), Assets: asset}), "amd64", Incompatible, "", false},
		{"no asset for arch", entryFor(Release{Version: "1.4.0", Protocol: v1Version(1, 0), Assets: asset}), "arm64", Incompatible, "", true},
	}
	for _, c := range cases {
		got := Resolve(c.entry, c.arch)
		if got.Compat != c.compat {
			t.Errorf("%s: compat %s, want %s", c.name, got.Compat, c.compat)
		}
		if c.version == "" && got.Release != nil || c.version != "" && (got.Release == nil || got.Release.Version != c.version) {
			t.Errorf("%s: release %+v, want %q", c.name, got.Release, c.version)
		}
		if got.Needs != c.entry.Protocol {
			t.Errorf("%s: needs %+v, want the tip's %+v", c.name, got.Needs, c.entry.Protocol)
		}
		if got.NoAsset != c.noAsset {
			t.Errorf("%s: NoAsset %v, want %v", c.name, got.NoAsset, c.noAsset)
		}
	}
}

func TestNewer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.10.0", "1.9.9", true},
		{"1.4.0", "1.4.0", false},
		{"1.3.9", "1.4.0", false},
		{"2.0.0", "garbage", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}
