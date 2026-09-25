package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

const fixtureID = "org.sysc.timer"

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func v1Version(major, minor int) v1.Version { return v1.Version{Major: major, Minor: minor} }

func entryFor(rels ...Release) Entry {
	e := Entry{ID: fixtureID, Name: "Timer", Author: "sysc", Description: "Countdown.", Category: "productivity", Release: rels[0]}
	e.Releases = rels[1:]
	return e
}

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
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("err = %v, want ErrSchema", err)
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

func TestSameSet(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b []string
		want bool
	}{
		{nil, nil, true},
		{[]string{"a", "b"}, []string{"b", "a"}, true},
		{[]string{"a", "a", "b"}, []string{"b", "a"}, true},
		{[]string{"a"}, []string{"a", "b"}, false},
	}
	for _, c := range cases {
		if got := SameSet(c.a, c.b); got != c.want {
			t.Errorf("SameSet(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
