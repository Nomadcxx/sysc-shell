package shell

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestPaletteSaveCurrentSnapshotsBothModes(t *testing.T) {
	reg := newPanelRegistry(t)
	withPaletteStore(t, reg)
	cfg := reg.cfg
	cfg.ThemeGen.Source, cfg.ThemeGen.Seed = "palette", "nord"

	slug, err := reg.PaletteSaveCurrent(cfg, "Nord copy")
	if err != nil || slug != "nord-copy" {
		t.Fatalf("slug = %q, err = %v", slug, err)
	}
	f, err := reg.paletteStore.Load(slug)
	if err != nil {
		t.Fatal(err)
	}
	wantDark, _ := theme.NamedPalette("nord", "dark", false)
	wantLight, _ := theme.NamedPalette("nord", "light", false)
	if f.Dark["primary"] != wantDark.Primary || f.Light["primary"] != wantLight.Primary {
		t.Fatal("the saved palette is not the compiled Nord in both modes")
	}
}

func TestPalettesListMarksTheActiveOne(t *testing.T) {
	reg := newPanelRegistry(t)
	withPaletteStore(t, reg)
	cfg := reg.cfg
	cfg.ThemeGen.Source, cfg.ThemeGen.Seed = "palette", "gruvbox"
	slug, err := reg.PaletteSaveCurrent(cfg, "Gruv")
	if err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	reg.cfg.ThemeGen.Source, reg.cfg.ThemeGen.Seed = "custom", slug
	reg.mu.Unlock()

	list, err := reg.PalettesList()
	if err != nil || len(list) != 1 || list[0].Slug != slug || !list[0].Active {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestPaletteDeleteOfTheActivePaletteKeepsPaintingAndExplains(t *testing.T) {
	reg := newPanelRegistry(t)
	withPaletteStore(t, reg)
	cfg := reg.cfg
	cfg.ThemeGen.Source, cfg.ThemeGen.Seed = "palette", "nord"
	slug, _ := reg.PaletteSaveCurrent(cfg, "Doomed")
	reg.mu.Lock()
	reg.cfg.ThemeGen.Source, reg.cfg.ThemeGen.Seed = "custom", slug
	before := reg.tokens
	reg.mu.Unlock()

	if err := reg.PaletteDelete(slug); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.tokens != before {
		t.Fatal("the published palette changed when the active one was deleted")
	}
	if reg.themeErr == "" {
		t.Fatal("no reason recorded for the missing palette")
	}
}

func TestPaletteRenameAndExportAndImportRoundTrip(t *testing.T) {
	reg := newPanelRegistry(t)
	st := withPaletteStore(t, reg)
	cfg := reg.cfg
	cfg.ThemeGen.Source, cfg.ThemeGen.Seed = "palette", "dracula"
	slug, _ := reg.PaletteSaveCurrent(cfg, "Dracula")
	if err := reg.PaletteRename(slug, "Renamed"); err != nil {
		t.Fatal(err)
	}
	data, err := st.ExportJSON(slug)
	if err != nil {
		t.Fatal(err)
	}
	got, adjusted, err := reg.PaletteImport(data, "ignored")
	if err != nil || adjusted != 0 || got != "renamed" {
		t.Fatalf("import = %q adjusted %d err %v; want slug renamed (the new display name) and no adjustment", got, adjusted, err)
	}
}

func TestPaletteOperationsReportMissingStore(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.mu.Lock()
	reg.paletteStore, reg.themeGen.Custom = nil, nil // as when there is no config directory
	reg.mu.Unlock()
	if _, err := reg.PalettesList(); err == nil {
		t.Fatal("PalettesList succeeded without a store")
	}
	if err := reg.PaletteDelete("x"); err == nil {
		t.Fatal("PaletteDelete succeeded without a store")
	}
}

func TestPaletteSuggestedName(t *testing.T) {
	reg := newPanelRegistry(t)
	withPaletteStore(t, reg)
	reg.mu.Lock()
	reg.palettes = []theme.PaletteInfo{{Slug: "my-nord", Name: "My Nord"}, {Slug: "nord-copy", Name: "Nord copy"}}
	reg.mu.Unlock()
	cases := []struct{ source, seed, want string }{
		{"palette", "nord", "Nord copy 2"},    // "Nord copy" is taken (R13)
		{"custom", "my-nord", "My Nord copy"}, // the display name, not the slug (R13)
		{"hex", "#112233", "Colour #112233"},
		{"stock", "ocean", "Ocean"},
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	for _, c := range cases {
		cfg := reg.cfg
		cfg.ThemeGen.Source, cfg.ThemeGen.Seed = c.source, c.seed
		if got := reg.paletteSuggestedNameLocked(cfg); got != c.want {
			t.Errorf("%s/%s: %q, want %q", c.source, c.seed, got, c.want)
		}
	}
	cfg := reg.cfg
	cfg.ThemeGen.Source, cfg.ThemeGen.Seed = "wallpaper", ""
	if got := reg.paletteSuggestedNameLocked(cfg); !strings.HasPrefix(got, "Wallpaper ") {
		t.Errorf("wallpaper suggestion = %q", got)
	}
}

func TestPaletteImportFileExpandsHome(t *testing.T) {
	reg := newPanelRegistry(t)
	st := withPaletteStore(t, reg)
	home := t.TempDir()
	t.Setenv("HOME", home)
	dark, _ := theme.NamedPalette("nord", "dark", false)
	light, _ := theme.NamedPalette("nord", "light", false)
	data, _ := json.Marshal(theme.PaletteFile{Name: "From home", Dark: dark.Roles(), Light: light.Roles()})
	if err := os.WriteFile(filepath.Join(home, "p.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	slug, _, err := reg.PaletteImportFile("~/p.json")
	if err != nil || slug != "from-home" {
		t.Fatalf("import ~/p.json = %q, %v", slug, err)
	}
	reg.mu.Lock()
	n := len(reg.palettes)
	reg.mu.Unlock()
	if n != 1 || len(st.List()) != 1 {
		t.Fatalf("snapshot holds %d after import, want 1 (mutations refresh it)", n)
	}
}

func TestThemeCallPalettesVerbs(t *testing.T) {
	reg := newPanelRegistry(t)
	withPaletteStore(t, reg)
	call := func(method string, params any) (map[string]any, error) {
		raw, _ := json.Marshal(params)
		return reg.ThemeCall(method, raw)
	}
	if _, err := call("theme.palettes.save", map[string]any{"name": ""}); err == nil {
		t.Fatal("save with no name succeeded")
	}
	reg.mu.Lock()
	reg.cfg.ThemeGen.Source, reg.cfg.ThemeGen.Seed = "palette", "nord"
	reg.mu.Unlock()
	out, err := call("theme.palettes.save", map[string]any{"name": "Via IPC"})
	if err != nil || out["slug"] != "via-ipc" {
		t.Fatalf("save = %v, %v", out, err)
	}
	list, err := call("theme.palettes.list", nil)
	if err != nil || len(list["palettes"].([]map[string]any)) != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}
	if _, err := call("theme.palettes.rename", map[string]any{"slug": "via-ipc", "name": "Renamed"}); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	out, err = call("theme.palettes.export", map[string]any{"slug": "via-ipc"})
	if err != nil || !strings.HasSuffix(out["path"].(string), "via-ipc.sysc-palette.json") {
		t.Fatalf("export = %v, %v", out, err)
	}
	if _, err := call("theme.palettes.delete", map[string]any{"slug": "via-ipc"}); err != nil {
		t.Fatal(err)
	}
	if _, err := call("theme.palettes.delete", map[string]any{"slug": "via-ipc"}); err == nil {
		t.Fatal("second delete succeeded")
	}
	if _, err := call("theme.palettes.import", map[string]any{"path": "/does/not/exist.json"}); err == nil {
		t.Fatal("import of a missing path succeeded")
	}
}
