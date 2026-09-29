package theming

import (
	"strings"
	"testing"
)

func TestHelixTemplateRendersCompleteOutput(t *testing.T) {
	t.Parallel()
	out := renderComplete(t, "helix")
	assertHexAssignments(t, out)
	for _, want := range []string{"\"attribute\"", "\"comment\"", "\"keyword\"", "\"type\"", "\"function\""} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s", want)
		}
	}
}
