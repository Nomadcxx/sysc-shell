package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMkdirUntitledSkipsExisting(t *testing.T) {
	root := t.TempDir()
	first, err := MkdirUntitled(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first) != "Untitled Folder" {
		t.Fatalf("first = %q", first)
	}
	second, err := MkdirUntitled(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(second) != "Untitled Folder 2" {
		t.Fatalf("second = %q", second)
	}
}

func TestCopyIntoWritesUniqueName(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CopyInto(root, root, []string{src}); err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(root, "a (copy).txt")
	b, err := os.ReadFile(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Fatalf("copy = %q", b)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("copy removed the source")
	}
}

func TestCopyIntoRefusesOutsideJail(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CopyInto(root, root, []string{src}); err == nil {
		t.Fatal("copied a path outside the jail")
	}
}

func TestMoveIntoRenamesUnderJail(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.Mkdir(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "a.txt")
	if err := os.WriteFile(src, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MoveInto(root, docs, []string{src}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source still exists: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(docs, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "z" {
		t.Fatalf("moved = %q", got)
	}
}

func TestCrumbTargetWalksPrefix(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "docs", "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	atRoot, err := CrumbTarget(root, sub, 0)
	if err != nil {
		t.Fatal(err)
	}
	rootRes, err := Contain(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if atRoot != rootRes {
		t.Fatalf("crumb 0 = %q, want %q", atRoot, rootRes)
	}
	docs, err := CrumbTarget(root, sub, 1)
	if err != nil {
		t.Fatal(err)
	}
	wantDocs, err := Contain(root, filepath.Join(root, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	if docs != wantDocs {
		t.Fatalf("crumb 1 = %q, want %q", docs, wantDocs)
	}
}

func TestCopyIntoRefusesDirectoryIntoItself(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.Mkdir(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := CopyInto(root, docs, []string{docs}); err == nil {
		t.Fatal("copied a directory into itself")
	} else if !strings.Contains(err.Error(), "into itself") {
		t.Fatalf("err = %v", err)
	}
}

func TestRenameMovesUnderJail(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	if err := os.WriteFile(src, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Rename(root, src, "b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "b.txt" {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("old name still exists")
	}
	b, err := os.ReadFile(got)
	if err != nil || string(b) != "z" {
		t.Fatalf("content = %q %v", b, err)
	}
}

func TestRenameRejectsSeparator(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	if err := os.WriteFile(src, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Rename(root, src, "b/c.txt"); err == nil {
		t.Fatal("rename accepted a separator")
	}
}

func TestRemoveDeletesContained(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.txt")
	if err := os.WriteFile(p, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove(root, []string{p}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("file survived Remove")
	}
}

func TestRemoveRefusesRoot(t *testing.T) {
	root := t.TempDir()
	if err := Remove(root, []string{root}); err == nil {
		t.Fatal("removed the jail root")
	}
}

func TestPeekPreviewReadsText(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "note.txt")
	if err := os.WriteFile(p, []byte("hello preview"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := PeekPreview(root, p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "hello preview" || got.Image != "" {
		t.Fatalf("preview = %+v", got)
	}
}
