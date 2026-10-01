package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// An edit made while Save is in flight is not in the file that Save wrote,
// so it must stay unsaved: marking it saved hid it until Back dropped it.
func TestAnEditDuringSaveStaysUnsaved(t *testing.T) {
	reg, h, slug := savedPalette(t)
	editRole(t, reg, h, "primary", "#81a1c1")
	reg.mu.Lock()
	if !reg.handlePalettes(h, findAction(h.root, "palette-save")) {
		reg.mu.Unlock()
		t.Fatal("Save was not handled")
	}
	// The save's result needs the lock this test holds, so this edit lands
	// while the save is still in flight.
	h.fields["palette-role:secondary"] = ui.NewField("#a3be8c")
	reg.paletteRoleEdited(h, "secondary", "#a3be8c")
	reg.mu.Unlock()
	settle(t, reg, h, func(p *paletteUI) bool { return p.busy == "" })

	reg.mu.Lock()
	dirty, preview := h.palettes.dirty, h.palettes.preview
	reg.mu.Unlock()
	if !dirty || !preview {
		t.Fatalf("dirty=%v preview=%v after an edit the save did not include", dirty, preview)
	}
	f, err := reg.paletteStore.Load(slug)
	if err != nil {
		t.Fatal(err)
	}
	if f.Dark["primary"] != "#81a1c1" {
		t.Fatalf("the save itself was lost: primary = %s", f.Dark["primary"])
	}
}

// The import field's starting folder comes from the registry, resolved once
// off the lock, not from reading user-dirs.dirs on every build (R2).
func TestImportFieldSeedsFromTheRegistry(t *testing.T) {
	reg, h := openPalettesPage(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.paletteImportDir = "~/Elsewhere"
	delete(h.fields, "palette-import-path")
	reg.rebuildPanel(h)
	if f := h.fields["palette-import-path"]; f == nil || f.Text != "~/Elsewhere/" {
		t.Fatalf("import field = %+v, want ~/Elsewhere/", f)
	}
}

// Leaving Palettes by IPC or a deep link ends the editor and its preview,
// as the rail does (P16: any route).
func TestLeavingPalettesByAnyRouteEndsTheEditor(t *testing.T) {
	reg, h, _ := savedPalette(t)
	editRole(t, reg, h, "primary", "#81a1c1")
	reg.mu.Lock()
	err := reg.selectPanelSectionLocked(PanelSettings, "Appearance")
	editing, preview := h.palettes.editing, h.palettes.preview
	reg.mu.Unlock()
	if err != nil || editing != "" || preview {
		t.Fatalf("err=%v editing=%q preview=%v after a section change by address", err, editing, preview)
	}
}

// Only a failure of the custom palette itself is blamed on it, and it is
// shown on Appearance as well as Palettes (P4, P10). A refused template
// shares themeErr and is not the palette's fault.
func TestOnlyACustomPaletteFailureIsBlamedOnThePalette(t *testing.T) {
	reg, h := openPalettesPage(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h.draft.ThemeGen.Source, h.draft.ThemeGen.Seed = "custom", "gone"
	reg.themeErr = "theme: external templates: foot: the file was changed by hand"
	reg.rebuildPanel(h)
	if hasText(h, "could not be applied") || hasText(h, "no longer saved") {
		t.Fatal("a template refusal was blamed on the palette")
	}
	reg.themeErr = `theme: custom palette "gone": palette "gone": open /x/gone.json: no such file or directory`
	reg.rebuildPanel(h)
	if !hasText(h, "no longer saved") {
		t.Fatal("Palettes does not say the palette in use is gone")
	}
	if hasText(h, "/x/gone.json") {
		t.Error("the reason quotes the file path")
	}
	h.section = "Appearance"
	reg.rebuildPanel(h)
	if !hasText(h, "no longer saved") {
		t.Fatal("Appearance does not say why the custom source is not in effect")
	}
}

// The Appearance pickers name saved palettes as the user named them; the
// slug is the stored value only (P5, R13).
func TestAppearanceCustomPickerShowsDisplayNames(t *testing.T) {
	reg, h := openPalettesPage(t)
	saveNord(t, reg, h)
	if err := reg.PaletteRename("nord-copy", "Evening"); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h.draft.ThemeGen.Source, h.draft.ThemeGen.Seed = "custom", "nord-copy"
	h.set = reg.settingsForLocked(h.draft)
	h.section = "Appearance"
	h.menus = nil
	reg.rebuildPanel(h)
	if !hasText(h, "Evening") {
		t.Fatal("the Appearance picker does not show the palette's display name")
	}
}
