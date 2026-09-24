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
