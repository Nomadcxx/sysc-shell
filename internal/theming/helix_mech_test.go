package theming

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestHelixAppliesSidecarAndThemeDirective(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "helix")
	conf := joined(home, ".config", "helix", "config.toml")
	if err := os.MkdirAll(filepath.Dir(conf), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conf, []byte("[editor]\nline-number = \"relative\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyForTest(home, func(name string) bool { return name == "helix" }, theme.Fallback); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(conf)
	content := string(b)
	themeAt, editorAt := strings.Index(content, `theme = "sysc-shell"`), strings.Index(content, "[editor]")
	if themeAt < 0 || editorAt < 0 || themeAt > editorAt {
		t.Fatalf("theme directive not at the config root: %q", b)
	}
	if err := applyForTest(home, func(string) bool { return false }, theme.Fallback); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(conf)
	if strings.Contains(string(b2), "sysc-shell") {
		t.Fatalf("disable left residue: %q", b2)
	}
}
