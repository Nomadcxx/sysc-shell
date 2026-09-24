# Plugin Store Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver tranche 1 of the plugin sources design: a `internal/plugin/store` package that reads git-hosted catalogs and installs, updates, rolls back and removes plugins from sha256-pinned release tarballs, the managed plugin root and the local-override rule in discovery, `plugins.sources` in config, and `plugins.*` IPC methods to drive it. There is no Settings UI.

**Architecture:** The store is a single worker goroutine that owns every git, network and disk operation. It publishes an immutable `State` that callers copy out under a mutex. Each change to the managed tree goes through a `Replace` callback the shell supplies, which stops the plugin, runs the swap, and rescans, so no plugin process runs from a directory mid-swap. Pure pieces (catalog decode and resolution, listing status, archive extraction) are table-tested without a network. Git, HTTP and the installer are tested against a real `git init` fixture repository and an `httptest` server.

**Tech Stack:** Go 1.26 standard library (`archive/tar`, `compress/gzip`, `crypto/sha256`, `net/http`, `os.Root`, `os/exec`). Runtime `git` on `PATH`. No new module dependencies.

**Spec:** `docs/plans/2026-09-24-plugin-sources-design.md` (D1–D8 and D10's catalog rules; D9 is tranche 2).

## Global Constraints

- Go only. No new module dependencies: `git diff --exit-code -- go.mod go.sum` must pass.
- Work in the worktree `/home/nomadx/.config/superpowers/worktrees/sysc-shell/feature/plugin-sources-store` on branch `feature/plugin-sources-store`, created from `main`.
- Run `bd` only from `/home/nomadx/sysc-shell`. Commits made from the worktree need `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db`.
- Never run `-race` together with `./...`. Gates are per package, e.g. `GOMAXPROCS=4 go test -race -count=1 ./internal/plugin/store`. Repo-wide commands run as `GOMAXPROCS=4 go test -count=1 -p 2 ./...` and `GOMAXPROCS=4 go vet ./...`.
- Before each commit: `gofmt -w . && test -z "$(gofmt -l .)"`. Screen the message with `~/.git-hooks/commit-msg <file>`; the hook rejects `both`, `bottom`, `agent`, `llm`, AI attribution trailers and similar.
- The store goroutine never holds `Registry.mu` and never runs on the Wayland owner. `pluginHost.replace` must be called with `Registry.mu` unlocked.
- Catalog schema is `1`. The category set is exactly `utilities, monitoring, system, appearance, productivity, media, audio, networking, weather, finance, social`; anything else maps to `other`.
- Limits: catalog 4 MiB, asset 64 MiB, archive 256 entries and 128 MiB extracted, git 60 s per invocation.
- Asset and screenshot URLs are `https`, or `http` to a loopback host only. `homepage` and `release_notes` are `https` only. Source URLs in config are `https://` or `file:///absolute/path`.
- Source names match `^[a-z0-9][a-z0-9-]{0,31}$`. The built-in source is `sysc` → `https://github.com/Nomadcxx/sysc-plugins`.
- Managed root is `$XDG_DATA_HOME/sysc-shell/plugins` (fallback `~/.local/share/sysc-shell/plugins`). The source cache is `$XDG_CACHE_HOME/sysc-shell/sources` (fallback `~/.cache/sysc-shell/sources`).
- This plan does **not** change the protocol ceiling's value (minor 6). Task 1 only names it.

## Spec corrections carried by this plan

- **D8** says the daily check runs "through the existing `schedule.go` path". `internal/plugin/schedule.go` is the 30 Hz view-publish gate, not a periodic scheduler. The daily check belongs to tranche 2 and will be a `time.Ticker` owned by the store worker. The design document is corrected in the same commit as this plan.
- **The protocol ceiling.** `internal/plugin/supervisor.go:134` and `:250` refuse minor > 6, but `plugin/v1` defines minor 7 and `sysc-plugins`' wallpaper-depth declares `1.7`. That mismatch predates this plan and is recorded in bd. Task 1 names the ceiling once so the store and the supervisor cannot drift apart.

## File Structure

| File | Responsibility |
|---|---|
| `internal/plugin/protocol.go` (new) | The host's protocol ceiling and `HostSupports`. |
| `internal/plugin/supervisor.go` (modify) | Uses the ceiling instead of literals. |
| `internal/plugin/discovery.go` (modify) | `SourceManaged`, `ManagedRoot`, dot-directory skipping, user-over-managed override. |
| `internal/plugin/store/errors.go` | `Kind`, `Error`, `KindOf`. |
| `internal/plugin/store/catalog.go` | Catalog types, `Decode`, `Resolve`, `Newer`, URL rules. |
| `internal/plugin/store/archive.go` | `Extract` with defensive limits over `os.Root`. |
| `internal/plugin/store/fetch.go` | `Download`, `NewHTTPClient`. |
| `internal/plugin/store/git.go` | `Git.Catalog`: blobless clone or fetch, `git show`. |
| `internal/plugin/store/installed.go` | `Record`, `Installed`, `LoadInstalled` reconciliation, atomic `Save`. |
| `internal/plugin/store/install.go` | `Installer`: install, update, rollback, remove, staging cleanup. |
| `internal/plugin/store/store.go` | Worker, queue, `State`, listings, consent check. |
| `internal/plugin/store/paths.go` | `CacheRoot`. |
| `internal/plugin/store/fixture_test.go` | Shared test fixtures: manifests, plugin dirs, tarballs, catalogs, git repos, asset server. |
| `internal/config/config.go`, `load.go`, `write.go` (modify) | `PluginSource`, `Plugins.Sources`, `EffectiveSources`, validation, write. |
| `internal/shell/pluginhost.go` (modify) | `pluginHost.replace`. |
| `internal/shell/pluginstore.go` (new) | Registry binding, source/local callbacks, `plugins.*` IPC handler. |
| `internal/ipc/server.go` (modify) | Routes `plugins.*` methods. |
| `cmd/sysc-shell/main.go` (modify) | Constructs and starts the store. |

---

### Task 0: Tracking

- [ ] **Step 1: Create the tranche issues** from `/home/nomadx/sysc-shell`. Run `git status --short .beads/` first. Another session may hold uncommitted tracker changes; if so, tell the owner before writing.

```bash
cd /home/nomadx/sysc-shell
bd create "Plugin store core (sources design tranche 1)" -p 1 --deps discovered-from:sysc-506
bd create "Plugin store Settings UI (sources design tranche 2)" -p 1 --deps discovered-from:sysc-506
bd create "sysc-plugins: catalog.json, release workflow, validate-catalog, first tagged release" -p 1 --deps discovered-from:sysc-506
bd create "Create sysc-community-plugins with CI catalog validator" -p 2 --deps discovered-from:sysc-506
bd create "Host protocol ceiling is minor 6 while plugin/v1 and wallpaper-depth use minor 7" -p 1 --deps discovered-from:sysc-506
```

Record the tranche 1 id and claim it with `bd update <id> --status in_progress`. Make the tranche 2 issue depend on tranche 1, and on the sysc-plugins release issue (its live gate installs a real release): `bd dep add <t2> <t1>` and `bd dep add <t2> <release>`.

- [ ] **Step 2: Create the worktree**

```bash
git -C /home/nomadx/sysc-shell worktree add \
  /home/nomadx/.config/superpowers/worktrees/sysc-shell/feature/plugin-sources-store \
  -b feature/plugin-sources-store main
```

---

### Task 1: Name the host protocol ceiling

**Files:**
- Create: `internal/plugin/protocol.go`, `internal/plugin/protocol_test.go`
- Modify: `internal/plugin/supervisor.go:134-135`, `:250-251`

**Interfaces:**
- Produces: `plugin.HostProtocolMajor = 1`, `plugin.HostProtocolMinor = 6`, `func plugin.HostSupports(v v1.Version) bool`.

- [ ] **Step 1: Write the failing test** `internal/plugin/protocol_test.go`

```go
package plugin

import (
	"testing"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func TestHostSupports(t *testing.T) {
	t.Parallel()
	cases := []struct {
		v    v1.Version
		want bool
	}{
		{v1.Version{Major: 1, Minor: 0}, true},
		{v1.Version{Major: 1, Minor: HostProtocolMinor}, true},
		{v1.Version{Major: 1, Minor: HostProtocolMinor + 1}, false},
		{v1.Version{Major: 2, Minor: 0}, false},
		{v1.Version{Major: 0, Minor: 9}, false},
		{v1.Version{Major: 1, Minor: -1}, false},
	}
	for _, c := range cases {
		if got := HostSupports(c.v); got != c.want {
			t.Errorf("HostSupports(%+v) = %v, want %v", c.v, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `GOMAXPROCS=4 go test -count=1 -run TestHostSupports ./internal/plugin`
Expected: FAIL, `undefined: HostSupports`.

- [ ] **Step 3: Implement** `internal/plugin/protocol.go`

```go
package plugin

import v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"

// The newest protocol this host speaks. The supervisor refuses anything newer,
// and the plugin store resolves catalog releases against the same ceiling, so
// a release the store offers is one the supervisor will start.
const (
	HostProtocolMajor = 1
	HostProtocolMinor = 6
)

// HostSupports reports whether a plugin declaring v can run on this host.
func HostSupports(v v1.Version) bool {
	return v.Major == HostProtocolMajor && v.Minor >= 0 && v.Minor <= HostProtocolMinor
}
```

In `internal/plugin/supervisor.go`, replace both literal checks:

```go
	if p := s.Manifest.Protocol; !HostSupports(p) {
		return nil, &IncompatibleError{Plugin: s.Manifest.ID, Want: HostProtocolMajor, MaxMinor: HostProtocolMinor, Got: p}
	}
```

```go
	if !HostSupports(reply.Protocol) {
		return &IncompatibleError{Plugin: s.Manifest.ID, Want: HostProtocolMajor, MaxMinor: HostProtocolMinor, Got: reply.Protocol}
	}
```

- [ ] **Step 4: Run the package**

Run: `GOMAXPROCS=4 go test -race -count=1 ./internal/plugin`
Expected: PASS. The existing incompatibility tests still pass, because the value did not change.

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/protocol.go internal/plugin/protocol_test.go internal/plugin/supervisor.go
git commit -m "refactor(plugin): name the host protocol ceiling"
```

---

### Task 2: Managed root and the local override in discovery

**Files:**
- Modify: `internal/plugin/discovery.go`
- Test: `internal/plugin/discovery_test.go`

**Interfaces:**
- Consumes: `install(t, root, id, manifest)`, `idOf`, `timerManifest` from existing tests.
- Produces: `plugin.SourceManaged Source = "managed"`, `func plugin.ManagedRoot() string`, the field `Candidate.ShadowedBy string`. `Lookup` and `Startable` ignore shadowed candidates. `DefaultRoots` returns user, managed, system, in that order.

- [ ] **Step 1: Write the failing tests** (append to `internal/plugin/discovery_test.go`)

```go
func TestDiscoverLetsAUserCopyShadowAManagedOne(t *testing.T) {
	t.Parallel()

	user, managed := t.TempDir(), t.TempDir()
	userDir := install(t, user, "timer", timerManifest)
	managedDir := install(t, managed, "org.sysc.timer", timerManifest)

	cat, err := Discover(Root{Path: user, Source: SourceUser}, Root{Path: managed, Source: SourceManaged})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	got, ok := cat.Lookup("org.sysc.timer")
	if !ok || got.Dir != userDir {
		t.Fatalf("Lookup = %+v, %v; want the user copy at %s", got, ok, userDir)
	}
	var shadowed *Candidate
	for i := range cat.Plugins {
		if cat.Plugins[i].Dir == managedDir {
			shadowed = &cat.Plugins[i]
		}
	}
	if shadowed == nil {
		t.Fatal("the managed copy disappeared; the manager must still show it")
	}
	if shadowed.Err != nil || shadowed.ShadowedBy != userDir || shadowed.Startable() {
		t.Errorf("managed = %+v; want valid, ShadowedBy %s, not startable", *shadowed, userDir)
	}
}

func TestDiscoverStillRejectsEveryOtherCollision(t *testing.T) {
	t.Parallel()

	cases := []struct{ a, b Source }{
		{SourceUser, SourceUser},
		{SourceManaged, SourceManaged},
		{SourceUser, SourceSystem},
		{SourceManaged, SourceSystem},
	}
	for _, c := range cases {
		ra, rb := t.TempDir(), t.TempDir()
		install(t, ra, "one", timerManifest)
		install(t, rb, "two", timerManifest)
		cat, err := Discover(Root{Path: ra, Source: c.a}, Root{Path: rb, Source: c.b})
		if err != nil {
			t.Fatalf("%v/%v: Discover: %v", c.a, c.b, err)
		}
		if _, ok := cat.Lookup("org.sysc.timer"); ok {
			t.Errorf("%v against %v: a duplicated id stayed usable", c.a, c.b)
		}
	}
}

func TestDiscoverSkipsDotDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	install(t, root, ".staging", timerManifest)
	install(t, root, ".prev", idOf(t, "org.sysc.other"))
	cat, err := Discover(Root{Path: root, Source: SourceManaged})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(cat.Plugins) != 0 {
		t.Fatalf("found %d candidates in dot-directories, want none", len(cat.Plugins))
	}
}

func TestManagedRootFollowsXDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/data")
	if got := ManagedRoot(); got != "/data/sysc-shell/plugins" {
		t.Errorf("ManagedRoot = %s", got)
	}
	t.Setenv("XDG_DATA_HOME", "relative")
	t.Setenv("HOME", "/home/u")
	if got := ManagedRoot(); got != "/home/u/.local/share/sysc-shell/plugins" {
		t.Errorf("ManagedRoot with a relative XDG_DATA_HOME = %s", got)
	}
}
```

Replace the existing `TestDefaultRootsNameTheUserAndSystemDirectories` (`discovery_test.go:234`), which expects exactly two roots, with:

```go
func TestDefaultRootsNameTheUserManagedAndSystemDirectories(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/cfg")
	t.Setenv("XDG_DATA_HOME", "/tmp/data")
	roots := DefaultRoots("/usr/share/sysc-shell/plugins")
	want := []Root{
		{Path: "/tmp/cfg/sysc-shell/plugins", Source: SourceUser},
		{Path: "/tmp/data/sysc-shell/plugins", Source: SourceManaged},
		{Path: "/usr/share/sysc-shell/plugins", Source: SourceSystem},
	}
	if !reflect.DeepEqual(roots, want) {
		t.Fatalf("roots = %+v, want %+v", roots, want)
	}
}
```

(Add `"reflect"` to the test file's imports.)

- [ ] **Step 2: Run and confirm failure**

Run: `GOMAXPROCS=4 go test -count=1 -run 'Shadow|OtherCollision|DotDirectories|ManagedRoot|DefaultRoots' ./internal/plugin`
Expected: FAIL (`undefined: SourceManaged`, `ShadowedBy`, `ManagedRoot`).

- [ ] **Step 3: Implement** in `internal/plugin/discovery.go`

Add the source constant and the root:

```go
const (
	SourceUser    Source = "user"
	SourceSystem  Source = "system"
	SourceManaged Source = "managed"
)

// ManagedRoot is the directory the plugin store installs into. The shell owns
// it; hand-managed plugins live in the user root instead, so an install or a
// removal can never touch a directory the user placed.
func ManagedRoot() string {
	base := os.Getenv("XDG_DATA_HOME")
	// The specification requires an absolute path, as StateRoot does.
	if !filepath.IsAbs(base) {
		base = filepath.Join(os.Getenv("HOME"), ".local", "share")
	}
	return filepath.Join(base, "sysc-shell", "plugins")
}
```

Update `DefaultRoots` so the managed root follows the user root:

```go
	if base, err := os.UserConfigDir(); err == nil {
		roots = append(roots, Root{Path: filepath.Join(base, "sysc-shell", "plugins"), Source: SourceUser})
	}
	roots = append(roots, Root{Path: ManagedRoot(), Source: SourceManaged})
```

(Make the slice capacity 3.)

Add to `Candidate`:

```go
	// ShadowedBy is the user directory overriding this managed copy. A
	// shadowed candidate is valid and shown, and never started.
	ShadowedBy string
```

Change `Startable` and `Lookup`:

```go
func (c Candidate) Startable() bool {
	return c.Err == nil && c.ShadowedBy == "" && len(c.MissingCommands) == 0
}
```

```go
		if p.Err == nil && p.ShadowedBy == "" && p.Manifest.ID == id {
```

In `Discover`'s directory loop, skip hidden names before the `Stat`:

```go
		for _, e := range entries {
			// The store keeps .staging and .prev beside installed plugins;
			// neither is a plugin, and nothing hidden is scanned.
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
```

(Add `"strings"` to the imports.)

Replace the body of `rejectDuplicates`'s collision handling so exactly one user plus one managed candidate resolves as an override:

```go
	for id, idx := range byID {
		if len(idx) < 2 {
			continue
		}
		if user, managed, ok := overridePair(found, idx); ok {
			found[managed].ShadowedBy = found[user].Dir
			continue
		}
		paths := make([]string, len(idx))
		// ... existing rejection unchanged ...
	}
```

```go
// overridePair reports whether a collision is one user copy over one managed
// copy: the only collision that resolves rather than rejects. A developer's
// checkout linked into the user root is how a plugin is worked on, and it has
// to be able to stand in front of the released copy the store installed.
// Every other pairing stays a fault, per the M6 rule.
func overridePair(found []Candidate, idx []int) (user, managed int, ok bool) {
	if len(idx) != 2 {
		return 0, 0, false
	}
	a, b := idx[0], idx[1]
	switch {
	case found[a].Source == SourceUser && found[b].Source == SourceManaged:
		return a, b, true
	case found[b].Source == SourceUser && found[a].Source == SourceManaged:
		return b, a, true
	}
	return 0, 0, false
}
```

Update the `Source` doc comment to say the source now also decides the one override.

- [ ] **Step 4: Run the package**

Run: `GOMAXPROCS=4 go test -race -count=1 ./internal/plugin`
Expected: PASS, including the unchanged `TestDiscoverRejectsBothSidesOfADuplicateID`.

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/discovery.go internal/plugin/discovery_test.go
git commit -m "feat(plugin): managed root, with a local copy overriding a managed one"
```

---

### Task 3: Catalog decoding and resolution

**Files:**
- Create: `internal/plugin/store/errors.go`, `internal/plugin/store/catalog.go`, `internal/plugin/store/fixture_test.go`, `internal/plugin/store/catalog_test.go`

**Interfaces:**
- Consumes: `plugin.HostSupports`, `plugin.HostProtocolMinor`, `v1.ValidPluginID`, `v1.Version`.
- Produces:
  - `type Kind string` with the constants below; `type Error struct{Kind Kind; Detail string; Err error}`; `func KindOf(error) Kind`
  - `type Asset struct{URL, SHA256 string; Size int64}`, `type Screenshot struct{URL, SHA256 string}`, `type Requires struct{Commands []string}`
  - `type Release struct{Version string; Protocol v1.Version; Capabilities []string; Requires Requires; Assets map[string]Asset; ReleaseNotes string}`
  - `type Entry struct{ID, Name, Author, Description, LongDescription, Category, License, Homepage string; Screenshot *Screenshot; AddedAt, UpdatedAt time.Time; Deprecated bool; Release; Releases []Release}`
  - `type RowError struct{Index int; ID string; Err error}`, `type Catalog struct{Entries []Entry; Rejected []RowError}`
  - `func Decode([]byte) (Catalog, error)`
  - `type Compat string` (`Compatible`, `HeldBack`, `Incompatible`); `type Resolution struct{Release *Release; Compat Compat; Needs v1.Version}`; `func Resolve(Entry, arch string) Resolution`
  - `func Newer(a, b string) bool`, `func checkFetchURL(string) error`, `func sameSet(a, b []string) bool`
  - Constants `Schema = 1`, `MaxCatalogBytes = 4 << 20`, `MaxAssetBytes int64 = 64 << 20`, `CategoryOther = "other"`, `var Categories []string`

- [ ] **Step 1: Write the shared fixtures** `internal/plugin/store/fixture_test.go`. Later tasks use all of them. They are written once here and must not be copied into other files.

```go
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
```

Add `v1Version` to the same file:

```go
func v1Version(major, minor int) v1.Version { return v1.Version{Major: major, Minor: minor} }
```

(with the import `v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"`).

- [ ] **Step 2: Write the failing tests** `internal/plugin/store/catalog_test.go`

```go
package store

import (
	"encoding/json"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
)

func validAsset() Asset {
	return Asset{URL: "https://example.com/t.tar.gz", SHA256: sum([]byte("x")), Size: 10}
}

func validEntry() Entry {
	return entryFor(Release{Version: "1.4.0", Protocol: v1Version(1, 3),
		Capabilities: []string{"panels"}, Assets: map[string]Asset{"linux-amd64": validAsset()}})
}

func decodeOne(t *testing.T, mutate func(map[string]any)) (Catalog, error) {
	t.Helper()
	raw, _ := json.Marshal(validEntry())
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	mutate(m)
	body, _ := json.Marshal(map[string]any{"schema": 1, "plugins": []any{m}})
	return Decode(body)
}

func TestDecodeReadsAValidRow(t *testing.T) {
	t.Parallel()
	cat, err := decodeOne(t, func(m map[string]any) {
		m["unknown_future_field"] = 7
		m["homepage"] = "https://example.com"
		m["updated_at"] = "2026-09-24T00:00:00Z"
	})
	if err != nil || len(cat.Entries) != 1 || len(cat.Rejected) != 0 {
		t.Fatalf("Decode = %+v, %v", cat, err)
	}
	e := cat.Entries[0]
	if e.ID != fixtureID || e.Version != "1.4.0" || e.Homepage != "https://example.com" || e.UpdatedAt.IsZero() {
		t.Errorf("entry = %+v", e)
	}
}

func TestDecodeRejectsAnUnknownSchema(t *testing.T) {
	t.Parallel()
	_, err := Decode([]byte(`{"schema": 2, "plugins": []}`))
	if KindOf(err) != KindSchema {
		t.Fatalf("err = %v, want %s", err, KindSchema)
	}
}

func TestDecodeRejectsBadRowsAndKeepsTheRest(t *testing.T) {
	t.Parallel()
	cases := map[string]func(map[string]any){
		"bad id":             func(m map[string]any) { m["id"] = "Timer" },
		"missing name":       func(m map[string]any) { delete(m, "name") },
		"missing category":   func(m map[string]any) { delete(m, "category") },
		"bad version":        func(m map[string]any) { m["version"] = "1.4" },
		"http homepage":      func(m map[string]any) { m["homepage"] = "http://example.com" },
		"http release notes": func(m map[string]any) { m["release_notes"] = "http://example.com" },
		"no assets":          func(m map[string]any) { m["assets"] = map[string]any{} },
		"remote http asset": func(m map[string]any) {
			m["assets"] = map[string]any{"linux-amd64": map[string]any{"url": "http://example.com/a", "sha256": sum(nil), "size": 1}}
		},
		"short sha": func(m map[string]any) {
			m["assets"] = map[string]any{"linux-amd64": map[string]any{"url": "https://e.com/a", "sha256": "abc", "size": 1}}
		},
		"oversize asset": func(m map[string]any) {
			m["assets"] = map[string]any{"linux-amd64": map[string]any{"url": "https://e.com/a", "sha256": sum(nil), "size": MaxAssetBytes + 1}}
		},
		"bad asset key": func(m map[string]any) {
			m["assets"] = map[string]any{"darwin": map[string]any{"url": "https://e.com/a", "sha256": sum(nil), "size": 1}}
		},
		"bad timestamp": func(m map[string]any) { m["added_at"] = "yesterday" },
	}
	for name, mutate := range cases {
		cat, err := decodeOne(t, mutate)
		if err != nil {
			t.Errorf("%s: whole catalog failed: %v", name, err)
			continue
		}
		if len(cat.Entries) != 0 || len(cat.Rejected) != 1 {
			t.Errorf("%s: entries %d rejected %d, want 0 and 1", name, len(cat.Entries), len(cat.Rejected))
		}
	}
}

func TestDecodeAdmitsLoopbackHTTPAssets(t *testing.T) {
	t.Parallel()
	cat, err := decodeOne(t, func(m map[string]any) {
		m["assets"] = map[string]any{"linux-amd64": map[string]any{"url": "http://127.0.0.1:9/a", "sha256": sum(nil), "size": 1}}
	})
	if err != nil || len(cat.Entries) != 1 {
		t.Fatalf("Decode = %+v, %v", cat, err)
	}
}

func TestDecodeMapsAnUnknownCategoryToOther(t *testing.T) {
	t.Parallel()
	cat, err := decodeOne(t, func(m map[string]any) { m["category"] = "Stock" })
	if err != nil || len(cat.Entries) != 1 || cat.Entries[0].Category != CategoryOther {
		t.Fatalf("Decode = %+v, %v", cat, err)
	}
}

func TestDecodeRejectsEveryRowOfADuplicatedID(t *testing.T) {
	t.Parallel()
	e := validEntry()
	body, _ := json.Marshal(map[string]any{"schema": 1, "plugins": []Entry{e, e}})
	cat, err := Decode(body)
	if err != nil || len(cat.Entries) != 0 || len(cat.Rejected) != 2 {
		t.Fatalf("Decode = %+v, %v", cat, err)
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()
	asset := map[string]Asset{"linux-amd64": validAsset()}
	tooNew := v1Version(1, plugin.HostProtocolMinor+1)
	cases := []struct {
		name    string
		entry   Entry
		arch    string
		compat  Compat
		version string
	}{
		{"tip fits", entryFor(Release{Version: "1.4.0", Protocol: v1Version(1, 0), Assets: asset}), "amd64", Compatible, "1.4.0"},
		{"tip too new, older fits", entryFor(
			Release{Version: "2.0.0", Protocol: tooNew, Assets: asset},
			Release{Version: "1.3.2", Protocol: v1Version(1, 0), Assets: asset},
			Release{Version: "1.2.0", Protocol: v1Version(1, 0), Assets: asset},
		), "amd64", HeldBack, "1.3.2"},
		{"nothing fits", entryFor(Release{Version: "2.0.0", Protocol: v1Version(2, 0), Assets: asset}), "amd64", Incompatible, ""},
		{"no asset for arch", entryFor(Release{Version: "1.4.0", Protocol: v1Version(1, 0), Assets: asset}), "arm64", Incompatible, ""},
	}
	for _, c := range cases {
		got := Resolve(c.entry, c.arch)
		if got.Compat != c.compat {
			t.Errorf("%s: compat %s, want %s", c.name, got.Compat, c.compat)
		}
		if c.version == "" && got.Release != nil || c.version != "" && (got.Release == nil || got.Release.Version != c.version) {
			t.Errorf("%s: release %+v, want %q", c.name, got.Release, c.version)
		}
		if got.Needs != c.entry.Protocol {
			t.Errorf("%s: needs %+v, want the tip's %+v", c.name, got.Needs, c.entry.Protocol)
		}
	}
}

func TestNewer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.10.0", "1.9.9", true},
		{"1.4.0", "1.4.0", false},
		{"1.3.9", "1.4.0", false},
		{"2.0.0", "garbage", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}
```

- [ ] **Step 3: Run and confirm failure**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/plugin/store`
Expected: FAIL, undefined symbols.

- [ ] **Step 4: Implement** `internal/plugin/store/errors.go`

```go
// Package store reads plugin catalogs from git-hosted sources and installs,
// updates, rolls back and removes plugins from their pinned release assets.
package store

import (
	"errors"
	"fmt"
)

// Kind names why a store operation failed. It is what the manager shows on
// the row the failure belongs to, so each value reads as a cause.
type Kind string

const (
	KindUnreachable Kind = "source unreachable"
	KindGitMissing  Kind = "git not found on PATH"
	KindGitTimeout  Kind = "git timed out"
	KindSchema      Kind = "catalog schema unsupported"
	KindCatalog     Kind = "catalog invalid"
	KindNoAsset     Kind = "no release for this machine"
	KindTooLarge    Kind = "download too large"
	KindChecksum    Kind = "sha256 mismatch"
	KindArchive     Kind = "archive rejected"
	KindManifest    Kind = "manifest mismatch"
	KindConsent     Kind = "consent required"
	KindNoPrevious  Kind = "no previous version"
	KindNotListed   Kind = "not in any enabled source"
	KindBusy        Kind = "store busy"
	KindDisk        Kind = "disk error"
)

// Error is every failure the store reports.
type Error struct {
	Kind   Kind
	Detail string
	Err    error
}

func (e *Error) Error() string {
	msg := string(e.Kind)
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Err }

func fail(k Kind, err error, format string, args ...any) *Error {
	return &Error{Kind: k, Detail: fmt.Sprintf(format, args...), Err: err}
}

// KindOf returns the kind of a store error, or "" for any other error.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return ""
}
```

`internal/plugin/store/catalog.go`

```go
package store

import (
	"cmp"
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

const (
	// Schema is the one catalog schema this shell reads. It changes only for
	// a breaking change; new fields are additive and ignored by older shells.
	Schema = 1

	MaxCatalogBytes       = 4 << 20
	MaxAssetBytes   int64 = 64 << 20

	CategoryOther = "other"
)

// Categories is the closed set a catalog row may name: the DMS registry's
// working set with its duplicate spellings merged.
var Categories = []string{
	"utilities", "monitoring", "system", "appearance", "productivity",
	"media", "audio", "networking", "weather", "finance", "social",
}

var (
	versionPattern  = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	assetKeyPattern = regexp.MustCompile(`^linux-[a-z0-9]+$`)
)

type Asset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type Screenshot struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type Requires struct {
	Commands []string `json:"commands"`
}

// Release is one installable version of a plugin.
type Release struct {
	Version      string           `json:"version"`
	Protocol     v1.Version       `json:"protocol"`
	Capabilities []string         `json:"capabilities"`
	Requires     Requires         `json:"requires"`
	Assets       map[string]Asset `json:"assets"`
	ReleaseNotes string           `json:"release_notes,omitempty"`
}

// Entry is one catalog row. Its embedded Release is the newest version; older
// ones in Releases exist for hosts below that version's protocol.
type Entry struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	Author          string      `json:"author"`
	Description     string      `json:"description"`
	LongDescription string      `json:"long_description,omitempty"`
	Category        string      `json:"category"`
	License         string      `json:"license,omitempty"`
	Homepage        string      `json:"homepage,omitempty"`
	Screenshot      *Screenshot `json:"screenshot,omitempty"`
	AddedAt         time.Time   `json:"added_at,omitzero"`
	UpdatedAt       time.Time   `json:"updated_at,omitzero"`
	Deprecated      bool        `json:"deprecated,omitempty"`
	Release
	Releases []Release `json:"releases,omitempty"`
}

// RowError is one catalog row that did not decode or validate.
type RowError struct {
	Index int
	ID    string
	Err   error
}

// Catalog is one decoded source. A bad row is rejected alone, because one
// author's mistake must not hide every other plugin in the source.
type Catalog struct {
	Entries  []Entry
	Rejected []RowError
}

// Decode parses and validates a catalog. Unknown fields are ignored, unlike the
// strict plugin wire decode: a catalog is read by shells of every age.
func Decode(data []byte) (Catalog, error) {
	if len(data) > MaxCatalogBytes {
		return Catalog{}, fail(KindCatalog, nil, "larger than %d bytes", MaxCatalogBytes)
	}
	var doc struct {
		Schema  int               `json:"schema"`
		Plugins []json.RawMessage `json:"plugins"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return Catalog{}, fail(KindCatalog, err, "")
	}
	if doc.Schema != Schema {
		return Catalog{}, fail(KindSchema, nil, "schema %d; this shell reads %d", doc.Schema, Schema)
	}

	type row struct {
		index int
		entry Entry
	}
	var cat Catalog
	var rows []row
	count := map[string]int{}
	for i, raw := range doc.Plugins {
		var e Entry
		err := json.Unmarshal(raw, &e)
		if err == nil {
			err = e.validate()
		}
		if err != nil {
			cat.Rejected = append(cat.Rejected, RowError{Index: i, ID: e.ID, Err: err})
			continue
		}
		count[e.ID]++
		rows = append(rows, row{i, e})
	}
	for _, r := range rows {
		if count[r.entry.ID] > 1 {
			cat.Rejected = append(cat.Rejected, RowError{Index: r.index, ID: r.entry.ID,
				Err: fail(KindCatalog, nil, "id %q appears more than once", r.entry.ID)})
			continue
		}
		cat.Entries = append(cat.Entries, r.entry)
	}
	return cat, nil
}

func (e *Entry) validate() error {
	switch {
	case !v1.ValidPluginID(e.ID):
		return fail(KindCatalog, nil, "%q is not a plugin id", e.ID)
	case e.Name == "" || e.Author == "" || e.Description == "":
		return fail(KindCatalog, nil, "name, author and description are required")
	case e.Category == "":
		return fail(KindCatalog, nil, "category is required")
	case e.Homepage != "" && !isHTTPS(e.Homepage):
		return fail(KindCatalog, nil, "homepage %q is not an https URL", e.Homepage)
	}
	// A newer catalog may name a category this shell has never heard of; it
	// shows as Other rather than failing the row.
	if !slices.Contains(Categories, e.Category) {
		e.Category = CategoryOther
	}
	if s := e.Screenshot; s != nil {
		if err := checkFetchURL(s.URL); err != nil {
			return err
		}
		if !sha256Pattern.MatchString(s.SHA256) {
			return fail(KindCatalog, nil, "screenshot sha256 %q is not 64 lower-case hex digits", s.SHA256)
		}
	}
	if err := e.Release.validate(); err != nil {
		return err
	}
	for i := range e.Releases {
		if err := e.Releases[i].validate(); err != nil {
			return fail(KindCatalog, err, "releases[%d]", i)
		}
	}
	return nil
}

func (r Release) validate() error {
	switch {
	case !versionPattern.MatchString(r.Version):
		return fail(KindCatalog, nil, "version %q is not MAJOR.MINOR.PATCH", r.Version)
	case r.Protocol.Major < 1 || r.Protocol.Minor < 0:
		return fail(KindCatalog, nil, "protocol %d.%d is not a plugin protocol", r.Protocol.Major, r.Protocol.Minor)
	case len(r.Assets) == 0:
		return fail(KindCatalog, nil, "version %s has no assets", r.Version)
	case r.ReleaseNotes != "" && !isHTTPS(r.ReleaseNotes):
		return fail(KindCatalog, nil, "release notes %q is not an https URL", r.ReleaseNotes)
	}
	for key, a := range r.Assets {
		if !assetKeyPattern.MatchString(key) {
			return fail(KindCatalog, nil, "asset key %q is not linux-<arch>", key)
		}
		if err := checkFetchURL(a.URL); err != nil {
			return err
		}
		if !sha256Pattern.MatchString(a.SHA256) {
			return fail(KindCatalog, nil, "asset sha256 %q is not 64 lower-case hex digits", a.SHA256)
		}
		if a.Size < 1 || a.Size > MaxAssetBytes {
			return fail(KindCatalog, nil, "asset size %d is outside 1..%d", a.Size, MaxAssetBytes)
		}
	}
	return nil
}

// checkFetchURL admits https anywhere and plain http only to this machine. The
// sha256 pins content either way; the loopback exception lets authors and
// tests serve assets locally without a certificate.
func checkFetchURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fail(KindCatalog, err, "%q is not a URL", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return fail(KindCatalog, nil, "%q must use https", raw)
}

func isHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

type Compat string

const (
	Compatible   Compat = "compatible"
	HeldBack     Compat = "held back"
	Incompatible Compat = "incompatible"
)

// Resolution is what this host would install from one entry.
type Resolution struct {
	// Release is nil when nothing is installable.
	Release *Release
	Compat  Compat
	// Needs is the newest release's protocol, which the manager names when
	// the row is held back or incompatible.
	Needs v1.Version
}

// Resolve picks the newest release this host can run on arch.
func Resolve(e Entry, arch string) Resolution {
	key := "linux-" + arch
	candidates := append([]Release{e.Release}, e.Releases...)
	var best *Release
	for i := range candidates {
		r := &candidates[i]
		if !plugin.HostSupports(r.Protocol) {
			continue
		}
		if _, ok := r.Assets[key]; !ok {
			continue
		}
		if best == nil || Newer(r.Version, best.Version) {
			best = r
		}
	}
	res := Resolution{Release: best, Needs: e.Protocol}
	switch {
	case best == nil:
		res.Compat = Incompatible
	case best.Version == e.Version:
		res.Compat = Compatible
	default:
		res.Compat = HeldBack
	}
	return res
}

// Newer reports whether version a is later than b. Anything that is not
// MAJOR.MINOR.PATCH is never newer, so a malformed installed record cannot
// manufacture an update.
func Newer(a, b string) bool {
	if !versionPattern.MatchString(a) || !versionPattern.MatchString(b) {
		return false
	}
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range 3 {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if c := cmp.Compare(x, y); c != 0 {
			return c > 0
		}
	}
	return false
}

// sameSet compares two string lists as sets.
func sameSet(a, b []string) bool {
	return slices.Equal(slices.Compact(slices.Sorted(slices.Values(a))), slices.Compact(slices.Sorted(slices.Values(b))))
}
```

- [ ] **Step 5: Run the package**

Run: `GOMAXPROCS=4 go test -race -count=1 ./internal/plugin/store && GOMAXPROCS=4 go vet ./internal/plugin/store`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/plugin/store
git commit -m "feat(store): decode and resolve plugin catalogs"
```

---

### Task 4: Defensive archive extraction

**Files:**
- Create: `internal/plugin/store/archive.go`, `internal/plugin/store/archive_test.go`

**Interfaces:**
- Produces: `type Limits struct{MaxEntries int; MaxBytes int64}`, `var DefaultLimits = Limits{256, 128 << 20}`, `func Extract(r io.Reader, dest, root string, lim Limits) error`.

- [ ] **Step 1: Write the failing test** `internal/plugin/store/archive_test.go`

```go
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

func TestExtractRejectsSomethingThatIsNotGzip(t *testing.T) {
	t.Parallel()
	err := Extract(bytes.NewReader([]byte("plain")), t.TempDir(), fixtureID, DefaultLimits)
	if KindOf(err) != KindArchive {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run and confirm failure**

Run: `GOMAXPROCS=4 go test -count=1 -run Extract ./internal/plugin/store`
Expected: FAIL, `undefined: Extract`.

- [ ] **Step 3: Implement** `internal/plugin/store/archive.go`

```go
package store

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Limits bounds one archive.
type Limits struct {
	MaxEntries int
	MaxBytes   int64
}

var DefaultLimits = Limits{MaxEntries: 256, MaxBytes: 128 << 20}

// Extract unpacks a gzipped tar into dest, which must exist. The archive must
// hold exactly one top-level directory, named root, containing only regular
// files and directories.
//
// Writes go through os.Root, so no entry can land outside dest whatever its
// name; the name checks exist to reject such an archive by name rather than
// by an opaque error. Links are refused outright: a plugin directory has no
// use for one, and each is a way out of the tree.
func Extract(r io.Reader, dest, root string, lim Limits) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fail(KindArchive, err, "not a gzip stream")
	}
	defer gz.Close()
	fsys, err := os.OpenRoot(dest)
	if err != nil {
		return fail(KindDisk, err, "")
	}
	defer fsys.Close()

	tr := tar.NewReader(gz)
	var entries int
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fail(KindArchive, err, "")
		}
		if entries++; entries > lim.MaxEntries {
			return fail(KindArchive, nil, "more than %d entries", lim.MaxEntries)
		}
		name := strings.TrimSuffix(hdr.Name, "/")
		if !filepath.IsLocal(name) || path.Clean(name) != name {
			return fail(KindArchive, nil, "%q is not a plain relative path", hdr.Name)
		}
		if top, _, _ := strings.Cut(name, "/"); top != root {
			return fail(KindArchive, nil, "%q is outside %s/", hdr.Name, root)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := fsys.MkdirAll(name, 0o755); err != nil {
				return fail(KindArchive, err, "%q", hdr.Name)
			}
		case tar.TypeReg:
			if total += hdr.Size; hdr.Size < 0 || total > lim.MaxBytes {
				return fail(KindArchive, nil, "more than %d bytes", lim.MaxBytes)
			}
			if err := fsys.MkdirAll(path.Dir(name), 0o755); err != nil {
				return fail(KindArchive, err, "%q", hdr.Name)
			}
			mode := os.FileMode(0o644)
			if hdr.Mode&0o111 != 0 {
				mode = 0o755
			}
			// O_EXCL: an archive naming one file twice is malformed, and the
			// second write must not replace what was already checked.
			f, err := fsys.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				return fail(KindArchive, err, "%q", hdr.Name)
			}
			n, err := io.Copy(f, io.LimitReader(tr, hdr.Size))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err == nil && n != hdr.Size {
				err = io.ErrUnexpectedEOF
			}
			if err != nil {
				return fail(KindArchive, err, "%q", hdr.Name)
			}
		default:
			return fail(KindArchive, nil, "%q is not a regular file or directory", hdr.Name)
		}
	}
	if info, err := fsys.Stat(root); err != nil || !info.IsDir() {
		return fail(KindArchive, err, "no %s/ directory", root)
	}
	return nil
}
```

- [ ] **Step 4: Run**

Run: `GOMAXPROCS=4 go test -race -count=1 ./internal/plugin/store`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/store/archive.go internal/plugin/store/archive_test.go
git commit -m "feat(store): extract release archives defensively"
```

---

### Task 5: Pinned downloads

**Files:**
- Create: `internal/plugin/store/fetch.go`, `internal/plugin/store/fetch_test.go`

**Interfaces:**
- Consumes: `checkFetchURL`, `assetServer`, `sum`.
- Produces: `func Download(ctx context.Context, c *http.Client, rawURL, sha string, limit int64, dst string) error`, `func NewHTTPClient() *http.Client`.

- [ ] **Step 1: Write the failing test** `internal/plugin/store/fetch_test.go`

```go
package store

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownload(t *testing.T) {
	t.Parallel()
	srv := newAssetServer(t)
	body := []byte("release bytes")
	url := srv.put("/a.tar.gz", body)
	ctx := context.Background()

	cases := []struct {
		name  string
		url   string
		sha   string
		limit int64
		want  Kind
	}{
		{"ok", url, sum(body), int64(len(body)), ""},
		{"checksum", url, sum([]byte("other")), int64(len(body)), KindChecksum},
		{"too large", url, sum(body), int64(len(body)) - 1, KindTooLarge},
		{"not found", srv.URL + "/missing", sum(body), 100, KindUnreachable},
		{"remote http", "http://example.invalid/a", sum(body), 100, KindCatalog},
	}
	for _, c := range cases {
		dst := filepath.Join(t.TempDir(), "asset")
		err := Download(ctx, NewHTTPClient(), c.url, c.sha, c.limit, dst)
		if KindOf(err) != c.want || (c.want == "") != (err == nil) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
		_, statErr := os.Stat(dst)
		if c.want == "" && statErr != nil {
			t.Errorf("%s: no file written", c.name)
		}
		if c.want != "" && statErr == nil {
			t.Errorf("%s: a rejected download was left on disk", c.name)
		}
	}
}

func TestDownloadRefusesARedirectToRemoteHTTP(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.RedirectHandler("http://example.invalid/a", http.StatusFound))
	t.Cleanup(srv.Close)
	err := Download(context.Background(), NewHTTPClient(), srv.URL, sum(nil), 10, filepath.Join(t.TempDir(), "a"))
	if KindOf(err) != KindUnreachable {
		t.Fatalf("err = %v, want the redirect refused", err)
	}
}
```

- [ ] **Step 2: Run and confirm failure**

Run: `GOMAXPROCS=4 go test -count=1 -run Download ./internal/plugin/store`
Expected: FAIL, `undefined: Download`.

- [ ] **Step 3: Implement** `internal/plugin/store/fetch.go`

```go
package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"time"
)

// NewHTTPClient is the client every store download uses. Redirects are held to
// the same URL rule as the catalog, so a release host cannot bounce a download
// to plain http elsewhere.
func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return checkFetchURL(req.URL.String())
		},
	}
}

// Download fetches rawURL into dst, which must not exist. It refuses more than
// limit bytes and anything whose sha256 is not sha, and leaves nothing behind
// when it refuses.
func Download(ctx context.Context, c *http.Client, rawURL, sha string, limit int64, dst string) error {
	if err := checkFetchURL(rawURL); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fail(KindUnreachable, err, "%s", rawURL)
	}
	resp, err := c.Do(req)
	if err != nil {
		return fail(KindUnreachable, err, "%s", rawURL)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fail(KindUnreachable, nil, "%s: %s", rawURL, resp.Status)
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fail(KindDisk, err, "")
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, limit+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		err = fail(KindUnreachable, err, "%s", rawURL)
	case n > limit:
		err = fail(KindTooLarge, nil, "%s is larger than %d bytes", rawURL, limit)
	default:
		if got := hex.EncodeToString(h.Sum(nil)); got != sha {
			err = fail(KindChecksum, nil, "want %s, got %s", sha, got)
		}
	}
	if err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run**

Run: `GOMAXPROCS=4 go test -race -count=1 ./internal/plugin/store`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/store/fetch.go internal/plugin/store/fetch_test.go
git commit -m "feat(store): download release assets pinned by sha256"
```

---

### Task 6: Reading a catalog from a git source

**Files:**
- Create: `internal/plugin/store/git.go`, `internal/plugin/store/git_test.go`

**Interfaces:**
- Consumes: `newGitRepo`, `gitRepo.publish`, `gitRepo.url`, `requireGit`.
- Produces: `type Git struct{Bin string; Timeout time.Duration}`, `func (Git) Catalog(ctx context.Context, dir, url string) (body []byte, commit string, err error)`.

- [ ] **Step 1: Write the failing test** `internal/plugin/store/git_test.go`

```go
package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestGitCatalogClonesThenFetches(t *testing.T) {
	t.Parallel()
	repo := newGitRepo(t)
	first := repo.publish([]byte(`{"schema":1,"plugins":[]}`))
	cache := filepath.Join(t.TempDir(), "sources", "test")
	ctx := context.Background()

	body, commit, err := Git{}.Catalog(ctx, cache, repo.url())
	if err != nil || string(body) != `{"schema":1,"plugins":[]}` || commit != first {
		t.Fatalf("first read = %q, %s, %v; want commit %s", body, commit, err, first)
	}

	second := repo.publish([]byte(`{"schema":1,"plugins":[{}]}`))
	body, commit, err = Git{}.Catalog(ctx, cache, repo.url())
	if err != nil || string(body) != `{"schema":1,"plugins":[{}]}` || commit != second {
		t.Fatalf("second read = %q, %s, %v; want commit %s", body, commit, err, second)
	}
}

func TestGitCatalogReclonesWhenTheSourceMoves(t *testing.T) {
	t.Parallel()
	a, b := newGitRepo(t), newGitRepo(t)
	a.publish([]byte(`"a"`))
	want := b.publish([]byte(`"b"`))
	cache := filepath.Join(t.TempDir(), "test")
	if _, _, err := (Git{}).Catalog(context.Background(), cache, a.url()); err != nil {
		t.Fatal(err)
	}
	body, commit, err := Git{}.Catalog(context.Background(), cache, b.url())
	if err != nil || string(body) != `"b"` || commit != want {
		t.Fatalf("read = %q, %s, %v", body, commit, err)
	}
}

func TestGitCatalogErrors(t *testing.T) {
	t.Parallel()
	requireGit(t)
	ctx := context.Background()
	if _, _, err := (Git{Bin: "sysc-no-such-git"}).Catalog(ctx, t.TempDir(), "file:///x"); KindOf(err) != KindGitMissing {
		t.Errorf("missing git: %v", err)
	}
	if _, _, err := (Git{}).Catalog(ctx, filepath.Join(t.TempDir(), "c"), "file:///sysc/no/such/repo"); KindOf(err) != KindUnreachable {
		t.Errorf("missing repo: %v", err)
	}
	empty := newGitRepo(t)
	empty.git("commit", "-q", "--allow-empty", "-m", "nothing")
	if _, _, err := (Git{}).Catalog(ctx, filepath.Join(t.TempDir(), "c"), empty.url()); KindOf(err) != KindUnreachable {
		t.Errorf("repo without catalog.json: %v", err)
	}
}
```

- [ ] **Step 2: Run and confirm failure**

Run: `GOMAXPROCS=4 go test -count=1 -run GitCatalog ./internal/plugin/store`
Expected: FAIL, `undefined: Git`.

- [ ] **Step 3: Implement** `internal/plugin/store/git.go`

```go
package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CatalogFile is the one file a source repository must hold at its root.
const CatalogFile = "catalog.json"

var errOutputTooLarge = errors.New("output too large")

// Git reads catalogs through the git command, as Noctalia does: a blobless,
// checkout-free clone per source, and git show for the one file needed.
type Git struct {
	// Bin is the git executable; empty means git on PATH.
	Bin string
	// Timeout bounds each invocation; zero means 60 seconds.
	Timeout time.Duration
}

// Catalog clones or fetches url into dir and returns catalog.json at the
// remote's default branch together with the commit it was read from.
func (g Git) Catalog(ctx context.Context, dir, url string) ([]byte, string, error) {
	name := g.Bin
	if name == "" {
		name = "git"
	}
	bin, err := exec.LookPath(name)
	if err != nil {
		return nil, "", fail(KindGitMissing, err, "")
	}
	have, err := g.run(ctx, bin, dir, MaxCatalogBytes, "remote", "get-url", "origin")
	if err != nil || strings.TrimSpace(string(have)) != url {
		// No clone yet, a broken one, or a clone of a URL the source no
		// longer names: start again rather than fetch the wrong remote.
		if err := os.RemoveAll(dir); err != nil {
			return nil, "", fail(KindDisk, err, "")
		}
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return nil, "", fail(KindDisk, err, "")
		}
		if _, err := g.run(ctx, bin, "", MaxCatalogBytes, "clone", "--filter=blob:none", "--no-checkout", "--quiet", "--", url, dir); err != nil {
			return nil, "", err
		}
	} else if _, err := g.run(ctx, bin, dir, MaxCatalogBytes, "fetch", "--quiet", "origin"); err != nil {
		return nil, "", err
	}
	commit, err := g.run(ctx, bin, dir, MaxCatalogBytes, "rev-parse", "origin/HEAD")
	if err != nil {
		return nil, "", err
	}
	body, err := g.run(ctx, bin, dir, MaxCatalogBytes, "show", "origin/HEAD:"+CatalogFile)
	if err != nil {
		return nil, "", err
	}
	return body, strings.TrimSpace(string(commit)), nil
}

func (g Git) run(ctx context.Context, bin, dir string, max int, args ...string) ([]byte, error) {
	timeout := g.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
		// Stop git walking up into an enclosing repository when dir is not
		// yet a clone of its own.
		env = append(env, "GIT_CEILING_DIRECTORIES="+filepath.Dir(dir))
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	stdout := &capped{max: max}
	stderr := &capped{max: 512, truncate: true}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return nil, fail(KindGitTimeout, nil, "git %s", args[len(args)-1])
	case stdout.over:
		return nil, fail(KindCatalog, nil, "%s is larger than %d bytes", CatalogFile, max)
	case err != nil:
		return nil, fail(KindUnreachable, err, "%s", strings.TrimSpace(stderr.buf.String()))
	}
	return stdout.buf.Bytes(), nil
}

// capped is a writer with a ceiling. Over it, it either fails the command or,
// for stderr, keeps the head and drops the rest.
type capped struct {
	buf      bytes.Buffer
	max      int
	truncate bool
	over     bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); len(p) > room {
		c.over = true
		if !c.truncate {
			return 0, errOutputTooLarge
		}
		c.buf.Write(p[:max(room, 0)])
		return len(p), nil
	}
	return c.buf.Write(p)
}
```

- [ ] **Step 4: Run**

Run: `GOMAXPROCS=4 go test -race -count=1 ./internal/plugin/store`
Expected: PASS. If `git clone --filter` over `file://` prints "filtering not recognized", that is only a warning on stderr and the clone still succeeds.

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/store/git.go internal/plugin/store/git_test.go
git commit -m "feat(store): read catalogs from git sources"
```

---

### Task 7: Installed records

**Files:**
- Create: `internal/plugin/store/installed.go`, `internal/plugin/store/installed_test.go`

**Interfaces:**
- Consumes: `plugin.LoadManifest`, `writePluginDir`.
- Produces: `const InstalledName = "installed.json"`, `const SourceUnknown = "unknown"`, `type Record struct{Source, Version, CatalogCommit, SHA256 string; Capabilities, Requires []string; InstalledAt time.Time; Previous *Record}`, `type Installed map[string]Record`, `func LoadInstalled(root string) (Installed, error)`, `func (Installed) Save(root string) error`.

- [ ] **Step 1: Write the failing test** `internal/plugin/store/installed_test.go`

```go
package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadInstalledOfAnEmptyRoot(t *testing.T) {
	t.Parallel()
	in, err := LoadInstalled(filepath.Join(t.TempDir(), "absent"))
	if err != nil || len(in) != 0 {
		t.Fatalf("LoadInstalled = %v, %v", in, err)
	}
}

func TestInstalledRoundTrips(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writePluginDir(t, root, fixtureID, "1.4.0", defaultCaps, nil)
	want := Installed{fixtureID: {Source: "sysc", Version: "1.4.0", SHA256: "ab",
		Capabilities: defaultCaps, Previous: &Record{Source: "sysc", Version: "1.3.0"}}}
	if err := want.Save(root); err != nil {
		t.Fatal(err)
	}
	got, err := LoadInstalled(root)
	if err != nil || got[fixtureID].Source != "sysc" || got[fixtureID].Previous == nil {
		t.Fatalf("LoadInstalled = %+v, %v", got, err)
	}
}

func TestLoadInstalledReconcilesWithTheTree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writePluginDir(t, root, fixtureID, "1.4.0", defaultCaps, []string{"gh"})
	writePluginDir(t, root, "org.sysc.misnamed", "1.0.0", defaultCaps, nil)
	if err := os.Rename(filepath.Join(root, "org.sysc.misnamed"), filepath.Join(root, "org.sysc.renamed")); err != nil {
		t.Fatal(err)
	}
	stale := Installed{"org.sysc.gone": {Source: "sysc", Version: "1.0.0"}}
	if err := stale.Save(root); err != nil {
		t.Fatal(err)
	}
	got, err := LoadInstalled(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["org.sysc.gone"]; ok {
		t.Error("a record whose directory is gone survived")
	}
	if _, ok := got["org.sysc.misnamed"]; ok {
		t.Error("a directory whose name is not its manifest id was adopted")
	}
	rec, ok := got[fixtureID]
	if !ok || rec.Source != SourceUnknown || rec.Version != "1.4.0" || !sameSet(rec.Requires, []string{"gh"}) {
		t.Errorf("unrecorded directory = %+v, %v; want an unknown-source record from its manifest", rec, ok)
	}
}

func TestLoadInstalledRebuildsACorruptFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writePluginDir(t, root, fixtureID, "1.4.0", defaultCaps, nil)
	if err := os.WriteFile(filepath.Join(root, InstalledName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadInstalled(root)
	if err != nil || got[fixtureID].Source != SourceUnknown {
		t.Fatalf("LoadInstalled = %+v, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, fixtureID)); err != nil {
		t.Fatal("rebuilding deleted a plugin")
	}
}
```

- [ ] **Step 2: Run and confirm failure**

Run: `GOMAXPROCS=4 go test -count=1 -run Installed ./internal/plugin/store`
Expected: FAIL.

- [ ] **Step 3: Implement** `internal/plugin/store/installed.go`

```go
package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
)

const (
	InstalledName = "installed.json"
	// SourceUnknown marks a plugin whose provenance was lost: a directory with
	// no record, or a record that no longer matches its manifest.
	SourceUnknown = "unknown"
)

// Record is what the store knows about one installed plugin. Capabilities and
// Requires are kept so an update's consent check needs no disk read.
type Record struct {
	Source        string    `json:"source"`
	Version       string    `json:"version"`
	CatalogCommit string    `json:"catalog_commit,omitempty"`
	SHA256        string    `json:"sha256,omitempty"`
	Capabilities  []string  `json:"capabilities,omitempty"`
	Requires      []string  `json:"requires,omitempty"`
	InstalledAt   time.Time `json:"installed_at,omitzero"`
	Previous      *Record   `json:"previous,omitempty"`
}

// Installed maps plugin id to its record.
type Installed map[string]Record

// LoadInstalled reads the records under root and reconciles them with the
// directories actually there. The tree is the truth: a record without a
// directory is dropped, a directory without a matching record is adopted with
// an unknown source, and a corrupt file is rebuilt. Nothing is deleted.
func LoadInstalled(root string) (Installed, error) {
	in := Installed{}
	path := filepath.Join(root, InstalledName)
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, fail(KindDisk, err, "")
	default:
		if jerr := json.Unmarshal(data, &in); jerr != nil {
			slog.Warn("plugin store: installed records unreadable; rebuilding from disk", "path", path, "err", jerr)
			in = Installed{}
		}
	}

	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fail(KindDisk, err, "")
	}
	present := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		m, err := plugin.LoadManifest(filepath.Join(root, e.Name()))
		if err != nil || m.ID != e.Name() {
			continue
		}
		present[m.ID] = true
		if rec, ok := in[m.ID]; !ok || rec.Version != m.Version {
			in[m.ID] = recordFromManifest(m)
		}
	}
	for id := range in {
		if !present[id] {
			delete(in, id)
		}
	}
	return in, nil
}

func recordFromManifest(m plugin.Manifest) Record {
	caps := make([]string, len(m.Capabilities))
	for i, c := range m.Capabilities {
		caps[i] = string(c)
	}
	return Record{Source: SourceUnknown, Version: m.Version, Capabilities: caps, Requires: m.Requires}
}

// Save writes the records atomically.
func (in Installed) Save(root string) error {
	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return fail(KindDisk, err, "")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fail(KindDisk, err, "")
	}
	tmp, err := os.CreateTemp(root, ".installed-*.json")
	if err != nil {
		return fail(KindDisk, err, "")
	}
	_, err = tmp.Write(data)
	if serr := tmp.Sync(); err == nil {
		err = serr
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), filepath.Join(root, InstalledName))
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return fail(KindDisk, err, "")
	}
	return nil
}
```

- [ ] **Step 4: Run**

Run: `GOMAXPROCS=4 go test -race -count=1 ./internal/plugin/store`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/store/installed.go internal/plugin/store/installed_test.go
git commit -m "feat(store): installed records reconciled with the managed tree"
```

---

### Task 8: The installer

**Files:**
- Create: `internal/plugin/store/install.go`, `internal/plugin/store/install_test.go`

**Interfaces:**
- Consumes: `Download`, `Extract`, `DefaultLimits`, `LoadInstalled`, `Installed.Save`, `sameSet`, `plugin.LoadManifest`, `v1.ValidPluginID`, fixtures `release`, `newAssetServer`.
- Produces:
  - `type Installer struct{Root string; Client *http.Client; Arch string; Limits Limits; Replace func(id string, swap func() error) error; rename func(string, string) error}`
  - `type Plan struct{Source, CatalogCommit, ID string; Release Release}`
  - `func (*Installer) Install(ctx context.Context, p Plan) error`, `func (*Installer) Rollback(id string) error`, `func (*Installer) Remove(id string) error`, `func (*Installer) CleanStaging() error`

- [ ] **Step 1: Write the failing test** `internal/plugin/store/install_test.go`

```go
package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func newInstaller(t *testing.T) (*Installer, *assetServer) {
	t.Helper()
	return &Installer{Root: filepath.Join(t.TempDir(), "plugins"), Client: NewHTTPClient(), Arch: runtime.GOARCH}, newAssetServer(t)
}

func installedVersion(t *testing.T, root string) string {
	t.Helper()
	in, err := LoadInstalled(root)
	if err != nil {
		t.Fatal(err)
	}
	return in[fixtureID].Version
}

func plan(r Release) Plan { return Plan{Source: "test", CatalogCommit: "c0ffee", ID: fixtureID, Release: r} }

func TestInstallPlacesThePluginAndRecordsIt(t *testing.T) {
	t.Parallel()
	in, srv := newInstaller(t)
	r := release(t, srv, runtime.GOARCH, "1.4.0", defaultCaps, nil)
	if err := in.Install(context.Background(), plan(r)); err != nil {
		t.Fatalf("Install: %v", err)
	}
	recs, _ := LoadInstalled(in.Root)
	rec := recs[fixtureID]
	if rec.Source != "test" || rec.Version != "1.4.0" || rec.CatalogCommit != "c0ffee" || rec.SHA256 == "" || !sameSet(rec.Capabilities, defaultCaps) {
		t.Errorf("record = %+v", rec)
	}
	if entries, _ := os.ReadDir(filepath.Join(in.Root, ".staging")); len(entries) != 0 {
		t.Errorf("staging left %d entries behind", len(entries))
	}
}

func TestInstallRejectsAManifestThatDisagreesWithTheCatalog(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*Release){
		"version":      func(r *Release) { r.Version = "9.9.9" },
		"capabilities": func(r *Release) { r.Capabilities = []string{"panels"} },
		"requires":     func(r *Release) { r.Requires.Commands = []string{"gh"} },
		"protocol":     func(r *Release) { r.Protocol = v1Version(1, 2) },
	}
	for name, mutate := range cases {
		in, srv := newInstaller(t)
		r := release(t, srv, runtime.GOARCH, "1.4.0", defaultCaps, nil)
		mutate(&r)
		err := in.Install(context.Background(), plan(r))
		if KindOf(err) != KindManifest {
			t.Errorf("%s: err = %v, want %s", name, err, KindManifest)
		}
		if _, err := os.Stat(filepath.Join(in.Root, fixtureID)); err == nil {
			t.Errorf("%s: a mismatched plugin was installed", name)
		}
	}
}

func TestInstallRejectsAChecksumMismatch(t *testing.T) {
	t.Parallel()
	in, srv := newInstaller(t)
	r := release(t, srv, runtime.GOARCH, "1.4.0", defaultCaps, nil)
	a := r.Assets["linux-"+runtime.GOARCH]
	a.SHA256 = sum([]byte("tampered"))
	r.Assets["linux-"+runtime.GOARCH] = a
	if err := in.Install(context.Background(), plan(r)); KindOf(err) != KindChecksum {
		t.Fatalf("err = %v", err)
	}
}

func TestInstallWithoutAnAssetForThisMachine(t *testing.T) {
	t.Parallel()
	in, srv := newInstaller(t)
	r := release(t, srv, "sysc-other-arch", "1.4.0", defaultCaps, nil)
	if err := in.Install(context.Background(), plan(r)); KindOf(err) != KindNoAsset {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateKeepsThePreviousVersionAndRollbackRestoresIt(t *testing.T) {
	t.Parallel()
	in, srv := newInstaller(t)
	ctx := context.Background()
	if err := in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.4.0", defaultCaps, nil))); err != nil {
		t.Fatal(err)
	}
	if err := in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.5.0", defaultCaps, nil))); err != nil {
		t.Fatal(err)
	}
	if got := installedVersion(t, in.Root); got != "1.5.0" {
		t.Fatalf("after update: %s", got)
	}
	if err := in.Rollback(fixtureID); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if got := installedVersion(t, in.Root); got != "1.4.0" {
		t.Fatalf("after rollback: %s", got)
	}
	if err := in.Rollback(fixtureID); err != nil {
		t.Fatalf("second Rollback: %v", err)
	}
	if got := installedVersion(t, in.Root); got != "1.5.0" {
		t.Fatalf("rolling back twice should return to 1.5.0, got %s", got)
	}
}

func TestAFailedSwapKeepsThePreviousVersion(t *testing.T) {
	t.Parallel()
	for _, failAt := range []int{1, 2} {
		in, srv := newInstaller(t)
		ctx := context.Background()
		if err := in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.4.0", defaultCaps, nil))); err != nil {
			t.Fatal(err)
		}
		calls := 0
		in.rename = func(a, b string) error {
			if calls++; calls == failAt {
				return errors.New("injected")
			}
			return os.Rename(a, b)
		}
		err := in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.5.0", defaultCaps, nil)))
		if KindOf(err) != KindDisk {
			t.Errorf("rename %d failing: err = %v", failAt, err)
		}
		in.rename = nil
		if got := installedVersion(t, in.Root); got != "1.4.0" {
			t.Errorf("rename %d failing: installed %s, want 1.4.0 kept", failAt, got)
		}
	}
}

func TestRemoveDeletesEveryVersion(t *testing.T) {
	t.Parallel()
	in, srv := newInstaller(t)
	ctx := context.Background()
	_ = in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.4.0", defaultCaps, nil)))
	_ = in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.5.0", defaultCaps, nil)))
	if err := in.Remove(fixtureID); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(in.Root, fixtureID), filepath.Join(in.Root, ".prev", fixtureID)} {
		if _, err := os.Stat(dir); err == nil {
			t.Errorf("%s survived Remove", dir)
		}
	}
	if err := in.Rollback(fixtureID); KindOf(err) != KindNoPrevious {
		t.Errorf("Rollback after Remove: %v", err)
	}
}

func TestEveryTreeChangeGoesThroughReplace(t *testing.T) {
	t.Parallel()
	in, srv := newInstaller(t)
	var ids []string
	in.Replace = func(id string, swap func() error) error {
		ids = append(ids, id)
		return swap()
	}
	ctx := context.Background()
	_ = in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.4.0", defaultCaps, nil)))
	_ = in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.5.0", defaultCaps, nil)))
	_ = in.Rollback(fixtureID)
	_ = in.Remove(fixtureID)
	if len(ids) != 4 {
		t.Fatalf("Replace saw %v, want four calls", ids)
	}
}

func TestInstallerRefusesAnInvalidID(t *testing.T) {
	t.Parallel()
	in, _ := newInstaller(t)
	for _, id := range []string{"../escape", "org/sysc", ""} {
		if err := in.Remove(id); KindOf(err) != KindNotListed {
			t.Errorf("Remove(%q) = %v", id, err)
		}
		if err := in.Rollback(id); KindOf(err) != KindNotListed {
			t.Errorf("Rollback(%q) = %v", id, err)
		}
	}
}
```

- [ ] **Step 2: Run and confirm failure**

Run: `GOMAXPROCS=4 go test -count=1 -run 'Install|Update|Swap|Remove|Replace|InvalidID' ./internal/plugin/store`
Expected: FAIL, `undefined: Installer`.

- [ ] **Step 3: Implement** `internal/plugin/store/install.go`

```go
package store

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// Installer changes the managed plugin tree. It is used from the store's one
// worker goroutine only, so it holds no lock of its own.
type Installer struct {
	// Root is the managed plugin root.
	Root   string
	Client *http.Client
	// Arch is runtime.GOARCH; assets are keyed linux-<Arch>.
	Arch   string
	Limits Limits
	// Replace stops plugin id, runs swap, and rescans. Every change to the
	// tree goes through it, so the host never runs a directory mid-swap. Nil
	// runs swap directly.
	Replace func(id string, swap func() error) error

	// rename is os.Rename unless a test injects a failure.
	rename func(oldpath, newpath string) error
}

// Plan is one resolved release to install.
type Plan struct {
	Source        string
	CatalogCommit string
	ID            string
	Release       Release
}

// Install downloads, verifies and installs p, keeping any installed version as
// the one-step rollback. Everything that can fail is done in staging first; a
// failure before the final rename leaves the previous version in place.
func (in *Installer) Install(ctx context.Context, p Plan) error {
	if !v1.ValidPluginID(p.ID) {
		return fail(KindNotListed, nil, "%q is not a plugin id", p.ID)
	}
	asset, ok := p.Release.Assets["linux-"+in.Arch]
	if !ok {
		return fail(KindNoAsset, nil, "%s %s has no linux-%s asset", p.ID, p.Release.Version, in.Arch)
	}
	stage, err := in.stage("install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	archive := filepath.Join(stage, "asset.tar.gz")
	if err := Download(ctx, in.Client, asset.URL, asset.SHA256, asset.Size, archive); err != nil {
		return err
	}
	tree := filepath.Join(stage, "tree")
	if err := os.Mkdir(tree, 0o755); err != nil {
		return fail(KindDisk, err, "")
	}
	f, err := os.Open(archive)
	if err != nil {
		return fail(KindDisk, err, "")
	}
	lim := in.Limits
	if lim.MaxEntries == 0 {
		lim = DefaultLimits
	}
	err = Extract(f, tree, p.ID, lim)
	_ = f.Close()
	if err != nil {
		return err
	}
	staged := filepath.Join(tree, p.ID)
	m, err := plugin.LoadManifest(staged)
	if err != nil {
		return fail(KindManifest, err, "")
	}
	if err := matchManifest(m, p); err != nil {
		return err
	}
	rec := recordFromManifest(m)
	rec.Source, rec.CatalogCommit, rec.SHA256, rec.InstalledAt = p.Source, p.CatalogCommit, asset.SHA256, time.Now().UTC()
	return in.replace(p.ID, func() error { return in.swapIn(p.ID, staged, rec) })
}

// matchManifest proves the archive is the release the catalog described, so a
// catalog cannot under-declare what the plugin will ask for.
func matchManifest(m plugin.Manifest, p Plan) error {
	caps := make([]string, len(m.Capabilities))
	for i, c := range m.Capabilities {
		caps[i] = string(c)
	}
	switch {
	case m.ID != p.ID:
		return fail(KindManifest, nil, "id is %q; the catalog says %q", m.ID, p.ID)
	case m.Version != p.Release.Version:
		return fail(KindManifest, nil, "version is %s; the catalog says %s", m.Version, p.Release.Version)
	case m.Protocol != p.Release.Protocol:
		return fail(KindManifest, nil, "protocol is %d.%d; the catalog says %d.%d",
			m.Protocol.Major, m.Protocol.Minor, p.Release.Protocol.Major, p.Release.Protocol.Minor)
	case !sameSet(caps, p.Release.Capabilities):
		return fail(KindManifest, nil, "capabilities are %v; the catalog says %v", caps, p.Release.Capabilities)
	case !sameSet(m.Requires, p.Release.Requires.Commands):
		return fail(KindManifest, nil, "required commands are %v; the catalog says %v", m.Requires, p.Release.Requires.Commands)
	}
	return nil
}

func (in *Installer) swapIn(id, staged string, rec Record) error {
	records, err := LoadInstalled(in.Root)
	if err != nil {
		return err
	}
	active, prev := in.paths(id)
	had := exists(active)
	if had {
		if err := os.MkdirAll(filepath.Dir(prev), 0o755); err != nil {
			return fail(KindDisk, err, "")
		}
		if err := os.RemoveAll(prev); err != nil {
			return fail(KindDisk, err, "")
		}
		if err := in.doRename(active, prev); err != nil {
			return fail(KindDisk, err, "")
		}
	}
	if err := in.doRename(staged, active); err != nil {
		if had {
			if rerr := in.doRename(prev, active); rerr != nil {
				return fail(KindDisk, errors.Join(err, rerr), "restoring the previous version failed")
			}
		}
		return fail(KindDisk, err, "")
	}
	if old, ok := records[id]; ok && had {
		old.Previous = nil
		rec.Previous = &old
	}
	records[id] = rec
	return records.Save(in.Root)
}

// Rollback swaps the installed version with the kept previous one.
func (in *Installer) Rollback(id string) error {
	if !v1.ValidPluginID(id) {
		return fail(KindNotListed, nil, "%q is not a plugin id", id)
	}
	active, prev := in.paths(id)
	if !exists(prev) || !exists(active) {
		return fail(KindNoPrevious, nil, "%s", id)
	}
	return in.replace(id, func() error {
		records, err := LoadInstalled(in.Root)
		if err != nil {
			return err
		}
		hold, err := in.stage("rollback-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(hold)
		held := filepath.Join(hold, id)
		if err := in.doRename(active, held); err != nil {
			return fail(KindDisk, err, "")
		}
		if err := in.doRename(prev, active); err != nil {
			if rerr := in.doRename(held, active); rerr != nil {
				return fail(KindDisk, errors.Join(err, rerr), "restoring the installed version failed")
			}
			return fail(KindDisk, err, "")
		}
		keepErr := in.doRename(held, prev)

		cur := records[id]
		next := Record{Source: SourceUnknown}
		if cur.Previous != nil {
			next = *cur.Previous
		}
		cur.Previous = nil
		if keepErr == nil {
			next.Previous = &cur
		}
		records[id] = next
		if err := records.Save(in.Root); err != nil {
			return err
		}
		if keepErr != nil {
			return fail(KindDisk, keepErr, "rolled back, but the replaced version could not be kept")
		}
		return nil
	})
}

// Remove deletes every version of id. Its configuration is not the store's to
// touch: an absent plugin keeps its settings and placements.
func (in *Installer) Remove(id string) error {
	if !v1.ValidPluginID(id) {
		return fail(KindNotListed, nil, "%q is not a plugin id", id)
	}
	return in.replace(id, func() error {
		records, err := LoadInstalled(in.Root)
		if err != nil {
			return err
		}
		active, prev := in.paths(id)
		for _, dir := range []string{active, prev} {
			if err := os.RemoveAll(dir); err != nil {
				return fail(KindDisk, err, "")
			}
		}
		delete(records, id)
		return records.Save(in.Root)
	})
}

// CleanStaging removes work an interrupted run left behind.
func (in *Installer) CleanStaging() error {
	return os.RemoveAll(filepath.Join(in.Root, ".staging"))
}

func (in *Installer) stage(prefix string) (string, error) {
	staging := filepath.Join(in.Root, ".staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return "", fail(KindDisk, err, "")
	}
	dir, err := os.MkdirTemp(staging, prefix)
	if err != nil {
		return "", fail(KindDisk, err, "")
	}
	return dir, nil
}

func (in *Installer) paths(id string) (active, prev string) {
	return filepath.Join(in.Root, id), filepath.Join(in.Root, ".prev", id)
}

func (in *Installer) replace(id string, swap func() error) error {
	if in.Replace == nil {
		return swap()
	}
	return in.Replace(id, swap)
}

func (in *Installer) doRename(oldpath, newpath string) error {
	if in.rename != nil {
		return in.rename(oldpath, newpath)
	}
	return os.Rename(oldpath, newpath)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
```

- [ ] **Step 4: Run**

Run: `GOMAXPROCS=4 go test -race -count=1 ./internal/plugin/store && GOMAXPROCS=4 go vet ./internal/plugin/store`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/store/install.go internal/plugin/store/install_test.go
git commit -m "feat(store): install, update, roll back and remove managed plugins"
```

---

### Task 9: The store worker and listings

**Files:**
- Create: `internal/plugin/store/store.go`, `internal/plugin/store/paths.go`, `internal/plugin/store/store_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces:
  - `type Source struct{Name, URL string}`
  - `type Options struct{CacheDir string; Installer *Installer; Git Git; Sources func() []Source; Local func() map[string]string}`
  - `type Status string` with `StatusAvailable`, `StatusInstalled`, `StatusUpdateAvailable`, `StatusHeldBack`, `StatusIncompatible`, `StatusShadowed`, `StatusLocalOnly`
  - `type Listing struct{Source, CatalogCommit string; Entry Entry; Resolution Resolution; Status Status; Installed *Record; LocalDir string; Err error}`
  - `type SourceState struct{Name, URL, Commit string; FetchedAt time.Time; Plugins int; Rejected []RowError; Err error}`
  - `type State struct{Sources []SourceState; Listings []Listing; Busy string}`
  - `func New(Options) *Store`, `(*Store).Run(ctx)`, `(*Store).State() State`, `(*Store).Refresh() (<-chan error, error)`, `(*Store).Install(source, id string) (<-chan error, error)`, `(*Store).Update(id string, confirmed bool) (<-chan error, error)`, `(*Store).Rollback(id string) (<-chan error, error)`, `(*Store).Remove(id string) (<-chan error, error)`
  - `func buildListings(sources []SourceState, catalogs map[string]Catalog, installed Installed, local map[string]string, errs map[string]error, arch string) []Listing`
  - `func CacheRoot() string`

- [ ] **Step 1: Write the failing test** `internal/plugin/store/store_test.go`

```go
package store

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestListingStatus(t *testing.T) {
	t.Parallel()
	asset := map[string]Asset{"linux-amd64": {URL: "https://e.com/a", SHA256: sum(nil), Size: 1}}
	tip := entryFor(Release{Version: "1.5.0", Protocol: v1Version(1, 0), Assets: asset})
	tooNew := entryFor(Release{Version: "2.0.0", Protocol: v1Version(2, 0), Assets: asset})
	cases := []struct {
		name      string
		entry     Entry
		installed *Record
		local     string
		want      Status
	}{
		{"available", tip, nil, "", StatusAvailable},
		{"installed current", tip, &Record{Source: "s", Version: "1.5.0"}, "", StatusInstalled},
		{"installed older", tip, &Record{Source: "s", Version: "1.4.0"}, "", StatusUpdateAvailable},
		{"installed older from another source", tip, &Record{Source: "other", Version: "1.4.0"}, "", StatusInstalled},
		{"incompatible", tooNew, nil, "", StatusIncompatible},
		{"local only", tip, nil, "/home/u/p", StatusLocalOnly},
		{"shadowed", tip, &Record{Source: "s", Version: "1.4.0"}, "/home/u/p", StatusShadowed},
	}
	for _, c := range cases {
		installed := Installed{}
		if c.installed != nil {
			installed[fixtureID] = *c.installed
		}
		local := map[string]string{}
		if c.local != "" {
			local[fixtureID] = c.local
		}
		got := buildListings([]SourceState{{Name: "s"}}, map[string]Catalog{"s": {Entries: []Entry{c.entry}}}, installed, local, nil, "amd64")
		if len(got) != 1 || got[0].Status != c.want {
			t.Errorf("%s: %+v, want %s", c.name, got, c.want)
		}
	}
}

// storeFixture is a source repo, an asset server and a store over them.
type storeFixture struct {
	t    *testing.T
	repo *gitRepo
	srv  *assetServer
	st   *Store
	root string
}

func newStoreFixture(t *testing.T, local map[string]string) *storeFixture {
	t.Helper()
	f := &storeFixture{t: t, repo: newGitRepo(t), srv: newAssetServer(t)}
	f.root = filepath.Join(t.TempDir(), "plugins")
	f.st = New(Options{
		CacheDir:  filepath.Join(t.TempDir(), "sources"),
		Installer: &Installer{Root: f.root, Client: NewHTTPClient(), Arch: runtime.GOARCH},
		Sources:   func() []Source { return []Source{{Name: "test", URL: f.repo.url()}} },
		Local:     func() map[string]string { return local },
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go f.st.Run(ctx)
	return f
}

func (f *storeFixture) publish(rels ...Release) {
	f.repo.publish(catalogJSON(f.t, entryFor(rels...)))
	f.await(f.st.Refresh())
}

func (f *storeFixture) await(done <-chan error, err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
	if err := <-done; err != nil {
		f.t.Fatal(err)
	}
}

func (f *storeFixture) listing() Listing {
	f.t.Helper()
	ls := f.st.State().Listings
	if len(ls) != 1 {
		f.t.Fatalf("listings = %+v", ls)
	}
	return ls[0]
}

func TestStoreInstallsUpdatesRollsBackAndRemoves(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	arch := runtime.GOARCH
	v1rel := release(t, f.srv, arch, "1.4.0", defaultCaps, nil)
	f.publish(v1rel)
	if got := f.listing().Status; got != StatusAvailable {
		t.Fatalf("before install: %s", got)
	}

	f.await(f.st.Install("test", fixtureID))
	if l := f.listing(); l.Status != StatusInstalled || l.Installed.Version != "1.4.0" {
		t.Fatalf("after install: %+v", l)
	}

	f.publish(release(t, f.srv, arch, "1.5.0", defaultCaps, nil), v1rel)
	if got := f.listing().Status; got != StatusUpdateAvailable {
		t.Fatalf("after publishing 1.5.0: %s", got)
	}
	f.await(f.st.Update(fixtureID, false))
	if l := f.listing(); l.Installed.Version != "1.5.0" {
		t.Fatalf("after update: %+v", l.Installed)
	}

	f.await(f.st.Rollback(fixtureID))
	if l := f.listing(); l.Installed.Version != "1.4.0" {
		t.Fatalf("after rollback: %+v", l.Installed)
	}

	f.await(f.st.Remove(fixtureID))
	if l := f.listing(); l.Status != StatusAvailable || l.Installed != nil {
		t.Fatalf("after remove: %+v", l)
	}
	if _, err := os.Stat(filepath.Join(f.root, fixtureID)); err == nil {
		t.Fatal("the plugin directory survived Remove")
	}
}

func TestUpdateAsksAgainWhenCapabilitiesChange(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	arch := runtime.GOARCH
	f.publish(release(t, f.srv, arch, "1.4.0", []string{"panels", "settings"}, nil))
	f.await(f.st.Install("test", fixtureID))
	f.publish(release(t, f.srv, arch, "1.5.0", defaultCaps, nil))

	if _, err := f.st.Update(fixtureID, false); KindOf(err) != KindConsent {
		t.Fatalf("unconfirmed update: %v, want %s", err, KindConsent)
	}
	f.await(f.st.Update(fixtureID, true))
	if l := f.listing(); l.Installed.Version != "1.5.0" {
		t.Fatalf("after confirmed update: %+v", l.Installed)
	}
}

func TestRefreshKeepsTheLastGoodCatalogWhenAFetchFails(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	f.publish(release(t, f.srv, runtime.GOARCH, "1.4.0", defaultCaps, nil))
	if err := os.RemoveAll(f.repo.dir); err != nil {
		t.Fatal(err)
	}
	done, err := f.st.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("refresh of a deleted source reported success")
	}
	st := f.st.State()
	if st.Sources[0].Err == nil || st.Sources[0].FetchedAt.IsZero() || len(st.Listings) != 1 {
		t.Fatalf("state = %+v; want the source stale and its listing kept", st)
	}
}

func TestALocalCopyShadowsTheManagedListing(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, map[string]string{fixtureID: "/home/u/sysc-plugins/plugins/timer"})
	f.publish(release(t, f.srv, runtime.GOARCH, "1.4.0", defaultCaps, nil))
	if got := f.listing().Status; got != StatusLocalOnly {
		t.Fatalf("before install: %s", got)
	}
	f.await(f.st.Install("test", fixtureID))
	if got := f.listing().Status; got != StatusShadowed {
		t.Fatalf("after install: %s", got)
	}
}

func TestInstallOfSomethingUnlisted(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	f.publish(release(t, f.srv, runtime.GOARCH, "1.4.0", defaultCaps, nil))
	if _, err := f.st.Install("test", "org.sysc.nope"); KindOf(err) != KindNotListed {
		t.Fatalf("err = %v", err)
	}
	if _, err := f.st.Install("nosuch", fixtureID); KindOf(err) != KindNotListed {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run and confirm failure**

Run: `GOMAXPROCS=4 go test -count=1 -run 'Listing|Store|Update|Refresh|Shadow|Unlisted' ./internal/plugin/store`
Expected: FAIL, `undefined: New`.

- [ ] **Step 3: Implement** `internal/plugin/store/paths.go`

```go
package store

import (
	"os"
	"path/filepath"
)

// CacheRoot holds one git clone per source. It is disposable: deleting it
// costs one re-clone.
func CacheRoot() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if !filepath.IsAbs(base) {
		base = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	return filepath.Join(base, "sysc-shell", "sources")
}
```

`internal/plugin/store/store.go`

```go
package store

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// Source is one enabled catalog source.
type Source struct {
	Name string
	URL  string
}

// Options wires the store to the shell.
type Options struct {
	// CacheDir holds one git clone per source name.
	CacheDir  string
	Installer *Installer
	Git       Git
	// Sources returns the enabled sources. It is read at every refresh, so a
	// configuration change is picked up without restarting the store.
	Sources func() []Source
	// Local returns plugin id to directory for every usable user-root copy.
	Local func() map[string]string
}

type Status string

const (
	StatusAvailable       Status = "available"
	StatusInstalled       Status = "installed"
	StatusUpdateAvailable Status = "update available"
	StatusHeldBack        Status = "held back"
	StatusIncompatible    Status = "incompatible"
	StatusShadowed        Status = "shadowed"
	StatusLocalOnly       Status = "local only"
)

// Listing is one catalog row resolved against this machine. It carries every
// decoded field; the manager reads listings and never raw catalog JSON.
type Listing struct {
	Source        string
	CatalogCommit string
	Entry         Entry
	Resolution    Resolution
	Status        Status
	// Installed is the managed copy's record, when there is one.
	Installed *Record
	// LocalDir is a user-root copy with the same id, when there is one.
	LocalDir string
	// Err is the last failed operation on this plugin, until the next one.
	Err error
}

// SourceState is one source's last fetch. Err with a zero FetchedAt means the
// source was never read; with a non-zero one, its listings are stale.
type SourceState struct {
	Name      string
	URL       string
	Commit    string
	FetchedAt time.Time
	Plugins   int
	Rejected  []RowError
	Err       error
}

// State is an immutable snapshot for the manager and IPC.
type State struct {
	Sources  []SourceState
	Listings []Listing
	// Busy names the operation in flight, "" when idle.
	Busy string
}

const queueDepth = 16

type op struct {
	name string
	id   string
	run  func(ctx context.Context) error
	done chan error
}

// Store runs every git, network and disk operation on one goroutine. Callers
// enqueue and read snapshots; nothing they call blocks on I/O.
type Store struct {
	opts Options
	ops  chan op

	mu       sync.Mutex
	sources  []SourceState
	catalogs map[string]Catalog
	errs     map[string]error
	state    State
}

func New(opts Options) *Store {
	return &Store{opts: opts, ops: make(chan op, queueDepth), catalogs: map[string]Catalog{}, errs: map[string]error{}}
}

// Run is the worker. It returns when ctx is done.
func (s *Store) Run(ctx context.Context) {
	if err := s.opts.Installer.CleanStaging(); err != nil {
		slog.Warn("plugin store: cannot clear staging", "err", err)
	}
	s.rebuild()
	for {
		select {
		case <-ctx.Done():
			return
		case o := <-s.ops:
			s.setBusy(o.name)
			err := o.run(ctx)
			s.mu.Lock()
			if o.id != "" {
				s.errs[o.id] = err
			}
			s.mu.Unlock()
			s.rebuild()
			s.setBusy("")
			o.done <- err
		}
	}
}

// State returns the latest snapshot. Its slices are not shared with the worker.
func (s *Store) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return State{
		Sources:  slices.Clone(s.state.Sources),
		Listings: slices.Clone(s.state.Listings),
		Busy:     s.state.Busy,
	}
}

func (s *Store) Refresh() (<-chan error, error) {
	return s.enqueue(op{name: "refresh", run: s.refresh})
}

// Install queues installing id from source. The command itself is the consent:
// the manager shows its consent sheet before calling this.
func (s *Store) Install(source, id string) (<-chan error, error) {
	if _, err := s.find(source, id); err != nil {
		return nil, err
	}
	return s.enqueue(op{name: "install " + id, id: id, run: func(ctx context.Context) error {
		l, err := s.find(source, id)
		if err != nil {
			return err
		}
		return s.install(ctx, l)
	}})
}

// Update queues updating id from the source it was installed from. An update
// that changes capabilities or required commands is refused until confirmed.
func (s *Store) Update(id string, confirmed bool) (<-chan error, error) {
	l, err := s.updatable(id)
	if err != nil {
		return nil, err
	}
	rel := l.Resolution.Release
	if !confirmed && (!sameSet(l.Installed.Capabilities, rel.Capabilities) || !sameSet(l.Installed.Requires, rel.Requires.Commands)) {
		return nil, fail(KindConsent, nil, "%s %s asks for capabilities %v and commands %v; installed %s has %v and %v",
			id, rel.Version, rel.Capabilities, rel.Requires.Commands, l.Installed.Version, l.Installed.Capabilities, l.Installed.Requires)
	}
	return s.enqueue(op{name: "update " + id, id: id, run: func(ctx context.Context) error {
		l, err := s.updatable(id)
		if err != nil {
			return err
		}
		return s.install(ctx, l)
	}})
}

func (s *Store) Rollback(id string) (<-chan error, error) {
	return s.enqueue(op{name: "rollback " + id, id: id, run: func(context.Context) error {
		return s.opts.Installer.Rollback(id)
	}})
}

func (s *Store) Remove(id string) (<-chan error, error) {
	return s.enqueue(op{name: "remove " + id, id: id, run: func(context.Context) error {
		return s.opts.Installer.Remove(id)
	}})
}

func (s *Store) enqueue(o op) (<-chan error, error) {
	o.done = make(chan error, 1)
	select {
	case s.ops <- o:
		return o.done, nil
	default:
		return nil, fail(KindBusy, nil, "%d operations already queued", queueDepth)
	}
}

func (s *Store) find(source, id string) (Listing, error) {
	for _, l := range s.State().Listings {
		if l.Source == source && l.Entry.ID == id {
			return l, nil
		}
	}
	return Listing{}, fail(KindNotListed, nil, "%s in source %q", id, source)
}

func (s *Store) updatable(id string) (Listing, error) {
	for _, l := range s.State().Listings {
		if l.Entry.ID == id && l.Installed != nil && l.Installed.Source == l.Source {
			if l.Status != StatusUpdateAvailable && l.Status != StatusShadowed {
				return Listing{}, fail(KindNotListed, nil, "no update for %s", id)
			}
			if l.Resolution.Release == nil || !Newer(l.Resolution.Release.Version, l.Installed.Version) {
				return Listing{}, fail(KindNotListed, nil, "no update for %s", id)
			}
			return l, nil
		}
	}
	return Listing{}, fail(KindNotListed, nil, "%s is not installed from an enabled source", id)
}

func (s *Store) install(ctx context.Context, l Listing) error {
	if l.Resolution.Release == nil {
		return fail(KindNoAsset, nil, "%s needs protocol %d.%d", l.Entry.ID, l.Resolution.Needs.Major, l.Resolution.Needs.Minor)
	}
	return s.opts.Installer.Install(ctx, Plan{
		Source: l.Source, CatalogCommit: l.CatalogCommit, ID: l.Entry.ID, Release: *l.Resolution.Release,
	})
}

// refresh reads every enabled source. A source that fails keeps its last good
// catalog, marked stale, so a network outage never empties the manager.
func (s *Store) refresh(ctx context.Context) error {
	s.mu.Lock()
	prev := map[string]SourceState{}
	for _, st := range s.sources {
		prev[st.Name] = st
	}
	prevCats := s.catalogs
	s.mu.Unlock()

	var next []SourceState
	cats := map[string]Catalog{}
	var errs []error
	for _, src := range s.opts.Sources() {
		st := SourceState{Name: src.Name, URL: src.URL}
		if old, ok := prev[src.Name]; ok && old.URL == src.URL {
			st = old
			cats[src.Name] = prevCats[src.Name]
		}
		body, commit, err := s.opts.Git.Catalog(ctx, filepath.Join(s.opts.CacheDir, src.Name), src.URL)
		var cat Catalog
		if err == nil {
			cat, err = Decode(body)
		}
		if err != nil {
			st.Err = err
			errs = append(errs, err)
		} else {
			st.Err = nil
			st.Commit, st.FetchedAt, st.Plugins, st.Rejected = commit, time.Now(), len(cat.Entries), cat.Rejected
			cats[src.Name] = cat
		}
		next = append(next, st)
	}
	s.mu.Lock()
	s.sources, s.catalogs = next, cats
	s.mu.Unlock()
	return errors.Join(errs...)
}

// rebuild recomputes listings from the catalogs, the managed tree and the
// local copies. It runs on the worker only.
func (s *Store) rebuild() {
	installed, err := LoadInstalled(s.opts.Installer.Root)
	if err != nil {
		slog.Warn("plugin store: cannot read installed plugins", "err", err)
		installed = Installed{}
	}
	var local map[string]string
	if s.opts.Local != nil {
		local = s.opts.Local()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Sources = slices.Clone(s.sources)
	s.state.Listings = buildListings(s.sources, s.catalogs, installed, local, s.errs, s.opts.Installer.Arch)
}

func (s *Store) setBusy(what string) {
	s.mu.Lock()
	s.state.Busy = what
	s.mu.Unlock()
}

// buildListings resolves every catalog row, in source order then catalog order.
func buildListings(sources []SourceState, catalogs map[string]Catalog, installed Installed,
	local map[string]string, errs map[string]error, arch string) []Listing {

	var out []Listing
	for _, src := range sources {
		for _, e := range catalogs[src.Name].Entries {
			l := Listing{Source: src.Name, CatalogCommit: src.Commit, Entry: e, Resolution: Resolve(e, arch),
				LocalDir: local[e.ID], Err: errs[e.ID]}
			if rec, ok := installed[e.ID]; ok {
				l.Installed = &rec
			}
			l.Status = statusOf(l)
			out = append(out, l)
		}
	}
	return out
}

func statusOf(l Listing) Status {
	switch {
	case l.LocalDir != "" && l.Installed != nil:
		return StatusShadowed
	case l.LocalDir != "":
		return StatusLocalOnly
	case l.Installed != nil:
		if l.Installed.Source == l.Source && l.Resolution.Release != nil && Newer(l.Resolution.Release.Version, l.Installed.Version) {
			return StatusUpdateAvailable
		}
		return StatusInstalled
	case l.Resolution.Compat == Incompatible:
		return StatusIncompatible
	case l.Resolution.Compat == HeldBack:
		return StatusHeldBack
	}
	return StatusAvailable
}
```

- [ ] **Step 4: Run**

Run: `GOMAXPROCS=4 go test -race -count=1 ./internal/plugin/store && GOMAXPROCS=4 go vet ./internal/plugin/store`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/store/store.go internal/plugin/store/paths.go internal/plugin/store/store_test.go
git commit -m "feat(store): one worker, snapshots, listings and update consent"
```

---

### Task 10: Plugin sources in configuration

**Files:**
- Modify: `internal/config/config.go` (the `Plugins` struct at `:68` and `clone` at `:81`), `internal/config/load.go` (`wirePlugins` at `:185`, `applyPlugins` at `:361`), `internal/config/write.go` (`pluginsDiff` at `:322`)
- Test: `internal/config/plugins_test.go`

**Interfaces:**
- Produces: `type config.PluginSource struct{Name, URL string; Enabled bool}`, `var config.BuiltinPluginSource`, the field `Plugins.Sources []PluginSource`, `func (Plugins) EffectiveSources() []PluginSource`.

- [ ] **Step 1: Write the failing tests** (append to `internal/config/plugins_test.go`)

```go
func TestPluginSourcesRoundTrip(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"plugins": {"sources": [
		{"name": "sysc", "enabled": false},
		{"name": "mine", "url": "https://github.com/me/my-plugins"},
		{"name": "dev", "url": "file:///home/me/catalog", "enabled": false}
	]}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got := cfg.Plugins.EffectiveSources()
	want := []PluginSource{
		{Name: "sysc", URL: BuiltinPluginSource.URL, Enabled: false},
		{Name: "mine", URL: "https://github.com/me/my-plugins", Enabled: true},
		{Name: "dev", URL: "file:///home/me/catalog", Enabled: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EffectiveSources = %+v", got)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil || !reflect.DeepEqual(again.Plugins.EffectiveSources(), want) {
		t.Fatalf("after write: %+v, %v", again.Plugins.EffectiveSources(), err)
	}
}

func TestDefaultConfigHasTheBuiltinSourceEnabled(t *testing.T) {
	t.Parallel()
	got := Default().Plugins.EffectiveSources()
	if len(got) != 1 || got[0] != BuiltinPluginSource || !got[0].Enabled {
		t.Fatalf("EffectiveSources = %+v", got)
	}
}

func TestParseRejectsBadPluginSources(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"bad name":        `[{"name": "My Source", "url": "https://e.com/r"}]`,
		"duplicate":       `[{"name": "a", "url": "https://e.com/r"}, {"name": "a", "url": "https://e.com/s"}]`,
		"http":            `[{"name": "a", "url": "http://e.com/r"}]`,
		"relative file":   `[{"name": "a", "url": "file://relative/path"}]`,
		"missing url":     `[{"name": "a"}]`,
		"repointed sysc":  `[{"name": "sysc", "url": "https://e.com/fork"}]`,
		"ssh unsupported": `[{"name": "a", "url": "ssh://git@e.com/r"}]`,
	}
	for name, sources := range cases {
		if _, err := Parse([]byte(`{"plugins": {"sources": ` + sources + `}}`)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
```

- [ ] **Step 2: Run and confirm failure**

Run: `GOMAXPROCS=4 go test -count=1 -run 'PluginSources|BuiltinSource' ./internal/config`
Expected: FAIL, `undefined: EffectiveSources`.

- [ ] **Step 3: Implement**

In `internal/config/config.go`, beside `Plugins`:

```go
// PluginSource is one git repository the plugin store reads a catalog from.
type PluginSource struct {
	Name    string
	URL     string
	Enabled bool
}

// BuiltinPluginSource is the first-party catalog. It is always present: a
// configuration may disable it but not remove or repoint it.
var BuiltinPluginSource = PluginSource{Name: "sysc", URL: "https://github.com/Nomadcxx/sysc-plugins", Enabled: true}
```

Add to `Plugins`:

```go
	// Sources lists the sources the user added, plus an entry named for the
	// built-in source when the user disabled it. EffectiveSources merges in
	// the built-in one.
	Sources []PluginSource
```

```go
// EffectiveSources is every source in order, the built-in one first.
func (p Plugins) EffectiveSources() []PluginSource {
	out := []PluginSource{BuiltinPluginSource}
	for _, s := range p.Sources {
		if s.Name == BuiltinPluginSource.Name {
			out[0].Enabled = s.Enabled
			continue
		}
		out = append(out, s)
	}
	return out
}
```

In `clone`:

```go
	if p.Sources != nil {
		out.Sources = append([]PluginSource(nil), p.Sources...)
	}
```

In `internal/config/load.go`:

```go
type wirePlugins struct {
	Enabled   []string                  `json:"enabled,omitempty"`
	Settings  map[string]map[string]any `json:"settings,omitempty"`
	Instances map[string]map[string]any `json:"instances,omitempty"`
	Sources   []wirePluginSource        `json:"sources,omitempty"`
}

type wirePluginSource struct {
	Name    string `json:"name"`
	URL     string `json:"url,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
}

var sourceNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// validSourceURL admits https repositories and local file:// ones, which is
// how an author tests a catalog before publishing it.
func validSourceURL(raw string) error {
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return err
	case u.Scheme == "https" && u.Host != "":
		return nil
	case u.Scheme == "file" && u.Host == "" && path.IsAbs(u.Path):
		return nil
	}
	return fmt.Errorf("%q must be an https:// or file:/// URL", raw)
}
```

(Add the imports `"net/url"` and `"path"` if they are missing.)

At the end of `applyPlugins`, before `return out, nil`:

```go
	names := make(map[string]struct{}, len(w.Sources))
	for i, ws := range w.Sources {
		field := fmt.Sprintf("%s.sources[%d]", path, i)
		if !sourceNamePattern.MatchString(ws.Name) {
			return Plugins{}, pathErr(field+".name", "%q is not a source name", ws.Name)
		}
		if _, dup := names[ws.Name]; dup {
			return Plugins{}, pathErr(field+".name", "%q appears more than once", ws.Name)
		}
		names[ws.Name] = struct{}{}
		if ws.Name == BuiltinPluginSource.Name {
			if ws.URL != "" {
				return Plugins{}, pathErr(field+".url", "the built-in source cannot be repointed")
			}
		} else if err := validSourceURL(ws.URL); err != nil {
			return Plugins{}, pathErr(field+".url", "%v", err)
		}
		out.Sources = append(out.Sources, PluginSource{Name: ws.Name, URL: ws.URL, Enabled: ws.Enabled == nil || *ws.Enabled})
	}
```

(The parameter named `path` in `applyPlugins` shadows the `path` package inside that function. That's fine, because `validSourceURL` is a separate function.)

In `internal/config/write.go`:

```go
func pluginsDiff(got Plugins) *wirePlugins {
	if len(got.Enabled) == 0 && len(got.Settings) == 0 && len(got.Instances) == 0 && len(got.Sources) == 0 {
		return nil
	}
	c := got.clone()
	w := &wirePlugins{Enabled: c.Enabled, Settings: c.Settings, Instances: c.Instances}
	for _, s := range c.Sources {
		ws := wirePluginSource{Name: s.Name, URL: s.URL}
		if !s.Enabled {
			off := false
			ws.Enabled = &off
		}
		w.Sources = append(w.Sources, ws)
	}
	return w
}
```

- [ ] **Step 4: Run**

Run: `GOMAXPROCS=4 go test -race -count=1 ./internal/config`
Expected: PASS, including the unchanged `TestDefaultConfigHasNoPlugins`.

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "feat(config): plugin sources with a built-in first-party one"
```

---

### Task 11: Wiring: host swap, IPC, startup

**Files:**
- Modify: `internal/shell/pluginhost.go` (after `rescan` at `:1301`), `internal/shell/registry.go` (add `pluginStore *store.Store` to `Registry`), `internal/ipc/server.go` (`Handlers` at `:48`, `handleLine` at `:133`), `cmd/sysc-shell/main.go` (after `ctx, cancel := context.WithCancel(ctx)`, and the `ipc.Handlers` literal at `:260`)
- Create: `internal/shell/pluginstore.go`, `internal/shell/pluginstore_test.go`
- Test: `internal/ipc/server_test.go`

**Interfaces:**
- Consumes: `store.New`, `store.Options`, `store.Installer`, `store.NewHTTPClient`, `store.CacheRoot`, `store.State`, `plugin.ManagedRoot`, `config.Plugins.EffectiveSources`.
- Produces: `func (*pluginHost) replace(id string, swap func() error) error`; `(*Registry).BindPluginStore(*store.Store)`, `(*Registry).PluginSources() []store.Source`, `(*Registry).LocalPluginDirs() map[string]string`, `(*Registry).ReplacePlugin(id string, swap func() error) error`, `(*Registry).PluginStoreCall(method string, params json.RawMessage) (map[string]any, error)`; `ipc.Handlers.Plugins func(method string, params json.RawMessage) (map[string]any, error)`.

- [ ] **Step 1: Write the failing tests**

`internal/shell/pluginstore_test.go`:

```go
package shell

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
)

const storeTestManifest = `{"schema":1,"id":"org.sysc.timer","name":"Timer","version":"1.0.0",
"protocol":{"major":1,"minor":0},"exec":"bin/run","capabilities":[],"requires":{"commands":[]}}`

// writeStoreTestPlugin writes a minimal plugin. If plugin.ParseManifest
// rejects this manifest, use the shape of manifestJSON in
// internal/plugin/store/fixture_test.go, which that package proves loads.
func writeStoreTestPlugin(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "org.sysc.timer")
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(storeTestManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "run"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReplacePluginRescansAfterTheSwap(t *testing.T) {
	user, managed := t.TempDir(), t.TempDir()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	if err := reg.BindPlugins(PluginHostOptions{Roots: []plugin.Root{
		{Path: user, Source: plugin.SourceUser}, {Path: managed, Source: plugin.SourceManaged},
	}, StateDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	err := reg.ReplacePlugin("org.sysc.timer", func() error {
		writeStoreTestPlugin(t, managed)
		return nil
	})
	if err != nil {
		t.Fatalf("ReplacePlugin: %v", err)
	}
	if _, ok := reg.plugins.discovered().Lookup("org.sysc.timer"); !ok {
		t.Fatal("the swapped-in plugin was not discovered")
	}
	if dirs := reg.LocalPluginDirs(); len(dirs) != 0 {
		t.Errorf("LocalPluginDirs = %v; a managed copy is not local", dirs)
	}

	want := errors.New("swap failed")
	if err := reg.ReplacePlugin("org.sysc.timer", func() error { return want }); !errors.Is(err, want) {
		t.Errorf("ReplacePlugin = %v; want the swap's error", err)
	}
}

func TestLocalPluginDirsListsUserCopies(t *testing.T) {
	user := t.TempDir()
	dir := writeStoreTestPlugin(t, user)
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	if err := reg.BindPlugins(PluginHostOptions{Roots: []plugin.Root{{Path: user, Source: plugin.SourceUser}}, StateDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if got := reg.LocalPluginDirs()["org.sysc.timer"]; got != dir {
		t.Fatalf("LocalPluginDirs = %v", reg.LocalPluginDirs())
	}
}

func TestPluginSourcesListsOnlyEnabledOnes(t *testing.T) {
	cfg := config.Default()
	cfg.Plugins.Sources = []config.PluginSource{
		{Name: "sysc", Enabled: false},
		{Name: "mine", URL: "https://e.com/r", Enabled: true},
	}
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	got := reg.PluginSources()
	if len(got) != 1 || got[0].Name != "mine" {
		t.Fatalf("PluginSources = %+v", got)
	}
}

func TestPluginStoreCallWithoutAStore(t *testing.T) {
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	if _, err := reg.PluginStoreCall("plugins.store", nil); err == nil {
		t.Fatal("a registry with no store answered")
	}
}
```

In `internal/ipc/server_test.go`, add a test that follows the existing server tests' pattern for calling `handleLine`:

```go
func TestPluginsMethodsRouteToTheHandler(t *testing.T) {
	var gotMethod string
	var gotParams string
	s := NewServer("", Handlers{Plugins: func(method string, params json.RawMessage) (map[string]any, error) {
		gotMethod, gotParams = method, string(params)
		return map[string]any{"queued": method}, nil
	}})
	out := string(s.handleLine(`{"id":1,"method":"plugins.install","params":{"source":"sysc","id":"org.sysc.timer"}}`))
	if gotMethod != "plugins.install" || !strings.Contains(gotParams, "org.sysc.timer") || !strings.Contains(out, `"ok"`) {
		t.Fatalf("method %q params %q reply %s", gotMethod, gotParams, out)
	}
	out = string(NewServer("", Handlers{}).handleLine(`{"id":2,"method":"plugins.store"}`))
	if !strings.Contains(out, "plugin store handler unset") {
		t.Fatalf("reply without a handler: %s", out)
	}
}
```

(Read the top of `server_test.go` first and match its imports and helpers. If `envelope` shapes success differently from `"ok"`, assert on what an existing passing test asserts.)

- [ ] **Step 2: Run and confirm failure**

Run: `GOMAXPROCS=4 go test -count=1 -run 'ReplacePlugin|LocalPluginDirs|PluginSources|PluginStoreCall' ./internal/shell` and `GOMAXPROCS=4 go test -count=1 -run PluginsMethods ./internal/ipc`
Expected: FAIL, undefined symbols.

- [ ] **Step 3: Implement**

`internal/shell/pluginhost.go`, after `rescan`:

```go
// replace stops a plugin, lets the store swap its directory, and rescans. The
// store calls it for every change to the managed tree, so no process runs from
// a directory mid-swap; the rescan restarts the plugin when it is enabled and
// the swap left something startable. It takes Registry.mu itself, through
// syncEnabled, and must be called without it.
func (h *pluginHost) replace(id string, swap func() error) error {
	h.stopPlugin(id)
	err := swap()
	if serr := h.syncEnabled(); serr != nil {
		err = errors.Join(err, serr)
	}
	return err
}
```

(Add `"errors"` to the imports if it is missing.)

Add a `pluginStore *store.Store` field to `Registry` in `internal/shell/registry.go`, guarded by `mu` like the other bound services. Don't call it `store`, which would shadow the package.

`internal/shell/pluginstore.go`:

```go
package shell

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
)

// BindPluginStore attaches the plugin store. The store runs its own goroutine;
// the registry answers what it asks (enabled sources, local copies) and lends
// it the plugin host to swap directories under.
func (r *Registry) BindPluginStore(s *store.Store) {
	r.mu.Lock()
	r.pluginStore = s
	r.mu.Unlock()
}

// PluginSources returns the enabled sources from configuration.
func (r *Registry) PluginSources() []store.Source {
	r.mu.Lock()
	srcs := r.cfg.Plugins.EffectiveSources()
	r.mu.Unlock()
	var out []store.Source
	for _, s := range srcs {
		if s.Enabled {
			out = append(out, store.Source{Name: s.Name, URL: s.URL})
		}
	}
	return out
}

// LocalPluginDirs maps plugin id to directory for every usable user-root copy.
func (r *Registry) LocalPluginDirs() map[string]string {
	out := map[string]string{}
	if r.plugins == nil {
		return out
	}
	for _, c := range r.plugins.discovered().Plugins {
		if c.Source == plugin.SourceUser && c.Err == nil {
			out[c.Manifest.ID] = c.Dir
		}
	}
	return out
}

// ReplacePlugin is the store's Installer.Replace. It runs on the store worker,
// never under Registry.mu.
func (r *Registry) ReplacePlugin(id string, swap func() error) error {
	if r.plugins == nil {
		return swap()
	}
	return r.plugins.replace(id, swap)
}

// PluginStoreCall answers the plugins.* IPC methods. Operations are queued and
// return at once; callers read plugins.store for the outcome, because an
// install outlives the IPC client's two-second deadline. An IPC install is the
// user's own command at their own socket, so it is its own consent, as a
// hand-edited configuration is.
func (r *Registry) PluginStoreCall(method string, params json.RawMessage) (map[string]any, error) {
	r.mu.Lock()
	s := r.pluginStore
	r.mu.Unlock()
	if s == nil {
		return nil, errors.New("plugin store unavailable")
	}
	var p struct {
		Source  string `json:"source"`
		ID      string `json:"id"`
		Confirm bool   `json:"confirm"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, errors.New("malformed params")
		}
	}
	var err error
	switch method {
	case "plugins.store":
		return storeStateReply(s.State()), nil
	case "plugins.refresh":
		_, err = s.Refresh()
	case "plugins.install":
		_, err = s.Install(p.Source, p.ID)
	case "plugins.update":
		_, err = s.Update(p.ID, p.Confirm)
	case "plugins.rollback":
		_, err = s.Rollback(p.ID)
	case "plugins.remove":
		_, err = s.Remove(p.ID)
	default:
		return nil, fmt.Errorf("unknown method %s", method)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"queued": method}, nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func storeStateReply(st store.State) map[string]any {
	sources := make([]map[string]any, 0, len(st.Sources))
	for _, s := range st.Sources {
		sources = append(sources, map[string]any{
			"name": s.Name, "url": s.URL, "commit": s.Commit, "fetched_at": s.FetchedAt,
			"plugins": s.Plugins, "rejected": len(s.Rejected), "error": errText(s.Err),
		})
	}
	listings := make([]map[string]any, 0, len(st.Listings))
	for _, l := range st.Listings {
		row := map[string]any{
			"source": l.Source, "id": l.Entry.ID, "name": l.Entry.Name, "status": string(l.Status),
			"local_dir": l.LocalDir, "error": errText(l.Err),
		}
		if l.Resolution.Release != nil {
			row["version"] = l.Resolution.Release.Version
		}
		if l.Installed != nil {
			row["installed_version"] = l.Installed.Version
		}
		listings = append(listings, row)
	}
	return map[string]any{"busy": st.Busy, "sources": sources, "listings": listings}
}
```

`internal/ipc/server.go`: add to `Handlers`

```go
	// Plugins answers every plugins.* method.
	Plugins func(method string, params json.RawMessage) (map[string]any, error)
```

and in `handleLine`, replace the `default:` branch:

```go
	default:
		if strings.HasPrefix(req.Method, "plugins.") {
			if s.h.Plugins == nil {
				return envelope(req.ID, "", "plugin store handler unset")
			}
			body, err := s.h.Plugins(req.Method, req.Params)
			if err != nil {
				return envelope(req.ID, "", err.Error())
			}
			return envelope(req.ID, "ok", "", body)
		}
		return envelope(req.ID, "", "unknown method")
```

`cmd/sysc-shell/main.go`: directly after `ctx, cancel := context.WithCancel(ctx)` and `defer cancel()`:

```go
	pluginStore := store.New(store.Options{
		CacheDir: store.CacheRoot(),
		Installer: &store.Installer{
			Root: plugin.ManagedRoot(), Client: store.NewHTTPClient(), Arch: runtime.GOARCH,
			Replace: registry.ReplacePlugin,
		},
		Sources: registry.PluginSources,
		Local:   registry.LocalPluginDirs,
	})
	registry.BindPluginStore(pluginStore)
	go pluginStore.Run(ctx)
	if _, err := pluginStore.Refresh(); err != nil {
		log.Printf("sysc-shell: plugin store: %v", err)
	}
```

and in the `ipc.Handlers` literal:

```go
			Plugins: registry.PluginStoreCall,
```

(Add the imports `"runtime"` and `"github.com/Nomadcxx/sysc-shell/internal/plugin/store"`.)

- [ ] **Step 4: Run each touched package**

```bash
GOMAXPROCS=4 go test -race -count=1 ./internal/ipc
GOMAXPROCS=4 go test -race -count=1 -run 'ReplacePlugin|LocalPluginDirs|PluginSources|PluginStoreCall|Plugin' ./internal/shell
GOMAXPROCS=4 go build ./cmd/sysc-shell
```

Expected: PASS and a clean build. `internal/shell` has a known flaky handler-panic test under `-race` (bd). If it fails, rerun that single test and report it separately. Do not change code to silence it.

- [ ] **Step 5: Commit**

```bash
git add internal/shell/pluginhost.go internal/shell/registry.go internal/shell/pluginstore.go internal/shell/pluginstore_test.go internal/ipc/server.go internal/ipc/server_test.go cmd/sysc-shell/main.go
git commit -m "feat(shell): run the plugin store and expose it over IPC"
```

---

### Task 12: Gates and the live Niri check

**Files:** none new. This task records its output in bd.

- [ ] **Step 1: Repository gates** (capped, with no `-race` on `./...`)

```bash
gofmt -w . && test -z "$(gofmt -l .)"
GOMAXPROCS=4 go vet ./...
GOMAXPROCS=4 go test -count=1 -p 2 ./...
for p in ./internal/plugin ./internal/plugin/store ./internal/config ./internal/ipc ./internal/shell; do
  GOMAXPROCS=4 go test -race -count=1 "$p" || echo "RACE FAIL $p"
done
git diff --exit-code -- go.mod go.sum
```

Expected: every line passes. Report any per-package race substitution as a substitution; do not claim it as `go test -race ./...`.

- [ ] **Step 2: Ask the owner before the live run.** The live check stops their running `sysc-shell.service`. Only one shell instance can hold the IPC socket, so the branch binary has to replace it for the duration. Proceed only on a yes.

- [ ] **Step 3: Build a local source and asset.** Timer is not in the owner's user root, so it installs as a managed copy.

```bash
S=$(mktemp -d -t sysc-livestore.XXXXXX)
mkdir -p "$S/pkg/org.sysc.timer" "$S/www"
( cd ~/sysc-plugins && go build -trimpath -o "$S/pkg/org.sysc.timer/bin/sysc-plugin-timer" ./cmd/sysc-plugin-timer )
cp ~/sysc-plugins/plugins/timer/manifest.json "$S/pkg/org.sysc.timer/"
tar -czf "$S/www/timer-1.4.0.tar.gz" -C "$S/pkg" org.sysc.timer
git init -q -b main "$S/src"
python3 - "$S" <<'EOF'
import hashlib, json, os, platform, sys
s = sys.argv[1]
m = json.load(open(f"{s}/pkg/org.sysc.timer/manifest.json"))
tar = f"{s}/www/timer-1.4.0.tar.gz"
arch = {"x86_64": "amd64", "aarch64": "arm64"}[platform.machine()]
row = {"id": m["id"], "name": m["name"], "author": "sysc", "description": "Countdown and stopwatch.",
       "category": "productivity", "version": m["version"], "protocol": m["protocol"],
       "capabilities": m["capabilities"], "requires": m["requires"],
       "assets": {f"linux-{arch}": {"url": "http://127.0.0.1:8765/timer-1.4.0.tar.gz",
                  "sha256": hashlib.sha256(open(tar, "rb").read()).hexdigest(), "size": os.path.getsize(tar)}}}
json.dump({"schema": 1, "plugins": [row]}, open(f"{s}/src/catalog.json", "w"), indent=2)
EOF
git -C "$S/src" add catalog.json && git -C "$S/src" -c user.name=t -c user.email=t@t commit -qm catalog
python3 -m http.server 8765 --bind 127.0.0.1 -d "$S/www" >/dev/null 2>&1 &
echo "http server pid $!"
```

- [ ] **Step 4: Point config at the source and run the branch binary**

```bash
cp ~/.config/sysc-shell/config.json "$S/config.json.bak"
python3 - "$S" <<'EOF'
import json, os, sys
p = os.path.expanduser("~/.config/sysc-shell/config.json")
c = json.load(open(p))
pl = c.setdefault("plugins", {})
pl["sources"] = [{"name": "sysc", "enabled": False}, {"name": "live", "url": f"file://{sys.argv[1]}/src"}]
if "org.sysc.timer" not in pl.setdefault("enabled", []):
    pl["enabled"].append("org.sysc.timer")
json.dump(c, open(p, "w"), indent=2)
EOF
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1) WAYLAND_DISPLAY=wayland-1 XDG_RUNTIME_DIR=/run/user/1000
go build -o "$S/sysc-shell" ./cmd/sysc-shell
systemctl --user stop sysc-shell
"$S/sysc-shell" >"$S/shell.log" 2>&1 &
sleep 2; "$S/sysc-shell" ipc plugins.store
```

Expected: a `live` source with `plugins: 1` and one listing `org.sysc.timer` whose status is `available`.

- [ ] **Step 5: Exercise the lifecycle and record each observation**

```bash
Q() { "$S/sysc-shell" ipc "$@"; }
Q plugins.install '{"source":"live","id":"org.sysc.timer"}'; sleep 3; Q plugins.store
pgrep -af 'sysc-shell/plugins/org.sysc.timer/bin/sysc-plugin-timer'
niri msg -j layers | python3 -c 'import json,sys; print([l["namespace"] for l in json.load(sys.stdin)])'
grim -g "0,0 3440,48" "$S/bar-installed.png"
```

Expected: status `installed`, a timer process running from `~/.local/share/sysc-shell/plugins/org.sysc.timer`, the bar layer still mapped, and the timer widget visible in the screenshot.

Then publish 1.4.1. The manifest version is the only change, so the binary is the same:

```bash
python3 - "$S" <<'EOF'
import json, sys; s = sys.argv[1]
p = f"{s}/pkg/org.sysc.timer/manifest.json"; m = json.load(open(p)); m["version"] = "1.4.1"; json.dump(m, open(p, "w"))
EOF
tar -czf "$S/www/timer-1.4.1.tar.gz" -C "$S/pkg" org.sysc.timer
python3 - "$S" <<'EOF'
import hashlib, json, os, sys; s = sys.argv[1]
c = json.load(open(f"{s}/src/catalog.json")); r = c["plugins"][0]; old = dict(r)
tar = f"{s}/www/timer-1.4.1.tar.gz"
r["version"] = "1.4.1"
for a in r["assets"].values():
    a.update(url=a["url"].replace("1.4.0", "1.4.1"), sha256=hashlib.sha256(open(tar, "rb").read()).hexdigest(), size=os.path.getsize(tar))
r["releases"] = [{k: old[k] for k in ("version", "protocol", "capabilities", "requires", "assets")}]
json.dump(c, open(f"{s}/src/catalog.json", "w"), indent=2)
EOF
git -C "$S/src" -c user.name=t -c user.email=t@t commit -qam 1.4.1
Q plugins.refresh; sleep 2; Q plugins.store                      # expect "update available"
OLD=$(pgrep -f 'org.sysc.timer/bin/sysc-plugin-timer')
Q plugins.update '{"id":"org.sysc.timer"}'; sleep 3; Q plugins.store   # expect installed_version 1.4.1
NEW=$(pgrep -f 'org.sysc.timer/bin/sysc-plugin-timer'); echo "pid $OLD -> $NEW"   # expect a new pid
Q plugins.rollback '{"id":"org.sysc.timer"}'; sleep 3; Q plugins.store # expect installed_version 1.4.0
Q plugins.remove '{"id":"org.sysc.timer"}'; sleep 3; Q plugins.store   # expect available, no installed_version
pgrep -af 'org.sysc.timer/bin/sysc-plugin-timer' || echo "no timer process (expected)"
ls ~/.local/share/sysc-shell/plugins
```

- [ ] **Step 6: The local override.** Link a checkout into the user root, restart the branch binary (kill it by pid, never `pkill -f`), install again, and check that the running process comes from the local copy:

```bash
kill "$(pgrep -f "$S/sysc-shell$" | head -1)"
ln -s ~/sysc-plugins/plugins/timer ~/.config/sysc-shell/plugins/timer
( cd ~/sysc-plugins && make build >/dev/null )
"$S/sysc-shell" >>"$S/shell.log" 2>&1 & sleep 2
Q plugins.install '{"source":"live","id":"org.sysc.timer"}'; sleep 3; Q plugins.store   # expect "shadowed"
pgrep -af 'sysc-plugin-timer'                                                          # expect the ~/sysc-plugins path
```

- [ ] **Step 7: Restore.** Kill the branch binary by pid, stop the http server by pid, `rm ~/.config/sysc-shell/plugins/timer`, restore `config.json` from `$S/config.json.bak`, remove `~/.local/share/sysc-shell/plugins/org.sysc.timer` if it's left, and run `systemctl --user start sysc-shell`. Confirm the owner's bar is back with `niri msg -j layers`.

- [ ] **Step 8: Record and close.** From `/home/nomadx/sysc-shell`, attach the gate output and live observations to the tranche 1 issue (`bd update <id> --notes "..."`): the statuses seen, the pids before and after the update, the screenshot path, and anything that differed from the expectations above. Close it with `bd close <id> --reason "..."` only if every step passed. Leave the branch unmerged unless the owner asks for a merge.

---

## Self-review against the spec

- **D1:** Task 10 (config), Task 6 (git read), Task 9 (per-source refresh, commit recorded), Task 7 (`catalog_commit` in the record).
- **D2:** Task 3. Closed categories and Other, lenient decode, https rules, resolution with held back and incompatible, required fields.
- **D3:** Task 4 (one top-level `<id>/`) and Task 8 (the manifest loads from it).
- **D4:** Task 2 (managed root, dot-dirs, user over managed, every other collision still rejects), Task 8 (`.prev`, `.staging`), Task 7 (`installed.json`).
- **D5:** Install is the explicit IPC command, an update that changes capabilities or requires is refused without `confirm` (Task 9), and sources in config are trusted. The Settings consent sheets are tranche 2.
- **D6:** Tasks 5, 4 and 8, with a failure injected at each rename and the previous version kept; rollback and remove; a corrupt `installed.json` rebuilt without deletion (Task 7).
- **D7:** Task 9, one worker, queue depth 16, snapshots, per-source and per-listing errors, stale sources keeping their catalog.
- **D8:** The daily ticker is tranche 2; the spec's `schedule.go` reference is corrected with this plan. Refresh at startup is in Task 11.
- **D9:** Tranche 2. `Listing` carries every decoded field, which is the seam D9 relies on.
- **D10:** Tranche 3 (the bd issues from Task 0). Task 3's validation is the shared rule set `tools/validate-catalog` will mirror.
- **Error model:** Task 3's `Kind` list covers every cause the spec names, plus `KindConsent`, `KindNoPrevious`, `KindNotListed` and `KindBusy`, which the IPC surface needs.
