package shell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// TestPalettesPageRendersToPNG paints every state the custom-palettes live
// gate names (spec P18 with review amendments R5-R14), at the narrow laptop
// output and the desktop. Each must lay out and paint; the PNGs are for the
// eye, and SYSC_PALETTES_PNG_DIR keeps them somewhere other than a temp
// directory.
func TestPalettesPageRendersToPNG(t *testing.T) {
	dir := os.Getenv("SYSC_PALETTES_PNG_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	nord := func() theme.PaletteFile {
		dark, _ := theme.NamedPalette("nord", "dark", false)
		light, _ := theme.NamedPalette("nord", "light", false)
		return theme.PaletteFile{Dark: dark.Roles(), Light: light.Roles()}
	}
	save := func(t *testing.T, st *theme.Store, name string) string {
		f := nord()
		f.Name = name
		slug, err := st.Save(f)
		if err != nil {
			t.Fatal(err)
		}
		return slug
	}
	three := func(t *testing.T, reg *Registry, h *PanelHost, st *theme.Store) {
		save(t, st, "Nord copy")
		save(t, st, "Evening")
		if err := os.WriteFile(filepath.Join(st.Dir, "broken.json"), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		reg.palettes = listPalettes(reg.paletteStore) // the lock is held
		h.draft.ThemeGen.Source, h.draft.ThemeGen.Seed = "custom", "nord-copy"
	}
	editor := func(t *testing.T, reg *Registry, h *PanelHost, st *theme.Store) {
		save(t, st, "Nord copy")
		reg.palettes = listPalettes(reg.paletteStore) // the lock is held
		reg.paletteOpenEditor(h, "nord-copy")
		h.palettes.mode = "dark"
	}
	states := []struct {
		name  string
		setup func(*testing.T, *Registry, *PanelHost, *theme.Store)
	}{
		{"list-empty", func(*testing.T, *Registry, *PanelHost, *theme.Store) {}},
		{"list-rows", three},
		{"list-busy-save", func(t *testing.T, r *Registry, h *PanelHost, st *theme.Store) {
			three(t, r, h, st)
			h.palettes.busy = "palette-save-current"
		}},
		{"list-delete-confirm", func(t *testing.T, r *Registry, h *PanelHost, st *theme.Store) {
			three(t, r, h, st)
			h.palettes.deleting = "evening"
		}},
		{"list-delete-confirm-in-use", func(t *testing.T, r *Registry, h *PanelHost, st *theme.Store) {
			three(t, r, h, st)
			h.palettes.deleting = "nord-copy"
		}},
		{"list-import-error", func(t *testing.T, r *Registry, h *PanelHost, st *theme.Store) {
			three(t, r, h, st)
			h.palettes.finish("", errors.New("theme: import /home/u/Downloads/x.json: open /home/u/Downloads/x.json: no such file or directory"))
		}},
		{"list-suggestion-taken", func(t *testing.T, r *Registry, h *PanelHost, st *theme.Store) {
			save(t, st, "Nord copy")
			r.palettes = listPalettes(r.paletteStore) // the lock is held; refreshPalettes would take it
			h.draft.ThemeGen.Source, h.draft.ThemeGen.Seed = "palette", "nord"
			delete(h.fields, "palette-name")
		}},
		{"editor-clean", editor},
		{"editor-dirty", func(t *testing.T, r *Registry, h *PanelHost, st *theme.Store) {
			editor(t, r, h, st)
			h.palettes.draft.Dark["primary"] = "#81a1c1"
			h.palettes.dirty = true
		}},
		{"editor-in-use", func(t *testing.T, r *Registry, h *PanelHost, st *theme.Store) {
			editor(t, r, h, st)
			h.draft.ThemeGen.Source, h.draft.ThemeGen.Seed = "custom", "nord-copy"
		}},
		{"editor-light", func(t *testing.T, r *Registry, h *PanelHost, st *theme.Store) {
			editor(t, r, h, st)
			h.palettes.mode = "light"
		}},
		{"editor-invalid-hex", func(t *testing.T, r *Registry, h *PanelHost, st *theme.Store) {
			editor(t, r, h, st)
			h.fields["palette-role:primary"] = ui.NewField("#12")
		}},
		{"editor-contrast", func(t *testing.T, r *Registry, h *PanelHost, st *theme.Store) {
			editor(t, r, h, st)
			d := h.palettes.draft.Dark
			d["on_primary"] = d["primary"]
			h.palettes.draft.Light["on_surface"] = h.palettes.draft.Light["surface"]
			h.palettes.dirty = true
		}},
		{"editor-after-fix", func(t *testing.T, r *Registry, h *PanelHost, st *theme.Store) {
			editor(t, r, h, st)
			d := h.palettes.draft.Dark
			d["on_primary"] = d["primary"]
			paletteFixDraft(&h.palettes)
		}},
		{"editor-save-refused", func(t *testing.T, r *Registry, h *PanelHost, st *theme.Store) {
			editor(t, r, h, st)
			d := h.palettes.draft.Dark
			d["on_primary"] = d["primary"]
			h.palettes.dirty = true
			r.paletteSaveDraft(h, "palette-save", false)
		}},
	}
	outputs := []struct {
		w, h, scale int
	}{{1280, 720, 120}, {1280, 720, 180}, {3440, 1440, 120}, {3440, 1440, 180}}
	for _, o := range outputs {
		for _, tc := range states {
			name := fmt.Sprintf("palettes-%s-%dx%d-%d", tc.name, o.w, o.h, o.scale)
			t.Run(name, func(t *testing.T) {
				reg := newPanelRegistry(t)
				keepInvalidationsDrained(t, reg)
				st := withPaletteStore(t, reg)
				withTestBar(t, reg, 7, reg.cfg)
				if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: o.w, OutH: o.h}); err != nil {
					t.Fatal(err)
				}
				panel := drainAux(t, reg, 2)[1].Open
				reg.mu.Lock()
				h := reg.panelHosts[PanelSettings]
				h.section, h.scale120 = "Palettes", o.scale
				reg.rebuildPanel(h)
				// A bare test registry never generated a palette; its
				// "waiting for the first wallpaper" reason is not a state
				// of this page.
				reg.themeErr = ""
				tc.setup(t, reg, h, st)
				// A setup that edits the draft directly re-seeds the role
				// fields from it, as typing into them would have.
				if h.fields["palette-role:primary"] == nil || h.fields["palette-role:primary"].Text != "#12" {
					resetPaletteFields(h)
				}
				reg.mu.Unlock()
				// Settle through the serialised refresh, so the open's own
				// refresh cannot land after the setup's saves.
				reg.refreshPalettes()
				reg.mu.Lock()
				h.set = reg.settingsForLocked(h.draft)
				reg.rebuildPanel(h)
				reg.mu.Unlock()
				settleHostAnimation(reg, h)
				paintPluginStorePNG(t, panel, o.scale, filepath.Join(dir, name+".png"))
			})
		}
	}
}
