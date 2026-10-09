package theme

import (
	"fmt"
	"os"
	"path/filepath"
)

// PublishSelection atomically publishes a committed named palette; empty means
// a generated/custom palette that the named-theme consumers cannot reproduce.
func PublishSelection(path, name string) error {
	if name != "" && !HasPalette(name) {
		return fmt.Errorf("unknown shell palette %q", name)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".shell-theme-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0644); err != nil {
		return err
	}
	if _, err = f.WriteString(name + "\n"); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
