package theming

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func valuedFixture(t *testing.T, name, body string) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func readString(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func kittyOpacity(p, value string) valuedLine {
	return valuedLine{file: p, key: "background_opacity", line: "background_opacity " + value}
}

func TestValuedLineAdoptsUserLinesWithBackup(t *testing.T) {
	user := "font_size 12\nbackground_opacity 1.0\ninclude a.conf\nbackground_opacity 0.9\n"
	p := valuedFixture(t, "kitty.conf", user)
	if err := ensureValuedLine(kittyOpacity(p, "0.85"), false); err != nil {
		t.Fatal(err)
	}
	if got, want := readString(t, p), "font_size 12\ninclude a.conf\nbackground_opacity 0.85\n"; got != want {
		t.Fatalf("config = %q, want %q", got, want)
	}
	if got := readString(t, p+".bak"); got != user {
		t.Fatalf("backup = %q, want the user's original", got)
	}
}

func TestValuedLineRewritesItsOwnValue(t *testing.T) {
	p := valuedFixture(t, "kitty.conf", "font_size 12\n")
	if err := ensureValuedLine(kittyOpacity(p, "0.85"), false); err != nil {
		t.Fatal(err)
	}
	if err := ensureValuedLine(kittyOpacity(p, "0.70"), false); err != nil {
		t.Fatalf("changing the shell's own value = %v", err)
	}
	if got, want := readString(t, p), "font_size 12\nbackground_opacity 0.70\n"; got != want {
		t.Fatalf("config = %q, want %q", got, want)
	}
	if _, err := os.Stat(p + ".bak"); !os.IsNotExist(err) {
		t.Fatal("no user line was replaced, so no backup is due")
	}
}

func TestValuedLineReportsUserEditOfOwnedLine(t *testing.T) {
	p := valuedFixture(t, "kitty.conf", "font_size 12\n")
	if err := ensureValuedLine(kittyOpacity(p, "0.85"), false); err != nil {
		t.Fatal(err)
	}
	edited := "font_size 12\nbackground_opacity 0.5\n"
	if err := os.WriteFile(p, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureValuedLine(kittyOpacity(p, "0.85"), false); !errors.Is(err, ErrUserModified) {
		t.Fatalf("user edit = %v, want ErrUserModified", err)
	}
	if got := readString(t, p); got != edited {
		t.Fatalf("refused apply rewrote the config: %q", got)
	}
	if err := ensureValuedLine(kittyOpacity(p, "0.85"), true); err != nil {
		t.Fatalf("forced retake = %v", err)
	}
	if got, want := readString(t, p), "font_size 12\nbackground_opacity 0.85\n"; got != want {
		t.Fatalf("forced config = %q, want %q", got, want)
	}
}

func TestValuedLineRemoveTakesOnlyItsLine(t *testing.T) {
	p := valuedFixture(t, "kitty.conf", "font_size 12\ninclude a.conf\n")
	v := kittyOpacity(p, "0.85")
	if err := ensureValuedLine(v, false); err != nil {
		t.Fatal(err)
	}
	if err := removeValuedLine(v); err != nil {
		t.Fatal(err)
	}
	if got, want := readString(t, p), "font_size 12\ninclude a.conf\n"; got != want {
		t.Fatalf("after remove = %q, want %q", got, want)
	}
	// Nothing owned any more: a second remove, and a remove over a user line,
	// leave the file alone.
	if err := os.WriteFile(p, []byte("background_opacity 0.6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeValuedLine(v); err != nil || readString(t, p) != "background_opacity 0.6\n" {
		t.Fatalf("remove of an unowned key = %v, %q", err, readString(t, p))
	}
}

func TestValuedLinePlacement(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		v                func(p string) valuedLine
	}{
		{
			name: "alacritty.toml",
			body: "[general]\nimport = [\"x\"]\n",
			want: "[general]\nimport = [\"x\"]\n\n[window]\nopacity = 0.85\n",
			v: func(p string) valuedLine {
				return valuedLine{file: p, key: "opacity=", line: "opacity = 0.85", section: "window"}
			},
		},
		{
			name: "foot.ini",
			body: "[main]\nfont=Mono\n[colors-dark]\nforeground=ffffff\n",
			want: "[main]\nfont=Mono\n[colors-dark]\nalpha=0.85\nforeground=ffffff\n",
			v: func(p string) valuedLine {
				return valuedLine{file: p, key: "alpha=", line: "alpha=0.85", section: "colors-dark"}
			},
		},
		{
			name: "wezterm.lua",
			body: "local config = {}\nconfig.window_background_opacity = 1.0\nreturn config\n",
			want: "local config = {}\nconfig.window_background_opacity = 0.85\nreturn config\n",
			v: func(p string) valuedLine {
				return valuedLine{file: p, key: "config.window_background_opacity", line: "config.window_background_opacity = 0.85", returnConfig: true}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := valuedFixture(t, tc.name, tc.body)
			if err := ensureValuedLine(tc.v(p), false); err != nil {
				t.Fatal(err)
			}
			if got := readString(t, p); got != tc.want {
				t.Fatalf("config = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValuedLineNeverInventsAConfig(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p := filepath.Join(t.TempDir(), "kitty.conf")
	if err := ensureValuedLine(kittyOpacity(p, "0.85"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("a valued line must not create the terminal's config")
	}
}
