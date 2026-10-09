package theming

import (
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// Back at 100 the user's file returns byte for byte, including a section
// header the shell had to add for its line.
func TestRemovingTheLineRemovesTheSectionTheShellAdded(t *testing.T) {
	original := "[font]\nsize = 11\n"
	p := valuedFixture(t, "alacritty.toml", original)
	v := valuedLine{file: p, key: "opacity=", line: "opacity = 0.85", section: "window"}
	if err := ensureValuedLine(v, false); err != nil {
		t.Fatal(err)
	}
	if err := removeValuedLine(v); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, p); got != original {
		t.Fatalf("after removal = %q, want the original %q", got, original)
	}
}

// A section the user filled in since is theirs and stays.
func TestRemovingTheLineKeepsASectionTheUserFilled(t *testing.T) {
	p := valuedFixture(t, "alacritty.toml", "[font]\nsize = 11\n")
	v := valuedLine{file: p, key: "opacity=", line: "opacity = 0.85", section: "window"}
	if err := ensureValuedLine(v, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(readString(t, p)+"decorations = \"none\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeValuedLine(v); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, p); !strings.Contains(got, "[window]\ndecorations = \"none\"") {
		t.Fatalf("after removal = %q, want the user's [window] kept", got)
	}
}

// A failed ownership write leaves the config as it was: an unrecorded line
// would read as the user's on the next apply.
func TestValuedLineRollsBackWhenOwnershipCannotBeSaved(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.WriteFile(state, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", state)
	p := filepath.Join(root, "kitty.conf")
	original := "font_size 12\n"
	if err := os.WriteFile(p, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureValuedLine(kittyOpacity(p, "0.85"), false); err == nil {
		t.Fatal("an ownership write failure was ignored")
	}
	if got := readString(t, p); got != original {
		t.Fatalf("config after failed ownership write = %q, want %q", got, original)
	}
}

// Every adoption over a user's own value keeps that value somewhere: a second
// one, after the first backup exists, goes to the next numbered backup.
func TestEveryAdoptionKeepsTheUsersFile(t *testing.T) {
	first := "background_opacity 1.0\n"
	p := valuedFixture(t, "kitty.conf", first)
	if err := ensureValuedLine(kittyOpacity(p, "0.85"), false); err != nil {
		t.Fatal(err)
	}
	second := "background_opacity 0.6\n"
	if err := os.WriteFile(p, []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureValuedLine(kittyOpacity(p, "0.85"), true); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, p+".bak"); got != first {
		t.Fatalf(".bak = %q, want the first original", got)
	}
	if got := readString(t, p+".bak.2"); got != second {
		t.Fatalf(".bak.2 = %q, want the second edit", got)
	}
	// The same bytes again are not backed up twice.
	if err := os.WriteFile(p, []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureValuedLine(kittyOpacity(p, "0.85"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p + ".bak.3"); !os.IsNotExist(err) {
		t.Fatal("identical bytes were backed up again")
	}
}

// Backups reports numbered backups beside the first, so the adoption toast
// names every file that holds the user's own value.
func TestBackupsListsNumberedBackups(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	conf := filepath.Join(home, ".config", "kitty", "kitty.conf")
	if err := os.MkdirAll(filepath.Dir(conf), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{conf + ".bak", conf + ".bak.2"} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := Backups(home, "kitty"); !slices.Equal(got, []string{conf + ".bak", conf + ".bak.2"}) {
		t.Fatalf("Backups = %v", got)
	}
}

// kitty separates a key from its value with whitespace; a longer option that
// starts with the same name is a different setting.
func TestKittyOpacityKeyNeedsWhitespace(t *testing.T) {
	p := valuedFixture(t, "kitty.conf", "background_opacity_future 1\nbackground_opacity\t0.9\n")
	if err := ensureValuedLine(kittyOpacity(p, "0.85"), false); err != nil {
		t.Fatal(err)
	}
	got := readString(t, p)
	if !strings.Contains(got, "background_opacity_future 1") || strings.Contains(got, "background_opacity\t0.9") {
		t.Fatalf("kitty.conf = %q, want the longer option kept and the tab-separated line replaced", got)
	}
}

func TestApplyEnabledSignalsGhosttyWithUSR2(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "ghostty")
	got := make(chan os.Signal, 1)
	signal.Notify(got, syscall.SIGUSR2)
	t.Cleanup(func() { signal.Stop(got) })
	root := t.TempDir()
	dir := filepath.Join(root, strconv.Itoa(os.Getpid()))
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comm"), []byte("ghostty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := procRoot
	procRoot = root
	t.Cleanup(func() { procRoot = prev })
	only := func(name string) bool { return name == "ghostty" }
	if _, _, err := ApplyEnabled(home, only, theme.Fallback, 85); err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("ghostty did not receive SIGUSR2")
	}
}

func TestApplyEnabledWritesWeztermOpacityBeforeReturn(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "wezterm")
	p := filepath.Join(home, ".config", "wezterm", "wezterm.lua")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("local config = {}\nreturn config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	only := func(name string) bool { return name == "wezterm" }
	if _, _, err := ApplyEnabled(home, only, theme.Fallback, 85); err != nil {
		t.Fatal(err)
	}
	got := readString(t, p)
	line := strings.Index(got, "config.window_background_opacity = 0.85")
	ret := strings.Index(got, "return config")
	if line < 0 || line > ret {
		t.Fatalf("wezterm.lua = %q, want the opacity assignment before return config", got)
	}
}

// A user who deletes the shell's line by hand gets it back on the next apply:
// 100 is the way to stop managing it.
func TestHandDeletedOpacityLineIsRestored(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "ghostty")
	only := func(name string) bool { return name == "ghostty" }
	if _, _, err := ApplyEnabled(home, only, theme.Fallback, 85); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(home, ".config", "ghostty", "config")
	if err := os.WriteFile(p, []byte("theme = sysc-shell\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ApplyEnabled(home, only, theme.Fallback, 85); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, p); !strings.Contains(got, "background-opacity = 0.85") {
		t.Fatalf("config = %q, want the opacity line back", got)
	}
}
