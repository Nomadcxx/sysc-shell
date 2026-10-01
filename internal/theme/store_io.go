package theme

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParseImport reads a palette from foreign bytes: the palette shape, or any
// plain {dark, light} role map such as matugen's colors.json. Unknown role keys
// are dropped, every role is required, and a palette that fails the contrast
// floors is repaired and the number of roles moved is reported. A palette that
// still fails is refused. Nothing is written here.
func ParseImport(data []byte, fallbackName string) (PaletteFile, int, error) {
	if len(data) > MaxPaletteBytes {
		return PaletteFile{}, 0, fmt.Errorf("theme: import is larger than %d KiB", MaxPaletteBytes>>10)
	}
	var raw struct {
		Name  string            `json:"name"`
		Dark  map[string]string `json:"dark"`
		Light map[string]string `json:"light"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return PaletteFile{}, 0, fmt.Errorf("theme: import is not a palette: %w", err)
	}
	name := strings.TrimSpace(raw.Name)
	if name == "" {
		name = strings.TrimSpace(fallbackName)
	}
	if name == "" {
		name = "Imported palette"
	}
	name, err := cleanName(name)
	if err != nil {
		return PaletteFile{}, 0, err
	}

	out := PaletteFile{Name: name}
	adjusted := 0
	for _, m := range []struct {
		label string
		in    map[string]string
		out   *map[string]string
	}{{"dark", raw.Dark, &out.Dark}, {"light", raw.Light, &out.Light}} {
		kept := make(map[string]string, len(roles))
		for _, r := range roles {
			v, ok := m.in[r.name]
			if !ok {
				return PaletteFile{}, 0, fmt.Errorf("theme: import %s: role %s is missing", m.label, r.name)
			}
			if !strictHex.MatchString(v) {
				return PaletteFile{}, 0, fmt.Errorf("theme: import %s: role %s is %q, not #RRGGBB", m.label, r.name, v)
			}
			kept[r.name] = strings.ToLower(v)
		}
		tok, err := ParseRoles(kept)
		if err != nil {
			return PaletteFile{}, 0, fmt.Errorf("theme: import %s: %w", m.label, err)
		}
		if tok.Valid(false) != nil {
			fixed := tok.Repair(false)
			if err := fixed.Valid(false); err != nil {
				return PaletteFile{}, 0, fmt.Errorf("theme: import %s cannot be made readable: %w", m.label, err)
			}
			adjusted += changedRoles(tok, fixed)
			kept = fixed.Roles()
		}
		*m.out = kept
	}
	return out, adjusted, nil
}

func changedRoles(a, b Tokens) int {
	am, bm := a.Roles(), b.Roles()
	n := 0
	for k, v := range am {
		if bm[k] != v {
			n++
		}
	}
	return n
}

// ReadImportFile reads a candidate import: a regular file of at most
// MaxPaletteBytes. A FIFO, device or directory is refused without blocking.
func ReadImportFile(path string) ([]byte, error) {
	data, err := readLimited(path)
	if err != nil {
		return nil, fmt.Errorf("theme: import %s: %w", path, err)
	}
	return data, nil
}

// ImportName is the display name an unnamed import takes from its filename.
func ImportName(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".json")
	base = strings.TrimSuffix(base, ".sysc-palette")
	return base
}

// ExportJSON returns the stored palette exactly as it would be exported.
func (s *Store) ExportJSON(slug string) ([]byte, error) {
	f, err := s.Load(slug)
	if err != nil {
		return nil, err
	}
	return encodePalette(f)
}

// ExportTo writes <slug>.sysc-palette.json (mode 0644, for sharing) into dir,
// creating it if needed and replacing an earlier export of the same palette.
func (s *Store) ExportTo(slug, dir string) (string, error) {
	data, err := s.ExportJSON(slug)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("theme: export directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".sysc-palette-*.tmp")
	if err != nil {
		return "", fmt.Errorf("theme: export: %w", err)
	}
	_, werr := tmp.Write(data)
	cherr := tmp.Chmod(0o644)
	cerr := tmp.Close()
	if err := errors.Join(werr, cherr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("theme: export: %w", err)
	}
	path := filepath.Join(dir, slug+".sysc-palette.json")
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("theme: export: %w", err)
	}
	return path, nil
}
