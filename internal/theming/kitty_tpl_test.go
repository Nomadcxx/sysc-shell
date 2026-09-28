package theming

import (
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
