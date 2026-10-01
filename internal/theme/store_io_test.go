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

func TestExportThenImportRoundTrips(t *testing.T) {
	s := newTestStore(t)
	slug, _ := s.Save(testFile(t))
	data, err := s.ExportJSON(slug)
	if err != nil {
		t.Fatal(err)
	}
	f, adjusted, err := ParseImport(data, "ignored")
	if err != nil || adjusted != 0 {
		t.Fatalf("ParseImport = %v adjusted=%d", err, adjusted)
	}
	want := testFile(t).normalized()
	if f.Name != want.Name || len(f.Dark) != len(want.Dark) || f.Dark["primary"] != want.Dark["primary"] || f.Light["surface"] != want.Light["surface"] {
		t.Fatalf("round trip changed the palette: %+v", f.Name)
	}
}

func TestExportToWritesWorldReadableFileAndOverwrites(t *testing.T) {
	s := newTestStore(t)
	slug, _ := s.Save(testFile(t))
	dir := filepath.Join(t.TempDir(), "Downloads")
	path, err := s.ExportTo(slug, dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "my-nord.sysc-palette.json" {
		t.Fatalf("path = %s", path)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, want 0644", fi.Mode().Perm())
	}
	if _, err := s.ExportTo(slug, dir); err != nil {
		t.Fatalf("re-export: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("export left %d files, want 1", len(entries))
	}
}

func TestParseImportAcceptsAPlainRoleMap(t *testing.T) {
	f := testFile(t)
	f.Dark["unknown_extra"] = "#000000"
	data, _ := json.Marshal(map[string]any{"dark": f.Dark, "light": f.Light})
	got, _, err := ParseImport(data, "colors")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "colors" {
		t.Fatalf("name = %q, want the fallback", got.Name)
	}
	if _, ok := got.Dark["unknown_extra"]; ok {
		t.Fatal("an unknown role key was kept")
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestParseImportRefusals(t *testing.T) {
	good := testFile(t)
	mk := func(f PaletteFile) []byte { return marshal(t, f) }
	noRole := testFile(t)
	delete(noRole.Light, "surface")
	alpha := testFile(t)
	alpha.Dark["primary"] = "#11223344"
	badName := testFile(t)
	badName.Name = "!!!"
	cases := map[string][]byte{
		"not json":  []byte("<xml/>"),
		"int value": []byte(`{"dark":{"primary":5},"light":{}}`),
		"missing":   mk(noRole),
		"alpha":     mk(alpha),
		"bad name":  mk(badName),
		"oversize":  append(mk(good), []byte(strings.Repeat(" ", MaxPaletteBytes))...),
	}
	for name, data := range cases {
		if _, _, err := ParseImport(data, "x"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseImportRepairsContrastAndCountsRoles(t *testing.T) {
	f := testFile(t)
	f.Dark["on_surface"] = f.Dark["surface"]
	data, _ := json.Marshal(f)
	got, adjusted, err := ParseImport(data, "x")
	if err != nil {
		t.Fatal(err)
	}
	if adjusted < 1 {
		t.Fatalf("adjusted = %d, want at least 1", adjusted)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("imported palette is not valid as written: %v", err)
	}
}

func TestReadImportFileRefusesNonFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadImportFile(dir); err == nil {
		t.Error("a directory was read")
	}
	big := filepath.Join(dir, "big.json")
	_ = os.WriteFile(big, []byte(strings.Repeat("x", MaxPaletteBytes+1)), 0o600)
	if _, err := ReadImportFile(big); err == nil {
		t.Error("an oversize file was read")
	}
	fifo := filepath.Join(dir, "pipe.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("no FIFO support: %v", err)
	}
	done := make(chan error, 1)
	go func() { _, err := ReadImportFile(fifo); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a FIFO was read")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadImportFile blocked on a FIFO")
	}
}

func TestImportName(t *testing.T) {
	cases := map[string]string{
		"/x/My Palette.sysc-palette.json": "My Palette",
		"/x/colors.json":                  "colors",
		"nord":                            "nord",
	}
	for in, want := range cases {
		if got := ImportName(in); got != want {
			t.Errorf("ImportName(%q) = %q, want %q", in, got, want)
		}
	}
}
