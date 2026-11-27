package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// barCatalogListings is a catalog with one plugin of each kind the add list
// has to tell apart: installable, installed, held back and incompatible.
func barCatalogListings() []store.Listing {
	available := modelListing("org.sysc.airquality", "Air Quality & Pollen", "sysc", "weather", "0.4.0")
	installed := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.4.0")
	installed.Installed = &store.Record{Source: "sysc", Version: "1.4.0"}
	installed.Status = store.StatusInstalled
	held := modelListing("org.sysc.held", "Held Plugin", "sysc", "utilities", "2.0.0")
	held.Status = store.StatusHeldBack
	held.Resolution.Compat = store.HeldBack
	incompatible := modelListing("org.sysc.newer", "Newer Plugin", "sysc", "utilities", "3.0.0")
	incompatible.Status = store.StatusIncompatible
	incompatible.Resolution.Compat = store.Incompatible
	return []store.Listing{available, installed, held, incompatible}
}

func barAddActions(tree *ui.Node, prefix string) []string {
	var actions []string
	walkNodes(tree, func(n *ui.Node) {
		if strings.HasPrefix(n.Action, prefix) {
			actions = append(actions, n.Action)
		}
	})
	return actions
}

// F1 of the 2026-11-22 audit: a plugin the catalog offers but the machine does
// not have must still be findable from the bar editor.
func TestBarAddListOffersOnlyInstallableCatalogPlugins(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{})
	reg.mu.Lock()
	reg.pluginStoreSnapshot = store.State{Listings: barCatalogListings()}
	tree := barAddList(reg, h, "right", 240)
	reg.mu.Unlock()
	got := barAddActions(tree, "bar-add-catalog:")
	if len(got) != 1 || got[0] != "bar-add-catalog:right:sysc/org.sysc.airquality" {
		t.Fatalf("catalog rows = %v, want only Air Quality", got)
	}
	if !pluginStoreHasText(tree, "Available to install") {
		t.Error("catalog rows have no section heading")
	}
}

func TestBarAddListFiltersCatalogPlugins(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{})
	for query, want := range map[string]int{"air": 1, "AIRQUALITY": 1, "pollen": 1, "zzz": 0} {
		h.barAddFilter = query
		reg.mu.Lock()
		reg.pluginStoreSnapshot = store.State{Listings: barCatalogListings()}
		tree := barAddList(reg, h, "right", 240)
		reg.mu.Unlock()
		if got := len(barAddActions(tree, "bar-add-catalog:")); got != want {
			t.Errorf("query %q matched %d catalog rows, want %d", query, got, want)
		}
	}
}

func TestActivatingACatalogRowOpensItsStoreDetail(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{})
	reg.mu.Lock()
	reg.pluginStoreSnapshot = store.State{Listings: barCatalogListings()}
	reg.mu.Unlock()
	barActivateMu(reg, h, "bar-add:right")
	if !barActivateMu(reg, h, "bar-add-catalog:right:sysc/org.sysc.airquality") {
		t.Fatal("the catalog row was not handled")
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	storeHost := reg.panelHosts[PanelPluginStore]
	if storeHost == nil {
		t.Fatal("the catalog row did not open the plugin store")
	}
	if storeHost.pluginStoreDetail != "sysc/org.sysc.airquality" {
		t.Fatalf("store detail = %q, want Air Quality", storeHost.pluginStoreDetail)
	}
	if h.barAdding != "right" {
		t.Error("handing off to the store closed the add list the user will come back to")
	}
}

// An empty result is the one place the list cannot help, so it hands the
// query to the store, which also searches sources and categories.
func TestAnEmptyAddResultOffersToSearchTheStore(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{})
	for query, want := range map[string]bool{"zzz": true, "": false, "air": false} {
		h.barAddFilter = query
		reg.mu.Lock()
		reg.pluginStoreSnapshot = store.State{Listings: barCatalogListings()}
		tree := barAddList(reg, h, "right", 240)
		reg.mu.Unlock()
		if got := len(barAddActions(tree, "bar-add-store-search")) == 1; got != want {
			t.Errorf("query %q: store search row offered = %v, want %v", query, got, want)
		}
		if want && !pluginStoreHasText(tree, "Search the plugin store for “zzz”") {
			t.Errorf("query %q: the row does not name the query", query)
		}
	}
}

func TestSearchingTheStoreFromTheAddListCarriesTheQuery(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{})
	barActivateMu(reg, h, "bar-add:right")
	h.barAddFilter = "pollen count"
	if !barActivateMu(reg, h, "bar-add-store-search") {
		t.Fatal("the store search row was not handled")
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	storeHost := reg.panelHosts[PanelPluginStore]
	if storeHost == nil {
		t.Fatal("the store search row did not open the plugin store")
	}
	if storeHost.search == nil || storeHost.search.Text != "pollen count" {
		t.Fatalf("store search = %+v, want the add list's query", storeHost.search)
	}
	if storeHost.pluginStoreDetail != "" {
		t.Errorf("a search opened detail %q instead of the grid", storeHost.pluginStoreDetail)
	}
}

// writeUnstartablePlugin is a widget plugin whose required command cannot be
// on PATH, so discovery marks it not startable.
func writeUnstartablePlugin(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "org.sysc.phone")
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	const manifest = `{"schema":1,"id":"org.sysc.phone","name":"Phone Connect","version":"1.0.0",
"protocol":{"major":1,"minor":16},"exec":"bin/run","capabilities":[],
"requires":{"commands":["sysc-no-such-command-2399"]},"widgets":[{"id":"bar"}]}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "run"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// F2 of the audit: an installed plugin that cannot start used to vanish from
// the list, which reads the same as never having installed it.
func TestBarAddListExplainsAnInstalledPluginThatCannotStart(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{})
	bindWidgetPlugin(t, reg, writeUnstartablePlugin(t))
	reg.mu.Lock()
	tree := barAddList(reg, h, "right", 320)
	reg.mu.Unlock()
	if got := barAddActions(tree, "bar-add-plugin:"); len(got) != 0 {
		t.Fatalf("a plugin that cannot start is offered for placement: %v", got)
	}
	row := pluginStoreFindAction(tree, "bar-add-manage:org.sysc.phone")
	if row == nil {
		t.Fatal("the plugin that cannot start is not listed")
	}
	if !pluginStoreHasText(row, "Phone Connect — needs sysc-no-such-command-2399") {
		t.Error("the row does not say what the plugin is missing")
	}
}

func TestTheUnstartableRowOpensPluginSettings(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{})
	bindWidgetPlugin(t, reg, writeUnstartablePlugin(t))
	barActivateMu(reg, h, "bar-add:right")
	activateSettingsAction(t, reg, h, "bar-add-manage:org.sysc.phone")
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if h.section != "Plugins" {
		t.Fatalf("section = %q, want Plugins", h.section)
	}
}
