package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func digestOf(t *testing.T, dir string) string {
	t.Helper()
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	d, err := ContentDigest(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 64 {
		t.Fatalf("digest %q is not a hex sha256", d)
	}
	return d
}

func TestContentDigestCoversCodeAndAsksOnly(t *testing.T) {
	t.Parallel()
	a := writePlugin(t, timerManifest, "bin/sysc-plugin-timer")
	b := writePlugin(t, timerManifest, "bin/sysc-plugin-timer")
	if digestOf(t, a) != digestOf(t, b) {
		t.Fatal("identical plugins digest differently")
	}

	// State, caches and records written beside the plugin are not its code.
	base := digestOf(t, a)
	for _, extra := range []string{"state.json", "installed.json", "cache/thumb.png"} {
		path := filepath.Join(a, extra)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := digestOf(t, a); got != base {
		t.Fatal("digest covers files the plugin did not declare")
	}

	// A changed entry point is a changed plugin.
	if err := os.WriteFile(filepath.Join(b, "bin/sysc-plugin-timer"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if digestOf(t, b) == base {
		t.Fatal("digest ignores the entry point's bytes")
	}

	// So is a changed manifest: the asks are part of what was approved.
	c := writePlugin(t, edit(t, "capabilities", []string{"panels"}), "bin/sysc-plugin-timer")
	if digestOf(t, c) == base {
		t.Fatal("digest ignores the manifest's bytes")
	}
}

func TestContentDigestFramingCannotBeForged(t *testing.T) {
	t.Parallel()
	// Moving bytes between the two framed entries must change the digest:
	// the length prefix keeps one entry's tail from reading as the next
	// entry's head.
	one := contentDigestOf([]digestEntry{{"a", []byte("xy")}, {"b", []byte("z")}})
	two := contentDigestOf([]digestEntry{{"a", []byte("x")}, {"b", []byte("yz")}})
	if one == two {
		t.Fatal("framing lets bytes move between entries")
	}
}

func TestContentDigestRefusesAMissingEntryPoint(t *testing.T) {
	t.Parallel()
	dir := writePlugin(t, timerManifest, "bin/sysc-plugin-timer")
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(m.ExecPath); err != nil {
		t.Fatal(err)
	}
	if d, err := ContentDigest(m); err == nil {
		t.Fatalf("missing entry point digested to %q", d)
	}
}
