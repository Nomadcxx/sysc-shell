package theming

import (
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// foot keeps an alpha per colour section; the shell owns only the dark one.
func TestValuedLineLeavesOtherSectionsAlone(t *testing.T) {
	user := "[main]\nfont=mono:size=11\n\n[colors-light]\nalpha=0.95\n"
	p := valuedFixture(t, "foot.ini", user)
	v := valuedLine{file: p, key: "alpha=", line: "alpha=0.85", section: "colors-dark"}
	if err := ensureValuedLine(v, false); err != nil {
		t.Fatal(err)
	}
	if got, want := readString(t, p), user+"\n[colors-dark]\nalpha=0.85\n"; got != want {
		t.Fatalf("foot.ini = %q, want the light alpha kept and %q", got, want)
	}
	if err := ensureValuedLine(v, false); err != nil {
		t.Fatalf("second apply = %v; the light alpha is not a conflict", err)
	}
	if err := removeValuedLine(v); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, p); !containsLine(got, "alpha=0.95") {
		t.Fatalf("removal took the light alpha: %q", got)
	}
}

func containsLine(s, line string) bool {
	for _, ln := range splitLines(s) {
		if ln == line {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// A section header may carry a comment; it is still the section.
func TestValuedLineFindsCommentedSectionHeader(t *testing.T) {
	p := valuedFixture(t, "alacritty.toml", "[window] # looks\nopacity = 1.0\n")
	v := valuedLine{file: p, key: "opacity=", line: "opacity = 0.85", section: "window"}
	if err := ensureValuedLine(v, false); err != nil {
		t.Fatal(err)
	}
	if got, want := readString(t, p), "[window] # looks\nopacity = 0.85\n"; got != want {
		t.Fatalf("alacritty.toml = %q, want one [window] table %q", got, want)
	}
}

// TOML can define the table as a dotted key or inline; a second [window]
// header would make the whole config fail to load, so the shell refuses.
func TestValuedLineRefusesInlineTomlTables(t *testing.T) {
	for _, body := range []string{
		"window.opacity = 1.0\n",
		"window = { opacity = 1.0, padding = { x = 4, y = 4 } }\n",
	} {
		p := valuedFixture(t, "alacritty.toml", body)
		v := valuedLine{file: p, key: "opacity=", line: "opacity = 0.85", section: "window"}
		if err := ensureValuedLine(v, false); !errors.Is(err, ErrUserModified) {
			t.Fatalf("%q = %v, want ErrUserModified", body, err)
		}
		if got := readString(t, p); got != body {
			t.Fatalf("refused apply rewrote %q to %q", body, got)
		}
	}
}

// fakeKitty registers this test process as a kitty under a private proc root
// and returns the channel its SIGUSR1 arrives on.
func fakeKitty(t *testing.T) chan os.Signal {
	t.Helper()
	got := make(chan os.Signal, 4)
	signal.Notify(got, syscall.SIGUSR1)
	t.Cleanup(func() { signal.Stop(got) })
	root := t.TempDir()
	dir := filepath.Join(root, strconv.Itoa(os.Getpid()))
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comm"), []byte("kitty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := procRoot
	procRoot = root
	t.Cleanup(func() { procRoot = prev })
	return got
}

func signalled(ch chan os.Signal, wait time.Duration) bool {
	select {
	case <-ch:
		return true
	case <-time.After(wait):
		return false
	}
}

// Every enabled template is themed (owner decision, 2026-10-09), so a
// hand-edited opacity line is adopted: the shell's value returns, the edit is
// kept in <file>.bak, and the terminal hears exactly one reload -- the
// refused guarded pass must not announce one of its own.
func TestEditedOpacityIsAdoptedWithOneReload(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "kitty")
	got := fakeKitty(t)
	only := func(name string) bool { return name == "kitty" }
	if _, _, err := ApplyEnabled(home, only, theme.Fallback, 85); err != nil {
		t.Fatal(err)
	}
	signalled(got, 2*time.Second)
	p := filepath.Join(home, ".config", "kitty", "kitty.conf")
	edited := "include themes/sysc-shell.conf\nbackground_opacity 0.95\n"
	if err := os.WriteFile(p, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	// A colour change too, so the refused guarded pass writes the sidecar.
	if err := os.Remove(filepath.Join(home, ".config", "kitty", "themes", "sysc-shell.conf")); err != nil {
		t.Fatal(err)
	}
	_, adopted, err := ApplyEnabled(home, only, theme.Fallback, 85)
	if err != nil || len(adopted) != 1 || adopted[0] != "kitty" {
		t.Fatalf("apply over an edited opacity = adopted %v, %v; want kitty adopted", adopted, err)
	}
	if got := readString(t, p); !containsLine(got, "background_opacity 0.85") || containsLine(got, "background_opacity 0.95") {
		t.Fatalf("kitty.conf = %q, want the shell's 0.85 only", got)
	}
	if got := readString(t, p+".bak"); got != edited {
		t.Fatalf("backup = %q, want the edited file", got)
	}
	if !signalled(got, 2*time.Second) {
		t.Fatal("kitty was not told to reload")
	}
	if signalled(got, 300*time.Millisecond) {
		t.Fatal("kitty was told to reload twice for one apply")
	}
}

// Every shell config reload runs an apply; a terminal whose files did not
// change is not told to reload (ghostty announces each reload).
func TestUnchangedApplySendsNoReload(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "kitty")
	got := fakeKitty(t)
	only := func(name string) bool { return name == "kitty" }
	if _, _, err := ApplyEnabled(home, only, theme.Fallback, 85); err != nil {
		t.Fatal(err)
	}
	if !signalled(got, 2*time.Second) {
		t.Fatal("first apply did not reload kitty")
	}
	if _, _, err := ApplyEnabled(home, only, theme.Fallback, 85); err != nil {
		t.Fatal(err)
	}
	if signalled(got, 300*time.Millisecond) {
		t.Fatal("an apply that changed nothing reloaded kitty")
	}
	if _, _, err := ApplyEnabled(home, only, theme.Fallback, 70); err != nil {
		t.Fatal(err)
	}
	if !signalled(got, 2*time.Second) {
		t.Fatal("an opacity change did not reload kitty")
	}
}
