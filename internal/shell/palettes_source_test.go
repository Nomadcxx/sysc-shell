package shell

import (
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// A pinned scheme outranks the wallpaper; the custom kind must be pinned too,
// or the next wallpaper apply silently undoes the user's choice.
func TestWallpaperSeedDoesNotOverrideACustomPalette(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.mu.Lock()
	reg.cfg.ThemeGen.Source, reg.cfg.ThemeGen.Seed = "custom", "my-nord"
	reg.mu.Unlock()

	reg.setWallpaperSeed("wallpaper", "/tmp/a.png")

	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.cfg.ThemeGen.Source != "custom" || reg.cfg.ThemeGen.Seed != "my-nord" {
		t.Fatalf("theme = %q/%q after a wallpaper apply, want custom/my-nord",
			reg.cfg.ThemeGen.Source, reg.cfg.ThemeGen.Seed)
	}
}

// A config saved with a custom source must boot on that palette. NewRegistry
// generates the theme before main calls BindPersist, so the store has to exist
// at construction (R1).
func TestNewRegistryBootsOnACustomPalette(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	st := &theme.Store{Dir: filepath.Join(home, "sysc-shell", "palettes")}
	dark, _ := theme.NamedPalette("gruvbox", "dark", false)
	light, _ := theme.NamedPalette("gruvbox", "light", false)
	slug, err := st.Save(theme.PaletteFile{Name: "Boot", Dark: dark.Roles(), Light: light.Roles()})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.ThemeGen.Source, cfg.ThemeGen.Seed, cfg.ThemeGen.Mode = "custom", slug, "dark"
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.themeErr != "" {
		t.Fatalf("boot reported %q", reg.themeErr)
	}
	if reg.tokens != dark {
		t.Fatal("boot painted something other than the saved palette")
	}
}
