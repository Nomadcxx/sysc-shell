package shell

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// TestReplaceBlocksAConcurrentEnsureWhileSwapping proves F2: between
// stopPlugin and the swap finishing, a concurrent syncEnabled (a config
// change, another BindPlugins caller racing in) must not restart the plugin
// from whatever the tree looked like before the swap landed. The fake plugin
// binary these fixtures use has no protocol handshake, so starting a real
// process and asserting it ends up running is impractical here (see
// writeStoreTestPlugin); instead this asserts what F2 is actually about:
// ensure refuses while the id is marked swapping, and the mark is gone once
// replace returns.
func TestReplaceBlocksAConcurrentEnsureWhileSwapping(t *testing.T) {
	user, managed := t.TempDir(), t.TempDir()
	cfg := config.Default()
	cfg.Plugins.Enabled = []string{"org.sysc.timer"}
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	if err := reg.BindPlugins(PluginHostOptions{Roots: []plugin.Root{
		{Path: user, Source: plugin.SourceUser}, {Path: managed, Source: plugin.SourceManaged},
	}, StateDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}

	err := reg.ReplacePlugin("org.sysc.timer", func() error {
		writeStoreTestPlugin(t, managed)
		reg.plugins.mu.Lock()
		swapping := reg.plugins.swapping["org.sysc.timer"]
		reg.plugins.mu.Unlock()
		if !swapping {
			t.Fatal("replace did not mark the id swapping before calling swap")
		}
		// A concurrent syncEnabled, racing on another caller's goroutine in
		// production, landing while the id is swapping.
		if err := reg.plugins.syncEnabled(); err != nil {
			t.Fatal(err)
		}
		reg.plugins.mu.Lock()
		_, started := reg.plugins.slots["org.sysc.timer"]
		reg.plugins.mu.Unlock()
		if started {
			t.Fatal("ensure started the plugin while it was still swapping")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ReplacePlugin: %v", err)
	}

	reg.plugins.mu.Lock()
	swapping := reg.plugins.swapping["org.sysc.timer"]
	reg.plugins.mu.Unlock()
	if swapping {
		t.Fatal("replace left the id marked swapping after returning")
	}
}

// TestEnsureRefusesToInsertWhileSwapping reproduces sysc-513: an ensure that
// passed its swapping/slot check just before replace marked the id swapping,
// and is only now inserting the runtime it started against the pre-swap
// catalog. The insert-time check must see the mark, refuse to insert, and
// stop the runtime it just started rather than let replace's rescan miss it.
//
// The fixture is the race-built helper binary (see testTimerManifest in
// pluginhost_test.go) rather than the store package's inert fixture: a
// runtime that never completes its handshake never reaches ensure's insert
// point, so this race cannot be reproduced against it.
func TestEnsureRefusesToInsertWhileSwapping(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := plugin.WriteHelperPlugin(filepath.Join(root, "org.sysc.timer"), self, "", testTimerManifest); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	if err := reg.BindPlugins(PluginHostOptions{
		Roots:    []plugin.Root{{Path: root, Source: plugin.SourceUser}},
		StateDir: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	cat := reg.plugins.discovered()

	started := make(chan *plugin.Runtime, 1)
	proceed := make(chan struct{})
	reg.plugins.ensureRaceHook = func(id string, rt *plugin.Runtime) {
		started <- rt
		<-proceed
	}

	errCh := make(chan error, 1)
	go func() { errCh <- reg.plugins.ensure("org.sysc.timer", cat, false) }()

	rt := <-started
	// The interleaving under test: replace has marked the id swapping while
	// this ensure is mid-start, past its own check.
	reg.plugins.mu.Lock()
	reg.plugins.swapping["org.sysc.timer"] = true
	reg.plugins.mu.Unlock()
	close(proceed)

	if err := <-errCh; err != nil {
		t.Fatalf("ensure: %v", err)
	}

	reg.plugins.mu.Lock()
	_, inserted := reg.plugins.slots["org.sysc.timer"]
	delete(reg.plugins.swapping, "org.sysc.timer")
	reg.plugins.ensureRaceHook = nil
	reg.plugins.mu.Unlock()
	if inserted {
		t.Fatal("ensure inserted a slot for an id marked swapping")
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if rt.Status().State == plugin.StateDisabled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ensure did not stop the runtime it started; status = %+v", rt.Status())
		}
		time.Sleep(10 * time.Millisecond)
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
