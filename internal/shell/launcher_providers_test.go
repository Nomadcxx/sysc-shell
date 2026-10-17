package shell

import (
	"strings"
	"sync"
	"testing"
	"time"

	launcher "github.com/Nomadcxx/sysc-launch"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

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
	h := &PanelHost{id: PanelLauncher}
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
