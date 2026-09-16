package ipc

import "testing"

func TestClipboardPanelIsReachableOverIPC(t *testing.T) {
	if _, ok := knownPanels["clipboard"]; !ok {
		t.Fatal("the clipboard panel must be reachable over IPC like the other panels")
	}
}
