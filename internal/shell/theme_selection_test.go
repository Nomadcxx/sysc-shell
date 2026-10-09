package shell

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestCommittedThemeSelection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfg := config.Default()
	cfg.ThemeGen.Source = "palette"
	cfg.ThemeGen.Seed = "dracula"
	dracula, _ := theme.NamedPalette("dracula", "dark", false)
	blue, _ := theme.NamedPalette("blue", "dark", false)
	r := &Registry{cfg: cfg, tokens: dracula}
	r.publishCommittedThemeSelection(cfg, r.tokens)
	path := filepath.Join(dir, "sysc-shell", "shell-theme")
	check := func(want string) {
		t.Helper()
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want+"\n" {
			t.Fatalf("selection=%q err=%v", got, err)
		}
	}
	check("dracula")
	stale := cfg
	stale.ThemeGen.Seed = "blue"
	r.publishCommittedThemeSelection(stale, blue)
	check("dracula")
	r.cfg = stale
	r.themeErr = "generation failed"
	r.publishCommittedThemeSelection(stale, blue)
	check("dracula")
	r.themeErr = ""
	r.tokens = blue
	r.publishCommittedThemeSelection(stale, blue)
	check("blue")
	cfg.ThemeGen.Source = "hex"
	cfg.ThemeGen.Seed = "#123456"
	r.cfg = cfg
	r.publishCommittedThemeSelection(cfg, r.tokens)
	check("")
}

// Template outcomes are reported in a sysc-notify toast, not the theme error
// the wallpaper picker paints: an adoption says where the user's file went, a
// failure says why, and an unchanged report is not posted again (sysc-1084).
func TestTemplateOutcomesAreToasts(t *testing.T) {
	home := t.TempDir()
	rec := &pluginToastRecorder{}
	r := &Registry{}
	r.producerSender = rec

	r.reportTemplates(home, map[string]error{"btop": nil}, []string{"btop"})
	got := rec.commands()
	if len(got) != 1 || got[0].Producer == nil || got[0].Producer.Summary != "Theme applied to btop" {
		t.Fatalf("adoption toasts = %+v, want one naming btop", got)
	}
	if !strings.Contains(got[0].Producer.Body, "replaced") {
		t.Errorf("adoption body = %q, want it to say the user's setting was replaced", got[0].Producer.Body)
	}

	fail := map[string]error{"kitty": errors.New("theming: permission denied")}
	r.reportTemplates(home, fail, nil)
	r.reportTemplates(home, fail, nil)
	got = rec.commands()
	if len(got) != 2 {
		t.Fatalf("toasts = %d, want the failure posted once", len(got))
	}
	if p := got[1].Producer; p.Summary != "Could not theme kitty" || !strings.Contains(p.Body, "permission denied") {
		t.Fatalf("failure toast = %+v", p)
	}

	// A clean apply clears the memory, so the same failure later is news.
	r.reportTemplates(home, map[string]error{"kitty": nil}, nil)
	r.reportTemplates(home, fail, nil)
	if n := len(rec.commands()); n != 3 {
		t.Fatalf("toasts = %d, want the recurring failure posted again", n)
	}
}

// The first theme apply runs while the registry is built, before the
// notification client exists. Its takeover toast waits for the connection
// instead of being dropped and then deduplicated as sent (sysc-1087).
func TestTemplateToastWaitsForTheNotifyConnection(t *testing.T) {
	home := t.TempDir()
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)

	r.reportTemplates(home, map[string]error{"btop": nil}, []string{"btop"})

	rec := &pluginToastRecorder{}
	r.BindNotifications(rec)
	if n := len(rec.commands()); n != 0 {
		t.Fatalf("posted %d toasts before the client connected", n)
	}
	r.applyNotify(snap(1))
	got := rec.commands()
	if len(got) != 1 || got[0].Producer == nil || got[0].Producer.Summary != "Theme applied to btop" {
		t.Fatalf("toasts after connecting = %+v, want the startup takeover", got)
	}

	// Delivered once: a reconnect, or the same report again, posts nothing.
	r.applyNotify(snap(2))
	r.reportTemplates(home, map[string]error{"btop": nil}, []string{"btop"})
	if n := len(rec.commands()); n != 1 {
		t.Fatalf("toasts = %d, want the takeover posted once", n)
	}
}
