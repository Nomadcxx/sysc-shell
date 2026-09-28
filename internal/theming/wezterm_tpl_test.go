package theming

import (
	"strings"
	"testing"
)

func TestWeztermTemplateRendersCompleteOutput(t *testing.T) {
	t.Parallel()
	out := renderComplete(t, "wezterm")
	assertHexAssignments(t, out)
	for _, want := range []string{"[colors]", "ansi = [", "brights = [", "[colors.indexed]", "[colors.tab_bar.active_tab]", "name = \"sysc-shell\""} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if !strings.Contains(out, "background = \"#") || strings.Contains(out, "{{darken") {
		t.Error("darken did not render")
	}
}
