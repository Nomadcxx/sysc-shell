package theming

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// TestUserTemplateOverlayTakesPrecedence is the D6 golden test: a body in
// $XDG_CONFIG_HOME/sysc-shell/theming-templates/<name>.tpl replaces the
// embedded one for that name at apply time.
func TestUserTemplateOverlayTakesPrecedence(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	dir := filepath.Join(xdg, "sysc-shell", "theming-templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A body the embedded catalog can never produce.
	sentinel := "# >>> user-overlay-body <<< background = "
	if err := os.WriteFile(filepath.Join(dir, "foot.tpl"), []byte(sentinel+"#000000\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := Catalog().Template("foot"); strings.Contains(got, "user-overlay-body") {
		t.Fatal("Catalog() must stay the embedded catalog")
	}
	if got := Catalog().WithOverlay().Template("foot"); !strings.Contains(got, "user-overlay-body") {
		t.Fatalf("WithOverlay() did not replace foot: %q", got)
	}

	home := t.TempDir()
	only := func(name string) bool { return name == "foot" }
	if _, _, err := ApplyEnabled(home, only, theme.Fallback); err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(home, ".config", "foot", "themes", "sysc-shell")
	b, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "user-overlay-body") {
		t.Fatalf("apply wrote the embedded body, not the overlay: %q", b)
	}
}

// TestUserTemplateOverlayBrokenBodyRefused: D6 -- an overlay body that does
// not parse renders empty, and apply must report it rather than blank the
// target's theme file.
func TestUserTemplateOverlayBrokenBodyRefused(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	dir := filepath.Join(xdg, "sysc-shell", "theming-templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foot.tpl"), []byte("{{"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	only := func(name string) bool { return name == "foot" }
	outcomes, _, err := ApplyEnabled(home, only, theme.Fallback)
	if err == nil || outcomes["foot"] == nil {
		t.Fatalf("broken overlay accepted: err=%v outcomes=%v", err, outcomes)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".config", "foot", "themes", "sysc-shell")); statErr == nil {
		t.Fatal("broken overlay wrote the sidecar")
	}
}
