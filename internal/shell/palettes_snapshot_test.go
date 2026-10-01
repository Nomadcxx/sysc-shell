package shell

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

// A refresh that read the directory before a save must never land after the
// refresh that read it after the save: the saved palette would vanish from the
// page until the next refresh. The renders caught exactly this when Settings
// opened and a palette was saved before the open's refresh finished.
func TestAStaleRefreshNeverOverwritesANewerSnapshot(t *testing.T) {
	reg := newPanelRegistry(t)
	st := withPaletteStore(t, reg)
	listed, release := make(chan struct{}), make(chan struct{})
	first := true
	reg.paletteLister = func(s *theme.Store) []theme.PaletteInfo {
		list := listPalettes(s)
		if first {
			first = false
			close(listed)
			<-release // a slow, soon-stale listing
		}
		return list
	}
	stale := make(chan struct{})
	go func() { reg.refreshPalettes(); close(stale) }()
	<-listed
	dark, _ := theme.NamedPalette("nord", "dark", false)
	light, _ := theme.NamedPalette("nord", "light", false)
	if _, err := st.Save(theme.PaletteFile{Name: "Fresh", Dark: dark.Roles(), Light: light.Roles()}); err != nil {
		t.Fatal(err)
	}
	fresh := make(chan struct{})
	go func() { reg.refreshPalettes(); close(fresh) }()
	time.Sleep(50 * time.Millisecond) // unserialised, the fresh refresh lands here
	close(release)
	<-stale
	<-fresh
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if len(reg.palettes) != 1 {
		t.Fatalf("snapshot holds %d palettes after the save, want 1", len(reg.palettes))
	}
}
