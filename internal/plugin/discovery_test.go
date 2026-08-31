package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// install writes one plugin directory named after its id under root.
func install(t *testing.T, root, id, manifest string) string {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExec(t, filepath.Join(dir, "bin", "sysc-plugin-timer"), 0o755)
	return dir
}

// idOf returns the manifest fixture with a replacement id.
func idOf(t *testing.T, id string) string {
	t.Helper()
	return edit(t, "id", id)
}

func TestDiscoverReadsImmediateChildDirectories(t *testing.T) {
	t.Parallel()

	user := t.TempDir()
	system := t.TempDir()
	install(t, user, "org.example.notes", idOf(t, "org.example.notes"))
	install(t, system, "org.sysc.timer", timerManifest)

	cat, err := Discover(Root{Path: user, Source: SourceUser}, Root{Path: system, Source: SourceSystem})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(cat.Plugins) != 2 {
		t.Fatalf("found %d plugins, want 2", len(cat.Plugins))
	}
	// The catalogue is ordered by id so the manager renders stably.
	if cat.Plugins[0].Manifest.ID != "org.example.notes" || cat.Plugins[1].Manifest.ID != "org.sysc.timer" {
		t.Fatalf("order = %q, %q", cat.Plugins[0].Manifest.ID, cat.Plugins[1].Manifest.ID)
	}
	if cat.Plugins[0].Source != SourceUser || cat.Plugins[1].Source != SourceSystem {
		t.Errorf("sources = %v, %v", cat.Plugins[0].Source, cat.Plugins[1].Source)
	}
	if _, ok := cat.Lookup("org.sysc.timer"); !ok {
		t.Error("Lookup missed a discovered plugin")
	}
}

func TestDiscoverDoesNotRecurse(t *testing.T) {
	t.Parallel()

	user := t.TempDir()
	nested := filepath.Join(user, "vendor", "pack")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	install(t, nested, "org.sysc.timer", timerManifest)

	cat, err := Discover(Root{Path: user, Source: SourceUser})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	// "vendor" is an immediate child with no manifest; the plugin two levels
	// down must stay invisible.
	for _, p := range cat.Plugins {
		if p.Manifest.ID == "org.sysc.timer" {
			t.Fatal("Discover recursed into a nested directory")
		}
	}
}

func TestDiscoverSkipsDirectoriesWithNoManifest(t *testing.T) {
	t.Parallel()

	user := t.TempDir()
	if err := os.MkdirAll(filepath.Join(user, "notes-backup"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	install(t, user, "org.sysc.timer", timerManifest)

	cat, err := Discover(Root{Path: user, Source: SourceUser})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(cat.Plugins) != 1 {
		t.Fatalf("found %d plugins, want only the one with a manifest", len(cat.Plugins))
	}
}

func TestDiscoverKeepsRejectedManifestsVisible(t *testing.T) {
	t.Parallel()

	user := t.TempDir()
	install(t, user, "broken", `{"schema": 1, "id": "org.sysc.broken"`)
	install(t, user, "org.sysc.timer", timerManifest)

	cat, err := Discover(Root{Path: user, Source: SourceUser})
	if err != nil {
		t.Fatalf("a rejected manifest must not fail the scan: %v", err)
	}
	if len(cat.Plugins) != 2 {
		t.Fatalf("found %d entries, want the valid and the rejected one", len(cat.Plugins))
	}
	var rejected *Candidate
	for i := range cat.Plugins {
		if cat.Plugins[i].Err != nil {
			rejected = &cat.Plugins[i]
		}
	}
	if rejected == nil {
		t.Fatal("the malformed manifest was not reported")
	}
	if !strings.HasSuffix(rejected.Dir, "broken") {
		t.Errorf("rejected dir = %q, want the offending directory", rejected.Dir)
	}
	if rejected.Manifest.ID != "" {
		t.Error("a rejected candidate must not expose a manifest")
	}
}

func TestDiscoverRejectsBothSidesOfADuplicateID(t *testing.T) {
	t.Parallel()

	// User content must not shadow packaged code, and packaged code must not
	// silently win either: the collision itself is the fault to report.
	user := t.TempDir()
	system := t.TempDir()
	userDir := install(t, user, "timer", timerManifest)
	systemDir := install(t, system, "org.sysc.timer", timerManifest)

	cat, err := Discover(Root{Path: user, Source: SourceUser}, Root{Path: system, Source: SourceSystem})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if _, ok := cat.Lookup("org.sysc.timer"); ok {
		t.Fatal("a duplicated id stayed usable")
	}
	if len(cat.Plugins) != 2 {
		t.Fatalf("found %d entries, want each side of the collision", len(cat.Plugins))
	}
	for _, p := range cat.Plugins {
		if p.Err == nil {
			t.Fatalf("%s survived the collision", p.Dir)
		}
		if !strings.Contains(p.Err.Error(), userDir) || !strings.Contains(p.Err.Error(), systemDir) {
			t.Errorf("err = %v, want it to name each path", p.Err)
		}
	}
}

func TestDiscoverBoundsTheDirectoryCount(t *testing.T) {
	t.Parallel()

	user := t.TempDir()
	for i := 0; i <= MaxPluginDirs; i++ {
		if err := os.MkdirAll(filepath.Join(user, "p"+itoa(i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Discover(Root{Path: user, Source: SourceUser})
	if err == nil {
		t.Fatalf("more than %d directories accepted", MaxPluginDirs)
	}
	if !strings.Contains(err.Error(), user) {
		t.Errorf("err = %v, want it to name the root", err)
	}
}

func TestDiscoverTreatsAMissingRootAsEmpty(t *testing.T) {
	t.Parallel()

	// A user who has never installed a plugin has no plugin directory, which
	// is not a fault.
	cat, err := Discover(Root{Path: filepath.Join(t.TempDir(), "absent"), Source: SourceUser})
	if err != nil {
		t.Fatalf("a missing root must not fail the scan: %v", err)
	}
	if len(cat.Plugins) != 0 {
		t.Fatalf("found %d plugins in a missing root", len(cat.Plugins))
	}
}

// Not parallel: it replaces PATH for the process.
func TestDiscoverReportsMissingDependenciesWithoutRejecting(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	user := t.TempDir()
	withDep := edit(t, "requires", map[string]any{"commands": []string{"gpu-screen-recorder"}})
	install(t, user, "recorder", withDep)

	cat, err := Discover(Root{Path: user, Source: SourceUser})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	p, ok := cat.Lookup("org.sysc.timer")
	if !ok {
		t.Fatal("a plugin with a missing dependency vanished from the catalogue")
	}
	if p.Err != nil {
		t.Fatalf("a missing dependency rejected the manifest: %v", p.Err)
	}
	if got := p.MissingCommands; len(got) != 1 || got[0] != "gpu-screen-recorder" {
		t.Fatalf("missing = %v, want the one dependency", got)
	}
}

func TestDiscoverIgnoresAFileNamedLikeAPlugin(t *testing.T) {
	t.Parallel()

	user := t.TempDir()
	if err := os.WriteFile(filepath.Join(user, "org.sysc.timer"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := Discover(Root{Path: user, Source: SourceUser})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(cat.Plugins) != 0 {
		t.Fatalf("found %d plugins, want none", len(cat.Plugins))
	}
}

// Not parallel: it replaces XDG_CONFIG_HOME for the process.
func TestDefaultRootsNameTheUserAndSystemDirectories(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/cfg")
	roots := DefaultRoots("/usr/share/sysc-shell/plugins")
	if len(roots) != 2 {
		t.Fatalf("roots = %+v, want the user and system directories", roots)
	}
	if roots[0].Path != "/tmp/cfg/sysc-shell/plugins" || roots[0].Source != SourceUser {
		t.Errorf("user root = %+v", roots[0])
	}
	if roots[1].Path != "/usr/share/sysc-shell/plugins" || roots[1].Source != SourceSystem {
		t.Errorf("system root = %+v", roots[1])
	}
}
