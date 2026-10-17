package shell

import (
	"errors"
	"strings"

	launcher "github.com/Nomadcxx/sysc-launch"

	"github.com/Nomadcxx/sysc-shell/internal/calc"
	"github.com/Nomadcxx/sysc-shell/internal/emoji"
	"github.com/Nomadcxx/sysc-shell/internal/i18n"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

// launcherProviders is the shell's contribution to sysc-launch's provider
// table. Provider functions run on the service goroutine while the caller
// of Activate waits without holding Registry.mu, so they may take it.
func (r *Registry) launcherProviders() []launcher.Provider {
	providers := []launcher.Provider{r.calcProvider(), r.emojiProvider(), r.notesProvider(), r.sessionProvider()}
	for i := range providers {
		prefix, query := providers[i].Prefix, providers[i].Query
		providers[i].Query = func(q string) []launcher.Result {
			rows := query(q)
			r.noteLauncherRows(prefix, q, rows)
			return rows
		}
	}
	return providers
}

const launcherAppsPrefix = "/apps"

const calcHintComment = "Try 6*7 · sqrt(2) · 2^10 · sin(pi/2)"

func (r *Registry) calcProvider() launcher.Provider {
	const prefix = "/calc"
	return launcher.Provider{
		Name: "Calculator", Prefix: prefix, Glyph: "glyph:calculate", Inline: true,
		Description: "Arithmetic as you type · Enter copies",
		Query: func(q string) []launcher.Result {
			if !calc.IsExpression(q) {
				return nil
			}
			v, err := calc.Eval(q)
			if err != nil {
				return nil
			}
			s := calc.Format(v)
			return []launcher.Result{{Entry: launcher.Entry{ID: "calc:" + s, Name: "= " + s,
				Comment: strings.TrimSpace(q) + " · Enter copies", IconName: "glyph:calculate"}}}
		},
		Activate: func(_, id, _ string) error { return r.launcherCopy(strings.TrimPrefix(id, "calc:")) },
	}
}

// launcherWithHints adds the /calc hint when the calculator had nothing to
// show for an explicit /calc query.
func launcherWithHints(query string, results []launcher.Result) []launcher.Result {
	q := strings.TrimSpace(query)
	if len(results) == 0 && (q == "/calc" || strings.HasPrefix(q, "/calc ")) {
		return []launcher.Result{{Entry: launcher.Entry{
			Name: "Invalid expression", Comment: calcHintComment, IconName: "glyph:calculate"}}}
	}
	return results
}

func (r *Registry) emojiProvider() launcher.Provider {
	const prefix = "/emo"
	return launcher.Provider{
		Name: "Emoji", Prefix: prefix, Glyph: "glyph:mood",
		Description: "Search emoji by name · Enter copies",
		Query: func(q string) []launcher.Result {
			hits := emoji.Search(q, 50)
			out := make([]launcher.Result, 0, len(hits))
			for _, e := range hits {
				kw := e.Keywords[:min(3, len(e.Keywords))]
				out = append(out, launcher.Result{Entry: launcher.Entry{
					ID: e.Char, Name: e.Name, Comment: strings.Join(kw, " · "), IconName: "text:" + e.Char}})
			}
			return out
		},
		Activate: func(_, id, _ string) error { return r.launcherCopy(id) },
	}
}

// launcherCopy puts text on the system clipboard with the serial of the key
// or pointer press that activated the row.
func (r *Registry) launcherCopy(text string) error {
	r.mu.Lock()
	h := r.panelHosts[PanelLauncher]
	var serial uint32
	if h != nil {
		serial = h.inputSerial
	}
	r.mu.Unlock()
	if text == "" {
		return errors.New("nothing to copy")
	}
	r.requestSelection(wayland.SelectionRequest{Copy: text, Serial: serial})
	return nil
}

// launcherServiceConfig is the service wiring shared by the live launcher and
// its tests; the caller adds Scan, Run and History as it needs.
func (r *Registry) launcherServiceConfig() launcher.ServiceConfig {
	r.ensureLauncherSnaps()
	return launcher.ServiceConfig{
		Rank:              r.rankLauncher,
		Providers:         r.launcherProviders(),
		ApplicationsGlyph: "glyph:apps",
	}
}

func (r *Registry) sessionProvider() launcher.Provider {
	const prefix = "/session"
	return launcher.Provider{
		Name: "Session", Prefix: prefix, Glyph: "glyph:power_settings_new",
		Inline:      true,
		Description: "Session and power actions",
		Query: func(query string) []launcher.Result {
			r.mu.Lock()
			language := r.cfg.Session.Language
			r.mu.Unlock()
			locale := i18n.Resolve(language)
			query = strings.ToLower(strings.TrimSpace(query))
			actions := sessionActionOptions()
			rows := make([]launcher.Result, 0, len(actions)-1)
			for _, action := range actions {
				if action.id == "session-lock" {
					continue
				}
				name := i18n.T(locale, "session.action."+action.name, action.name)
				if query != "" && !strings.Contains(strings.ToLower(name), query) &&
					!strings.Contains(strings.ToLower(action.name), query) {
					continue
				}
				rows = append(rows, launcher.Result{Entry: launcher.Entry{
					ID: action.id, Name: name, IconName: "glyph:" + action.icon,
				}})
			}
			return rows
		},
		Activate: func(_, id, _ string) error {
			if id == "session-lock" || sessionArgv(id, "") == nil {
				return errors.New("launcher: unsupported session action")
			}
			r.mu.Lock()
			defer r.mu.Unlock()
			h := r.panelHosts[PanelLauncher]
			if h == nil {
				return errors.New("launcher closed")
			}
			r.runSessionAction(h, id)
			return nil
		},
	}
}

func (r *Registry) notesProvider() launcher.Provider {
	const prefix = "/nt"
	return launcher.Provider{
		Name: "Notes", Prefix: prefix, Glyph: "glyph:description",
		Description: "Search notes or capture with /nt <text>",
		Query: func(q string) []launcher.Result {
			rows, _ := notesLauncherResults(strings.TrimSpace("/nt "+q), cfgT(r.cfg))
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
