package theming

import (
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
	if _, _, err := ApplyEnabled(home, only, theme.Fallback, 100); err != nil {
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
	// An edited managed block is adopted: the shell's block is restored and
	// the edit kept in the backup (owner decision, 2026-10-09).
	if _, adopted, err := ApplyEnabled(home, only, theme.Fallback, 100); err != nil || len(adopted) != 1 || adopted[0] != "starship" {
		t.Fatalf("edited managed block: adopted %v, err %v", adopted, err)
	}
	if current, _ := os.ReadFile(cfg); strings.Contains(string(current), "# user edit") {
		t.Fatal("the edited block was not restored")
	}
	backup, err := os.ReadFile(cfg + ".bak")
	if err != nil || !strings.Contains(string(backup), "# user edit") {
		t.Fatalf("managed block backup = %q, %v", backup, err)
	}
	// Re-apply must be idempotent.
	if _, _, err := ApplyEnabled(home, only, theme.Fallback, 100); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(cfg)
	if strings.Count(string(b2), blockOpen) != 1 || strings.Count(string(b2), `palette = "sysc-shell"`) != 1 {
		t.Fatalf("re-apply duplicated: %s", b2)
	}
	if _, _, err := ApplyEnabled(home, func(string) bool { return false }, theme.Fallback, 100); err != nil {
		t.Fatal(err)
	}
	b3, _ := os.ReadFile(cfg)
	if strings.TrimSpace(string(b3)) != strings.TrimSpace(user) {
		t.Fatalf("disable did not restore user file: %q", b3)
	}
}

func TestStarshipKeepsRootDirectiveOutsideManagedBlock(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "starship")
	only := func(name string) bool { return name == "starship" }

	if err := applyForTest(home, only, theme.Fallback); err != nil {
		t.Fatal(err)
	}
	cfg := joined(home, ".config", "starship.toml")
	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if palette, block := strings.Index(s, `palette = "sysc-shell"`), strings.Index(s, blockOpen); palette < 0 || block < 0 || palette > block {
		t.Fatalf("palette directive must stay outside managed block: %s", s)
	}
	if err := applyForTest(home, only, theme.Fallback); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if err := applyForTest(home, func(string) bool { return false }, theme.Fallback); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := os.Stat(cfg); !os.IsNotExist(err) {
		t.Fatalf("empty managed config survived disable: %v", err)
	}
}
