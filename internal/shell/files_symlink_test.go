package shell

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/files"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// symlinkSession gives a jail with foo/keep.txt and the links current and
// also, listed through files.List so entry paths carry the fixed lexical
// identity.
func symlinkSession(t *testing.T) *filesSession {
	t.Helper()
	root := t.TempDir()
	foo := filepath.Join(root, "foo")
	if err := os.Mkdir(foo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foo, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"current", "also"} {
		if err := os.Symlink(foo, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	ents, err := files.List(root, root)
	if err != nil {
		t.Fatal(err)
	}
	return &filesSession{root: root, cwd: root, mode: files.ModeOpen, entries: ents}
}

func linkEntryIndex(sess *filesSession, name string) int {
	for i, e := range sess.entries {
		if e.Name == name {
			return i
		}
	}
	return -1
}

func TestFilesDeleteSymlinkRowKeepsTarget(t *testing.T) {
	sess := symlinkSession(t)
	foo := filepath.Join(sess.root, "foo")
	idx := linkEntryIndex(sess, "current")
	if idx < 0 {
		t.Fatal("current link not listed")
	}
	sess.pointerOnEntry(idx, 0, 1)
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	reg.mu.Lock()
	reg.files = sess
	h := &PanelHost{id: PanelFiles}
	if !h.activateFiles(reg, &ui.Node{Action: files.ActionDelete}) {
		reg.mu.Unlock()
		t.Fatal("delete start not handled")
	}
	if len(sess.pendingDelete) != 1 || sess.pendingDelete[0] != sess.entries[idx].Path {
		reg.mu.Unlock()
		t.Fatalf("pending = %v", sess.pendingDelete)
	}
	if !h.activateFiles(reg, &ui.Node{Action: files.ActionDeleteOK}) {
		reg.mu.Unlock()
		t.Fatal("delete confirm not handled")
	}
	reg.mu.Unlock()
	waitFilesIdle(t, reg, sess)
	if _, err := os.Lstat(filepath.Join(sess.root, "current")); !os.IsNotExist(err) {
		t.Fatal("link survived the confirmed delete")
	}
	if _, err := os.Stat(filepath.Join(foo, "keep.txt")); err != nil {
		t.Fatalf("deleting the link row destroyed its target: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sess.root, "also", "keep.txt")); err != nil {
		t.Fatalf("the second link stopped working: %v", err)
	}
}

func TestFilesRenameSymlinkEditsLinkName(t *testing.T) {
	sess := symlinkSession(t)
	foo := filepath.Join(sess.root, "foo")
	link := filepath.Join(sess.root, "current")
	idx := linkEntryIndex(sess, "current")
	sess.pointerOnEntry(idx, 0, 1)
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	reg.mu.Lock()
	reg.files = sess
	h := &PanelHost{id: PanelFiles}
	if !h.activateFiles(reg, &ui.Node{Action: files.ActionRename}) {
		reg.mu.Unlock()
		t.Fatal("rename start not handled")
	}
	if sess.renameFrom != link {
		reg.mu.Unlock()
		t.Fatalf("renameFrom = %q, want the link path %q", sess.renameFrom, link)
	}
	if sess.renameDraft != "current" {
		reg.mu.Unlock()
		t.Fatalf("renameDraft = %q, want the link name", sess.renameDraft)
	}
	if !h.activateFiles(reg, &ui.Node{Action: files.ActionRename, Text: "later"}) {
		reg.mu.Unlock()
		t.Fatal("rename commit not handled")
	}
	reg.mu.Unlock()
	waitFilesIdle(t, reg, sess)
	li, err := os.Lstat(filepath.Join(sess.root, "later"))
	if err != nil {
		t.Fatal(err)
	}
	if li.Mode()&os.ModeSymlink == 0 {
		t.Fatal("renaming the link produced a regular file")
	}
	target, err := os.Readlink(filepath.Join(sess.root, "later"))
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
