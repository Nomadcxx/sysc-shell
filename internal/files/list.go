package files

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// MaxEntries caps one listing so the KindList stays under the wire child limit.
const MaxEntries = 200

// Entry is one contained directory child the panel may show.
type Entry struct {
	Name    string
	Path    string
	Dir     bool
	Size    int64
	ModTime time.Time
}

// ListLimited lists at most limit entries and reports whether more exist.
func ListLimited(root, dir string, limit int, hidden bool) ([]Entry, bool, error) {
	out, err := list(root, dir, hidden)
	if err != nil {
		return nil, false, err
	}
	if len(out) > limit {
		return out[:limit], true, nil
	}
	return out, false, nil
}

// List lists at most MaxEntries entries.
func List(root, dir string) ([]Entry, error) {
	out, _, err := ListLimited(root, dir, MaxEntries, false)
	return out, err
}

// list reads dir after jailing it under root. Hidden names and symlink
// escapes are omitted, not errors, unless hidden is set.
func list(root, dir string, hidden bool) ([]Entry, error) {
	resolved, err := Contain(root, dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("files: stat: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("files: %s is not a directory", resolved)
	}
	ents, err := os.ReadDir(resolved)
	if err != nil {
		return nil, fmt.Errorf("files: read: %w", err)
	}
	out := make([]Entry, 0, len(ents))
	for _, ent := range ents {
		name := ent.Name()
		if name == "" || name == "." || name == ".." {
			continue
		}
		if name[0] == '.' && !hidden {
			continue
		}
		if strings.ContainsRune(name, 0) || strings.ContainsAny(name, "/\\") {
			continue
		}
		child := filepath.Join(resolved, name)
		contained, err := Contain(root, child)
		if err != nil {
			continue
		}
		st, err := os.Stat(contained)
		if err != nil {
			continue
		}
		out = append(out, Entry{Name: name, Path: contained, Dir: st.IsDir(), Size: st.Size(), ModTime: st.ModTime()})
	}
	slices.SortFunc(out, func(a, b Entry) int {
		if a.Dir != b.Dir {
			if a.Dir {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out, nil
}

// Walk moves from cwd to name (".." or a single path element) still under root.
func Walk(root, cwd, name string) (string, error) {
	here, err := Contain(root, cwd)
	if err != nil {
		return "", err
	}
	if name == ".." {
		rootRes, err := Contain(root, root)
		if err != nil {
			return "", err
		}
		if here == rootRes {
			return here, nil
		}
		return Contain(root, filepath.Dir(here))
	}
	if name == "" || name == "." || strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("files: name %q is not a single path element", name)
	}
	return Contain(root, filepath.Join(here, name))
}

// Rel displays path relative to root. The jail itself is "/".
func Rel(root, path string) (string, error) {
	rootRes, err := Contain(root, root)
	if err != nil {
		return "", err
	}
	pathRes, err := Contain(root, path)
	if err != nil {
		return "", err
	}
	if pathRes == rootRes {
		return "/", nil
	}
	rel, err := filepath.Rel(rootRes, pathRes)
	if err != nil {
		return "", err
	}
	return "/" + filepath.ToSlash(rel), nil
}

// CrumbTarget is the contained path for breadcrumb index 0 (the jail root)
// or the first index segments of cwd.
func CrumbTarget(root, cwd string, index int) (string, error) {
	if index < 0 {
		return "", fmt.Errorf("files: crumb %d", index)
	}
	if index == 0 {
		return Contain(root, root)
	}
	rel, err := Rel(root, cwd)
	if err != nil {
		return "", err
	}
	parts := relParts(rel)
	if index > len(parts) {
		return "", fmt.Errorf("files: crumb %d", index)
	}
	return Contain(root, filepath.Join(append([]string{root}, parts[:index]...)...))
}

func relParts(rel string) []string {
	if rel == "/" || rel == "" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(rel, "/"), "/")
}
