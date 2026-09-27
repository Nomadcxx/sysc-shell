package theming

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestBtopTemplateRendersCompleteOutput(t *testing.T) {
	t.Parallel()
	out := renderComplete(t, "btop")
	assertHexAssignments(t, out)
	if n := strings.Count(out, "theme["); n < 30 {
		t.Errorf("theme entries: got %d", n)
	}
}

func TestBtopAppliesSidecarAndThemeDirective(t *testing.T) {
	home := t.TempDir()
	markTemplatesComplete(t, "btop")
	conf := joined(home, ".config", "btop", "btop.conf")
	if err := os.MkdirAll(filepath.Dir(conf), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conf, []byte("theme_background = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyEnabled(home, func(name string) bool { return name == "btop" }, theme.Fallback); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(conf)
	if strings.Count(string(b), `color_theme = "sysc-shell"`) != 1 {
		t.Fatalf("directive not managed: %q", b)
	}
	side, err := os.ReadFile(joined(home, ".config", "btop", "themes", "sysc-shell.theme"))
	if err != nil || !strings.Contains(string(side), "theme[main_bg]=") {
		t.Fatalf("sidecar missing or wrong: %v", err)
	}
	if err := ApplyEnabled(home, func(string) bool { return false }, theme.Fallback); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(conf)
	if strings.Contains(string(b2), "sysc-shell") {
		t.Fatalf("disable left directive: %q", b2)
	}
}
