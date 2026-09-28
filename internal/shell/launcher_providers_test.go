package shell

import (
	"strings"
	"testing"

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
