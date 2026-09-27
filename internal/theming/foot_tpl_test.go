package theming

import (
	"strings"
	"testing"
)

func TestFootTemplateRendersCompleteOutput(t *testing.T) {
	t.Parallel()
	out := renderComplete(t, "foot")
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "=") && strings.Contains(line, "#") {
			t.Errorf("foot wants hashless hex: %s", line)
		}
	}
	if !strings.Contains(out, "[colors-dark]") {
		t.Error("missing [colors-dark] section")
	}
	if !strings.Contains(out, "cursor=") {
		t.Error("missing cursor")
	}
}
