package theming

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestStarshipTemplateRendersCompleteOutput(t *testing.T) {
	t.Parallel()
	out := renderComplete(t, "starship")
	assertHexAssignments(t, out)
	if !strings.Contains(out, "[palettes.sysc-shell]") {
		t.Error("missing palette table")
	}
}

func TestStarshipManagesPaletteBlockInUserConfig(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "starship")
	cfg := joined(home, ".config", "starship.toml")
	user := "[[character]]\nsymbol = \"❯\"\n"
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	only := func(name string) bool { return name == "starship" }
	if _, err := ApplyEnabled(home, only, theme.Fallback, nil); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(cfg)
	s := string(b)
	if !strings.Contains(s, "symbol = \"❯\"") {
		t.Fatalf("user config lost: %s", s)
	}
	if strings.Count(s, blockOpen) != 1 || strings.Count(s, blockClose) != 1 {
		t.Fatalf("block not managed: %s", s)
	}
	if strings.Index(s, `palette = "sysc-shell"`) > strings.Index(s, "[[character]]") {
		t.Fatalf("palette line not in root zone: %s", s)
	}
	edited := strings.Replace(s, blockClose, "# user edit\n"+blockClose, 1)
	if err := os.WriteFile(cfg, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyEnabled(home, only, theme.Fallback, nil); !errors.Is(err, ErrUserModified) {
		t.Fatalf("edited managed block refusal = %v", err)
	}
	if current, _ := os.ReadFile(cfg); !strings.Contains(string(current), "# user edit") {
		t.Fatal("apply overwrote an edited managed block")
	}
	if _, err := ApplyEnabled(home, only, theme.Fallback, only); err != nil {
		t.Fatalf("confirmed block overwrite: %v", err)
	}
	backup, err := os.ReadFile(cfg + ".bak")
	if err != nil || !strings.Contains(string(backup), "# user edit") {
		t.Fatalf("managed block backup = %q, %v", backup, err)
	}
	// Re-apply must be idempotent.
	if _, err := ApplyEnabled(home, only, theme.Fallback, nil); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(cfg)
	if strings.Count(string(b2), blockOpen) != 1 || strings.Count(string(b2), `palette = "sysc-shell"`) != 1 {
		t.Fatalf("re-apply duplicated: %s", b2)
	}
	if _, err := ApplyEnabled(home, func(string) bool { return false }, theme.Fallback, nil); err != nil {
		t.Fatal(err)
	}
	b3, _ := os.ReadFile(cfg)
	if strings.TrimSpace(string(b3)) != strings.TrimSpace(user) {
		t.Fatalf("disable did not restore user file: %q", b3)
	}
}
