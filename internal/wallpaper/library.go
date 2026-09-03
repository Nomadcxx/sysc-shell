package wallpaper

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// Filter is the All / Images / Videos selector in the picker chrome. It never
// hides a directory: filtering the media must not make the library
// unnavigable.
type Filter uint8

const (
	FilterAll Filter = iota
	FilterImages
	FilterVideos
)

// Entry is one row in a directory: a playable file, or a child directory the
// picker can descend into. Directories carry KindUnknown.
type Entry struct {
	Name  string
	Path  string
	Kind  Kind
	IsDir bool
}

// Library is a directory index, built once away from the Wayland owner and
// read many times. Opening the picker projects this; it does not rescan (D11).
type Library struct {
	// dirs maps an absolute directory to its immediate entries, already sorted
	// with directories first.
	dirs map[string][]Entry
	// roots bound navigation: Up stops at a root rather than walking out into
	// the rest of the filesystem.
	roots []string
	// Err is the first scan failure, kept as a string because it is projected
	// into a banner rather than handled.
	Err string
}

// Scan indexes each root recursively. Duplicate roots are indexed once, which
// is what makes D9's shared image-and-video directory list each file a single
// time.
func Scan(roots []string) *Library {
	lib := &Library{dirs: map[string][]Entry{}}
	for _, root := range roots {
		abs, err := filepath.Abs(expandHome(root))
		if err != nil {
			lib.note(err)
			continue
		}
		if slices.Contains(lib.roots, abs) {
			continue
		}
		lib.roots = append(lib.roots, abs)
		lib.walk(abs)
	}
	for dir, entries := range lib.dirs {
		slices.SortFunc(entries, compareEntries)
		lib.dirs[dir] = entries
	}
	return lib
}

// compareEntries puts directories first, then sorts by name, so navigation
// stays where the eye expects it as the library grows.
func compareEntries(a, b Entry) int {
	if a.IsDir != b.IsDir {
		if a.IsDir {
			return -1
		}
		return 1
	}
	return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
}

func (l *Library) note(err error) {
	if l.Err == "" && err != nil {
		l.Err = err.Error()
	}
}

// walk indexes one root. A directory that cannot be read is recorded and
// skipped: one bad subdirectory must not lose the rest of the library.
func (l *Library) walk(root string) {
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			l.note(err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == root {
			// Ensure the root lists even when it holds nothing.
			if _, ok := l.dirs[root]; !ok {
				l.dirs[root] = nil
			}
			return nil
		}
		name := d.Name()
		// A name that is not UTF-8 cannot be carried through the panel or the
		// assignment file, so it is dropped at the index rather than failing
		// later at apply time (D11).
		if !utf8.ValidString(name) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		parent := filepath.Dir(path)
		if d.IsDir() {
			l.dirs[parent] = append(l.dirs[parent], Entry{Name: name, Path: path, IsDir: true})
			if _, ok := l.dirs[path]; !ok {
				l.dirs[path] = nil
			}
			return nil
		}
		if kind := ClassifyName(name); kind != KindUnknown {
			l.dirs[parent] = append(l.dirs[parent], Entry{Name: name, Path: path, Kind: kind})
		}
		return nil
	})
	l.note(err)
}

// Roots returns the indexed library roots.
func (l *Library) Roots() []string { return slices.Clone(l.roots) }

// View returns one directory's entries after the filter and the search box.
//
// The two narrow differently. The kind filter exempts child directories, so
// switching to Videos does not hide the folders you need to reach them. Search
// applies to every entry by name, because a folder that does not match what
// was typed is noise in a result list rather than a route.
func (l *Library) View(dir string, filter Filter, search string) []Entry {
	entries := l.dirs[dir]
	needle := strings.ToLower(strings.TrimSpace(search))
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir && !matchesFilter(e.Kind, filter) {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(e.Name), needle) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func matchesFilter(kind Kind, filter Filter) bool {
	switch filter {
	case FilterImages:
		return kind == KindImage
	case FilterVideos:
		return kind == KindVideo
	}
	return true
}

// Parent returns the directory to go Up to, and false at a root. Navigation is
// bounded by the roots so the picker cannot be walked out of the library.
func (l *Library) Parent(dir string) (string, bool) {
	if slices.Contains(l.roots, dir) {
		return "", false
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		return "", false
	}
	if _, ok := l.dirs[parent]; !ok {
		return "", false
	}
	return parent, true
}

// expandHome resolves a leading ~ against the current user's home. The
// configured directories keep the tilde literal so config.Default() stays
// independent of the environment; this is where it is resolved.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
}
