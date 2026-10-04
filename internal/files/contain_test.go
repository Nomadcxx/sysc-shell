package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContainAcceptsPathsUnderTheRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "docs", "a.txt")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Contain(root, inside)
	if err != nil {
		t.Fatalf("Contain: %v", err)
	}
	want, err := filepath.EvalSymlinks(inside)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Contain = %q, want %q", got, want)
	}
	rootGot, err := Contain(root, root)
	if err != nil {
		t.Fatalf("Contain(root, root): %v", err)
	}
	rootWant, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if rootGot != rootWant {
		t.Fatalf("Contain(root, root) = %q, want %q", rootGot, rootWant)
	}
}

func TestContainRejectsRelativeAndEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, root, path string
	}{
		{"relative root", "tmp", filepath.Join(root, "x")},
		{"relative path", root, "docs/a.txt"},
		{"tilde", root, "~/docs"},
		{"file url", root, "file://" + root},
		{"dotdot out", root, filepath.Join(root, "..", filepath.Base(outside), "secret")},
		{"sibling prefix", root + "extra", filepath.Join(root, "x")},
		{"empty", "", root},
	}
	for _, c := range cases {
		if _, err := Contain(c.root, c.path); err == nil {
			t.Errorf("%s: Contain(%q, %q) succeeded", c.name, c.root, c.path)
		}
	}
}

func TestContainRefusesSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "out")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Contain(root, link); err == nil {
		t.Fatal("Contain followed a symlink out of the root")
	}
	if !strings.Contains(mustContainErr(t, root, link), "outside") {
		t.Fatalf("error = %v, want it to say outside", mustContainErr(t, root, link))
	}
}

func TestContainAllowsSymlinkThatStaysInside(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "alias.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	got, err := Contain(root, link)
	if err != nil {
		t.Fatalf("inside symlink: %v", err)
	}
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Contain = %q, want resolved target %q", got, want)
	}
}

func mustContainErr(t *testing.T, root, path string) string {
	t.Helper()
	_, err := Contain(root, path)
	if err == nil {
		t.Fatal("expected error")
	}
	return err.Error()
}
