package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// MaxEntries caps one listing so the KindList stays under the wire child limit.
const MaxEntries = 200

// Entry is one contained directory child the panel may show. Path is the
// lexical child of the resolved directory the listing ran in: for a symlink
// it names the link itself, so selection, delete and rename act on the link
// and two links to one target stay distinct rows. Target is Path after
// symlink resolution; follow it to open or preview what the link points at.
// Children whose resolution escapes the root are omitted, so Target is
// always in the jail.
type Entry struct {
	Name    string
	Path    string
	Target  string
	Dir     bool
	Size    int64
	ModTime time.Time
}

// ListLimited lists at most limit entries and reports whether more exist.
func ListLimited(root, dir string, limit int, hidden bool) ([]Entry, bool, error) {
	if limit <= 0 {
		return nil, false, nil
	}
	resolved, err := listingDir(root, dir)
	if err != nil {
		return nil, false, err
	}
	candidates, total, err := readListingCandidates(root, resolved, limit, hidden)
	if err != nil {
		return nil, false, err
	}
	out := make([]Entry, 0, limit)
	for _, c := range candidates {
		if len(out) == limit {
			break
		}
		entry, ok := resolveCandidate(root, resolved, c)
		if !ok {
			continue
		}
		out = append(out, entry)
	}
	slices.SortFunc(out, compareEntries)
	return out, total > len(out), nil
}

// listingCandidate keeps only what ordering needs. Paths and metadata are
// resolved for the chosen page so a directory with many children cannot
// make the panel resolve every one of them.
type listingCandidate struct {
	name string
	dir  bool
}

const listingBatch = 4096

// listingCandidateSlack leaves room for entries that disappear or fail to
// resolve between the scan and the final stat.
const listingCandidateSlack = 16

func listingDir(root, dir string) (string, error) {
	resolved, err := Contain(root, dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("files: stat: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("files: %s is not a directory", resolved)
	}
	return resolved, nil
}

func validListingName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsRune(name, 0) && !strings.ContainsAny(name, "/\\")
}

// readListingCandidates streams the directory and retains only the first
// page worth of names from each bucket (directories then files).
func readListingCandidates(root, resolved string, limit int, hidden bool) ([]listingCandidate, int, error) {
	f, err := os.Open(resolved)
	if err != nil {
		return nil, 0, fmt.Errorf("files: read: %w", err)
	}
	defer f.Close()

	keep := limit + listingCandidateSlack
	dirs := make([]listingCandidate, 0, keep)
	regular := make([]listingCandidate, 0, keep)
	total := 0
	for {
		ents, readErr := f.ReadDir(listingBatch)
		for _, ent := range ents {
			name := ent.Name()
			if !validListingName(name) {
				continue
			}
			if name[0] == '.' && !hidden {
				continue
			}
			isDir := ent.IsDir()
			if ent.Type()&fs.ModeSymlink != 0 {
				contained, err := Contain(root, filepath.Join(resolved, name))
				if err != nil {
					continue
				}
				st, err := os.Stat(contained)
				if err != nil {
					continue
				}
				isDir = st.IsDir()
			}
			total++
			c := listingCandidate{name: name, dir: isDir}
			if isDir {
				dirs = append(dirs, c)
				if len(dirs) > keep*2 {
					dirs = firstCandidates(dirs, keep)
				}
			} else {
				regular = append(regular, c)
				if len(regular) > keep*2 {
					regular = firstCandidates(regular, keep)
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, 0, fmt.Errorf("files: read: %w", readErr)
		}
	}
	candidates := append(firstCandidates(dirs, keep), firstCandidates(regular, keep)...)
	return candidates, total, nil
}

func firstCandidates(in []listingCandidate, keep int) []listingCandidate {
	slices.SortFunc(in, compareCandidates)
	if len(in) > keep {
		return in[:keep]
	}
	return in
}

func resolveCandidate(root, resolved string, c listingCandidate) (Entry, bool) {
	child := filepath.Join(resolved, c.name)
	contained, err := Contain(root, child)
	if err != nil {
		return Entry{}, false
	}
	st, err := os.Stat(contained)
	if err != nil {
		return Entry{}, false
	}
	return Entry{Name: c.name, Path: child, Target: contained, Dir: st.IsDir(), Size: st.Size(), ModTime: st.ModTime()}, true
}

func compareCandidates(a, b listingCandidate) int {
	if a.dir != b.dir {
		if a.dir {
			return -1
		}
		return 1
	}
	return compareEntryNames(a.name, b.name)
}

func compareEntries(a, b Entry) int {
	if a.Dir != b.Dir {
		if a.Dir {
			return -1
		}
		return 1
	}
	return compareEntryNames(a.Name, b.Name)
}

func compareEntryNames(a, b string) int {
	if n := strings.Compare(strings.ToLower(a), strings.ToLower(b)); n != 0 {
		return n
	}
	return strings.Compare(a, b)
}

// LoadErrorCopy words a folder-read failure for the panel. A folder that
// cannot be reached at all — a stale SFTP/GVFS mount answers ENOENT,
// ENOTCONN, EIO, ENODEV, ESHUTDOWN or ESTALE — gets mount-disappeared copy
// distinct from operation failures, which keep their raw text. The errno is
// preserved after the dash for diagnosis.
//
// ponytail: errno classification; /proc/mounts freshness would be the
// upgrade if the wording ever needs to be exact.
func LoadErrorCopy(err error) string {
	for _, gone := range []error{
		fs.ErrNotExist,
		syscall.ENOTCONN, syscall.EIO, syscall.ENODEV, syscall.ESHUTDOWN, syscall.ESTALE,
	} {
		if errors.Is(err, gone) {
			return fmt.Sprintf("Folder unavailable, it may have unmounted: %v", err)
		}
	}
	return err.Error()
}

// List lists at most MaxEntries entries.
func List(root, dir string) ([]Entry, error) {
	out, _, err := ListLimited(root, dir, MaxEntries, false)
	return out, err
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
