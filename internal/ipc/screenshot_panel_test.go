package ipc

import "testing"

// The launcher panel is a plugin's now; only the screenshot verb stays in the
// shell, and it needs no panel name.
func TestScreenshotIsNotAShellPanel(t *testing.T) {
	if _, ok := knownPanels["screenshot"]; ok {
		t.Fatal("the shell no longer owns a screenshot panel")
	}
}
