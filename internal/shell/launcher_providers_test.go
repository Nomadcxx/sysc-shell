package shell

import (
	"context"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	launcher "github.com/Nomadcxx/sysc-launch"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

func TestLauncherServiceConfigReadsDefaultTerminalAfterConfigReload(t *testing.T) {
	r := newPanelRegistry(t)
	next := config.Default()
	next.Applications.DefaultTerminal = "foot"
	prepared, err := r.PrepareConfig(next, nil)
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	prepared.Commit()

	serviceConfig := r.launcherServiceConfig()
	if serviceConfig.TerminalDesktopID == nil {
		t.Fatal("launcher service has no live terminal preference callback")
	}
	if got := serviceConfig.TerminalDesktopID(); got != "foot" {
		t.Fatalf("selected terminal ID = %q, want foot", got)
	}
}

func TestLauncherServiceUsesSelectedTerminalAfterConfigReload(t *testing.T) {
	r := newPanelRegistry(t)
	next := config.Default()
	next.Applications.DefaultTerminal = "foot"
	prepared, err := r.PrepareConfig(next, nil)
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	prepared.Commit()

	var spawned []string
	serviceConfig := r.launcherServiceConfig()
	serviceConfig.Scan = func() []launcher.Entry {
		return []launcher.Entry{
			{ID: "foot", Name: "Foot", Argv: []string{"foot"}, TerminalApp: true},
			{ID: "editor", Name: "Editor", Argv: []string{"system-terminal", "-e", "editor"},
				ExecArgv: []string{"editor"}, Terminal: true},
		}
	}
	serviceConfig.LookPath = func(name string) (string, error) {
		if name == "foot" {
			return "/usr/bin/foot", nil
		}
		return "", exec.ErrNotFound
	}
	serviceConfig.Run = func(_ context.Context, argv []string) error {
		spawned = slices.Clone(argv)
		return nil
	}
	svc := launcher.NewService(serviceConfig)
	defer svc.Close()
	<-svc.Results()
	if err := svc.Activate("editor", ""); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	want := []string{"niri", "msg", "action", "spawn", "--", "/usr/bin/foot", "-e", "editor"}
	if !slices.Equal(spawned, want) {
		t.Fatalf("spawn argv = %v, want %v", spawned, want)
	}
}

func TestCalcProviderRows(t *testing.T) {
	r := newPanelRegistry(t)
	p := r.calcProvider()
	if !p.Inline || p.Prefix != "/calc" || p.Glyph != "glyph:calculate" {
		t.Fatalf("provider %+v", p)
	}
	rows := p.Query("6*7")
	if len(rows) != 1 || rows[0].Entry.ID != "calc:42" || rows[0].Entry.Name != "= 42" ||
		rows[0].Entry.Comment != "6*7 · Enter copies" || rows[0].Entry.IconName != "glyph:calculate" {
		t.Fatalf("result row %+v", rows)
	}
	for _, q := range []string{"2+", "firefox", "2048", ""} {
		if rows := p.Query(q); len(rows) != 0 {
			t.Fatalf("%q: rows %+v, want none", q, rows)
		}
	}
}

func TestCalcPrefixWithoutAnExpressionShowsTheHint(t *testing.T) {
	for _, q := range []string{"/calc 2+", "/calc", "/calc 2048"} {
		rows := launcherWithHints(q, nil)
		if len(rows) != 1 || rows[0].Entry.ID != "" || rows[0].Entry.Name != "Invalid expression" ||
			rows[0].Entry.Comment != calcHintComment {
			t.Fatalf("%q: %+v", q, rows)
		}
	}
	if rows := launcherWithHints("firefox", nil); len(rows) != 0 {
		t.Fatalf("bare text never gets the calculator hint: %+v", rows)
	}
}

func TestInlineCalcShowsOnlyRealExpressions(t *testing.T) {
	r := newPanelRegistry(t)
	cfg := r.launcherServiceConfig()
	cfg.Scan = func() []launcher.Entry { return []launcher.Entry{{ID: "ff.desktop", Name: "Firefox"}} }
	svc := launcher.NewService(cfg)
	defer svc.Close()
	<-svc.Results()
	for q, wantCalc := range map[string]bool{"6*7": true, "2048": false, "firefox": false} {
		svc.Query(q)
		got := <-svc.Results()
		hasCalc := len(got) > 0 && strings.HasPrefix(got[0].Entry.ID, "calc:")
		if hasCalc != wantCalc {
			t.Errorf("%q: calc row %v, want %v (%+v)", q, hasCalc, wantCalc, got)
		}
	}
}

func TestEmojiProviderRows(t *testing.T) {
	r := newPanelRegistry(t)
	rows := r.emojiProvider().Query("party")
	if len(rows) == 0 || rows[0].Entry.ID != "🎉" || rows[0].Entry.Name != "party popper" || rows[0].Entry.IconName != "text:🎉" {
		t.Fatalf("rows %+v", rows[:min(2, len(rows))])
	}
	if strings.Count(rows[0].Entry.Comment, " · ") > 2 {
		t.Fatalf("comment carries at most three keywords: %q", rows[0].Entry.Comment)
	}
}

func TestCopyActivationCarriesTheActivatingSerial(t *testing.T) {
	r := newPanelRegistry(t)
	r.mu.Lock()
	r.panelHosts[PanelLauncher] = &PanelHost{id: PanelLauncher, inputSerial: 77, stopAnim: make(chan struct{})}
	r.mu.Unlock()
	if err := r.calcProvider().Activate("6*7", "calc:42", ""); err != nil {
		t.Fatal(err)
	}
	if err := r.emojiProvider().Activate("party", "🎉", ""); err != nil {
		t.Fatal(err)
	}
	got := []wayland.SelectionRequest{<-r.Selections(), <-r.Selections()}
	if got[0].Copy != "42" || got[0].Serial != 77 || got[1].Copy != "🎉" {
		t.Fatalf("requests %+v", got)
	}
}

// Review focus 5 (with every existing Notes launcher test unchanged).
func TestNotesIsAProvider(t *testing.T) {
	r := newPanelRegistry(t)
	var notes *launcher.Provider
	for _, p := range r.launcherProviders() {
		if p.Prefix == "/nt" {
			p := p
			notes = &p
		}
	}
	if notes == nil || notes.Activate == nil || notes.Glyph != "glyph:description" {
		t.Fatalf("notes provider %+v", notes)
	}
	rows := notes.Query("buy milk")
	if len(rows) != 1 || rows[0].Entry.ID != notesLauncherActionID || rows[0].Entry.Name != "Capture note: buy milk" {
		t.Fatalf("rows %+v", rows)
	}
}

func TestLauncherActionProviderSearchesLabelsAndHidesDangerousActions(t *testing.T) {
	r := newPanelRegistry(t)
	r.cfg.Launcher.Actions = []config.LauncherAction{
		{ID: "open-search", Label: "Open search", Comment: "Search the web", Argv: []string{"browser"}},
		{ID: "clean", Label: "Clean temporary files", Argv: []string{"cleaner"}, Dangerous: true},
		{ID: "wipe-cache", Label: "Wipe cache", Argv: []string{"cleaner", "--cache"}, Dangerous: true, AllowDangerous: true},
	}
	p := r.launcherActionProvider()
	if !p.Inline || p.Prefix != "/action" {
		t.Fatalf("action provider = %+v, want inline /action provider", p)
	}
	rows := p.Query("open search")
	if len(rows) != 1 || rows[0].Entry.ID != "launcher-action:open-search" ||
		rows[0].Entry.Name != "Open search" || rows[0].Entry.Comment != "Search the web" {
		t.Fatalf("search rows = %+v", rows)
	}
	if repeated := p.Query("open search"); len(repeated) != 1 || repeated[0].Entry.ID != rows[0].Entry.ID {
		t.Fatalf("result ID changed across queries: first=%+v repeated=%+v", rows, repeated)
	}
	rows = p.Query("wipe")
	if len(rows) != 1 || rows[0].Entry.ID != "launcher-action:wipe-cache" {
		t.Fatalf("dangerous search rows = %+v, want opted-in action only", rows)
	}
}

func TestLauncherActionActivationUsesExactArgvAndRequiresDangerousOptIn(t *testing.T) {
	r := newPanelRegistry(t)
	started := make(chan []string, 1)
	r.mu.Lock()
	r.cfg.Launcher.Actions = []config.LauncherAction{
		{ID: "open-search", Label: "Open search", Argv: []string{"browser", "--query=two words; touch /tmp/no", "", "$(not a shell)"}},
		{ID: "wipe-cache", Label: "Wipe cache", Argv: []string{"cleaner", "--cache"}, Dangerous: true},
	}
	r.runArgv = func(argv []string) error {
		started <- append([]string(nil), argv...)
		return nil
	}
	r.panelHosts[PanelLauncher] = &PanelHost{id: PanelLauncher, launcherAttempt: 1, launcherPendingAttempt: 1}
	r.mu.Unlock()
	t.Cleanup(func() {
		r.mu.Lock()
		delete(r.panelHosts, PanelLauncher)
		r.mu.Unlock()
	})
	p := r.launcherActionProvider()
	if err := p.Activate("open search", "launcher-action:open-search", ""); err != nil {
		t.Fatal(err)
	}
	want := []string{"browser", "--query=two words; touch /tmp/no", "", "$(not a shell)"}
	select {
	case got := <-started:
		if !slices.Equal(got, want) {
			t.Fatalf("argv = %#v, want %#v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("configured action did not start")
	}
	if err := p.Activate("wipe cache", "launcher-action:wipe-cache", ""); err == nil {
		t.Fatal("dangerous action activated without explicit opt-in")
	}
	select {
	case got := <-started:
		t.Fatalf("dangerous action unexpectedly started with argv %v", got)
	default:
	}
}

func TestLauncherCommandProviderRunsConfiguredArgvAndActivatesSelection(t *testing.T) {
	printf, err := exec.LookPath("printf")
	if err != nil {
		t.Skip("printf is unavailable")
	}
	r := newPanelRegistry(t)
	query := "milk; printf should stay data"
	wantArgv := []string{"file-open", "--query=" + query, "--selection=" + query + "\tDocument"}
	started := make(chan []string, 1)
	r.mu.Lock()
	r.cfg.Launcher.Providers = []config.LauncherCommandProvider{{
		Name: "Files", Prefix: "/files", Glyph: "glyph:description", Description: "Search files",
		Command:  []string{printf, "%s\\t%s\\n", "{query}", "Document"},
		OnSelect: []string{"file-open", "--query={query}", "--selection={selection}"},
	}}
	r.runArgv = func(argv []string) error {
		started <- slices.Clone(argv)
		return nil
	}
	r.panelHosts[PanelLauncher] = &PanelHost{id: PanelLauncher, launcherAttempt: 1, launcherPendingAttempt: 1}
	r.mu.Unlock()
	t.Cleanup(func() {
		r.mu.Lock()
		delete(r.panelHosts, PanelLauncher)
		r.mu.Unlock()
	})

	var provider *launcher.Provider
	for _, candidate := range r.launcherProviders() {
		if candidate.Prefix == "/files" {
			candidate := candidate
			provider = &candidate
			break
		}
	}
	if provider == nil {
		t.Fatal("configured command provider was not registered")
	}
	rows := provider.Query(query)
	if len(rows) != 1 || rows[0].Entry.ID != "/files:row:"+query+"\tDocument" ||
		rows[0].Entry.Name != query || rows[0].Entry.Comment != "Document" {
		t.Fatalf("provider rows = %+v", rows)
	}
	if err := provider.Activate(query, rows[0].Entry.ID, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-started:
		if !slices.Equal(got, wantArgv) {
			t.Fatalf("selection argv = %#v, want %#v", got, wantArgv)
		}
	case <-time.After(time.Second):
		t.Fatal("provider selection did not start")
	}
}

func TestLauncherOnlyTreatsExactProviderPrefixesAsOverviewRows(t *testing.T) {
	r := newPanelRegistry(t)
	r.cfg.Launcher.Providers = []config.LauncherCommandProvider{{
		Name: "Files", Prefix: "/files", Command: []string{"list"}, OnSelect: []string{"open"},
	}}
	for _, tc := range []struct {
		id   string
		want bool
	}{
		{config.LauncherAppsPrefix, true},
		{config.LauncherNotesPrefix, true},
		{"/files", true},
		{"/files:row:item\tFile", false},
		{"/files:error", false},
	} {
		if got := r.launcherOverviewPrefix(tc.id); got != tc.want {
			t.Errorf("launcherOverviewPrefix(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func TestLauncherSelectionArgvDoesNotReprocessQueryData(t *testing.T) {
	query := "literal {selection} marker"
	selection := "literal {query} marker\tFile"
	got := launcherSelectionArgv([]string{"open", "--query={query}", "--selection={selection}", "{query}/{selection}"}, query, selection)
	want := []string{
		"open", "--query=" + query, "--selection=" + selection, query + "/" + selection,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("expanded argv = %#v, want %#v", got, want)
	}
}

func TestLauncherServiceRebuildsWhenCommandProviderConfigChanges(t *testing.T) {
	r := newPanelRegistry(t)
	r.mu.Lock()
	first := r.launcherServiceLocked()
	r.mu.Unlock()
	r.mu.Lock()
	r.cfg.Launcher.Providers = []config.LauncherCommandProvider{{
		Name: "Files", Prefix: "/files", Command: []string{"list"}, OnSelect: []string{"open"},
	}}
	second := r.launcherServiceLocked()
	r.mu.Unlock()
	if second == first {
		t.Fatal("launcher service kept the old provider registry after config changed")
	}
}

func TestLauncherSessionProviderSearchesNonLockActions(t *testing.T) {
	r := newPanelRegistry(t)
	r.mu.Lock()
	r.cfg.Session.Locker = "swaylock"
	r.cfg.Session.Language = "en"
	r.mu.Unlock()

	p := r.sessionProvider()
	rows := p.Query("reboot")
	if len(rows) != 1 || rows[0].Entry.ID != "session-reboot" || rows[0].Entry.Name != "Reboot" {
		t.Fatalf("reboot rows = %+v", rows)
	}

	rows = p.Query("")
	want := []string{"session-logout", "session-suspend", "session-display-off", "session-reboot", "session-poweroff"}
	if len(rows) != len(want) {
		t.Fatalf("session rows = %+v, want %v", rows, want)
	}
	for i := range want {
		if rows[i].Entry.ID != want[i] {
			t.Fatalf("session row %d id = %q, want %q", i, rows[i].Entry.ID, want[i])
		}
	}
	r.mu.Lock()
	r.cfg.Session.Language = "es"
	r.mu.Unlock()
	rows = p.Query("reboot")
	if len(rows) != 1 || rows[0].Entry.Name != "Reiniciar" {
		t.Fatalf("localized reboot rows = %+v", rows)
	}
}

func TestLauncherSearchFindsBareSessionAction(t *testing.T) {
	r := newPanelRegistry(t)
	cfg := r.launcherServiceConfig()
	cfg.Scan = func() []launcher.Entry { return nil }
	svc := launcher.NewService(cfg)
	defer svc.Close()
	<-svc.Results()

	svc.Query("reboot")
	select {
	case rows := <-svc.Results():
		for _, row := range rows {
			if row.Entry.ID == "session-reboot" && row.Entry.Name == "Reboot" {
				return
			}
		}
		t.Fatalf("bare reboot search rows = %+v", rows)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for session search results")
	}
}

func TestLauncherSessionActivationUsesExistingActionPath(t *testing.T) {
	r := newPanelRegistry(t)
	started := make(chan []string, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRun := func() { releaseOnce.Do(func() { close(release) }) }
	r.runArgv = func(argv []string) error {
		started <- argv
		<-release
		return nil
	}
	h := &PanelHost{id: PanelLauncher, launcherAttempt: 1, launcherPendingAttempt: 1}
	r.mu.Lock()
	r.panelHosts[PanelLauncher] = h
	r.mu.Unlock()
	t.Cleanup(func() {
		r.mu.Lock()
		delete(r.panelHosts, PanelLauncher)
		r.mu.Unlock()
		releaseRun()
	})

	p := r.sessionProvider()
	if p.Activate == nil {
		t.Fatal("session provider has no activation handler")
	}
	if err := p.Activate("reboot", "session-reboot", ""); err != nil {
		t.Fatal(err)
	}
	select {
	case argv := <-started:
		if strings.Join(argv, "\x00") != strings.Join([]string{"loginctl", "reboot"}, "\x00") {
			t.Fatalf("session activation argv = %q", argv)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session action was not started")
	}
	r.mu.Lock()
	delete(r.panelHosts, PanelLauncher)
	r.mu.Unlock()
	releaseRun()
}

func TestLauncherSessionActivationDropsStaleQuery(t *testing.T) {
	r := newPanelRegistry(t)
	started := make(chan []string, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRun := func() { releaseOnce.Do(func() { close(release) }) }
	r.runArgv = func(argv []string) error {
		started <- argv
		<-release
		return nil
	}

	queryEntered := make(chan struct{})
	queryRelease := make(chan struct{})
	var releaseQueryOnce sync.Once
	releaseQuery := func() { releaseQueryOnce.Do(func() { close(queryRelease) }) }
	activationResult := make(chan error, 1)

	cfg := r.launcherServiceConfig()
	cfg.Scan = func() []launcher.Entry { return nil }
	for i := range cfg.Providers {
		if cfg.Providers[i].Prefix != "/session" {
			continue
		}
		query, activate := cfg.Providers[i].Query, cfg.Providers[i].Activate
		cfg.Providers[i].Query = func(q string) []launcher.Result {
			if q == "reboot" {
				close(queryEntered)
				<-queryRelease
			}
			return query(q)
		}
		cfg.Providers[i].Activate = func(q, id, action string) error {
			err := activate(q, id, action)
			activationResult <- err
			return err
		}
	}
	svc := launcher.NewService(cfg)
	r.mu.Lock()
	r.launcherSvc = svc
	h := &PanelHost{id: PanelLauncher, query: "reboot"}
	r.panelHosts[PanelLauncher] = h
	r.mu.Unlock()
	t.Cleanup(releaseQuery)
	t.Cleanup(releaseRun)
	t.Cleanup(func() {
		r.mu.Lock()
		delete(r.panelHosts, PanelLauncher)
		r.mu.Unlock()
	})

	select {
	case <-svc.Results(): // initial snapshot
	case <-time.After(asyncTestWait):
		t.Fatal("timed out waiting for initial launcher snapshot")
	}
	svc.Query("reboot")
	select {
	case <-queryEntered:
	case <-time.After(asyncTestWait):
		t.Fatal("session query did not block")
	}

	r.mu.Lock()
	h.launcherSpawn(r, "session-reboot", "", false)
	h.query = "suspend"
	h.launcherAttempt++
	r.mu.Unlock()
	releaseQuery()

	select {
	case err := <-activationResult:
		if err == nil {
			t.Fatal("stale session activation was accepted")
		}
	case <-time.After(asyncTestWait):
		t.Fatal("timed out waiting for stale session activation")
	}
	select {
	case argv := <-started:
		t.Fatalf("stale session action started: %v", argv)
	default:
	}
}
