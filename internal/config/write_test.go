package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
