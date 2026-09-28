package theming

import (
	"os"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestScrollTemplateRendersCompleteOutput(t *testing.T) {
	t.Parallel()
	out := renderComplete(t, "scroll")
	for _, want := range []string{"set $primary  ", "set $on_secondary_transp #", "client.focused ", "jump_labels_background"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "set $") && strings.Contains(line, "#") {
			val := line[strings.Index(line, "#"):]
			if _, err := theme.ParseColor(strings.TrimRight(val, "A")); err != nil && !strings.Contains(val, "$") {
				t.Errorf("bad set line: %s: %v", line, err)
			}
		}
	}
}

func TestScrollAppliesSidecarAndInclude(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	markTemplatesComplete(t, "scroll")
	dir := joined(home, ".config", "scroll")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	user := "font_family Iosevka\n"
	if err := os.WriteFile(dir+"/config", []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyForTest(home, func(string) bool { return true }, theme.Fallback); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dir + "/config")
	s := string(b)
	if !strings.Contains(s, "font_family Iosevka") || strings.Count(s, "include ~/.config/scroll/sysc-shell") != 1 {
		t.Fatalf("include not managed: %s", s)
	}
	side, err := os.ReadFile(dir + "/sysc-shell")
	if err != nil || !strings.Contains(string(side), "client.focused ") {
		t.Fatalf("sidecar: %v %q", err, side)
	}
	if err := applyForTest(home, func(string) bool { return false }, theme.Fallback); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(dir + "/config")
	if strings.TrimSpace(string(b)) != strings.TrimSpace(user) {
		t.Fatalf("disable did not restore: %q", b)
	}
	if _, err := os.Stat(dir + "/sysc-shell"); !os.IsNotExist(err) {
		t.Fatal("sidecar survived disable")
	}
}
