package shell

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// openPalettesPage opens Settings on the Palettes section with a temp store.
func openPalettesPage(t *testing.T) (*Registry, *PanelHost) {
	t.Helper()
	reg := newPanelRegistry(t)
	keepInvalidationsDrained(t, reg)
	withPaletteStore(t, reg)
	withTestBar(t, reg, 7, reg.cfg)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	drainAux(t, reg, 2)
	reg.mu.Lock()
	h := reg.panelHosts[PanelSettings]
	h.section = "Palettes"
	reg.rebuildPanel(h)
	reg.mu.Unlock()
	return reg, h
}

func press(t *testing.T, reg *Registry, h *PanelHost, action string) {
	t.Helper()
	reg.mu.Lock()
	target := findAction(h.root, action)
	if target == nil {
		reg.mu.Unlock()
		t.Fatalf("no node with action %q", action)
	}
	handled := reg.handlePalettes(h, target)
	reg.mu.Unlock()
	if !handled {
		t.Fatalf("action %q was not handled", action)
	}
}

// saveNord stores the compiled Nord scheme as "Nord copy". The name field is
// seeded once with a suggestion, so the test sets it explicitly.
func saveNord(t *testing.T, reg *Registry, h *PanelHost) {
	t.Helper()
	reg.mu.Lock()
	h.draft.ThemeGen.Source, h.draft.ThemeGen.Seed = "palette", "nord"
	if h.fields == nil {
		h.fields = map[string]*ui.Field{}
	}
	h.fields["palette-name"] = ui.NewField("Nord copy")
	reg.rebuildPanel(h)
	reg.mu.Unlock()
	press(t, reg, h, "palette-save-current")
	settle(t, reg, h, func(p *paletteUI) bool { return p.busy == "" && (p.notice != "" || p.err != "") })
}

func settle(t *testing.T, reg *Registry, h *PanelHost, ok func(*paletteUI) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		done := ok(&h.palettes)
		reg.mu.Unlock()
		if done {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the palette operation")
}

func hasText(h *PanelHost, text string) bool {
	for _, n := range walk(h.root) {
		if strings.Contains(n.Text, text) {
			return true
		}
	}
	return false
}

func TestPalettesPageStartsEmptyWithSaveAndImport(t *testing.T) {
	reg, h := openPalettesPage(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	for _, action := range []string{"palette-save-current", "palette-import-file", "palette-import-paste"} {
		if findAction(h.root, action) == nil {
			t.Errorf("no %s control on an empty page", action)
		}
	}
	if !hasText(h, "No saved palettes") {
		t.Error("the empty state is not explained")
	}
	// R6: the import field starts in the folder Export writes to.
	if f := h.fields["palette-import-path"]; f == nil || !strings.HasSuffix(f.Text, "/") {
		t.Errorf("import field = %+v, want a directory ending in /", f)
	}
}

func TestSaveCurrentAddsARowAndUseSelectsIt(t *testing.T) {
	reg, h := openPalettesPage(t)
	saveNord(t, reg, h)
	reg.mu.Lock()
	if h.palettes.err != "" {
		reg.mu.Unlock()
		t.Fatalf("save failed: %s", h.palettes.err)
	}
	// R13: the outcome names the palette as the user named it.
	if !strings.Contains(h.palettes.notice, "Nord copy") || strings.Contains(h.palettes.notice, "nord-copy") {
		t.Errorf("notice = %q, want the display name, not the slug", h.palettes.notice)
	}
	reg.mu.Unlock()

	press(t, reg, h, "palette-use:nord-copy")
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if h.draft.ThemeGen.Source != "custom" || h.draft.ThemeGen.Seed != "nord-copy" {
		t.Fatalf("draft theme = %q/%q after Use", h.draft.ThemeGen.Source, h.draft.ThemeGen.Seed)
	}
	// R8: the active row shows a label, not a button that re-applies.
	if findAction(h.root, "palette-use:nord-copy") != nil {
		t.Error("the palette in use still offers Use")
	}
	if !hasText(h, "In use") {
		t.Error("the palette in use is not marked")
	}
}

// R8: a row chooses (Use), opens the editor, or deletes. Everything else
// about one palette is in the editor.
func TestRowsOfferUseEditAndDeleteOnly(t *testing.T) {
	reg, h := openPalettesPage(t)
	saveNord(t, reg, h)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	for _, a := range []string{"palette-use:nord-copy", "palette-edit:nord-copy", "palette-delete:nord-copy"} {
		n := findAction(h.root, a)
		if n == nil || n.Name == "" {
			t.Errorf("row action %s missing or unnamed", a)
		}
	}
	for _, a := range []string{"palette-rename:nord-copy", "palette-export:nord-copy", "palette-copy:nord-copy"} {
		if findAction(h.root, a) != nil {
			t.Errorf("row still carries %s; it belongs to the editor", a)
		}
	}
	if hasText(h, "nord-copy") {
		t.Error("the slug is shown on the list; it belongs to the editor's ID line")
	}
}

func TestDeleteNeedsASecondClickAndWarnsWhenInUse(t *testing.T) {
	reg, h := openPalettesPage(t)
	saveNord(t, reg, h)
	press(t, reg, h, "palette-use:nord-copy")

	press(t, reg, h, "palette-delete:nord-copy")
	if list, _ := reg.PalettesList(); len(list) != 1 {
		t.Fatal("the first click deleted the palette")
	}
	reg.mu.Lock()
	warned := hasText(h, "in use")
	named := hasText(h, "Nord copy")
	reg.mu.Unlock()
	if !warned || !named {
		t.Fatalf("confirm row: warned=%v named=%v; it must name the palette and say it is in use", warned, named)
	}
	press(t, reg, h, "palette-delete-confirm:nord-copy")
	settle(t, reg, h, func(p *paletteUI) bool { return p.busy == "" && p.deleting == "" })
	if list, _ := reg.PalettesList(); len(list) != 0 {
		t.Fatalf("palette still listed after confirmation: %+v", list)
	}
}

// R5: while an operation runs, nothing else starts and every operation
// button says so.
func TestASecondClickWhileBusyIsRefused(t *testing.T) {
	reg, h := openPalettesPage(t)
	reg.mu.Lock()
	h.palettes.busy = "palette-save-current"
	h.fields["palette-name"] = ui.NewField("Twice")
	reg.rebuildPanel(h)
	save := findAction(h.root, "palette-save-current")
	if save == nil || !save.State.Has(ui.StateDisabled) || !strings.Contains(save.Text, "Saving") {
		reg.mu.Unlock()
		t.Fatalf("busy Save = %+v, want disabled and labelled Saving…", save)
	}
	if imp := findAction(h.root, "palette-import-file"); imp == nil || !imp.State.Has(ui.StateDisabled) {
		reg.mu.Unlock()
		t.Fatal("Import stays enabled while another operation runs")
	}
	reg.handlePalettes(h, save) // a click that slipped through
	reg.mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	if list, _ := reg.PalettesList(); len(list) != 0 {
		t.Fatalf("a refused click stored %+v", list)
	}
}

func TestImportErrorIsShownAndNothingIsWritten(t *testing.T) {
	reg, h := openPalettesPage(t)
	reg.mu.Lock()
	h.fields["palette-import-path"] = ui.NewField("/does/not/exist.json")
	reg.mu.Unlock()
	press(t, reg, h, "palette-import-file")
	settle(t, reg, h, func(p *paletteUI) bool { return p.busy == "" && p.err != "" })
	reg.mu.Lock()
	if strings.HasPrefix(h.palettes.err, "theme:") {
		t.Errorf("error %q carries the package prefix (R13)", h.palettes.err)
	}
	reg.mu.Unlock()
	if list, _ := reg.PalettesList(); len(list) != 0 {
		t.Fatalf("a failed import wrote %+v", list)
	}
}

// R2: building the page reads the snapshot. With the directory gone, a
// rebuild still shows the row; only a refresh notices.
func TestPageRendersFromTheSnapshotNotTheDisk(t *testing.T) {
	reg, h := openPalettesPage(t)
	saveNord(t, reg, h)
	if err := os.RemoveAll(reg.paletteStore.Dir); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	reg.rebuildPanel(h)
	shown := hasText(h, "Nord copy")
	reg.mu.Unlock()
	if !shown {
		t.Fatal("a rebuild read the disk instead of the snapshot")
	}
}

func TestSwatchRasterMatchesTheColour(t *testing.T) {
	n := paletteSwatch("#102030", 24, "Primary swatch")
	if n.Kind != ui.KindImage || n.ImageSize != 24 || n.Image == nil {
		t.Fatalf("swatch node = %+v", n)
	}
	px := n.Image.Pix[:4] // BGRA
	if px[0] != 0x30 || px[1] != 0x20 || px[2] != 0x10 || px[3] != 0xff {
		t.Fatalf("pixel = % x, want 30 20 10 ff", px)
	}
	if n.Role != "img" || n.Name != "Primary swatch" {
		t.Fatalf("accessibility = %q/%q", n.Role, n.Name)
	}
}

func TestPaletteNamesAreNeverCardHeadings(t *testing.T) {
	reg, h := openPalettesPage(t)
	saveNord(t, reg, h)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	for _, n := range walk(h.root) {
		if n.Role == "heading" && strings.EqualFold(n.Text, "nord copy") {
			t.Fatal("a palette name was used as a heading, which lowercases it (sysc-872)")
		}
	}
	if !hasText(h, "Nord copy") {
		t.Fatal("the palette's name is not shown with its case intact")
	}
}
