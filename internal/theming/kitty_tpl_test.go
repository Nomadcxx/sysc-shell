package theming

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKittyTemplateRendersCompleteOutput(t *testing.T) {
	t.Parallel()
	out := renderComplete(t, "kitty")
	assertHexAssignments(t, out)
	for _, want := range []string{"color0 ", "color15 ", "cursor_text_color", "active_tab_background", "url_color", "cursor_trail_color"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// A kitty config holds many includes -- tab styles, other colour files -- and
// none of them is a choice the shell's include replaces. Treating any include
// as a conflict refused to wire the theme at all.
func TestKittyDirectiveCoexistsWithUserIncludes(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	directive := templateTargets["kitty"].directives(home)[0]
	original := "font_size 12.0\ninclude dank-tabs.conf\ninclude dank-theme.conf\n"
	if err := os.MkdirAll(filepath.Dir(directive.file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(directive.file, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDirective(directive); err != nil {
		t.Fatalf("wiring beside user includes = %v", err)
	}
	want := original + "include themes/sysc-shell.conf\n"
	if got, err := os.ReadFile(directive.file); err != nil || string(got) != want {
		t.Fatalf("kitty.conf = %q, %v; want the user's includes kept and ours appended last", got, err)
	}
	if err := EnsureDirective(directive); err != nil {
		t.Fatalf("second apply = %v, want a no-op", err)
	}
}
