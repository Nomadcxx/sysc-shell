package shell

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
)

func TestPluginManagerInstalledHeadlessShowsManagedOverrideUnlistedErrorsAndUpdates(t *testing.T) {
	managed := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.5.0")
	managed.Installed = &store.Record{Source: "sysc", Version: "1.4.0", Previous: &store.Record{Version: "1.3.0"}}
	managed.Status = store.StatusUpdateAvailable
	managed.UpdateAvailable = true
	override := modelListing("org.sysc.editor", "Editor", "sysc", "appearance", "2.0.0")
	override.LocalDir = "/tmp/plugins/editor"
	override.Installed = &store.Record{Source: "sysc", Version: "1.9.0"}
	override.Status = store.StatusShadowed
	unlisted := store.Listing{
		Source: "gone", Entry: catalog.Entry{ID: "org.sysc.orphan", Name: "Orphan"},
		Installed: &store.Record{Source: "gone", Version: "0.9.0"}, Status: store.StatusUnlisted,
		Err: errors.New("plugin failed to start"),
	}
	state := store.State{Listings: []store.Listing{managed, override, unlisted}}
	reg, host, panel := openPluginManagerTestPanel(t, config.Default(), state)
	reg.mu.Lock()
	host.pluginManagerTab = "installed"
	reg.rebuildPanel(host)
	reg.mu.Unlock()
	renderPluginManagerPanel(t, panel)
	for _, want := range []string{"Installed", "updates (1)", "Update to v1.5.0", "local override", "managed, unlisted", "plugin failed to start"} {
		if !pluginStoreHasText(host.root, want) {
			t.Errorf("Installed page lacks %q", want)
		}
	}
	if !pluginStoreHasKind(host.root, ui.KindToggle) {
		t.Fatal("Installed page has no enable switch")
	}
}

func TestPluginManagerSourcesHeadlessShowsFreshStaleAndAddWarning(t *testing.T) {
	cfg := config.Default()
	cfg.Plugins.Sources = []config.PluginSource{
		{Name: "fresh", URL: "https://example.com/fresh", Enabled: true},
		{Name: "stale", URL: "https://example.com/stale", Enabled: true},
	}
	state := store.State{Sources: []store.SourceState{
		{Name: "sysc", URL: config.BuiltinPluginSource.URL, FetchedAt: time.Date(2026, 9, 27, 1, 0, 0, 0, time.UTC), Plugins: 1},
		{Name: "fresh", URL: "https://example.com/fresh", FetchedAt: time.Date(2026, 9, 27, 1, 30, 0, 0, time.UTC), Plugins: 2},
		{Name: "stale", URL: "https://example.com/stale", FetchedAt: time.Date(2026, 9, 27, 1, 0, 0, 0, time.UTC), Plugins: 1, Err: errors.New("offline")},
	}}
	reg, host, panel := openPluginManagerTestPanel(t, cfg, state)
	reg.mu.Lock()
	host.pluginManagerTab = "sources"
	reg.rebuildPanel(host)
	reg.mu.Unlock()
	renderPluginManagerPanel(t, panel)
	text := treeText(host.root)
	for _, want := range []string{"Sources", "Official", "fresh", "2 plugins", "Stale: offline", "suggested", "Not published yet", "local plugin directory", "Rescan"} {
		if !strings.Contains(text, want) {
			t.Errorf("Sources page lacks %q", want)
		}
	}
	if !pluginStoreHasAction(host.root, "plugins-source-add") {
		t.Fatal("Sources page has no Add Source action")
	}

	reg.mu.Lock()
	host.fields = map[string]*ui.Field{
		"plugins-source-name": ui.NewField("test-source"),
		"plugins-source-url":  ui.NewField("https://example.com/plugins"),
	}
	if !reg.handlePluginManager(host, &ui.Node{Action: "plugins-source-add"}) {
		reg.mu.Unlock()
		t.Fatal("Add Source did not open the warning")
	}
	if len(reg.cfg.Plugins.Sources) != 2 {
		reg.mu.Unlock()
		t.Fatal("opening the warning changed configured sources before confirmation")
	}
	reg.rebuildPanel(host)
	reg.mu.Unlock()
	renderPluginManagerPanel(t, panel)
	text = treeText(host.root)
	for _, want := range []string{"full file and network access", "Confirm", "Cancel"} {
		if !strings.Contains(text, want) {
			t.Errorf("source warning lacks %q", want)
		}
	}
}

func TestPluginManagerUpdateAllRendersMixedConsentReviewSet(t *testing.T) {
	a := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.5.0")
	b := modelListing("org.sysc.editor", "Editor", "sysc", "appearance", "2.0.0")
	state := store.State{Listings: []store.Listing{a, b}}
	reg, host, panel := openPluginManagerTestPanel(t, config.Default(), state)
	reg.mu.Lock()
	host.pluginManagerTab = "installed"
	host.pluginUpdateAllNeedsConsent = []string{pluginStoreKey(a), pluginStoreKey(b)}
	reg.rebuildPanel(host)
	reg.mu.Unlock()
	renderPluginManagerPanel(t, panel)
	for _, want := range []string{"These updates need review", "Timer", "Editor"} {
		if !pluginStoreHasText(host.root, want) {
			t.Errorf("Update all review set lacks %q", want)
		}
	}
	if countPluginManagerAction(host.root, "plugins-review:") != 2 {
		t.Fatal("mixed Update all review set should offer one Review action per plugin")
	}
}

func TestPluginManagerReviewOpensTheStoreDetail(t *testing.T) {
	listing := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.5.0")
	reg, host, _ := openPluginManagerTestPanel(t, config.Default(), store.State{Listings: []store.Listing{listing}})
	key := pluginStoreKey(listing)
	reg.mu.Lock()
	host.pluginUpdateAllNeedsConsent = []string{key}
	reg.rebuildPanel(host)
	button := pluginStoreFindAction(host.root, "plugins-review:"+key)
	if button == nil {
		reg.mu.Unlock()
		t.Fatal("Update all consent row has no Review action")
	}
	reg.handlePluginManager(host, button)
	storeHost := reg.panelHosts[PanelPluginStore]
	if storeHost == nil {
		reg.mu.Unlock()
		t.Fatal("Review did not open the plugin store")
	}
	if storeHost.pluginStoreDetail != key {
		reg.mu.Unlock()
		t.Fatalf("Review detail = %q, want %q", storeHost.pluginStoreDetail, key)
	}
	reg.mu.Unlock()
}

func TestPluginManagerAddSourceWritesOnlyAfterValidatedConfirmation(t *testing.T) {
	reg, host, _ := openPluginManagerTestPanel(t, config.Default(), store.State{})
	configPath := filepath.Join(t.TempDir(), "config.json")
	reg.configPath = configPath
	reg.pluginStore = store.New(store.Options{})
	host.fields = map[string]*ui.Field{
		"plugins-source-name": ui.NewField("test-source"),
		"plugins-source-url":  ui.NewField("https://example.com/plugins"),
	}
	reg.mu.Lock()
	reg.handlePluginManager(host, &ui.Node{Action: "plugins-source-add"})
	reg.mu.Unlock()
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("opening the consent step wrote config: %v", err)
	}
	if len(reg.cfg.Plugins.Sources) != 0 {
		t.Fatal("opening the consent step changed configured sources")
	}
	reg.mu.Lock()
	reg.handlePluginManager(host, &ui.Node{Action: "plugins-source-confirm"})
	reg.mu.Unlock()
	if len(reg.cfg.Plugins.Sources) != 1 || reg.cfg.Plugins.Sources[0].Name != "test-source" {
		t.Fatalf("confirmed sources = %+v", reg.cfg.Plugins.Sources)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("confirmed source was not persisted: %v", err)
	}
}

func TestPluginManagerAddSourceRejectsInvalidNameWithoutWriting(t *testing.T) {
	reg, host, _ := openPluginManagerTestPanel(t, config.Default(), store.State{})
	configPath := filepath.Join(t.TempDir(), "config.json")
	reg.configPath = configPath
	host.fields = map[string]*ui.Field{
		"plugins-source-name": ui.NewField("bad name"),
		"plugins-source-url":  ui.NewField("https://example.com/plugins"),
	}
	reg.mu.Lock()
	reg.handlePluginManager(host, &ui.Node{Action: "plugins-source-add"})
	reg.handlePluginManager(host, &ui.Node{Action: "plugins-source-confirm"})
	reg.mu.Unlock()
	if len(reg.cfg.Plugins.Sources) != 0 || host.pluginManagerError == "" {
		t.Fatal("invalid source name was not rejected")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("invalid source wrote config: %v", err)
	}
}

func TestPluginManagerAddSourceRejectsInsecureAndReservedSources(t *testing.T) {
	for _, tc := range []struct {
		name, sourceURL string
	}{
		{name: "test-source", sourceURL: "http://example.com/plugins"},
		{name: "test-source", sourceURL: "file://relative/plugins"},
		{name: "sysc", sourceURL: "https://example.com/plugins"},
	} {
		t.Run(tc.name+"/"+tc.sourceURL, func(t *testing.T) {
			reg, host, _ := openPluginManagerTestPanel(t, config.Default(), store.State{})
			reg.configPath = filepath.Join(t.TempDir(), "config.json")
			host.fields = map[string]*ui.Field{
				"plugins-source-name": ui.NewField(tc.name),
				"plugins-source-url":  ui.NewField(tc.sourceURL),
			}
			reg.mu.Lock()
			reg.handlePluginManager(host, &ui.Node{Action: "plugins-source-add"})
			reg.handlePluginManager(host, &ui.Node{Action: "plugins-source-confirm"})
			reg.mu.Unlock()
			if len(reg.cfg.Plugins.Sources) != 0 || host.pluginManagerError == "" {
				t.Fatal("invalid source was not rejected")
			}
			if _, err := os.Stat(reg.configPath); !os.IsNotExist(err) {
				t.Fatalf("invalid source wrote config: %v", err)
			}
		})
	}
}

func openPluginManagerTestPanel(t *testing.T, cfg config.Config, state store.State) (*Registry, *PanelHost, *wayland.AuxSpec) {
	t.Helper()
	return openPluginManagerTestPanelOn(t, cfg, state, ui.Rect{W: 1280, H: 900})
}

func openPluginManagerTestPanelOn(t *testing.T, cfg config.Config, state store.State, output ui.Rect) (*Registry, *PanelHost, *wayland.AuxSpec) {
	t.Helper()
	reg := newPanelRegistry(t)
	reg.mu.Lock()
	reg.cfg = cfg
	reg.pluginStoreSnapshot = state
	reg.mu.Unlock()
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{OutW: output.W, OutH: output.H}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	panel := reqs[1].Open
	width, height := int(panel.Width), int(panel.Height)
	if err := panel.Callbacks.Configure(width, height, 120); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	host := reg.panelHosts[PanelSettings]
	host.section = "Plugins"
	host.pluginManagerTab = "installed"
	reg.rebuildPanel(host)
	reg.mu.Unlock()
	return reg, host, panel
}

func renderPluginManagerPanel(t *testing.T, panel *wayland.AuxSpec) {
	t.Helper()
	width, height := int(panel.Width), int(panel.Height)
	pixels := make([]byte, width*height*4)
	if err := panel.Callbacks.Render(pixels, width, height, width*4); err != nil {
		t.Fatalf("render plugin manager: %v", err)
	}
}

func pluginStoreHasAction(n *ui.Node, action string) bool {
	return pluginStoreFindAction(n, action) != nil
}

func countPluginManagerAction(n *ui.Node, prefix string) int {
	count := 0
	var walk func(*ui.Node)
	walk = func(node *ui.Node) {
		if node == nil {
			return
		}
		if strings.HasPrefix(node.Action, prefix) {
			count++
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(n)
	return count
}
