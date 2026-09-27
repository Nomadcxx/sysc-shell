package theming

import (
	"strings"
	"testing"
)

func TestGhosttyTemplateRendersCompleteOutput(t *testing.T) {
	t.Parallel()
	out := renderComplete(t, "ghostty")
	assertHexAssignments(t, out)
	if n := strings.Count(out, "palette = "); n != 16 {
		t.Errorf("palette entries: got %d, want 16", n)
	}
	for _, want := range []string{"background = #", "foreground = #", "cursor-color = #", "cursor-text = #", "selection-background = #", "selection-foreground = #"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}
