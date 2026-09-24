package store

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

const fixtureID = "org.sysc.timer"

// manifestJSON is a manifest that passes plugin.ParseManifest. Capabilities
// and required commands vary per test so manifest/catalog agreement can be
// exercised; "panels" stays because the manifest declares a panel. Every
// capability set a test passes must load: writePluginDir fails the test at
// once if it does not, and the fix is to change the set, not the loader.
func manifestJSON(id, version string, caps, requires []string) string {
	m := map[string]any{
		"schema": 1, "id": id, "name": "Timer", "version": version,
		"protocol":     map[string]int{"major": 1, "minor": 0},
		"exec":         "bin/sysc-plugin-timer",
		"capabilities": caps,
		"requires":     map[string]any{"commands": requires},
		"services":     []any{map[string]string{"id": "timer"}},
		"widgets":      []any{map[string]any{"id": "bar", "settings": []any{}}},
		"panels":       []any{map[string]any{"id": "panel", "width": 320, "height": 280, "placement": "attached"}},
		"settings":     []any{},
	}
	b, _ := json.Marshal(m)
	return string(b)
}

var defaultCaps = []string{"notifications", "panels", "settings", "state"}

// writePluginDir lays out <parent>/<id> as an installable plugin directory.
func writePluginDir(t *testing.T, parent, id, version string, caps, requires []string) string {
	t.Helper()
	dir := filepath.Join(parent, id)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifestJSON(id, version, caps, requires)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "sysc-plugin-timer"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.LoadManifest(dir); err != nil {
		t.Fatalf("fixture manifest does not load: %v", err)
	}
	return dir
}

type tarEntry struct {
	name string
	typ  byte
	body string
	mode int64
	link string
}

// tarball builds a gzipped tar from explicit entries, hostile ones included.
func tarball(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		hdr := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: mode, Size: int64(len(e.body)), Linkname: e.link}
		if e.typ != tar.TypeReg {
			hdr.Size = 0
		}
		if e.typ == tar.TypeXGlobalHeader {
			// The tar writer refuses any field but PAXRecords on this type.
			hdr = &tar.Header{Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": "test"}}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// tarDir packs parent/<root> into a release tarball.
func tarDir(t *testing.T, parent, root string) []byte {
	t.Helper()
	var entries []tarEntry
	err := filepath.WalkDir(filepath.Join(parent, root), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(parent, p)
		info, _ := d.Info()
		if d.IsDir() {
			entries = append(entries, tarEntry{name: rel + "/", typ: tar.TypeDir, mode: 0o755})
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		entries = append(entries, tarEntry{name: rel, typ: tar.TypeReg, body: string(body), mode: int64(info.Mode().Perm())})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tarball(t, entries...)
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// assetServer serves byte bodies by path over loopback http.
type assetServer struct {
	*httptest.Server
	mu    sync.Mutex
	files map[string][]byte
}

func newAssetServer(t *testing.T) *assetServer {
	t.Helper()
	s := &assetServer{files: map[string][]byte{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		body, ok := s.files[r.URL.Path]
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *assetServer) put(path string, body []byte) string {
	s.mu.Lock()
	s.files[path] = body
	s.mu.Unlock()
	return s.URL + path
}

// release builds a plugin at version, serves its tarball, and returns the
// catalog release describing it for arch.
func release(t *testing.T, srv *assetServer, arch, version string, caps, requires []string) Release {
	t.Helper()
	parent := t.TempDir()
	writePluginDir(t, parent, fixtureID, version, caps, requires)
	body := tarDir(t, parent, fixtureID)
	url := srv.put("/"+version+"-"+arch+".tar.gz", body)
	return Release{
		Version: version, Protocol: v1Version(1, 0), Capabilities: caps,
		Requires: Requires{Commands: requires},
		Assets:   map[string]Asset{"linux-" + arch: {URL: url, SHA256: sum(body), Size: int64(len(body))}},
	}
}

// catalogJSON renders a schema-1 catalog of one entry per release list; the
// first release of each list is the top-level one.
func catalogJSON(t *testing.T, entries ...Entry) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"schema": 1, "plugins": entries})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func entryFor(rels ...Release) Entry {
	e := Entry{ID: fixtureID, Name: "Timer", Author: "sysc", Description: "Countdown.", Category: "productivity", Release: rels[0]}
	e.Releases = rels[1:]
	return e
}

func requireGit(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	return path
}

// gitRepo is a source repository fixture served over file://.
type gitRepo struct {
	t   *testing.T
	dir string
}

func newGitRepo(t *testing.T) *gitRepo {
	t.Helper()
	requireGit(t)
	r := &gitRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	return r
}

func (r *gitRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// publish commits catalog.json and returns the new commit.
func (r *gitRepo) publish(body []byte) string {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, "catalog.json"), body, 0o644); err != nil {
		r.t.Fatal(err)
	}
	r.git("add", "catalog.json")
	r.git("commit", "-q", "-m", "catalog")
	return r.git("rev-parse", "HEAD")
}

func (r *gitRepo) url() string { return "file://" + r.dir }

func v1Version(major, minor int) v1.Version { return v1.Version{Major: major, Minor: minor} }
