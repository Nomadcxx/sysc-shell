package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Dir: filepath.Join(t.TempDir(), "palettes")}
}

func writeRaw(t *testing.T, s *Store, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func marshal(t *testing.T, f PaletteFile) []byte {
	t.Helper()
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStoreListAndLoad(t *testing.T) {
	s := newTestStore(t)
	writeRaw(t, s, "my-nord.json", marshal(t, testFile(t)))
	got := s.List()
	if len(got) != 1 || got[0].Slug != "my-nord" || got[0].Name != "My Nord" || got[0].Err != nil {
		t.Fatalf("List = %+v", got)
	}
	if got[0].File.Dark["primary"] != testFile(t).Dark["primary"] {
		t.Fatal("List did not carry the decoded file")
	}
	f, err := s.Load("my-nord")
	if err != nil || f.Name != "My Nord" {
		t.Fatalf("Load = %+v, %v", f, err)
	}
}

func TestStoreMissingDirectoryIsEmpty(t *testing.T) {
	if got := newTestStore(t).List(); len(got) != 0 {
		t.Fatalf("List on a missing directory = %+v", got)
	}
}

func TestStoreRefusesBadFiles(t *testing.T) {
	good := marshal(t, testFile(t))
	withExtra := strings.Replace(string(good), `{"name"`, `{"extra":1,"name"`, 1)
	noRole := testFile(t)
	delete(noRole.Dark, "primary")
	alpha := testFile(t)
	alpha.Light["primary"] = "#11223344"
	cases := map[string][]byte{
		"unknown-key":  []byte(withExtra),
		"missing-role": marshal(t, noRole),
		"alpha-colour": marshal(t, alpha),
		"trailing":     append(append([]byte{}, good...), []byte(` {"x":1}`)...),
		"not-json":     []byte("palette?"),
		"oversize":     append(append([]byte{}, good...), []byte(strings.Repeat(" ", MaxPaletteBytes))...),
		"empty-file":   nil,
		"wrong-type":   []byte(`{"name":"x","dark":{"primary":5},"light":{}}`),
	}
	for slug, data := range cases {
		s := newTestStore(t)
		writeRaw(t, s, slug+".json", data)
		if _, err := s.Load(slug); err == nil {
			t.Errorf("%s: Load accepted it", slug)
		}
		list := s.List()
		if len(list) != 1 || list[0].Err == nil || list[0].Name != slug {
			t.Errorf("%s: List = %+v, want one unavailable entry named by its slug", slug, list)
		}
	}
}

func TestStoreIgnoresForeignNames(t *testing.T) {
	s := newTestStore(t)
	good := marshal(t, testFile(t))
	for _, n := range []string{"README.txt", "Bad Name.json", "UPPER.json", ".palette-x.tmp", "a--b.json"} {
		writeRaw(t, s, n, good)
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("List = %+v, want none", got)
	}
}

func TestStoreLoadRejectsPathsThatAreNotSlugs(t *testing.T) {
	s := newTestStore(t)
	for _, slug := range []string{"", "../x", "a/b", "UPPER", "a b"} {
		if _, err := s.Load(slug); err == nil {
			t.Errorf("Load(%q) accepted", slug)
		}
	}
}

func TestStoreDoesNotBlockOnAFIFO(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(s.Dir, "pipe.json"), 0o600); err != nil {
		t.Skipf("no FIFO support: %v", err)
	}
	done := make(chan []PaletteInfo, 1)
	go func() { done <- s.List() }()
	select {
	case list := <-done:
		if len(list) != 1 || list[0].Err == nil {
			t.Fatalf("List = %+v, want one unavailable entry", list)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("List blocked on a FIFO")
	}
}

func TestStoreTokens(t *testing.T) {
	s := newTestStore(t)
	writeRaw(t, s, "my-nord.json", marshal(t, testFile(t)))
	for _, mode := range []string{"dark", "light"} {
		tok, err := s.Tokens("my-nord", mode, false)
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if err := tok.Valid(false); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
	}
	if _, err := s.Tokens("missing", "dark", false); err == nil {
		t.Fatal("Tokens for a missing palette succeeded")
	}
	// High contrast resolves when the accents allow it (see sysc-888).
	hcDark, _ := NamedPalette("nord", "dark", true)
	hcLight, _ := NamedPalette("nord", "light", true)
	writeRaw(t, s, "nord-hc.json", marshal(t, PaletteFile{Name: "Nord HC", Dark: hcDark.Roles(), Light: hcLight.Roles()}))
	if tok, err := s.Tokens("nord-hc", "dark", true); err != nil || tok.Valid(true) != nil {
		t.Fatalf("high contrast: %v", err)
	}
}
