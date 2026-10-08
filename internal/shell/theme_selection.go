package shell

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// Template failures do not invalidate the palette committed by the shell.
var errThemeTemplates = errors.New("theme: external templates")

func generatedTheme(err error) bool { return err == nil || errors.Is(err, errThemeTemplates) }

const greeterThemeDir = "/var/lib/sysc-greet/shell-theme"

func publishThemeSelection(cfg config.Config) error {
	name := ""
	if cfg.ThemeGen.Source == "palette" {
		name = cfg.ThemeGen.Seed
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	if err = theme.PublishSelection(filepath.Join(dir, "sysc-shell", "shell-theme"), name); err != nil {
		return err
	}
	// The installer assigns this directory to the account controlling the greeter.
	// Other accounts publish only their own lock-screen selection.
	info, err := os.Lstat(greeterThemeDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || !ok || int(stat.Uid) != os.Getuid() {
		return nil
	}
	if err = theme.PublishSelection(filepath.Join(greeterThemeDir, "theme"), name); err != nil {
		return fmt.Errorf("publish greeter theme: %w", err)
	}
	return nil
}

// Serialize exports with generation, then reject work superseded by a later
// commit. File work stays off the Wayland owner after startup.
func (r *Registry) publishCommittedThemeSelection(expected config.Config, expectedTokens theme.Tokens) {
	r.themeGenMu.Lock()
	defer r.themeGenMu.Unlock()
	r.mu.Lock()
	current := r.cfg
	valid := r.tokens == expectedTokens && current.ThemeGen.Source == expected.ThemeGen.Source && current.ThemeGen.Seed == expected.ThemeGen.Seed
	r.mu.Unlock()
	if !valid {
		return
	}
	if err := publishThemeSelection(current); err != nil {
		log.Printf("shell: theme selection: %v", err)
	}
}
