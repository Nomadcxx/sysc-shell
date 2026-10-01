package ipc

import "testing"

func TestScreenshotPanelIsReachableOverIPC(t *testing.T) {
	if _, ok := knownPanels["screenshot"]; !ok {
		t.Fatal("the screenshot panel must be reachable over IPC like the other panels")
	}
}
