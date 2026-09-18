package ipc

import "testing"

func TestWeatherPanelIsReachableOverIPC(t *testing.T) {
	if _, ok := knownPanels["weather"]; !ok {
		t.Fatal("the weather panel must be reachable over IPC like the other panels")
	}
}
