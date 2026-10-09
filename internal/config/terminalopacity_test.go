package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTerminalOpacityDefaultsLoadsAndWritesSparse(t *testing.T) {
	if got := Default().TerminalOpacity; got != 100 {
		t.Fatalf("default = %d, want 100 (unmanaged)", got)
	}
	cfg, err := Parse([]byte(`{"terminal-opacity": 85}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TerminalOpacity != 85 {
		t.Fatalf("loaded %d, want 85", cfg.TerminalOpacity)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	back, err := Parse(out)
	if err != nil || back.TerminalOpacity != 85 {
		t.Fatalf("round trip = %d, %v; written %s", back.TerminalOpacity, err, out)
	}
	if err := Write(path, Default()); err != nil {
		t.Fatal(err)
	}
	if out, _ := os.ReadFile(path); strings.Contains(string(out), "terminal-opacity") {
		t.Fatalf("the default is written: %s", out)
	}
	for _, bad := range []string{`{"terminal-opacity": 49}`, `{"terminal-opacity": 101}`} {
		if _, err := Parse([]byte(bad)); err == nil || !strings.Contains(err.Error(), "terminal-opacity") {
			t.Errorf("%s = %v, want a terminal-opacity range error", bad, err)
		}
	}
}
