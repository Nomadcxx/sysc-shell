package files

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListOrdersDirsFirstAndSkipsHidden(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"zeta.txt", "alpha", ".secret", "Beta"} {
		p := filepath.Join(root, name)
		if strings.HasSuffix(name, ".txt") {
			if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := List(root, root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
		if e.Name == ".secret" {
			t.Fatal("listed a hidden name")
		}
	}
	want := []string{"alpha", "Beta", "zeta.txt"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("names = %v, want %v", names, want)
	}
	if !entries[0].Dir || !entries[1].Dir || entries[2].Dir {
		t.Fatalf("dir flags = %+v", entries)
	}
}

func TestListOmitsSymlinksThatEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "out")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := List(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "ok.txt" {
		t.Fatalf("entries = %+v, want only ok.txt", entries)
	}
}

func TestWalkEntersAndStopsAtRoot(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "docs")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Walk(root, root, "docs")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(child)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("enter = %q, want %q", got, want)
	}
	up, err := Walk(root, got, "..")
	if err != nil {
		t.Fatal(err)
	}
	rootRes, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if up != rootRes {
		t.Fatalf("up = %q, want %q", up, rootRes)
	}
	stay, err := Walk(root, root, "..")
	if err != nil {
		t.Fatal(err)
	}
	if stay != rootRes {
		t.Fatalf("up at root = %q, want %q", stay, rootRes)
	}
}

func TestListIncludesHiddenWhenAsked(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	hidden, _, err := ListLimited(root, root, MaxEntries, true)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range hidden {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != ".secret,ok.txt" && strings.Join(names, ",") != "ok.txt,.secret" {
		if !containsName(hidden, ".secret") || !containsName(hidden, "ok.txt") {
			t.Fatalf("hidden listing = %v, want .secret and ok.txt", names)
		}
	}
	shown, err := List(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if containsName(shown, ".secret") {
		t.Fatal("default list showed a hidden name")
	}
}

func containsName(ents []Entry, name string) bool {
	for _, e := range ents {
		if e.Name == name {
			return true
		}
	}
	return false
}

func TestWalkRejectsSeparatorInName(t *testing.T) {
	root := t.TempDir()
	if _, err := Walk(root, root, "a/b"); err == nil {
		t.Fatal("Walk accepted a path separator")
	}
}

func TestListLimitedReportsTruncation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("f%d", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ents, truncated, err := ListLimited(root, root, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 2 || !truncated {
		t.Fatalf("got %d entries truncated=%v, want 2 true", len(ents), truncated)
	}
	ents, truncated, err = ListLimited(root, root, 3, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 3 || truncated {
		t.Fatalf("got %d entries truncated=%v, want 3 false", len(ents), truncated)
	}
}
