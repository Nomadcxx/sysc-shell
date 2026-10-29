package shell

import (
	"context"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	launcher "github.com/Nomadcxx/sysc-launch"

	clipboardprotocol "github.com/Nomadcxx/sysc-clipboard/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/calc"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/emoji"
	"github.com/Nomadcxx/sysc-shell/internal/i18n"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

// launcherProviders is the shell's contribution to sysc-launch's provider
// table. Provider functions run on the service goroutine while the caller
// of Activate waits without holding Registry.mu, so they may take it.
func (r *Registry) launcherProviders() []launcher.Provider {
	providers := []launcher.Provider{
		r.calcProvider(), r.emojiProvider(), r.clipboardProvider(), r.windowsProvider(), r.notesProvider(), r.sessionProvider(), r.launcherActionProvider(),
	}
	for _, cfg := range r.cfg.Launcher.Providers {
		cfg := cfg
		cfg.OnSelect = slices.Clone(cfg.OnSelect)
		providers = append(providers, launcher.CommandProvider(launcher.CommandProviderConfig{
			Name: cfg.Name, Prefix: cfg.Prefix, Glyph: cfg.Glyph, Description: cfg.Description,
			Command: cfg.Command,
			OnSelect: func(query, selection string) error {
				r.mu.Lock()
				h := r.panelHosts[PanelLauncher]
				if h == nil {
					r.mu.Unlock()
					return errors.New("launcher closed")
				}
				attempt := h.launcherPendingAttempt
				if attempt == 0 || h.launcherAttempt != attempt {
					r.mu.Unlock()
					return errors.New("launcher: command provider activation is stale")
				}
				run := r.runArgv
				r.mu.Unlock()

				if run == nil {
					return errors.New("launcher: command runner unavailable")
				}
				return run(launcherSelectionArgv(cfg.OnSelect, query, selection))
			},
		}))
	}
	r.launcherMu.Lock()
	providerGeneration := r.launcherProviderGeneration
	r.launcherMu.Unlock()
	for i := range providers {
		prefix, query := providers[i].Prefix, providers[i].Query
		providers[i].Query = func(q string) []launcher.Result {
			rows := query(q)
			r.noteLauncherRowsForGeneration(providerGeneration, prefix, q, rows)
			return rows
		}
	}
	return providers
}

const launcherWindowResultPrefix = "window:"

func (r *Registry) windowsProvider() launcher.Provider {
	return launcher.Provider{
		Name: "Windows", Prefix: config.LauncherWindowsPrefix, Glyph: "glyph:desktop_windows",
		Description: "Browse open windows · Search titles and applications",
		Query: func(query string) []launcher.Result {
			r.mu.Lock()
			entries := make([]launcher.Entry, 0)
			for _, app := range r.running {
				for _, window := range app.Members {
					name := window.Title
					if name == "" {
						name = app.Name
					}
					entry := launcher.Entry{
						ID:   launcherWindowResultPrefix + strconv.FormatUint(window.ID, 10),
						Name: name, Comment: app.Name, IconName: app.Icon,
					}
					if window.AppID != "" {
						entry.Keywords = []string{window.AppID}
					}
					entries = append(entries, entry)
				}
			}
			r.mu.Unlock()
			return launcherRank(entries, query, nil)
		},
		Activate: func(_, id, _ string) error {
			value, ok := strings.CutPrefix(id, launcherWindowResultPrefix)
			if !ok || value == "" {
				return errors.New("launcher: unsupported window")
			}
			windowID, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return errors.New("launcher: invalid window ID")
			}

			r.mu.Lock()
			found := false
			for _, app := range r.running {
				for _, window := range app.Members {
					if window.ID == windowID {
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			send := r.niriSend
			r.mu.Unlock()
			if !found {
				return errors.New("launcher: window no longer exists")
			}

			action := niri.FocusWindow{ID: windowID}
			if send != nil {
				return send(action)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			return niri.Action(ctx, os.Getenv("NIRI_SOCKET"), action)
		},
	}
}

const launcherActionResultPrefix = "launcher-action:"

func (r *Registry) launcherActionProvider() launcher.Provider {
	const prefix = config.LauncherActionsPrefix
	return launcher.Provider{
		Name: "Actions", Prefix: prefix, Glyph: "glyph:terminal", Inline: true,
		Description: "Configured command actions",
		Query: func(query string) []launcher.Result {
			r.mu.Lock()
			actions := slices.Clone(r.cfg.Launcher.Actions)
			r.mu.Unlock()

			query = strings.ToLower(strings.TrimSpace(query))
			rows := make([]launcher.Result, 0, len(actions))
			for _, action := range actions {
				if action.Dangerous && !action.AllowDangerous {
					continue
				}
				if query != "" && !strings.Contains(strings.ToLower(action.Label), query) &&
					!strings.Contains(strings.ToLower(action.Comment), query) {
					continue
				}
				rows = append(rows, launcher.Result{Entry: launcher.Entry{
					ID: launcherActionResultPrefix + action.ID, Name: action.Label,
					Comment: action.Comment, IconName: "glyph:terminal",
				}})
			}
			return rows
		},
		Activate: func(_, id, _ string) error {
			actionID, ok := strings.CutPrefix(id, launcherActionResultPrefix)
			if !ok || actionID == "" {
				return errors.New("launcher: unsupported configured action")
			}

			r.mu.Lock()
			h := r.panelHosts[PanelLauncher]
			if h == nil {
				r.mu.Unlock()
				return errors.New("launcher closed")
			}
			attempt := h.launcherPendingAttempt
			if attempt == 0 || h.launcherAttempt != attempt {
				r.mu.Unlock()
				return errors.New("launcher: configured action activation is stale")
			}
			var selected *config.LauncherAction
			for i := range r.cfg.Launcher.Actions {
				if r.cfg.Launcher.Actions[i].ID == actionID {
					action := r.cfg.Launcher.Actions[i]
					action.Argv = slices.Clone(action.Argv)
					selected = &action
					break
				}
			}
			run := r.runArgv
			r.mu.Unlock()

			if selected == nil {
				return errors.New("launcher: configured action no longer exists")
			}
			if selected.Dangerous && !selected.AllowDangerous {
				return errors.New("launcher: dangerous action requires explicit opt-in")
			}
			if run == nil {
				return errors.New("launcher: command runner unavailable")
			}
			return run(selected.Argv)
		},
	}
}

const launcherAppsPrefix = config.LauncherAppsPrefix

func cloneLauncherCommandProviders(providers []config.LauncherCommandProvider) []config.LauncherCommandProvider {
	cloned := slices.Clone(providers)
	for i := range cloned {
		cloned[i].Command = slices.Clone(cloned[i].Command)
		cloned[i].OnSelect = slices.Clone(cloned[i].OnSelect)
	}
	return cloned
}

func cloneLauncherActions(actions []config.LauncherAction) []config.LauncherAction {
	cloned := slices.Clone(actions)
	for i := range cloned {
		cloned[i].Argv = slices.Clone(cloned[i].Argv)
	}
	return cloned
}

func sameLauncherCommandProviders(a, b []config.LauncherCommandProvider) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Prefix != b[i].Prefix || a[i].Glyph != b[i].Glyph ||
			a[i].Description != b[i].Description || !slices.Equal(a[i].Command, b[i].Command) ||
			!slices.Equal(a[i].OnSelect, b[i].OnSelect) {
			return false
		}
	}
	return true
}

func sameLauncherActions(a, b []config.LauncherAction) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Label != b[i].Label || a[i].Comment != b[i].Comment ||
			a[i].Dangerous != b[i].Dangerous || a[i].AllowDangerous != b[i].AllowDangerous ||
			!slices.Equal(a[i].Argv, b[i].Argv) {
			return false
		}
	}
	return true
}

func launcherSelectionArgv(template []string, query, selection string) []string {
	argv := make([]string, len(template))
	replacer := strings.NewReplacer("{query}", query, "{selection}", selection)
	for i, arg := range template {
		argv[i] = replacer.Replace(arg)
	}
	return argv
}

const calcHintComment = "Try 6*7 · sqrt(2) · 2^10 · sin(pi/2) · 12 km to mi"

func (r *Registry) calcProvider() launcher.Provider {
	const prefix = config.LauncherCalcPrefix
	return launcher.Provider{
		Name: "Calculator", Prefix: prefix, Glyph: "glyph:calculate", Inline: true,
		Description: "Arithmetic and conversions as you type · Enter copies",
		Query: func(q string) []launcher.Result {
			v, unit, converted := calc.ConvertLength(q)
			if !converted {
				if !calc.IsExpression(q) {
					return nil
				}
				var err error
				v, err = calc.Eval(q)
				if err != nil {
					return nil
				}
			}
			s := calc.Format(v)
			if unit != "" {
				s += " " + unit
			}
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
	const prefix = config.LauncherEmojiPrefix
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

const (
	launcherClipboardPrefix       = "/clipboard"
	launcherClipboardResultPrefix = "sysc-clipboard-history:"
)

func (r *Registry) clipboardProvider() launcher.Provider {
	return launcher.Provider{
		Name: "Clipboard History", Prefix: launcherClipboardPrefix, Glyph: "glyph:content_paste",
		Description: "Search with /clipboard · Enter pastes",
		Query: func(query string) []launcher.Result {
			query = strings.TrimSpace(query)
			r.mu.Lock()
			if !r.clipboard.Connected || r.clipboard.Snapshot.Wayland != clipboardprotocol.WaylandReady {
				r.mu.Unlock()
				return nil
			}
			entries := make([]launcher.Entry, 0, len(r.clipboard.Snapshot.Entries))
			for _, item := range r.clipboard.Snapshot.Entries {
				name := clipboardPreview(item)
				icon := "glyph:content_paste"
				if item.Kind == clipboardprotocol.KindImage {
					name = "Image · " + item.MIME
					icon = "glyph:image"
				} else if name == "" {
					name = "Text · " + item.MIME
				}
				entries = append(entries, launcher.Entry{
					ID: launcherClipboardResultPrefix + item.ID, Name: name,
					Comment: string(item.Kind) + " · " + item.MIME, IconName: icon,
				})
			}
			r.mu.Unlock()

			if query != "" {
				return launcherRank(entries, query, nil)
			}
			if len(entries) > launcherSearchLimit {
				entries = entries[:launcherSearchLimit]
			}
			rows := make([]launcher.Result, 0, len(entries))
			for _, entry := range entries {
				rows = append(rows, launcher.Result{Entry: entry})
			}
			return rows
		},
		Activate: func(_, id, _ string) error {
			entryID, ok := strings.CutPrefix(id, launcherClipboardResultPrefix)
			if !ok || entryID == "" {
				return errors.New("launcher: unsupported clipboard entry")
			}

			r.mu.Lock()
			h := r.panelHosts[PanelLauncher]
			if h == nil {
				r.mu.Unlock()
				return errors.New("launcher closed")
			}
			attempt := h.launcherPendingAttempt
			if attempt == 0 || h.launcherAttempt != attempt {
				r.mu.Unlock()
				return errors.New("launcher: clipboard activation is stale")
			}
			if !r.clipboard.Connected || r.clipboard.Snapshot.Wayland != clipboardprotocol.WaylandReady {
				r.mu.Unlock()
				return errors.New("clipboard daemon unavailable")
			}
			if _, exists := clipboardEntryByID(r.clipboard.Snapshot.Entries, entryID); !exists {
				r.mu.Unlock()
				return errors.New("clipboard entry no longer exists")
			}
			sender, lookPath, run := r.clipboardSender, r.lookPath, r.runArgv
			r.mu.Unlock()

			if lookPath == nil || run == nil {
				return errors.New("launcher: paste command unavailable")
			}
			if _, err := lookPath("wtype"); err != nil {
				return errors.New("launcher: install wtype to paste clipboard history")
			}
			completion, err := r.enqueueClipboardRestore(sender, entryID)
			if err != nil {
				return err
			}
			return r.waitClipboardCommand(completion)
		},
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

// launcherRun starts an activated entry through the compositor. The spawn argv
// arrives as "niri msg action spawn --" plus the command, the same shape the
// launcher's own default used. The env helper runs inside Niri's spawn so the
// token reaches the application process rather than the IPC client.
//
// sysc-launch v0.5.0 starts the desktop command directly when no Run is
// configured. That would make the shell the parent of every application it
// launches, leave the compositor unaware of them, and give them the shell's
// lifetime. The shell is Niri first, so it names the spawn path itself.
func (r *Registry) launcherRun(ctx context.Context, argv []string) error {
	if len(argv) == 0 {
		return errors.New("launcher: empty spawn argv")
	}
	r.mu.Lock()
	run := r.runArgv
	serial := r.launcherActivationSerial
	r.mu.Unlock()
	if run == nil {
		return errors.New("launcher: no spawn runner is wired")
	}
	if token := r.requestActivationToken(ctx, serial); token != "" {
		argv = niriSpawnWithActivationToken(argv, token)
	}
	return run(argv)
}

func niriSpawnWithActivationToken(argv []string, token string) []string {
	prefix := []string{"niri", "msg", "action", "spawn", "--"}
	if token == "" || strings.ContainsRune(token, '\x00') ||
		len(argv) < len(prefix) || !slices.Equal(argv[:len(prefix)], prefix) {
		return argv
	}
	spawn := make([]string, 0, len(argv)+2)
	spawn = append(spawn, argv[:len(prefix)]...)
	spawn = append(spawn, "env", "XDG_ACTIVATION_TOKEN="+token)
	return append(spawn, argv[len(prefix):]...)
}

// launcherServiceConfig is the service wiring shared by the live launcher and
// its tests; the caller adds Scan and History as it needs.
func (r *Registry) launcherServiceConfig() launcher.ServiceConfig {
	r.ensureLauncherSnaps()
	r.launcherMu.Lock()
	providerGeneration := r.launcherProviderGeneration
	r.launcherMu.Unlock()
	return launcher.ServiceConfig{
		Rank: func(entries []launcher.Entry, query string, boost func(query, identifier string) int) []launcher.Result {
			return r.rankLauncher(providerGeneration, entries, query, boost)
		},
		Providers:         r.launcherProviders(),
		ApplicationsGlyph: "glyph:apps",
		Run:               r.launcherRun,
		TerminalDesktopID: func() string {
			if prefs := r.launcherAppPrefs.Load(); prefs != nil {
				return prefs.defaultTerminalID
			}
			return ""
		},
	}
}

func (r *Registry) sessionProvider() launcher.Provider {
	const prefix = config.LauncherSessionPrefix
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
			attempt := h.launcherPendingAttempt
			if attempt == 0 || h.launcherAttempt != attempt {
				return errors.New("launcher: session activation is stale")
			}
			r.runSessionActionAtAttempt(h, id, attempt)
			return nil
		},
	}
}

func (r *Registry) storeLauncherAppPreferences(prefs config.ApplicationPreferences) {
	r.launcherAppPrefs.Store(newLauncherAppPreferenceSnapshot(prefs))
}

func (r *Registry) notesProvider() launcher.Provider {
	const prefix = config.LauncherNotesPrefix
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
