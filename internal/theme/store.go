package theme

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
)

// Store is a directory of custom palettes, one <slug>.json each. It does no
// caching: every call reads the directory, so a hand-edited file is picked up
// on the next use. Writes are serialised by mu.
type Store struct {
	Dir string
	mu  sync.Mutex
}

// PaletteInfo describes one file in the store. Err is non-nil when the file
// cannot be used; Name is then the slug, because the name is unreadable, and
// File is empty. A usable entry carries the decoded file.
type PaletteInfo struct {
	Slug string
	Name string
	File PaletteFile
	Err  error
}

func (s *Store) path(slug string) (string, error) {
	if !ValidSlug(slug) {
		return "", fmt.Errorf("theme: %q is not a palette id", slug)
	}
	return filepath.Join(s.Dir, slug+".json"), nil
}

// readLimited reads a regular file of at most MaxPaletteBytes. It opens
// non-blocking and checks the mode after opening, so a FIFO or device where a
// file should be is refused instead of hanging the caller.
func readLimited(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxPaletteBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxPaletteBytes {
		return nil, fmt.Errorf("larger than %d KiB", MaxPaletteBytes>>10)
	}
	return data, nil
}

func decodePalette(data []byte) (PaletteFile, error) {
	var f PaletteFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return PaletteFile{}, fmt.Errorf("theme: palette file: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return PaletteFile{}, errors.New("theme: palette file has data after the palette")
	}
	if err := f.validateShape(); err != nil {
		return PaletteFile{}, err
	}
	return f, nil
}

func encodePalette(f PaletteFile) ([]byte, error) {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("theme: encode palette: %w", err)
	}
	return append(data, '\n'), nil
}

// Load reads one palette, checking its shape but not its contrast.
func (s *Store) Load(slug string) (PaletteFile, error) {
	p, err := s.path(slug)
	if err != nil {
		return PaletteFile{}, err
	}
	data, err := readLimited(p)
	if err != nil {
		return PaletteFile{}, fmt.Errorf("theme: palette %q: %w", slug, err)
	}
	f, err := decodePalette(data)
	if err != nil {
		return PaletteFile{}, fmt.Errorf("palette %q: %w", slug, err)
	}
	return f, nil
}

// Tokens resolves one mode of a stored palette for the floor in force.
func (s *Store) Tokens(slug, mode string, highContrast bool) (Tokens, error) {
	f, err := s.Load(slug)
	if err != nil {
		return Tokens{}, err
	}
	return f.Tokens(mode, highContrast)
}

// List returns every palette file whose name is a valid slug, usable or not,
// ordered by display name. A missing directory is an empty store.
func (s *Store) List() []PaletteInfo {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil
	}
	var out []PaletteInfo
	for _, e := range entries {
		slug, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !ValidSlug(slug) {
			continue
		}
		f, err := s.Load(slug)
		info := PaletteInfo{Slug: slug, Name: f.Name, File: f, Err: err}
		if err != nil {
			info.Name, info.File = slug, PaletteFile{}
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}
