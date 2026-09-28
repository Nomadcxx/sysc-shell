package shell

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
)

// BindPluginStore attaches the plugin store. The store runs its own goroutine;
// the registry answers what it asks (enabled sources, local copies) and lends
// it the plugin host to swap directories under.
func (r *Registry) BindPluginStore(s *store.Store) {
	snapshot := s.State()
	r.mu.Lock()
	r.pluginStore = s
	r.pluginStoreSnapshot = snapshot
	r.mu.Unlock()
	go r.relayPluginStore(s)
}

// relayPluginStore publishes worker snapshots to the open Settings Plugins
// page and the store panel, off the Wayland owner like relayWallpaper. A
// README is read only for an open detail, and always outside Registry.mu.
func (r *Registry) relayPluginStore(st *store.Store) {
	r.relayPluginStoreUpdates(st, st.Updates())
}

func (r *Registry) relayPluginStoreUpdates(st *store.Store, updates <-chan store.State) {
	for {
		select {
		case <-r.closed:
			return
		case snapshot, ok := <-updates:
			if !ok {
				return
			}
			r.mu.Lock()
			if r.pluginStore != st {
				r.mu.Unlock()
				continue
			}
			storeHost := r.panelHosts[PanelPluginStore]
			detailKey := ""
			if storeHost != nil {
				detailKey = storeHost.pluginStoreDetail
			}
			readmeSHA, readmePath := pluginStoreReadmeTarget(snapshot, detailKey)
			r.mu.Unlock()

			readme := ""
			if readmePath != "" {
				readme = readPluginStoreReadme(readmePath)
			}

			r.mu.Lock()
			if r.pluginStore != st {
				r.mu.Unlock()
				continue
			}
			r.pluginStoreSnapshot = snapshot
			r.pluginStoreReadmes = nil
			storeHost = r.panelHosts[PanelPluginStore]
			if storeHost != nil && storeHost.pluginStoreDetail == detailKey && readmeSHA != "" && readme != "" {
				r.pluginStoreReadmes = map[string]string{readmeSHA: readme}
			}

			type publication struct {
				output uint32
				panel  PanelID
			}
			var publish []publication
			if settingsHost := r.panelHosts[PanelSettings]; settingsHost != nil && settingsHost.section == "Plugins" {
				r.rebuildPanel(settingsHost)
				publish = append(publish, publication{settingsHost.output, PanelSettings})
			}
			if storeHost != nil {
				r.rebuildPanel(storeHost)
				publish = append(publish, publication{storeHost.output, PanelPluginStore})
			}
			r.mu.Unlock()

			for _, p := range publish {
				r.publishSurface(p.output, panelSurfaceID(p.panel))
			}
		}
	}
}

func pluginStoreReadmeTarget(snapshot store.State, key string) (sha, path string) {
	if key == "" {
		return "", ""
	}
	listing, ok := pluginStoreFind(snapshot.Listings, key)
	if !ok || listing.Entry.Readme == nil {
		return "", ""
	}
	sha = listing.Entry.Readme.SHA256
	media, ok := snapshot.Media[sha]
	if !ok || media.Err != nil || media.Path == "" {
		return sha, ""
	}
	return sha, media.Path
}

func readPluginStoreReadme(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, catalog.MaxReadmeBytes+1))
	if err != nil || int64(len(data)) > catalog.MaxReadmeBytes {
		return ""
	}
	return string(data)
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
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
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
		_, err = s.Install(p.Source, p.ID, store.ReleaseRef{Version: p.Version, SHA256: p.SHA256})
	case "plugins.update":
		_, err = s.Update(p.ID, store.ReleaseRef{Version: p.Version, SHA256: p.SHA256}, p.Confirm)
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
			"local_dir": l.LocalDir, "error": errText(l.Err), "update_available": l.UpdateAvailable,
		}
		if l.Resolution.Release != nil {
			row["version"] = l.Resolution.Release.Version
		}
		if l.Installed != nil {
			row["installed_version"] = l.Installed.Version
		}
		if l.Status == store.StatusIncompatible {
			if l.Resolution.NoAsset {
				row["reason"] = "no asset for this machine"
			} else {
				row["reason"] = fmt.Sprintf("needs protocol %d.%d", l.Resolution.Needs.Major, l.Resolution.Needs.Minor)
			}
		}
		listings = append(listings, row)
	}
	return map[string]any{"busy": st.Busy, "sources": sources, "listings": listings}
}
