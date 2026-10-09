package theming

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestOpacityValue(t *testing.T) {
	for in, want := range map[int]string{85: "0.85", 50: "0.50", 99: "0.99"} {
		if got := opacityValue(in); got != want {
			t.Errorf("opacityValue(%d) = %q, want %q", in, got, want)
		}
	}
}

// Each terminal gets its own key in its own place; the user's opacity line is
// replaced once with a backup, and 100 removes the shell's line again.
func TestApplyEnabledWritesTerminalOpacity(t *testing.T) {
	for _, tc := range []struct {
		name, rel, user, line string
	}{
		{"kitty", ".config/kitty/kitty.conf", "background_opacity 1.0\n", "background_opacity 0.85"},
		{"ghostty", ".config/ghostty/config", "background-opacity = 1.00\nbackground-opacity-cells = true\n", "background-opacity = 0.85"},
		{"alacritty", ".config/alacritty/alacritty.toml", "[window]\nopacity = 1.0\n", "opacity = 0.85"},
		{"foot", ".config/foot/foot.ini", "[colors-dark]\nalpha=1.0\n", "alpha=0.85"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			home := t.TempDir()
			markTemplatesComplete(t, tc.name)
			p := filepath.Join(home, tc.rel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(tc.user), 0o644); err != nil {
				t.Fatal(err)
			}
			only := func(name string) bool { return name == tc.name }
			if _, _, err := ApplyEnabled(home, only, theme.Fallback, 85); err != nil {
				t.Fatalf("apply at 85 = %v", err)
			}
			got := readString(t, p)
			if strings.Count(got, tc.line) != 1 {
				t.Fatalf("%s = %q, want exactly one %q", tc.rel, got, tc.line)
			}
			if tc.name == "ghostty" && !strings.Contains(got, "background-opacity-cells = true") {
				t.Fatalf("ghostty lost its background-opacity-cells line: %q", got)
			}
			if readString(t, p+".bak") == "" {
				t.Fatal("the user's opacity line was replaced without a backup")
			}
			if _, _, err := ApplyEnabled(home, only, theme.Fallback, 100); err != nil {
				t.Fatalf("apply at 100 = %v", err)
			}
			if got := readString(t, p); strings.Contains(got, tc.line) {
				t.Fatalf("100 left the shell's line behind: %q", got)
			}
		})
	}
}

// A template that is off never gets the line, and turning one off takes the
// line away with its include.
func TestTerminalOpacityFollowsTheTemplateToggle(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "kitty")
	p := filepath.Join(home, ".config", "kitty", "kitty.conf")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("font_size 12\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	off := func(string) bool { return false }
	on := func(name string) bool { return name == "kitty" }
	if _, _, err := ApplyEnabled(home, off, theme.Fallback, 85); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, p); strings.Contains(got, "background_opacity") {
		t.Fatalf("a disabled template got the line: %q", got)
	}
	if _, _, err := ApplyEnabled(home, on, theme.Fallback, 85); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ApplyEnabled(home, off, theme.Fallback, 85); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, p); strings.Contains(got, "background_opacity") || strings.Contains(got, "sysc-shell.conf") {
		t.Fatalf("turning kitty off left shell lines: %q", got)
	}
}

// A zero opacity is a config that never set the field, not a request for
// invisible terminals: it must leave the terminal alone like 100 does.
func TestZeroTerminalOpacityIsUnmanaged(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "kitty")
	p := filepath.Join(home, ".config", "kitty", "kitty.conf")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("font_size 12\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	on := func(name string) bool { return name == "kitty" }
	if _, _, err := ApplyEnabled(home, on, theme.Fallback, 0); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, p); strings.Contains(got, "background_opacity") {
		t.Fatalf("opacity 0 wrote a line: %q", got)
	}
}

// kitty applies a reloaded background_opacity only to windows started with
// dynamic_background_opacity on, so the shell owns that line too while it
// manages kitty's opacity, and removes it with the opacity.
func TestKittyOpacityEnablesDynamicOpacity(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "kitty")
	p := filepath.Join(home, ".config", "kitty", "kitty.conf")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("font_size 12\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	only := func(name string) bool { return name == "kitty" }
	if _, _, err := ApplyEnabled(home, only, theme.Fallback, 85); err != nil {
		t.Fatal(err)
	}
	got := readString(t, p)
	if strings.Count(got, "dynamic_background_opacity yes") != 1 || strings.Count(got, "background_opacity 0.85") != 1 {
		t.Fatalf("kitty.conf = %q, want background_opacity 0.85 and dynamic_background_opacity yes once each", got)
	}
	if _, _, err := ApplyEnabled(home, only, theme.Fallback, 100); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, p); strings.Contains(got, "background_opacity") {
		t.Fatalf("100 left an opacity line: %q", got)
	}
}
