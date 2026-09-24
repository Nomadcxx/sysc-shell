package store

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractUnpacksOnePluginDirectory(t *testing.T) {
	t.Parallel()
	body := tarball(t,
		tarEntry{name: fixtureID + "/", typ: tar.TypeDir, mode: 0o700},
		tarEntry{name: fixtureID + "/manifest.json", typ: tar.TypeReg, body: "{}", mode: 0o600},
		tarEntry{name: fixtureID + "/bin/run", typ: tar.TypeReg, body: "#!", mode: 0o700},
	)
	dest := t.TempDir()
	if err := Extract(bytes.NewReader(body), dest, fixtureID, DefaultLimits); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	info, err := os.Stat(filepath.Join(dest, fixtureID, "bin", "run"))
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("executable = %v, %v; want it kept executable", info, err)
	}
	info, _ = os.Stat(filepath.Join(dest, fixtureID, "manifest.json"))
	if info.Mode().Perm()&0o044 == 0 {
		t.Errorf("manifest mode %v; want masked to 0644", info.Mode().Perm())
	}
}

func TestExtractRejectsHostileArchives(t *testing.T) {
	t.Parallel()
	root := fixtureID + "/"
	cases := map[string]struct {
		entries []tarEntry
		lim     Limits
	}{
		"parent escape":     {entries: []tarEntry{{name: root + "../../evil", typ: tar.TypeReg, body: "x"}}},
		"absolute path":     {entries: []tarEntry{{name: "/tmp/evil", typ: tar.TypeReg, body: "x"}}},
		"symlink":           {entries: []tarEntry{{name: root + "link", typ: tar.TypeSymlink, link: "/etc"}}},
		"hardlink":          {entries: []tarEntry{{name: root + "hard", typ: tar.TypeLink, link: "/etc/passwd"}}},
		"device":            {entries: []tarEntry{{name: root + "dev", typ: tar.TypeChar}}},
		"second top level":  {entries: []tarEntry{{name: root + "a", typ: tar.TypeReg, body: "x"}, {name: "other/a", typ: tar.TypeReg, body: "x"}}},
		"dot prefix":        {entries: []tarEntry{{name: "./" + root + "a", typ: tar.TypeReg, body: "x"}}},
		"duplicate entry":   {entries: []tarEntry{{name: root + "a", typ: tar.TypeReg, body: "x"}, {name: root + "a", typ: tar.TypeReg, body: "y"}}},
		"no root directory": {entries: nil},
		"too many entries": {
			entries: []tarEntry{{name: root + "a", typ: tar.TypeReg, body: "x"}, {name: root + "b", typ: tar.TypeReg, body: "x"}},
			lim:     Limits{MaxEntries: 1, MaxBytes: 1 << 20},
		},
		"too many bytes": {
			entries: []tarEntry{{name: root + "a", typ: tar.TypeReg, body: "0123456789"}},
			lim:     Limits{MaxEntries: 10, MaxBytes: 5},
		},
	}
	for name, c := range cases {
		lim := c.lim
		if lim.MaxEntries == 0 {
			lim = DefaultLimits
		}
		parent := t.TempDir()
		dest := filepath.Join(parent, "dest")
		if err := os.Mkdir(dest, 0o755); err != nil {
			t.Fatal(err)
		}
		err := Extract(bytes.NewReader(tarball(t, c.entries...)), dest, fixtureID, lim)
		if KindOf(err) != KindArchive {
			t.Errorf("%s: err = %v, want %s", name, err, KindArchive)
		}
		if _, err := os.Stat(filepath.Join(parent, "evil")); err == nil {
			t.Errorf("%s: a file escaped the destination", name)
		}
	}
}

// TestExtractSkipsAPaxGlobalHeader proves F9: git archive prefixes its
// tarball with a pax_global_header entry, which is metadata rather than part
// of the plugin tree and must not sink extraction.
func TestExtractSkipsAPaxGlobalHeader(t *testing.T) {
	t.Parallel()
	body := tarball(t,
		tarEntry{name: "pax_global_header", typ: tar.TypeXGlobalHeader, body: "52 comment=abcdef\n"},
		tarEntry{name: fixtureID + "/", typ: tar.TypeDir, mode: 0o755},
		tarEntry{name: fixtureID + "/manifest.json", typ: tar.TypeReg, body: "{}", mode: 0o644},
	)
	dest := t.TempDir()
	if err := Extract(bytes.NewReader(body), dest, fixtureID, DefaultLimits); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, fixtureID, "manifest.json")); err != nil {
		t.Fatalf("manifest.json missing: %v", err)
	}
}

func TestExtractRejectsSomethingThatIsNotGzip(t *testing.T) {
	t.Parallel()
	err := Extract(bytes.NewReader([]byte("plain")), t.TempDir(), fixtureID, DefaultLimits)
	if KindOf(err) != KindArchive {
		t.Fatalf("err = %v", err)
	}
}
