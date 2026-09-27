package theming

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestCavaTemplateRendersCompleteOutput(t *testing.T) {
	t.Parallel()
	out := renderComplete(t, "cava")
	assertHexAssignments(t, out)
	if !strings.Contains(out, "[color]") || !strings.Contains(out, "gradient_color_5") {
		t.Error("missing cava keys")
	}
}

func TestCavaAppliesSidecarIntoColorSection(t *testing.T) {
	home := t.TempDir()
	markTemplatesComplete(t, "cava")
	conf := joined(home, ".config", "cava", "config")
	if err := os.MkdirAll(filepath.Dir(conf), 0o755); err != nil {
		t.Fatal(err)
	}
	user := "[general]\nbars = 24\n\n[color]\ngradient = 0\n"
	if err := os.WriteFile(conf, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyEnabled(home, func(name string) bool { return name == "cava" }, theme.Fallback); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(conf)
	s := string(b)
	i := strings.Index(s, "[color]")
	j := strings.Index(s, `theme = "sysc-shell"`)
	if j <= i {
		t.Fatalf("directive not under [color]: %q", s)
	}
	if err := ApplyEnabled(home, func(string) bool { return false }, theme.Fallback); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(conf)
	if string(b2) != user {
		t.Fatalf("disable left residue: %q", b2)
	}
}
