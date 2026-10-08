package shell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestCommittedThemeSelection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfg := config.Default()
	cfg.ThemeGen.Source = "palette"
	cfg.ThemeGen.Seed = "dracula"
	dracula, _ := theme.NamedPalette("dracula", "dark", false)
	blue, _ := theme.NamedPalette("blue", "dark", false)
	r := &Registry{cfg: cfg, tokens: dracula}
	r.publishCommittedThemeSelection(cfg, r.tokens)
	path := filepath.Join(dir, "sysc-shell", "shell-theme")
	check := func(want string) {
		t.Helper()
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want+"\n" {
			t.Fatalf("selection=%q err=%v", got, err)
		}
	}
	check("dracula")
	stale := cfg
	stale.ThemeGen.Seed = "blue"
	r.publishCommittedThemeSelection(stale, blue)
	check("dracula")
	r.cfg = stale
	r.themeErr = "generation failed"
	r.publishCommittedThemeSelection(stale, blue)
	check("dracula")
	r.themeErr = ""
	r.tokens = blue
	r.publishCommittedThemeSelection(stale, blue)
	check("blue")
	cfg.ThemeGen.Source = "hex"
	cfg.ThemeGen.Seed = "#123456"
	r.cfg = cfg
	r.publishCommittedThemeSelection(cfg, r.tokens)
	check("")
}

func TestTemplateFailureDoesNotInvalidateSelection(t *testing.T) {
	if !generatedTheme(fmt.Errorf("%w: modified template", errThemeTemplates)) {
		t.Fatal("template error blocked palette following")
	}
	if generatedTheme(errors.New("generation failed")) {
		t.Fatal("generation failure allowed export")
	}
}
