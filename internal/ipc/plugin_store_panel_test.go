package ipc

import "testing"

func TestPluginStoreIsKnownPanel(t *testing.T) {
	if _, ok := knownPanels["plugin-store"]; !ok {
		t.Fatal("plugin-store is not accepted by panel.open")
	}
}
