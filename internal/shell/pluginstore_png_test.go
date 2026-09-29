package shell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
)

// pluginStorePNGListings is the first-party catalog the grid is judged
// against: ten rows, one installed, one with an update, one local override.
func pluginStorePNGListings() []store.Listing {
	rows := []struct{ id, name, category, version string }{
		{"org.sysc.timer", "Timer", "productivity", "1.4.0"},
		{"org.sysc.calendar", "Calendar", "productivity", "1.2.0"},
		{"org.sysc.weather", "Weather", "utilities", "2.1.0"},
		{"org.sysc.screen-recorder", "Screen Recorder", "utilities", "1.0.3"},
		{"org.sysc.github-notifications", "GitHub Notifications", "development", "0.9.0"},
		{"org.sysc.protonvpn", "ProtonVPN", "system", "1.1.0"},
		{"org.sysc.kdeconnect", "Phone Connect", "system", "0.4.0"},
		{"org.sysc.aiusage", "AI Usage", "monitoring", "1.0.0"},
		{"org.sysc.games", "Games", "fun", "0.3.0"},
		{"org.sysc.wallpaper-depth", "Wallpaper Depth", "appearance", "1.0.0"},
	}
	listings := make([]store.Listing, 0, len(rows))
	for _, row := range rows {
		l := modelListing(row.id, row.name, "sysc", row.category, row.version)
		l.Entry.Description = row.name + " for the sysc shell, with its own panel, bar widget and settings."
		l.Entry.Author = "Nomadcxx"
		l.Entry.License = "MIT"
		listings = append(listings, l)
	}
	listings[0].Installed = &store.Record{Source: "sysc", Version: "1.4.0"}
	listings[0].Status = store.StatusInstalled
	listings[1].Installed = &store.Record{Source: "sysc", Version: "1.1.0", Previous: &store.Record{Version: "1.0.0"}}
	listings[1].Status = store.StatusUpdateAvailable
	listings[1].UpdateAvailable = true
	listings[2].LocalDir = "/home/user/.config/sysc-shell/plugins/weather"
	listings[2].Installed = &store.Record{Source: "sysc", Version: "2.0.0"}
	listings[2].Status = store.StatusShadowed
	listings[2].UpdateAvailable = true
	return listings
}

const pluginStorePNGReadme = `# Timer

A countdown and stopwatch for the bar.

## Features

- Start, pause and reset from the panel
- Presets for **five**, **ten** and **twenty-five** minutes
- A notification when time is up

## Configuration

` + "```" + `
"org.sysc.timer": { "presets": [5, 10, 25] }
` + "```" + `

See the [project page](https://github.com/Nomadcxx/sysc-plugins) for more.
`

// TestPluginStoreRendersToPNG paints every state the design's verification
// bar names, at the desktop's 1.0 and the laptop's 1.25. The PNGs are for the
// eye; SYSC_PLUGIN_STORE_PNG_DIR keeps them somewhere other than a temp
// directory.
func TestPluginStoreRendersToPNG(t *testing.T) {
	dir := os.Getenv("SYSC_PLUGIN_STORE_PNG_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	outputs := []struct {
		scale120 int
		out      ui.Rect
	}{
		{120, ui.Rect{W: 3440, H: 1440}},
		{150, ui.Rect{W: 1536, H: 864}},
	}
	listings := pluginStorePNGListings()
	sources := []store.SourceState{{Name: "sysc", URL: config.BuiltinPluginSource.URL, FetchedAt: time.Now(), Plugins: len(listings)}}
	readmeSHA := "readme-sha"
	withReadme := func() []store.Listing {
		out := pluginStorePNGListings()
		out[0].Entry.Readme = &catalog.Screenshot{URL: "https://example.com/README.md", SHA256: readmeSHA}
		return out
	}
	grid := []struct {
		name   string
		state  store.State
		detail int // index into Listings, or -1 for the grid
		setup  func(*Registry, *PanelHost)
	}{
		{name: "grid-results", state: store.State{Sources: sources, Listings: listings}, detail: -1},
		{name: "grid-loading", state: store.State{Sources: sources, Busy: "refreshing sources"}, detail: -1},
		{name: "grid-empty", state: store.State{Sources: sources, Listings: listings}, detail: -1, setup: func(_ *Registry, h *PanelHost) {
			h.search = ui.NewField("nothing matches")
			h.pluginStoreQuery.Text = "nothing matches"
		}},
		{name: "grid-failed", state: store.State{Sources: []store.SourceState{{Name: "sysc", URL: config.BuiltinPluginSource.URL, Err: errors.New("git fetch: could not resolve host github.com")}}}, detail: -1},
		{name: "detail-readme", state: store.State{Sources: sources, Listings: withReadme(), Media: map[string]store.MediaState{readmeSHA: {Path: "/nonexistent"}}}, detail: 0, setup: func(r *Registry, _ *PanelHost) {
			r.pluginStoreReadmes = map[string]string{readmeSHA: pluginStorePNGReadme}
		}},
		{name: "detail-plain", state: store.State{Sources: sources, Listings: listings}, detail: 3},
		{name: "detail-consent", state: store.State{Sources: sources, Listings: listings}, detail: 3, setup: func(r *Registry, h *PanelHost) {
			h.roving.Set(pluginStoreFocusIndex(h, "store-primary"))
			h.activatePluginStore(r, pluginStoreFindAction(h.root, "store-primary"))
		}},
	}
	for _, o := range outputs {
		for _, tc := range grid {
			name := fmt.Sprintf("store-%s-%d", tc.name, o.scale120)
			t.Run(name, func(t *testing.T) {
				reg, host, panel := openPluginStoreTestPanel(t, tc.state, o.out)
				reg.mu.Lock()
				if tc.detail >= 0 {
					host.pluginStoreDetail = pluginStoreKey(tc.state.Listings[tc.detail])
					reg.rebuildPanel(host)
				}
				if tc.setup != nil {
					tc.setup(reg, host)
				}
				reg.rebuildPanel(host)
				reg.mu.Unlock()
				settleHostAnimation(reg, host)
				paintPluginStorePNG(t, panel, o.scale120, filepath.Join(dir, name+".png"))
			})
		}
		manager := []struct {
			name, tab string
			cfg       func(*config.Config)
			state     store.State
			setup     func(*Registry, *PanelHost)
		}{
			{name: "installed", tab: "installed", state: store.State{Sources: sources, Listings: listings}},
			{name: "installed-errors", tab: "installed", state: store.State{Sources: sources, Listings: func() []store.Listing {
				out := pluginStorePNGListings()
				out[0].Err = errors.New("plugin failed to start: read plugin.hello: EOF")
				return append(out, store.Listing{
					Source: "gone", Entry: catalog.Entry{ID: "org.sysc.orphan", Name: "Orphan"},
					Installed: &store.Record{Source: "gone", Version: "0.9.0"}, Status: store.StatusUnlisted,
				})
			}()}},
			{name: "sources", tab: "sources", cfg: func(c *config.Config) {
				c.Plugins.Sources = []config.PluginSource{{Name: "work", URL: "https://git.example.com/plugins", Enabled: true}}
			}, state: store.State{Sources: append(sources, store.SourceState{
				Name: "work", URL: "https://git.example.com/plugins", FetchedAt: time.Now().Add(-26 * time.Hour), Plugins: 3,
				Err: errors.New("offline"),
			})}},
			{name: "sources-add-warning", tab: "sources", state: store.State{Sources: sources}, setup: func(r *Registry, h *PanelHost) {
				h.fields = map[string]*ui.Field{
					"plugins-source-name": ui.NewField("community"),
					"plugins-source-url":  ui.NewField("https://github.com/Nomadcxx/sysc-community-plugins"),
				}
				r.handlePluginManager(h, &ui.Node{Action: "plugins-source-add"})
			}},
		}
		for _, tc := range manager {
			name := fmt.Sprintf("settings-%s-%d", tc.name, o.scale120)
			t.Run(name, func(t *testing.T) {
				cfg := config.Default()
				if tc.cfg != nil {
					tc.cfg(&cfg)
				}
				reg, host, panel := openPluginManagerTestPanelOn(t, cfg, tc.state, o.out)
				reg.mu.Lock()
				host.pluginManagerTab = tc.tab
				reg.rebuildPanel(host)
				if tc.setup != nil {
					tc.setup(reg, host)
				}
				reg.rebuildPanel(host)
				reg.mu.Unlock()
				settleHostAnimation(reg, host)
				paintPluginStorePNG(t, panel, o.scale120, filepath.Join(dir, name+".png"))
			})
		}
	}
}

func paintPluginStorePNG(t *testing.T, panel *wayland.AuxSpec, scale120 int, path string) {
	t.Helper()
	w, h := int(panel.Width), int(panel.Height)
	if err := panel.Callbacks.Configure(w, h, scale120); err != nil {
		t.Fatal(err)
	}
	s := ui.Scale120(scale120)
	pw, ph := s.Physical(w), s.Physical(h)
	pix := make([]byte, pw*ph*4)
	if err := panel.Callbacks.Render(pix, pw, ph, pw*4); err != nil {
		t.Fatal(err)
	}
	writeCardPNG(t, path, pix, pw, ph)
}
