package theming

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestKcolorschemeTemplateRendersCompleteOutput(t *testing.T) {
	t.Parallel()
	out := renderComplete(t, "kcolorscheme")
	for _, line := range strings.Split(out, "\n") {
		key, val, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(key, "[") {
			continue
		}
		if strings.TrimSpace(val) == "" {
			t.Errorf("empty value: %s", line)
		}
		if strings.Contains(val, ",") {
			for _, part := range strings.Split(val, ",") {
				for _, ch := range part {
					if ch < '0' || ch > '9' {
						t.Errorf("non-numeric rgb component in: %s", line)
						break
					}
				}
			}
		}
	}
	for _, want := range []string{"[Colors:View]", "[Colors:Window]", "[WM]", "ColorScheme=sysc-shell"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestKcolorschemeAppliesSidecarAndKdeGlobals(t *testing.T) {
	home := t.TempDir()
	markTemplatesComplete(t, "kcolorscheme")
	user := "[General]\nFont=Sans Serif,9,-1,5,50,0,0,0,0,0\n"
	cfg := joined(home, ".config", "kdeglobals")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyEnabled(home, func(string) bool { return true }, theme.Fallback); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(cfg)
	if !strings.Contains(string(b), "Font=Sans Serif") || strings.Count(string(b), "ColorSchemeName=") != 1 {
		t.Fatalf("kdeglobals mangled: %s", b)
	}
	side, err := os.ReadFile(templateTargets["kcolorscheme"].sidecar(home)[0])
	if err != nil || !strings.Contains(string(side), "[Colors:View]") {
		t.Fatalf("sidecar missing or wrong: %v", err)
	}
	if err := ApplyEnabled(home, func(string) bool { return false }, theme.Fallback); err != nil {
		t.Fatal(err)
	}
	b3, _ := os.ReadFile(cfg)
	if string(b3) != user {
		t.Fatalf("disable did not restore: %q", b3)
	}
	if _, err := os.Stat(templateTargets["kcolorscheme"].sidecar(home)[0]); !os.IsNotExist(err) {
		t.Fatal("sidecar survived disable")
	}
}
