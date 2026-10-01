package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/screenshot"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// PaletteSummary is one saved palette as the list and the IPC verbs show it.
type PaletteSummary struct {
	Slug   string
	Name   string
	Err    string // why the file is unusable; empty when it loads
	Active bool
}

var errNoPaletteStore = errors.New("palettes are unavailable: the shell has no config directory")

// paletteStoreOrErr returns the store. It takes Registry.mu itself, so it must
// not be called with the lock held; locked callers read r.paletteStore.
func (r *Registry) paletteStoreOrErr() (*theme.Store, error) {
	r.mu.Lock()
	st := r.paletteStore
	r.mu.Unlock()
	if st == nil {
		return nil, errNoPaletteStore
	}
	return st, nil
}

func paletteActive(cfg config.Config, slug string) bool {
	return cfg.ThemeGen.Source == "custom" && cfg.ThemeGen.Seed == slug
}

// PalettesList is the saved palettes with the in-use one marked, per the
// persisted configuration (the file the IPC and settings writers share). It
// re-reads the directory first, so a hand edit is reported.
func (r *Registry) PalettesList() ([]PaletteSummary, error) {
	if _, err := r.paletteStoreOrErr(); err != nil {
		return nil, err
	}
	r.refreshPalettes()
	cfg := r.themeSnapshot()
	r.mu.Lock()
	list := r.palettes
	r.mu.Unlock()
	var out []PaletteSummary
	for _, p := range list {
		s := PaletteSummary{Slug: p.Slug, Name: p.Name, Active: paletteActive(cfg, p.Slug)}
		if p.Err != nil {
			s.Err = p.Err.Error()
		}
		out = append(out, s)
	}
	return out, nil
}

// PaletteSaveCurrent stores the palette cfg resolves to, both modes, under
// name. It generates in an isolated cache (matugen can take seconds), so it
// must run off the owner and never under Registry.mu.
func (r *Registry) PaletteSaveCurrent(cfg config.Config, name string) (string, error) {
	st, err := r.paletteStoreOrErr()
	if err != nil {
		return "", err
	}
	defer r.refreshPalettes()
	cfg.Accessibility.HighContrast = false // store the standard values
	f := theme.PaletteFile{Name: name}
	for _, mode := range []string{"dark", "light"} {
		c := cfg
		c.ThemeGen.Mode = mode
		tok, err := r.generatePreviewOnly(c)
		if err != nil {
			return "", fmt.Errorf("%s palette: %w", mode, err)
		}
		if mode == "dark" {
			f.Dark = tok.Roles()
		} else {
			f.Light = tok.Roles()
		}
	}
	return st.Save(f)
}

// PaletteUpdate replaces a saved palette's colours. If it is the one in use the
// theme is regenerated.
func (r *Registry) PaletteUpdate(slug string, f theme.PaletteFile) error {
	st, err := r.paletteStoreOrErr()
	if err != nil {
		return err
	}
	defer r.refreshPalettes()
	if err := st.Update(slug, f); err != nil {
		return err
	}
	r.repaintIfActive(slug)
	return nil
}

// PaletteRename changes the display name; the slug and the config are untouched.
func (r *Registry) PaletteRename(slug, name string) error {
	st, err := r.paletteStoreOrErr()
	if err != nil {
		return err
	}
	defer r.refreshPalettes()
	return st.Rename(slug, name)
}

// PaletteDelete removes a palette. If it was in use the shell keeps painting
// its last complete palette and records why it is not the requested one.
func (r *Registry) PaletteDelete(slug string) error {
	st, err := r.paletteStoreOrErr()
	if err != nil {
		return err
	}
	defer r.refreshPalettes()
	if err := st.Delete(slug); err != nil {
		return err
	}
	r.repaintIfActive(slug)
	return nil
}

func (r *Registry) repaintIfActive(slug string) {
	cfg := r.themeSnapshot()
	if paletteActive(cfg, slug) {
		r.republishTheme(cfg)
	}
}

// PaletteExport writes <slug>.sysc-palette.json to the Downloads directory and
// returns the path. The directory is created if it does not exist.
func (r *Registry) PaletteExport(slug string) (string, error) {
	st, err := r.paletteStoreOrErr()
	if err != nil {
		return "", err
	}
	return st.ExportTo(slug, screenshot.UserDownloadsDir())
}

// PaletteCopyJSON puts the exported JSON on the clipboard.
func (r *Registry) PaletteCopyJSON(slug string) error {
	st, err := r.paletteStoreOrErr()
	if err != nil {
		return err
	}
	data, err := st.ExportJSON(slug)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return plugin.WriteClipboardText(ctx, string(data))
}

// PaletteImport stores a palette parsed from bytes and reports how many roles
// were adjusted to meet the contrast floor.
func (r *Registry) PaletteImport(data []byte, fallbackName string) (string, int, error) {
	st, err := r.paletteStoreOrErr()
	if err != nil {
		return "", 0, err
	}
	f, adjusted, err := theme.ParseImport(data, fallbackName)
	if err != nil {
		return "", 0, err
	}
	defer r.refreshPalettes()
	slug, err := st.Save(f)
	return slug, adjusted, err
}

// expandHome resolves a leading "~/" the way a shell would; people type it,
// and the Downloads path the field starts with is shown with it (R6).
func expandHome(path string) string {
	path = strings.TrimSpace(path)
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return path
}

// PaletteImportFile imports from a path; only a regular file under 64 KiB.
func (r *Registry) PaletteImportFile(path string) (string, int, error) {
	path = expandHome(path)
	data, err := theme.ReadImportFile(path)
	if err != nil {
		return "", 0, err
	}
	return r.PaletteImport(data, theme.ImportName(path))
}

// paletteSuggestedNameLocked proposes a name for saving the palette cfg
// resolves to. It never proposes a name already in the snapshot ("Nord copy
// 2"), and a custom source is named by its display name, not its slug (R13).
// Registry.mu is held.
func (r *Registry) paletteSuggestedNameLocked(cfg config.Config) string {
	title := func(s string) string {
		s = strings.NewReplacer("-", " ", "_", " ").Replace(s)
		if s == "" {
			return s
		}
		return strings.ToUpper(s[:1]) + s[1:]
	}
	g := cfg.ThemeGen
	base := "Wallpaper " + time.Now().Format("2006-01-02")
	switch g.Source {
	case "palette":
		base = title(g.Seed) + " copy"
	case "custom":
		name := title(g.Seed)
		for _, p := range r.palettes {
			if p.Slug == g.Seed && p.Err == nil {
				name = p.Name
			}
		}
		base = name + " copy"
	case "hex":
		base = "Colour " + g.Seed
	case "stock":
		base = title(g.Seed)
	}
	taken := make(map[string]bool, len(r.palettes))
	for _, p := range r.palettes {
		taken[strings.ToLower(p.Name)] = true
	}
	name := base
	for n := 2; taken[strings.ToLower(name)]; n++ {
		name = fmt.Sprintf("%s %d", base, n)
	}
	return name
}
