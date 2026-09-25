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
