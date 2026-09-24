package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MaxPluginDirs bounds how many candidate directories one scan may consider.
// The ceiling is per root, so a mistake in the user's directory cannot hide
// packaged plugins by exhausting a shared budget.
const MaxPluginDirs = 128

// Source says where a candidate came from. It is shown in the manager, and it
// decides one thing: a user copy overrides a managed copy of the same plugin
// ID. Every other pairing of sources is a collision, and neither side wins.
type Source string

const (
	SourceUser    Source = "user"
	SourceSystem  Source = "system"
	SourceManaged Source = "managed"
)

// ManagedRoot is the directory the plugin store installs into. The shell owns
// it; hand-managed plugins live in the user root instead, so an install or a
// removal can never touch a directory the user placed.
func ManagedRoot() string {
	base := os.Getenv("XDG_DATA_HOME")
	// The specification requires an absolute path, as StateRoot does.
	if !filepath.IsAbs(base) {
		base = filepath.Join(os.Getenv("HOME"), ".local", "share")
	}
	return filepath.Join(base, "sysc-shell", "plugins")
}

// Root is one directory to scan.
type Root struct {
	Path   string
	Source Source
}

// DefaultRoots names the user plugin directory, the managed one, and the
// packaged one.
func DefaultRoots(systemDir string) []Root {
	roots := make([]Root, 0, 3)
	if base, err := os.UserConfigDir(); err == nil {
		roots = append(roots, Root{Path: filepath.Join(base, "sysc-shell", "plugins"), Source: SourceUser})
	}
	roots = append(roots, Root{Path: ManagedRoot(), Source: SourceManaged})
	if systemDir != "" {
		roots = append(roots, Root{Path: systemDir, Source: SourceSystem})
	}
	return roots
}

// Candidate is one discovered plugin directory.
//
// A rejected candidate keeps its directory and its reason and drops its
// manifest: the manager shows why a directory did not load, and nothing else
// in the shell can mistake a rejection for something startable.
type Candidate struct {
	Dir    string
	Source Source
	// Err is nil on a usable candidate.
	Err      error
	Manifest Manifest
	// MissingCommands are declared dependencies absent from PATH. Such a
	// candidate is valid and visible, but must not be started.
	MissingCommands []string
	// ShadowedBy is the user directory overriding this managed copy. A
	// shadowed candidate is valid and shown, and never started.
	ShadowedBy string
}

// Startable reports whether this candidate can run right now.
func (c Candidate) Startable() bool {
	return c.Err == nil && c.ShadowedBy == "" && len(c.MissingCommands) == 0
}

// Catalog is one complete scan result.
type Catalog struct {
	// Plugins holds every candidate, valid or rejected, ordered by directory
	// name so the manager renders stably across rescans.
	Plugins []Candidate
}

// Lookup returns the usable candidate with the given plugin ID.
func (c Catalog) Lookup(id string) (Candidate, bool) {
	for _, p := range c.Plugins {
		if p.Err == nil && p.ShadowedBy == "" && p.Manifest.ID == id {
			return p, true
		}
	}
	return Candidate{}, false
}

// Discover scans each root's immediate children and returns one complete
// candidate set.
//
// A scan either produces a whole set or fails: the caller replaces its
// discovery state atomically, so a rescan that cannot be completed leaves the
// previous state in place rather than half-retiring running plugins. An
// individual directory that fails to load is a candidate carrying its reason,
// not a scan failure, because one broken plugin must not hide the others.
func Discover(roots ...Root) (Catalog, error) {
	var found []Candidate
	for _, root := range roots {
		entries, err := os.ReadDir(root.Path)
		if os.IsNotExist(err) {
			// A user who has installed nothing has no plugin directory.
			continue
		}
		if err != nil {
			return Catalog{}, fmt.Errorf("plugin: scan %s: %w", root.Path, err)
		}
		var dirs []os.DirEntry
		for _, e := range entries {
			// The store keeps .staging and .prev beside installed plugins;
			// neither is a plugin, and nothing hidden is scanned.
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			// Stat rather than DirEntry.IsDir: a symlinked plugin directory
			// is a supported install (make install links a checkout into the
			// user root), and the dirent type of a symlink is not a dir. A
			// broken symlink fails Stat and is skipped.
			info, err := os.Stat(filepath.Join(root.Path, e.Name()))
			if err != nil || !info.IsDir() {
				continue
			}
			dirs = append(dirs, e)
		}
		if len(dirs) > MaxPluginDirs {
			return Catalog{}, fmt.Errorf("plugin: %s holds %d directories, more than the %d this shell scans",
				root.Path, len(dirs), MaxPluginDirs)
		}
		for _, e := range dirs {
			dir := filepath.Join(root.Path, e.Name())
			if _, err := os.Stat(filepath.Join(dir, ManifestName)); err != nil {
				// A directory with no manifest is not a plugin at all, so it
				// is not something to report as broken.
				continue
			}
			c := Candidate{Dir: dir, Source: root.Source}
			m, err := LoadManifest(dir)
			if err != nil {
				c.Err = err
			} else {
				c.Manifest = m
				c.MissingCommands = m.MissingCommands()
			}
			found = append(found, c)
		}
	}

	rejectDuplicates(found)
	sort.Slice(found, func(i, j int) bool {
		if a, b := found[i].Manifest.ID, found[j].Manifest.ID; a != b {
			return a < b
		}
		return found[i].Dir < found[j].Dir
	})
	return Catalog{Plugins: found}, nil
}

// rejectDuplicates fails every side of an ID collision.
//
// Neither side wins. Letting user content shadow packaged code would make a
// dropped-in directory silently replace a shipped plugin; letting packaged
// code win would hide the user's copy with no explanation. The collision is
// the fault, so each candidate names the other's path and a user can delete
// the one they did not mean to install.
func rejectDuplicates(found []Candidate) {
	byID := make(map[string][]int, len(found))
	for i, c := range found {
		if c.Err == nil {
			byID[c.Manifest.ID] = append(byID[c.Manifest.ID], i)
		}
	}
	for id, idx := range byID {
		if len(idx) < 2 {
			continue
		}
		if user, managed, ok := overridePair(found, idx); ok {
			found[managed].ShadowedBy = found[user].Dir
			continue
		}
		paths := make([]string, len(idx))
		for n, i := range idx {
			paths[n] = found[i].Dir
		}
		sort.Strings(paths)
		for _, i := range idx {
			found[i].Err = fmt.Errorf("plugin id %q is declared by more than one directory: %v", id, paths)
			found[i].Manifest = Manifest{}
			found[i].MissingCommands = nil
		}
	}
}

// overridePair reports whether a collision is one user copy over one managed
// copy: the only collision that resolves rather than rejects. A developer's
// checkout linked into the user root is how a plugin is worked on, and it has
// to be able to stand in front of the released copy the store installed.
// Every other pairing stays a fault, per the M6 rule.
func overridePair(found []Candidate, idx []int) (user, managed int, ok bool) {
	if len(idx) != 2 {
		return 0, 0, false
	}
	a, b := idx[0], idx[1]
	switch {
	case found[a].Source == SourceUser && found[b].Source == SourceManaged:
		return a, b, true
	case found[b].Source == SourceUser && found[a].Source == SourceManaged:
		return b, a, true
	}
	return 0, 0, false
}
