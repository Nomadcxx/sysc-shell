package shell

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
)

func TestPluginStoreRelayReadsREADMEAndRebuildsOpenDetail(t *testing.T) {
	readme := &catalog.Screenshot{URL: "https://example.com/readme.md", SHA256: "readme-sha"}
	listing := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.5.0")
	listing.Entry.Readme = readme
	reg, host, _ := openPluginStoreTestPanel(t, store.State{Listings: []store.Listing{listing}}, ui.Rect{W: 1440, H: 900})
	st := store.New(store.Options{})
	updates := make(chan store.State, 1)
	reg.mu.Lock()
	reg.pluginStore = st
	host.pluginStoreDetail = pluginStoreKey(listing)
	reg.rebuildPanel(host)
	reg.mu.Unlock()
	go reg.relayPluginStoreUpdates(st, updates)

	body := "# Relay README\n\nREADME body from relay."
	path := filepath.Join(t.TempDir(), "README.md")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot := store.State{
		Listings: []store.Listing{listing},
		Media:    map[string]store.MediaState{readme.SHA256: {Path: path}},
	}
	updates <- snapshot
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		loaded := reg.pluginStoreReadmes[readme.SHA256]
		rendered := pluginStoreHasText(host.root, "README body from relay.")
		reg.mu.Unlock()
		if loaded == body && rendered {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("README relay did not publish the cached text and rebuild the open detail")
}

func TestPluginStoreRelayRebuildsOpenSettingsPluginsPage(t *testing.T) {
	listing := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.5.0")
	listing.Installed = &store.Record{Source: "sysc", Version: "1.4.0"}
	reg, host, _ := openPluginManagerTestPanel(t, config.Default(), store.State{Listings: []store.Listing{listing}})
	st := store.New(store.Options{})
	updates := make(chan store.State, 1)
	reg.mu.Lock()
	reg.pluginStore = st
	reg.mu.Unlock()
	go reg.relayPluginStoreUpdates(st, updates)
	listing.Err = errors.New("updated from relay")
	updates <- store.State{Listings: []store.Listing{listing}}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		rendered := pluginStoreHasText(host.root, "updated from relay")
		reg.mu.Unlock()
		if rendered {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("store update did not rebuild the open Settings Plugins page")
}

func TestPluginStoreRelaySkipsREADMEWorkWhenPanelsAreClosed(t *testing.T) {
	readme := &catalog.Screenshot{URL: "https://example.com/readme.md", SHA256: "readme-sha"}
	listing := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.5.0")
	listing.Entry.Readme = readme
	reg := newPanelRegistry(t)
	st := store.New(store.Options{})
	updates := make(chan store.State, 1)
	reg.mu.Lock()
	reg.pluginStore = st
	reg.mu.Unlock()
	go reg.relayPluginStoreUpdates(st, updates)
	path := filepath.Join(t.TempDir(), "README.md")
	if err := os.WriteFile(path, []byte("README not needed"), 0o600); err != nil {
		t.Fatal(err)
	}
	updates <- store.State{
		Listings: []store.Listing{listing},
		Media:    map[string]store.MediaState{readme.SHA256: {Path: path}},
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		published := len(reg.pluginStoreSnapshot.Listings) == 1
		readmes := len(reg.pluginStoreReadmes)
		closed := reg.panelHosts[PanelPluginStore] == nil && reg.panelHosts[PanelSettings] == nil
		reg.mu.Unlock()
		if published {
			if !closed || readmes != 0 {
				t.Fatal("closed panels retained or loaded README content")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("closed-panel relay did not publish the worker snapshot")
}
