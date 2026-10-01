package theme

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// writeTemp writes f to a new temporary file in the store directory (mode
// 0600) and returns its path. The directory is created on first use.
func (s *Store) writeTemp(f PaletteFile) (string, error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return "", fmt.Errorf("theme: palettes directory: %w", err)
	}
	data, err := encodePalette(f)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(s.Dir, ".palette-*.tmp")
	if err != nil {
		return "", fmt.Errorf("theme: write palette: %w", err)
	}
	_, werr := tmp.Write(data)
	serr := tmp.Sync()
	cerr := tmp.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("theme: write palette: %w", err)
	}
	return tmp.Name(), nil
}

// Save stores a new palette and returns its slug. The file is written to a
// temporary name and hard-linked into place, so claiming a slug is atomic and
// two saves of the same name can never overwrite each other.
func (s *Store) Save(f PaletteFile) (string, error) {
	f = f.normalized()
	if err := f.Validate(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tmp, err := s.writeTemp(f)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp)
	base := baseSlug(f.Name)
	for n := 1; n <= 999; n++ {
		slug := base
		if n > 1 {
			slug = fmt.Sprintf("%s-%d", base, n)
		}
		err := os.Link(tmp, filepath.Join(s.Dir, slug+".json"))
		if err == nil {
			return slug, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("theme: save palette: %w", err)
		}
	}
	return "", errors.New("theme: too many palettes share that name")
}

// Update replaces an existing palette. The new content must validate as
// written; a refused update leaves the stored file untouched.
func (s *Store) Update(slug string, f PaletteFile) error {
	f = f.normalized()
	if err := f.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replaceLocked(slug, f)
}

// Rename changes the display name only. It checks the file's shape but not its
// contrast, so a hand-edited palette that loads can still be renamed. The read
// and the write share one hold of mu, so a concurrent Update is never undone.
func (s *Store) Rename(slug, name string) error {
	name, err := cleanName(name)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.Load(slug)
	if err != nil {
		return err
	}
	f.Name = name
	return s.replaceLocked(slug, f)
}

// replaceLocked swaps a stored file for f. The caller holds s.mu.
func (s *Store) replaceLocked(slug string, f PaletteFile) error {
	path, err := s.path(slug)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("theme: no palette %q", slug)
	}
	tmp, err := s.writeTemp(f)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("theme: replace palette: %w", err)
	}
	return nil
}

// Delete removes one palette.
func (s *Store) Delete(slug string) error {
	path, err := s.path(slug)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("theme: no palette %q", slug)
		}
		return fmt.Errorf("theme: delete palette: %w", err)
	}
	return nil
}
