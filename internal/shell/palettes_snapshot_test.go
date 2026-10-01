package shell

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// The settings factory runs on every settings edit under Registry.mu; it must
// read the snapshot, never the directory (R2).
func TestSettingsFactoryReadsTheSnapshotNotTheDisk(t *testing.T) {
	reg := newPanelRegistry(t)
	st := withPaletteStore(t, reg)
	dark, _ := theme.NamedPalette("nord", "dark", false)
	light, _ := theme.NamedPalette("nord", "light", false)
	if _, err := st.Save(theme.PaletteFile{Name: "Snap", Dark: dark.Roles(), Light: light.Roles()}); err != nil {
		t.Fatal(err)
	}
	reg.refreshPalettes()
	if err := os.RemoveAll(st.Dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.settingsFor(config.Default()).Lookup("appearance.custom"); !ok {
		t.Fatal("the factory re-read the deleted directory instead of the snapshot")
	}
	reg.refreshPalettes()
	if _, ok := reg.settingsFor(config.Default()).Lookup("appearance.custom"); ok {
		t.Fatal("refreshPalettes did not pick up the deletion")
	}
}

// withPaletteStore gives a registry its own temp-dir store, in place of the
// one NewRegistry built, and an empty snapshot. Call it before anything runs
// on the registry; paletteStore is otherwise never reassigned.
func withPaletteStore(t *testing.T, reg *Registry) *theme.Store {
	t.Helper()
	st := &theme.Store{Dir: filepath.Join(t.TempDir(), "palettes")}
	reg.mu.Lock()
	reg.paletteStore = st
	reg.themeGen.Custom = st
	reg.palettes = nil
	reg.mu.Unlock()
	return st
}
