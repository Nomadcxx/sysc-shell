package ipc

import "testing"

func TestAudioPanelIsReachableOverIPC(t *testing.T) {
	if _, ok := knownPanels["audio"]; !ok {
		t.Fatal("the audio panel must be reachable over IPC like the launcher")
	}
}
