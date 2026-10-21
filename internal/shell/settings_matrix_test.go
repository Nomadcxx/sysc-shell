package shell

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// TestEverySettingsPageLaysOutEverywhere opens every section and page at the
// fitted size on three outputs and three scales. A layout error closes the
// surface in production (the audio panel did exactly that), so any error here
// is a failure a user would see.
func TestEverySettingsPageLaysOutEverywhere(t *testing.T) {
	for _, out := range [][2]int{{1280, 720}, {1536, 864}, {3440, 1440}} {
		reg := newPanelRegistry(t)
		entries := []launcher.Entry{
			{ID: "photo-editor", Name: "Photo Editor", Comment: "Edit photographs"},
			{ID: "foot", Name: "Foot", Comment: "Terminal emulator", TerminalApp: true},
		}
		reg.launcherSvc = launcher.NewService(launcher.ServiceConfig{Scan: func() []launcher.Entry { return entries }})
		reg.launcherApplications.Store(&launcherApplicationSnapshot{
			entries: entries, attemptedAt: time.Now(), available: true,
		})
		withTestBar(t, reg, 7, reg.cfg)
		if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: out[0], OutH: out[1]}); err != nil {
			t.Fatal(err)
		}
		panel := drainAux(t, reg, 2)[1].Open
		for _, section := range settings.SectionNames() {
			pages := settings.SectionPages(section)
			if len(pages) == 0 {
				pages = []string{""}
			}
			for _, page := range pages {
				for _, scale := range []int{120, 150, 180} {
					reg.mu.Lock()
					h := reg.panelHosts[PanelSettings]
					h.section, h.settingsPage, h.scale120 = section, page, scale
					reg.rebuildPanel(h)
					reg.mu.Unlock()
					if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), scale); err != nil {
						t.Errorf("%dx%d %s/%s @%d: %v", out[0], out[1], section, page, scale, err)
						continue
					}
					// Layout accepts a short body without complaint; the live
					// gate found one cut at the 240 fallback.
					reg.mu.Lock()
					body := keyedScroll(reg.panelHosts[PanelSettings].root, settingsBodyKey)
					if body == nil {
						t.Errorf("%dx%d %s/%s @%d: no scroll body", out[0], out[1], section, page, scale)
					} else if body.Bounds.H < int(panel.Height)/2 {
						t.Errorf("%dx%d %s/%s @%d: body %+v in a %d-tall pane", out[0], out[1], section, page, scale, body.Bounds, panel.Height)
					}
					if section == "Applications" && body != nil {
						for _, action := range []string{
							"applications:favourite:photo-editor",
							"applications:hidden:photo-editor",
							"applications:terminal:foot",
						} {
							node := findAction(reg.panelHosts[PanelSettings].root, action)
							if node == nil {
								t.Errorf("%dx%d %s/%s @%d: control %q is missing", out[0], out[1], section, page, scale, action)
								continue
							}
							if node.Bounds.W <= 0 || node.Bounds.H <= 0 ||
								node.Bounds.X < body.Bounds.X ||
								node.Bounds.X+node.Bounds.W > body.Bounds.X+body.Bounds.W ||
								node.Bounds.Y < body.Bounds.Y ||
								node.Bounds.Y+node.Bounds.H > body.Bounds.Y+body.Bounds.H {
								t.Errorf("%dx%d %s/%s @%d: control %q has bounds %+v outside body %+v",
									out[0], out[1], section, page, scale, action, node.Bounds, body.Bounds)
							}
						}
					}
					reg.mu.Unlock()
				}
			}
		}
	}
}

// TestPalettesListLaysOutWithEveryRowState lays out the Palettes list with an
// active, a corrupt and a delete-pending row, at the narrowest output and every
// scale. A row's name must keep a visible width beside its controls (R8).
func TestPalettesListLaysOutWithEveryRowState(t *testing.T) {
	for _, out := range [][2]int{{1280, 720}, {3440, 1440}} {
		reg := newPanelRegistry(t)
		st := withPaletteStore(t, reg)
		dark, _ := theme.NamedPalette("nord", "dark", false)
		light, _ := theme.NamedPalette("nord", "light", false)
		for _, name := range []string{"Active palette", "Pending deletion"} {
			if _, err := st.Save(theme.PaletteFile{Name: name, Dark: dark.Roles(), Light: light.Roles()}); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(st.Dir, "broken.json"), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		reg.refreshPalettes()
		withTestBar(t, reg, 7, reg.cfg)
		if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: out[0], OutH: out[1]}); err != nil {
			t.Fatal(err)
		}
		panel := drainAux(t, reg, 2)[1].Open
		for _, scale := range []int{120, 150, 180} {
			reg.mu.Lock()
			h := reg.panelHosts[PanelSettings]
			h.section, h.scale120 = "Palettes", scale
			h.draft.ThemeGen.Source, h.draft.ThemeGen.Seed = "custom", "active-palette"
			h.palettes.deleting = "pending-deletion"
			reg.rebuildPanel(h)
			reg.mu.Unlock()
			if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), scale); err != nil {
				t.Errorf("%dx%d @%d: %v", out[0], out[1], scale, err)
				continue
			}
			reg.mu.Lock()
			for _, n := range walk(reg.panelHosts[PanelSettings].root) {
				if (n.Text == "Active palette" || n.Text == "Pending deletion") && n.Bounds.W < 40 {
					t.Errorf("%dx%d @%d: name %q squeezed to %dpx", out[0], out[1], scale, n.Text, n.Bounds.W)
				}
			}
			reg.mu.Unlock()
		}
	}
}

// TestPalettesEditorLaysOutEverywhere lays out the role editor, with a failing
// pair so the summary and a row error are present, on three outputs and three
// scales. Save must sit in the first viewport: R9 moved it above the 49 rows
// so nobody scrolls to it.
func TestPalettesEditorLaysOutEverywhere(t *testing.T) {
	for _, out := range [][2]int{{1280, 720}, {1536, 864}, {3440, 1440}} {
		reg := newPanelRegistry(t)
		st := withPaletteStore(t, reg)
		dark, _ := theme.NamedPalette("nord", "dark", false)
		light, _ := theme.NamedPalette("nord", "light", false)
		slug, err := st.Save(theme.PaletteFile{Name: "Editor matrix", Dark: dark.Roles(), Light: light.Roles()})
		if err != nil {
			t.Fatal(err)
		}
		reg.refreshPalettes()
		withTestBar(t, reg, 7, reg.cfg)
		if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: out[0], OutH: out[1]}); err != nil {
			t.Fatal(err)
		}
		panel := drainAux(t, reg, 2)[1].Open
		for _, scale := range []int{120, 150, 180} {
			reg.mu.Lock()
			h := reg.panelHosts[PanelSettings]
			h.section, h.scale120 = "Palettes", scale
			reg.paletteOpenEditor(h, slug)
			h.palettes.mode = "dark"
			h.palettes.draft.Dark["on_surface"] = h.palettes.draft.Dark["surface"]
			h.palettes.dirty = true
			reg.rebuildPanel(h)
			reg.mu.Unlock()
			if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), scale); err != nil {
				t.Errorf("%dx%d @%d: %v", out[0], out[1], scale, err)
				continue
			}
			reg.mu.Lock()
			root := reg.panelHosts[PanelSettings].root
			body := findSettingsBody(root)
			save := findAction(root, "palette-save")
			reg.mu.Unlock()
			if body == nil || body.Bounds.H < int(panel.Height)/2 {
				t.Errorf("%dx%d @%d: body %+v in a %d-tall pane", out[0], out[1], scale, body, panel.Height)
			}
			if save == nil || save.Bounds.Y+save.Bounds.H > int(panel.Height) {
				t.Errorf("%dx%d @%d: Save at %+v, below the %d-tall first viewport", out[0], out[1], scale, save, panel.Height)
			}
		}
	}
}
