package shell

import (
	"errors"
	"strings"

	launcher "github.com/Nomadcxx/sysc-launch"
)

// launcherProviders is the shell's contribution to sysc-launch's provider
// table. Provider functions run on the service goroutine while the caller
// of Activate waits without holding Registry.mu, so they may take it.
func (r *Registry) launcherProviders() []launcher.Provider {
	return []launcher.Provider{r.notesProvider()}
}

// launcherServiceConfig is the service wiring shared by the live launcher and
// its tests; the caller adds Scan, Run and History as it needs.
func (r *Registry) launcherServiceConfig() launcher.ServiceConfig {
	return launcher.ServiceConfig{
		Rank:              launcherRank,
		Providers:         r.launcherProviders(),
		ApplicationsGlyph: "glyph:apps",
	}
}

func (r *Registry) notesProvider() launcher.Provider {
	return launcher.Provider{
		Name: "Notes", Prefix: "/nt", Glyph: "glyph:description",
		Description: "Search notes or capture with /nt <text>",
		Query: func(q string) []launcher.Result {
			rows, _ := notesLauncherResults(strings.TrimSpace("/nt " + q))
			return rows
		},
		// launcherNotesAction reads the capture body from the panel's query
		// and closes the panel (or shows its error) itself.
		Activate: func(_, id, _ string) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			h := r.panelHosts[PanelLauncher]
			if h == nil {
				return errors.New("launcher closed")
			}
			h.launcherNotesAction(r, id)
			return nil
		},
	}
}
