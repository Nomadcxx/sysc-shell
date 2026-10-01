package theme

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSaveAllocatesCollisionFreeSlugs(t *testing.T) {
	s := newTestStore(t)
	var got []string
	for i := 0; i < 3; i++ {
		slug, err := s.Save(testFile(t))
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, slug)
	}
	if strings.Join(got, ",") != "my-nord,my-nord-2,my-nord-3" {
		t.Fatalf("slugs = %v", got)
	}
	info, err := os.Stat(filepath.Join(s.Dir, "my-nord.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, %v; want 0600", info, err)
	}
	dir, _ := os.Stat(s.Dir)
	if dir.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %v, want 0700", dir.Mode().Perm())
	}
	if f, err := s.Load("my-nord-2"); err != nil || f.Name != "My Nord" {
		t.Fatalf("Load = %+v, %v", f, err)
	}
}

func TestSaveConcurrentSameNameNeverOverwrites(t *testing.T) {
	s := newTestStore(t)
	const n = 12
	slugs := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slug, err := s.Save(testFile(t))
			if err != nil {
				t.Error(err)
				return
			}
			slugs <- slug
		}()
	}
	wg.Wait()
	close(slugs)
	seen := map[string]bool{}
	for slug := range slugs {
		if seen[slug] {
			t.Fatalf("slug %s allocated twice", slug)
		}
		seen[slug] = true
	}
	if len(s.List()) != n {
		t.Fatalf("store holds %d palettes, want %d", len(s.List()), n)
	}
}

func TestSaveRefusesWithoutLeavingFiles(t *testing.T) {
	s := newTestStore(t)
	weak := testFile(t)
	weak.Dark["on_surface"] = weak.Dark["surface"]
	alpha := testFile(t)
	alpha.Dark["primary"] = "#11223344"
	punct := testFile(t)
	punct.Name = "!!!"
	for name, f := range map[string]PaletteFile{"weak contrast": weak, "alpha": alpha, "punctuation name": punct} {
		if _, err := s.Save(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	entries, _ := os.ReadDir(s.Dir)
	if len(entries) != 0 {
		t.Fatalf("directory holds %d leftovers after refused saves", len(entries))
	}
}

func TestSaveNonASCIINameUsesFallbackSlug(t *testing.T) {
	s := newTestStore(t)
	f := testFile(t)
	f.Name = "日本語"
	slug, err := s.Save(f)
	if err != nil || slug != "palette" {
		t.Fatalf("slug = %q, err = %v; want palette", slug, err)
	}
}

func TestUpdateReplacesAndKeepsOldFileOnRefusal(t *testing.T) {
	s := newTestStore(t)
	slug, _ := s.Save(testFile(t))
	next := testFile(t)
	next.Dark["primary"] = "#81a1c1"
	if err := s.Update(slug, next); err != nil {
		t.Fatal(err)
	}
	if f, _ := s.Load(slug); f.Dark["primary"] != "#81a1c1" {
		t.Fatalf("primary = %s after Update", f.Dark["primary"])
	}
	bad := testFile(t)
	bad.Dark["on_surface"] = bad.Dark["surface"]
	if err := s.Update(slug, bad); err == nil {
		t.Fatal("Update accepted a weak palette")
	}
	if f, _ := s.Load(slug); f.Dark["primary"] != "#81a1c1" {
		t.Fatal("a refused Update changed the stored palette")
	}
	if err := s.Update("missing", testFile(t)); err == nil {
		t.Fatal("Update of a missing palette succeeded")
	}
}

func TestRenameChangesOnlyTheName(t *testing.T) {
	s := newTestStore(t)
	slug, _ := s.Save(testFile(t))
	if err := s.Rename(slug, "  Renamed  "); err != nil {
		t.Fatal(err)
	}
	f, err := s.Load(slug)
	if err != nil || f.Name != "Renamed" {
		t.Fatalf("Load = %+v, %v", f, err)
	}
	if err := s.Rename(slug, "!!!"); err == nil {
		t.Fatal("Rename accepted a punctuation-only name")
	}
}

// Rename reads then writes; with the read outside the lock a concurrent
// Update in between was silently undone (R4).
func TestRenameAndUpdateDoNotLoseEachOther(t *testing.T) {
	s := newTestStore(t)
	slug, _ := s.Save(testFile(t))
	for i := 0; i < 50; i++ {
		next := testFile(t)
		next.Dark["primary"] = "#81a1c1"
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _ = s.Rename(slug, "Renamed") }()
		go func() { defer wg.Done(); _ = s.Update(slug, next) }()
		wg.Wait()
		f, err := s.Load(slug)
		if err != nil {
			t.Fatal(err)
		}
		if f.Dark["primary"] != "#81a1c1" && f.Name == "Renamed" {
			// Rename read the file before Update wrote it and wrote it back after.
			t.Fatal("a concurrent Rename undid an Update")
		}
		_ = s.Update(slug, testFile(t))
	}
}

func TestRenameWorksOnAWeakButLoadablePalette(t *testing.T) {
	s := newTestStore(t)
	weak := testFile(t)
	weak.Dark["on_surface"] = weak.Dark["surface"]
	writeRaw(t, s, "weak.json", marshal(t, weak))
	if err := s.Rename("weak", "Fixed name"); err != nil {
		t.Fatalf("Rename refused a loadable palette: %v", err)
	}
}

func TestDelete(t *testing.T) {
	s := newTestStore(t)
	slug, _ := s.Save(testFile(t))
	if err := s.Delete(slug); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(slug); err == nil {
		t.Fatal("second Delete succeeded")
	}
	if err := s.Delete("../x"); err == nil {
		t.Fatal("Delete accepted a traversal path")
	}
	if len(s.List()) != 0 {
		t.Fatal("palette still listed after Delete")
	}
}
