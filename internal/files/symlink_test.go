package files

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// linkJail builds root with foo/keep.txt and two links, current and also,
// pointing at foo. It returns root and foo's path.
func linkJail(t *testing.T) (root, foo string) {
	t.Helper()
	root = t.TempDir()
	foo = filepath.Join(root, "foo")
	if err := os.Mkdir(foo, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(foo, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"current", "also"} {
		if err := os.Symlink(foo, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	return root, foo
}

func entryByName(t *testing.T, root, dir, name string) Entry {
	t.Helper()
	ents, err := List(root, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("no entry %q in %v", name, ents)
	return Entry{}
}

func isLink(path string) bool {
	li, err := os.Lstat(path)
	return err == nil && li.Mode()&os.ModeSymlink != 0
}

func TestListSymlinkEntryPathIsLexical(t *testing.T) {
	root, foo := linkJail(t)
	e := entryByName(t, root, root, "current")
	if want := filepath.Join(root, "current"); e.Path != want {
		t.Fatalf("Path = %q, want lexical %q", e.Path, want)
	}
	if e.Target != foo {
		t.Fatalf("Target = %q, want %q", e.Target, foo)
	}
	if !e.Dir {
		t.Fatal("link to directory should report Dir")
	}
}

func TestTwoLinksSameTargetAreDistinctEntries(t *testing.T) {
	root, foo := linkJail(t)
	current := entryByName(t, root, root, "current")
	also := entryByName(t, root, root, "also")
	if current.Path == also.Path {
		t.Fatalf("both links share identity %q", current.Path)
	}
	if err := Remove(root, []string{current.Path}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, "current")); !os.IsNotExist(err) {
		t.Fatal("first link survived")
	}
	if !isLink(filepath.Join(root, "also")) {
		t.Fatal("second link was removed with the first")
	}
	if _, err := os.Stat(filepath.Join(foo, "keep.txt")); err != nil {
		t.Fatal("removing a link deleted its target")
	}
}

func TestRemoveSymlinkEntryRemovesLinkOnly(t *testing.T) {
	root, foo := linkJail(t)
	link := filepath.Join(root, "current")
	if err := Remove(root, []string{link}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatal("link survived Remove")
	}
	if _, err := os.Stat(filepath.Join(foo, "keep.txt")); err != nil {
		t.Fatalf("target destroyed through the link: %v", err)
	}
}

func TestRemoveRefusesJailRootViaSymlinkedRoot(t *testing.T) {
	root, foo := linkJail(t)
	// A link that points at the jail itself is still just a link.
	self := filepath.Join(root, "self")
	if err := os.Symlink(root, self); err != nil {
		t.Fatal(err)
	}
	if err := Remove(root, []string{self}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(self); !os.IsNotExist(err) {
		t.Fatal("self link survived")
	}
	if _, err := os.Stat(filepath.Join(foo, "keep.txt")); err != nil {
		t.Fatal("removing the self link deleted jail contents")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("removing the self link deleted the jail")
	}
	// The jail named directly is refused in both spellings.
	if err := Remove(root, []string{root}); err == nil {
		t.Fatal("Remove accepted the jail root")
	}
	if err := Remove(root, []string{self + "/.."}); err == nil {
		t.Fatal("Remove accepted root/..")
	}
}

func TestRenameSymlinkEntryRenamesLink(t *testing.T) {
	root, foo := linkJail(t)
	link := filepath.Join(root, "current")
	got, err := Rename(root, link, "later")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "later"); got != want {
		t.Fatalf("Rename returned %q, want %q", got, want)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatal("old link name survived")
	}
	target, err := os.Readlink(filepath.Join(root, "later"))
	if err != nil {
		t.Fatal(err)
	}
	if target != foo {
		t.Fatalf("renamed link points at %q, want %q", target, foo)
	}
	if _, err := os.Stat(filepath.Join(foo, "keep.txt")); err != nil {
		t.Fatal("renaming the link destroyed its target")
	}
}

func TestMoveSymlinkEntryMovesLink(t *testing.T) {
	root, foo := linkJail(t)
	dst := filepath.Join(root, "dst")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	made, err := MoveInto(root, dst, []string{filepath.Join(root, "current")})
	if err != nil {
		t.Fatal(err)
	}
	if len(made) != 1 || made[0] != filepath.Join(dst, "current") {
		t.Fatalf("made = %v", made)
	}
	if !isLink(filepath.Join(dst, "current")) {
		t.Fatal("moved entry is not a link")
	}
	target, err := os.Readlink(filepath.Join(dst, "current"))
	if err != nil {
		t.Fatal(err)
	}
	if target != foo {
		t.Fatalf("moved link points at %q, want %q", target, foo)
	}
	if _, err := os.Lstat(filepath.Join(root, "current")); !os.IsNotExist(err) {
		t.Fatal("original link survived the move")
	}
}

func TestContainEntryRejectsParentOutsideJail(t *testing.T) {
	root := t.TempDir()
	elsewhere := t.TempDir()
	secret := filepath.Join(elsewhere, "secret.txt")
	if err := os.WriteFile(secret, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(root, "escape")
	if err := os.Symlink(elsewhere, escape); err != nil {
		t.Fatal(err)
	}
	// The link itself is an in-jail entry.
	if _, err := ContainEntry(root, escape); err != nil {
		t.Fatalf("ContainEntry rejected the link: %v", err)
	}
	// But a path whose parent leaves the jail is refused.
	if _, err := ContainEntry(root, filepath.Join(escape, "secret.txt")); err == nil {
		t.Fatal("ContainEntry jailed a child through an escaping parent")
	}
}

// forceRename swaps the rename seam for the duration of the test.
func forceRename(t *testing.T, fn func(old, new string) error) {
	t.Helper()
	orig := renameFn
	renameFn = fn
	t.Cleanup(func() { renameFn = orig })
}

func exdev(_ string, _ string) error {
	return &os.LinkError{Op: "rename", Err: syscall.EXDEV}
}

func moveSrcTree(t *testing.T) (root, src, dst string) {
	t.Helper()
	root = t.TempDir()
	src = filepath.Join(root, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(src, "run.sh")
	if err := os.WriteFile(run, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(run, 0o755); err != nil { // defeat umask on create
		t.Fatal(err)
	}
	key := filepath.Join(src, "sub", "key")
	if err := os.WriteFile(key, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(key, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(src, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(src, 0o755); err != nil {
		t.Fatal(err)
	}
	dst = filepath.Join(root, "dst")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	return root, src, dst
}

func TestMoveCrossDevicePreservesModes(t *testing.T) {
	root, src, dst := moveSrcTree(t)
	forceRename(t, exdev)
	made, err := MoveInto(root, dst, []string{src})
	if err != nil {
		t.Fatal(err)
	}
	dest := made[0]
	checks := []struct {
		path string
		want os.FileMode
	}{
		{filepath.Join(dest, "run.sh"), 0o755},
		{filepath.Join(dest, "sub", "key"), 0o600},
		{filepath.Join(dest, "sub"), 0o750},
		{dest, 0o755},
	}
	for _, c := range checks {
		li, err := os.Stat(c.path)
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		if got := li.Mode().Perm(); got != c.want {
			t.Errorf("%s mode = %o, want %o", c.path, got, c.want)
		}
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source survived a completed move")
	}
}

func TestMoveCrossDevicePreservesSymlinks(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "target.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(src, "alias")); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "dst")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	forceRename(t, exdev)
	made, err := MoveInto(root, dst, []string{src})
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(made[0], "alias")
	if !isLink(alias) {
		t.Fatal("moved tree contains a regular file where the link was")
	}
	text, err := os.Readlink(alias)
	if err != nil {
		t.Fatal(err)
	}
	if text != "target.txt" {
		t.Fatalf("link text = %q, want target.txt", text)
	}
	if _, err := os.Stat(filepath.Join(root, "src")); !os.IsNotExist(err) {
		t.Fatal("source survived a completed move")
	}
}

func TestMoveCrossDeviceSpecialFileAbortsAndKeepsSource(t *testing.T) {
	root, src, dst := moveSrcTree(t)
	if err := syscall.Mkfifo(filepath.Join(src, "pipe"), 0o644); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	forceRename(t, exdev)
	if _, err := MoveInto(root, dst, []string{src}); err == nil {
		t.Fatal("move over a fifo reported success")
	}
	if _, err := os.Stat(filepath.Join(src, "run.sh")); err != nil {
		t.Fatal("source was removed after the abort")
	}
	ents, err := List(root, dst)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		t.Fatalf("destination kept partial copy %q", e.Name)
	}
}

func TestMoveNonEXDEVRenameErrorDoesNotCopy(t *testing.T) {
	root, src, dst := moveSrcTree(t)
	forceRename(t, func(_, _ string) error {
		return &os.LinkError{Op: "rename", Err: syscall.EACCES}
	})
	if _, err := MoveInto(root, dst, []string{src}); err == nil {
		t.Fatal("move succeeded despite rename failure")
	} else if !errors.Is(err, syscall.EACCES) {
		t.Fatalf("error %v lost the underlying cause", err)
	}
	ents, err := List(root, dst)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		t.Fatalf("destination received a copy after a non-EXDEV rename error: %q", e.Name)
	}
	if _, err := os.Stat(filepath.Join(src, "run.sh")); err != nil {
		t.Fatal("source was removed after the failed move")
	}
}

func TestCopyPreservesModes(t *testing.T) {
	root, src, dst := moveSrcTree(t)
	made, err := CopyInto(root, dst, []string{src})
	if err != nil {
		t.Fatal(err)
	}
	dest := made[0]
	run, err := os.Stat(filepath.Join(dest, "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if run.Mode().Perm() != 0o755 {
		t.Fatalf("copied run.sh mode = %o, want 0755", run.Mode().Perm())
	}
	key, err := os.Stat(filepath.Join(dest, "sub", "key"))
	if err != nil {
		t.Fatal(err)
	}
	if key.Mode().Perm() != 0o600 {
		t.Fatalf("copied key mode = %o, want 0600", key.Mode().Perm())
	}
	sub, err := os.Stat(filepath.Join(dest, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	if sub.Mode().Perm() != 0o750 {
		t.Fatalf("copied sub dir mode = %o, want 0750", sub.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(src, "run.sh")); err != nil {
		t.Fatal("copy removed the source")
	}
}
