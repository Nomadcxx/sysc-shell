package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisplayRootsAreIndependent(t *testing.T) {
	cfg, err := Parse([]byte(`{"displays":{"niri_config_path":"/niri/config.kdl","sysc_config_path":"/sysc/config.kdl"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Displays.NiriConfigPath != "/niri/config.kdl" || cfg.Displays.SyscConfigPath != "/sysc/config.kdl" {
		t.Fatalf("roots = %+v", cfg.Displays)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got.Displays != cfg.Displays {
		t.Fatalf("round trip = %+v, %v", got.Displays, err)
	}
	if _, err := Parse([]byte(`{"displays":{"sysc_config_path":"relative.kdl"}}`)); err == nil {
		t.Fatal("accepted relative sysc root")
	}
}

// The shell's own file records the managed configuration root and nothing
// else, so a document that sets it must come back exactly.
func TestDisplaysBlockRoundTrips(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "config.json")
	c := Default()
	c.Displays.NiriConfigPath = "/home/u/.config/niri/config.kdl"
	if err := Write(p, c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"niri_config_path"`) {
		t.Fatalf("the written document does not carry the path:\n%s", data)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Displays.NiriConfigPath != c.Displays.NiriConfigPath {
		t.Fatalf("path = %q, want %q", got.Displays.NiriConfigPath, c.Displays.NiriConfigPath)
	}
}

// An unset path means the shell has not claimed a root yet, so the block is
// omitted rather than written as an empty string.
func TestDisplaysBlockIsOmittedByDefault(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := Write(p, Default()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "displays") {
		t.Fatalf("a default document mentions the displays block:\n%s", data)
	}
}

// A relative path would resolve against a different directory under the
// compositor's include rules than under the shell's, so it is refused.
func TestDisplaysBlockRefusesARelativePath(t *testing.T) {
	t.Parallel()
	_, err := Parse([]byte(`{"displays":{"niri_config_path":"niri/config.kdl"}}`))
	if err == nil {
		t.Fatal("Parse accepted a relative configuration root")
	}
	if !strings.Contains(err.Error(), "niri_config_path") {
		t.Fatalf("error = %v, want it to name the field", err)
	}

	p := filepath.Join(t.TempDir(), "config.json")
	c := Default()
	c.Displays.NiriConfigPath = "niri/config.kdl"
	if err := Write(p, c); err == nil {
		t.Fatal("Write accepted a relative configuration root")
	}
}

// The path is cleaned, so a document written by hand with a trailing slash or
// a dot segment still names one file.
func TestDisplaysBlockCleansThePath(t *testing.T) {
	t.Parallel()
	got, err := Parse([]byte(`{"displays":{"niri_config_path":"/home/u/.config/niri/../niri/config.kdl"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := "/home/u/.config/niri/config.kdl"; got.Displays.NiriConfigPath != want {
		t.Fatalf("path = %q, want %q", got.Displays.NiriConfigPath, want)
	}
}

// An unknown field inside the block must fail like every other block, so a
// typo in a hand-edited document is loud instead of silently ignored.
func TestDisplaysBlockRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	if _, err := Parse([]byte(`{"displays":{"niriConfigPath":"/x"}}`)); err == nil {
		t.Fatal("Parse accepted a camel-cased field name")
	}
}
