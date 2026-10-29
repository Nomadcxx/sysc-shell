package shell

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	clipboardprotocol "github.com/Nomadcxx/sysc-clipboard/protocol"
	launcher "github.com/Nomadcxx/sysc-launch"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

func TestWindowsProvider(t *testing.T) {
	r := newPanelRegistry(t)
	r.running = []runningAppSlot{
		{Name: "Firefox", Icon: "firefox", Members: []niri.Window{
			{ID: 42, Title: "Niri Manual", AppID: "org.mozilla.firefox"},
			{ID: 43, Title: "Inbox - Webmail", AppID: "org.mozilla.firefox"},
		}},
		{Name: "Code", Icon: "code", Members: []niri.Window{
			{ID: 44, Title: "Untitled editor", AppID: "org.example.editor"},
		}},
	}

	provider := findWindowsProvider(t, r)
	if provider.Name != "Windows" || provider.Glyph != "glyph:desktop_windows" || provider.Inline {
		t.Fatalf("windows provider metadata = %+v", provider)
	}

	browse := provider.Query("")
	if len(browse) != 3 {
		t.Fatalf("browse returned %d windows, want 3: %+v", len(browse), browse)
	}
	byID := make(map[string]launcher.Result, len(browse))
	for _, row := range browse {
		byID[row.Entry.ID] = row
	}
	manual, ok := byID["window:42"]
	if !ok || manual.Entry.Name != "Niri Manual" || manual.Entry.Comment != "Firefox" || manual.Entry.IconName != "firefox" ||
		!slices.Contains(manual.Entry.Keywords, "org.mozilla.firefox") {
		t.Fatalf("window row = %+v, want title, app name, app ID, and resolved icon", manual)
	}

	for _, tc := range []struct {
		query string
		want  []string
	}{
		{query: "Manual", want: []string{"window:42"}},
		{query: "org.mozilla.firefox", want: []string{"window:42", "window:43"}},
		{query: "Firefox", want: []string{"window:42", "window:43"}},
	} {
		rows := provider.Query(tc.query)
		got := make([]string, 0, len(rows))
		for _, row := range rows {
			got = append(got, row.Entry.ID)
		}
		slices.Sort(got)
		if !slices.Equal(got, tc.want) {
			t.Errorf("query %q returned %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestWindowsProviderActivation(t *testing.T) {
	r := newPanelRegistry(t)
	r.running = []runningAppSlot{{Name: "Firefox", Members: []niri.Window{{ID: 42, Title: "Niri Manual", AppID: "org.mozilla.firefox"}}}}
	provider := findWindowsProvider(t, r)

	var sent []any
	r.niriSend = func(action any) error {
		sent = append(sent, action)
		return nil
	}
	if err := provider.Activate("Manual", "window:42", ""); err != nil {
		t.Fatalf("Activate current window: %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("sent %d Niri actions, want 1", len(sent))
	}
	if action, ok := sent[0].(niri.FocusWindow); !ok || action.ID != 42 {
		t.Fatalf("sent action = %#v, want FocusWindow{ID: 42}", sent[0])
	}

	if err := provider.Activate("Manual", "window:bad", ""); err == nil {
		t.Fatal("malformed window ID activated without an error")
	}
	if len(sent) != 1 {
		t.Fatalf("malformed ID sent a Niri action: %v", sent)
	}

	r.running = nil
	if err := provider.Activate("Manual", "window:42", ""); err == nil {
		t.Fatal("closed window activated without an error")
	}
	if len(sent) != 1 {
		t.Fatalf("stale window sent a Niri action: %v", sent)
	}

	r.running = []runningAppSlot{{Name: "Firefox", Members: []niri.Window{{ID: 42, Title: "Niri Manual", AppID: "org.mozilla.firefox"}}}}
	r.niriSend = func(any) error { return errors.New("niri refused focus") }
	if err := provider.Activate("Manual", "window:42", ""); err == nil || err.Error() != "niri refused focus" {
		t.Fatalf("Niri action error = %v, want refusal propagated", err)
	}
}

func findWindowsProvider(t *testing.T, r *Registry) launcher.Provider {
	t.Helper()
	for _, provider := range r.launcherProviders() {
		if provider.Prefix == "/win" {
			return provider
		}
	}
	t.Fatal("launcher has no /win provider")
	return launcher.Provider{}
}

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

func TestLauncherServiceConfigSpawnsThroughTheCompositor(t *testing.T) {
	r := newPanelRegistry(t)
	var mu sync.Mutex
	var spawned []string
	r.runArgv = func(argv []string) error {
		mu.Lock()
		spawned = slices.Clone(argv)
		mu.Unlock()
		return nil
	}

	serviceConfig := r.launcherServiceConfig()
	if serviceConfig.Run == nil {
		t.Fatal("the launcher service has no spawn runner: an application would be started directly")
	}
	serviceConfig.Scan = func() []launcher.Entry {
		return []launcher.Entry{{ID: "firefox.desktop", Name: "Firefox", Argv: []string{"firefox"}}}
	}
	svc := launcher.NewService(serviceConfig)
	defer svc.Close()
	<-svc.Results()
	if err := svc.Activate("firefox.desktop", ""); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"niri", "msg", "action", "spawn", "--", "firefox"}
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
		hasCalc := len(got.Results) > 0 && strings.HasPrefix(got.Results[0].Entry.ID, "calc:")
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

func TestClipboardProviderSearchesMetadata(t *testing.T) {
	r := newPanelRegistry(t)
	entry := clipboardprotocol.Entry{
		ID: "text-one", Kind: clipboardprotocol.KindText, MIME: "text/plain", Preview: "remember launch command",
	}
	betterMatch := clipboardprotocol.Entry{
		ID: "text-two", Kind: clipboardprotocol.KindText, MIME: "text/plain", Preview: "launch command",
	}
	r.mu.Lock()
	r.clipboard = clipboardProjection{Connected: true, Snapshot: clipboardprotocol.Snapshot{
		Entries: []clipboardprotocol.Entry{entry, betterMatch}, Wayland: clipboardprotocol.WaylandReady,
	}}
	r.mu.Unlock()

	var provider *launcher.Provider
	for _, candidate := range r.launcherProviders() {
		if candidate.Prefix == launcherClipboardPrefix {
			candidate := candidate
			provider = &candidate
			break
		}
	}
	if provider == nil || provider.Inline || provider.Prefix != "/clipboard" || !strings.Contains(provider.Description, "/clipboard") {
		t.Fatalf("clipboard provider = %+v, want non-inline /clipboard provider", provider)
	}
	rows := provider.Query("launch")
	if len(rows) != 2 || rows[0].Entry.ID != launcherClipboardResultPrefix+"text-two" ||
		rows[1].Entry.ID != launcherClipboardResultPrefix+"text-one" {
		t.Fatalf("clipboard search rows = %+v", rows)
	}
	if rows := provider.Query("absent"); len(rows) != 0 {
		t.Fatalf("non-matching clipboard search rows = %+v", rows)
	}
}

func TestClipboardProviderRequiresExplicitPrefix(t *testing.T) {
	r := newPanelRegistry(t)
	clipID := launcherClipboardResultPrefix + "pav-entry"
	r.mu.Lock()
	r.clipboard = clipboardProjection{Connected: true, Snapshot: clipboardprotocol.Snapshot{
		Entries: []clipboardprotocol.Entry{{
			ID: "pav-entry", Kind: clipboardprotocol.KindText, MIME: "text/plain", Preview: "pav control",
		}}, Wayland: clipboardprotocol.WaylandReady,
	}}
	r.mu.Unlock()

	cfg := r.launcherServiceConfig()
	cfg.Scan = func() []launcher.Entry {
		return []launcher.Entry{{ID: "pavucontrol.desktop", Name: "Pavucontrol"}}
	}
	svc := launcher.NewService(cfg)
	defer svc.Close()
	<-svc.Results()
	query := func(text string) []launcher.Result {
		svc.Query(text)
		return (<-svc.Results()).Results
	}
	contains := func(rows []launcher.Result, id string) bool {
		for _, row := range rows {
			if row.Entry.ID == id {
				return true
			}
		}
		return false
	}

	bare := query("pav")
	if !contains(bare, "pavucontrol.desktop") {
		t.Errorf("bare pav search = %+v, want Pavucontrol", bare)
	}
	if contains(bare, clipID) {
		t.Errorf("bare pav search includes clipboard entry: %+v", bare)
	}

	explicit := query("/clipboard pav")
	if !contains(explicit, clipID) {
		t.Errorf("explicit clipboard search = %+v, want clipboard entry %q", explicit, clipID)
	}
}

type launcherClipboardSender struct {
	restored   chan string
	requestIDs map[string]string
}

func (s launcherClipboardSender) Restore(id string) error {
	s.restored <- id
	return nil
}
func (s launcherClipboardSender) RestoreWithRequestID(id string) (string, error) {
	if err := s.Restore(id); err != nil {
		return "", err
	}
	return s.requestIDs[id], nil
}
func (launcherClipboardSender) Pin(string, bool) error                   { return nil }
func (launcherClipboardSender) Delete(string) error                      { return nil }
func (launcherClipboardSender) Clear(clipboardprotocol.ClearScope) error { return nil }
func (launcherClipboardSender) Thumbnail(string, uint16) error           { return nil }
func (launcherClipboardSender) Resync() error                            { return nil }

func TestClipboardRestoreCompletionsMatchRequestIDs(t *testing.T) {
	reg := newPanelRegistry(t)
	sender := launcherClipboardSender{restored: make(chan string, 2), requestIDs: map[string]string{
		"first": "request-1", "second": "request-2",
	}}
	first, err := reg.enqueueClipboardRestore(sender, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := reg.enqueueClipboardRestore(sender, "second")
	if err != nil {
		t.Fatal(err)
	}
	<-sender.restored
	<-sender.restored

	reg.ApplyClipboard(clipboardUpdate{Connected: true, Message: clipboardprotocol.Message{
		Version: clipboardprotocol.Version, Type: clipboardprotocol.TypeError,
		Error: &clipboardprotocol.ErrorBody{Code: clipboardprotocol.ErrorUnavailable, RequestID: "request-1", Message: "first failed"},
	}})
	if got := <-first.done; got == nil || got.Error() != "first failed" {
		t.Fatalf("first completion = %v, want first failed", got)
	}
	select {
	case got := <-second.done:
		t.Fatalf("second completed out of order: %v", got)
	default:
	}

	reg.ApplyClipboard(clipboardUpdate{Connected: true, Message: clipboardprotocol.Message{
		Version: clipboardprotocol.Version, Type: clipboardprotocol.TypeAck,
		Ack: &clipboardprotocol.Ack{RequestID: "request-2"},
	}})
	if got := <-second.done; got != nil {
		t.Fatalf("second completion = %v, want success", got)
	}
}

func TestClipboardProviderRequiresWtypeBeforeRestore(t *testing.T) {
	reg := newPanelRegistry(t)
	entry := clipboardprotocol.Entry{ID: "text-one", Kind: clipboardprotocol.KindText, Preview: "copy me"}
	sender := launcherClipboardSender{restored: make(chan string, 1)}
	reg.mu.Lock()
	reg.clipboard = clipboardProjection{Connected: true, Snapshot: clipboardprotocol.Snapshot{
		Entries: []clipboardprotocol.Entry{entry}, Wayland: clipboardprotocol.WaylandReady,
	}}
	reg.panelHosts[PanelLauncher] = &PanelHost{
		launcherAttempt: 1, launcherPendingAttempt: 1, stopAnim: make(chan struct{}),
	}
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	reg.mu.Unlock()
	reg.BindClipboard(sender)

	var provider *launcher.Provider
	for _, candidate := range reg.launcherProviders() {
		if candidate.Prefix == launcherClipboardPrefix {
			candidate := candidate
			provider = &candidate
			break
		}
	}
	if provider == nil {
		t.Fatal("clipboard provider was not registered")
	}
	if err := provider.Activate("copy", launcherClipboardResultPrefix+entry.ID, ""); err == nil || !strings.Contains(err.Error(), "wtype") {
		t.Fatalf("activation error = %v, want missing wtype", err)
	}
	select {
	case id := <-sender.restored:
		t.Fatalf("restored %q without wtype", id)
	default:
	}
}

func TestClipboardLauncherActivationRestoresThenClosesAndPastes(t *testing.T) {
	reg, _, _ := openLauncherPanel(t, nil)
	entry := clipboardprotocol.Entry{
		ID: "text-one", Kind: clipboardprotocol.KindText, MIME: "text/plain", Preview: "remember launch command",
	}
	reg.mu.Lock()
	reg.clipboard = clipboardProjection{Connected: true, Snapshot: clipboardprotocol.Snapshot{
		Entries: []clipboardprotocol.Entry{entry}, Wayland: clipboardprotocol.WaylandReady,
	}}
	reg.clipboardSender = launcherClipboardSender{restored: make(chan string, 1), requestIDs: map[string]string{entry.ID: "request-1"}}
	reg.lookPath = func(name string) (string, error) {
		if name != "wtype" {
			t.Errorf("looked up %q, want wtype", name)
		}
		return "/usr/bin/wtype", nil
	}
	type paste struct {
		argv      []string
		panelOpen bool
	}
	pasted := make(chan paste, 1)
	reg.runArgv = func(argv []string) error {
		reg.mu.Lock()
		panelOpen := len(reg.panelHosts) != 0
		reg.mu.Unlock()
		pasted <- paste{argv: slices.Clone(argv), panelOpen: panelOpen}
		return nil
	}
	h := reg.panelHosts[PanelLauncher]
	reg.launcherSendQuery(h, "/clipboard launch")
	reg.rebuildPanel(h)
	reg.mu.Unlock()
	waitForLauncherResults(t, reg, 1)

	reg.mu.Lock()
	h = reg.panelHosts[PanelLauncher]
	if h == nil || len(h.launcherResults) != 1 {
		reg.mu.Unlock()
		t.Fatal("clipboard result disappeared before activation")
	}
	h.launcherSpawn(reg, h.launcherResults[0].Entry.ID, "", true)
	reg.mu.Unlock()

	select {
	case id := <-reg.clipboardSender.(launcherClipboardSender).restored:
		if id != entry.ID {
			t.Fatalf("restored id = %q, want %q", id, entry.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("clipboard selection was not restored")
	}
	select {
	case got := <-pasted:
		t.Fatalf("pasted before restore acknowledgement: %+v", got)
	default:
	}
	reg.mu.Lock()
	stillOpen := reg.panelHosts[PanelLauncher] != nil
	reg.mu.Unlock()
	if !stillOpen {
		t.Fatal("launcher closed before clipboard restore completed")
	}

	reg.ApplyClipboard(clipboardUpdate{Connected: true, Snapshot: reg.clipboard.Snapshot,
		Message: clipboardprotocol.Message{Version: clipboardprotocol.Version, Type: clipboardprotocol.TypeAck,
			Ack: &clipboardprotocol.Ack{RequestID: "request-2"}}})
	select {
	case got := <-pasted:
		t.Fatalf("unmatched acknowledgement triggered paste: %+v", got)
	case <-time.After(200 * time.Millisecond):
	}
	reg.mu.Lock()
	stillOpen = reg.panelHosts[PanelLauncher] != nil
	reg.mu.Unlock()
	if !stillOpen {
		t.Fatal("launcher closed after an unmatched clipboard acknowledgement")
	}

	reg.ApplyClipboard(clipboardUpdate{Connected: true, Snapshot: reg.clipboard.Snapshot,
		Message: clipboardprotocol.Message{Version: clipboardprotocol.Version, Type: clipboardprotocol.TypeAck,
			Ack: &clipboardprotocol.Ack{RequestID: "request-1"}}})
	var barrier wayland.AuxRequest
	closedLauncher, closedShield := false, false
	barrierTimeout := time.NewTimer(2 * time.Second)
	defer barrierTimeout.Stop()
	for !barrier.Barrier {
		select {
		case req := <-reg.AuxRequests():
			closedLauncher = closedLauncher || (req.ID == panelSurfaceID(PanelLauncher) && req.Open == nil && req.Update == nil)
			closedShield = closedShield || (req.ID == panelShieldSurfaceID(7) && req.Open == nil && req.Update == nil)
			if req.Barrier {
				barrier = req
			}
		case got := <-pasted:
			t.Fatalf("pasted before the Wayland close barrier: %+v", got)
		case <-barrierTimeout.C:
			t.Fatal("launcher paste did not wait for a Wayland close barrier")
		}
	}
	if !closedLauncher || !closedShield || barrier.Reply == nil {
		t.Fatalf("close requests before barrier: launcher=%v shield=%v barrier=%+v", closedLauncher, closedShield, barrier)
	}
	select {
	case got := <-pasted:
		t.Fatalf("pasted before the compositor acknowledged the close barrier: %+v", got)
	case <-time.After(200 * time.Millisecond):
	}
	barrier.Reply <- nil
	select {
	case got := <-pasted:
		want := []string{"wtype", "-M", "ctrl", "-k", "v", "-m", "ctrl"}
		if !slices.Equal(got.argv, want) || got.panelOpen {
			t.Fatalf("paste = %+v, want argv %v after panels close", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("launcher did not paste after clipboard restore completed")
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
		t.Skip("printf is not on PATH")
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
		for _, row := range rows.Results {
			if row.Entry.ID == "session-reboot" && row.Entry.Name == "Reboot" {
				return
			}
		}
		t.Fatalf("bare reboot search rows = %+v", rows.Results)
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
