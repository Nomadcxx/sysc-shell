package shell

import (
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
