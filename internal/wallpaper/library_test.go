package wallpaper

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// seedLibrary builds a root with one image, one nested image, one GIF, and a
// file the vocabulary does not name.
func seedLibrary(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, rel := range []string{"a.jpg", "c.gif", "skip.txt", "sub/b.png"} {
		if err := os.WriteFile(filepath.Join(root, rel), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func names(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	slices.Sort(out)
	return out
}

func TestLibraryListsCurrentDirectory(t *testing.T) {
	root := seedLibrary(t)
	lib := Scan([]string{root})
	if lib.Err != "" {
		t.Fatalf("scan: %s", lib.Err)
	}

	got := names(lib.View(root, FilterAll, ""))
	want := []string{"a.jpg", "c.gif", "sub"}
	if !slices.Equal(got, want) {
		t.Fatalf("view = %v, want %v (no skip.txt, no flattened sub/b.png)", got, want)
	}

	for _, e := range lib.View(root, FilterAll, "") {
		switch e.Name {
		case "a.jpg":
			if e.IsDir || e.Kind != KindImage {
				t.Errorf("a.jpg = %+v", e)
			}
		case "c.gif":
			if e.Kind != KindVideo {
				t.Errorf("gif must classify as video, got %+v", e)
			}
		case "sub":
			if !e.IsDir {
				t.Errorf("sub must be a directory entry, got %+v", e)
			}
		}
	}
}

func TestLibraryNavigatesAndReturns(t *testing.T) {
	root := seedLibrary(t)
	lib := Scan([]string{root})

	sub := filepath.Join(root, "sub")
	if got := names(lib.View(sub, FilterAll, "")); !slices.Equal(got, []string{"b.png"}) {
		t.Fatalf("sub view = %v, want [b.png]", got)
	}
	parent, ok := lib.Parent(sub)
	if !ok || parent != root {
		t.Fatalf("Parent(sub) = %q, %v; want the root", parent, ok)
	}
	if _, ok := lib.Parent(root); ok {
		t.Fatal("Up must stop at a root rather than escaping the library")
	}
}

func TestLibraryFilterAndSearch(t *testing.T) {
	root := seedLibrary(t)
	lib := Scan([]string{root})

	if got := names(lib.View(root, FilterImages, "")); !slices.Equal(got, []string{"a.jpg", "sub"}) {
		t.Errorf("images = %v, want the image and the directory", got)
	}
	if got := names(lib.View(root, FilterVideos, "")); !slices.Equal(got, []string{"c.gif", "sub"}) {
		t.Errorf("videos = %v, want the gif and the directory", got)
	}
	if got := names(lib.View(root, FilterAll, "A.J")); !slices.Equal(got, []string{"a.jpg"}) {
		t.Errorf("search = %v, want a case-insensitive filename match", got)
	}
}

func TestLibrarySkipsNonUTF8Names(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "\xff\xfe.jpg")
	if err := os.WriteFile(bad, []byte("x"), 0o644); err != nil {
		t.Skipf("filesystem refused a non-UTF-8 name: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "good.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	lib := Scan([]string{root})
	if got := names(lib.View(root, FilterAll, "")); !slices.Equal(got, []string{"good.jpg"}) {
		t.Fatalf("view = %v, want only the UTF-8 name", got)
	}
}

func TestLibraryUnreadableRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	lib := Scan([]string{root})
	if lib.Err == "" {
		t.Fatal("an unreadable root must report why, not look empty")
	}
	if len(lib.View(root, FilterAll, "")) != 0 {
		t.Fatal("an unreadable root lists nothing")
	}
}

func TestLibrarySharedRoot(t *testing.T) {
	// D9 allows the image and video directories to be the same path. Scanning
	// it twice must not double every entry.
	root := seedLibrary(t)
	lib := Scan([]string{root, root})
	if got := names(lib.View(root, FilterAll, "")); !slices.Equal(got, []string{"a.jpg", "c.gif", "sub"}) {
		t.Fatalf("view = %v, want each entry once", got)
	}
}
