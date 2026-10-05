package files

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ponytail: one-shot copy under the lock's caller; worker+progress if a tree
// actually hits these.
const (
	maxCopyFiles = 500
	maxCopyBytes = 512 << 20
)

// MkdirUntitled creates Untitled Folder, then Untitled Folder 2, … under cwd.
func MkdirUntitled(root, cwd string) (string, error) {
	here, err := Contain(root, cwd)
	if err != nil {
		return "", err
	}
	name := "Untitled Folder"
	for i := 2; i < 100; i++ {
		dest := filepath.Join(here, name)
		if _, err := os.Lstat(dest); err != nil {
			if !os.IsNotExist(err) {
				return "", err
			}
			if err := os.Mkdir(dest, 0o755); err != nil {
				return "", err
			}
			return Contain(root, dest)
		}
		name = fmt.Sprintf("Untitled Folder %d", i)
	}
	return "", fmt.Errorf("files: too many Untitled Folder names")
}

// CopyInto copies srcs into cwd. Existing names get " (copy)".
func CopyInto(root, cwd string, srcs []string) ([]string, error) {
	return transferInto(root, cwd, srcs, false)
}

// MoveInto moves srcs into cwd. Same-path moves are skipped.
func MoveInto(root, cwd string, srcs []string) ([]string, error) {
	return transferInto(root, cwd, srcs, true)
}

func transferInto(root, cwd string, srcs []string, cut bool) ([]string, error) {
	here, err := Contain(root, cwd)
	if err != nil {
		return nil, err
	}
	filesN, bytesN := 0, int64(0)
	var made []string
	for _, src := range srcs {
		dest, err := transferOne(root, here, src, cut, &filesN, &bytesN)
		if err != nil {
			return made, err
		}
		if dest != "" {
			made = append(made, dest)
		}
	}
	return made, nil
}

func transferOne(root, cwd, src string, cut bool, filesN *int, bytesN *int64) (string, error) {
	src, err := Contain(root, src)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if st.IsDir() && (cwd == src || inside(src, cwd)) {
		return "", fmt.Errorf("files: copy %s into itself", src)
	}
	base := filepath.Base(src)
	if cut && filepath.Join(cwd, base) == src {
		return "", nil
	}
	name := uniqueName(cwd, base)
	if name == "" {
		return "", fmt.Errorf("files: no unique name for %s", base)
	}
	dest := filepath.Join(cwd, name)
	if cut {
		if err := os.Rename(src, dest); err == nil {
			return Contain(root, dest)
		}
	}
	if st.IsDir() {
		if err := copyDir(src, dest, filesN, bytesN); err != nil {
			_ = os.RemoveAll(dest)
			return "", err
		}
	} else {
		if err := copyFile(src, dest, st.Size(), filesN, bytesN); err != nil {
			_ = os.Remove(dest)
			return "", err
		}
	}
	contained, err := Contain(root, dest)
	if err != nil {
		_ = os.RemoveAll(dest)
		return "", err
	}
	if cut {
		if err := os.RemoveAll(src); err != nil {
			return contained, err
		}
	}
	return contained, nil
}

func copyDir(src, dest string, filesN *int, bytesN *int64) error {
	if err := os.Mkdir(dest, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == src {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		child := filepath.Join(dest, rel)
		if !inside(dest, child) && child != dest {
			return fmt.Errorf("files: copy escaped to %s", child)
		}
		if d.IsDir() {
			return os.Mkdir(child, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, child, info.Size(), filesN, bytesN)
	})
}

func copyFile(src, dest string, size int64, filesN *int, bytesN *int64) error {
	if *filesN+1 > maxCopyFiles {
		return fmt.Errorf("files: copy exceeds %d files", maxCopyFiles)
	}
	if *bytesN+size > maxCopyBytes {
		return fmt.Errorf("files: copy exceeds %d bytes", maxCopyBytes)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	*filesN++
	*bytesN += size
	return nil
}

func uniqueName(dir, name string) string {
	if _, err := os.Lstat(filepath.Join(dir, name)); err != nil {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	cand := stem + " (copy)" + ext
	if _, err := os.Lstat(filepath.Join(dir, cand)); err != nil {
		return cand
	}
	for i := 2; i < 100; i++ {
		cand = fmt.Sprintf("%s (copy %d)%s", stem, i, ext)
		if _, err := os.Lstat(filepath.Join(dir, cand)); err != nil {
			return cand
		}
	}
	return ""
}

func inside(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != "." && filepath.IsLocal(rel)
}

// Rename moves src to a sibling named name. name is one path element.
func Rename(root, src, name string) (string, error) {
	src, err := Contain(root, src)
	if err != nil {
		return "", err
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("files: name %q is not a single path element", name)
	}
	dest := filepath.Join(filepath.Dir(src), name)
	if _, err := os.Lstat(dest); err == nil {
		return "", fmt.Errorf("files: %s already exists", name)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Rename(src, dest); err != nil {
		return "", err
	}
	return Contain(root, dest)
}

// Remove deletes contained paths. The jail root is refused.
func Remove(root string, paths []string) error {
	rootRes, err := Contain(root, root)
	if err != nil {
		return err
	}
	for _, p := range paths {
		p, err := Contain(root, p)
		if err != nil {
			return err
		}
		if p == rootRes {
			return fmt.Errorf("files: will not delete the jail root")
		}
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	return nil
}

// Preview is a cheap look at one contained path for the panel. Exactly one of
// Image, Text or Dir carries the body; Size and ModTime are the fallback for a
// file with no previewable content. A zero Preview means nothing was read, so
// the pane can say so rather than showing a blank card.
type Preview struct {
	Image   string
	Text    string
	Dir     bool
	Lines   []string
	More    bool
	Size    int64
	ModTime time.Time
}

const (
	previewBytes   = 4 << 10
	previewImage   = 4 << 20
	previewDirName = 8
)

// PeekPreview returns an image path, a short text sample, a few child names for
// a directory, or the size and date of a file with no previewable body. It
// never returns an error for a type it cannot preview.
func PeekPreview(root, path string) (Preview, error) {
	path, err := Contain(root, path)
	if err != nil {
		return Preview{}, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return Preview{}, err
	}
	if st.IsDir() {
		return peekDir(path), nil
	}
	if imageExt(filepath.Base(path)) && st.Size() > 0 && st.Size() <= previewImage {
		return Preview{Image: path, Size: st.Size(), ModTime: st.ModTime()}, nil
	}
	if textExt(filepath.Base(path)) && st.Size() > 0 {
		if text, ok := peekText(path); ok {
			return Preview{Text: text, Size: st.Size(), ModTime: st.ModTime()}, nil
		}
	}
	// A binary, an unknown extension or an unreadable body still earns a card:
	// the caller is selecting a file, and size and date are always true.
	return Preview{Size: st.Size(), ModTime: st.ModTime()}, nil
}

func peekText(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	buf := make([]byte, previewBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", false
	}
	buf = buf[:n]
	if !isPrintable(buf) {
		return "", false
	}
	return string(buf), true
}

// peekDir names a few children so a folder selection says something. It reads
// one bounded batch rather than the whole directory: on Phone Connect the jail
// root is an sshfs mount and a full ReadDir would cost a round trip per entry.
func peekDir(path string) Preview {
	p := Preview{Dir: true}
	f, err := os.Open(path)
	if err != nil {
		return p
	}
	defer f.Close()
	ents, err := f.ReadDir(previewDirName + 1)
	if err != nil && len(ents) == 0 {
		return p
	}
	for _, ent := range ents[:min(len(ents), previewDirName)] {
		p.Lines = append(p.Lines, ent.Name())
	}
	p.More = len(ents) > previewDirName
	return p
}

func textExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".md", ".go", ".json", ".xml", ".csv", ".log", ".conf", ".sh",
		".py", ".rs", ".toml", ".yml", ".yaml", ".css", ".js", ".ts", ".html":
		return true
	default:
		return false
	}
}

func isPrintable(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			return false
		}
	}
	return true
}
