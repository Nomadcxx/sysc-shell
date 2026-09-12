package shell

import (
	"testing"
)

func TestPanelNetworkTargetSizeAndDefaultTab(t *testing.T) {
	if got := panelTargetSize(PanelNetwork); got.W != 460 || got.H != 560 {
		t.Fatalf("panelTargetSize = %dx%d, want 460x560", got.W, got.H)
	}
	h := &PanelHost{id: PanelNetwork}
	_ = networkTree(&Registry{}, h)
	if h.networkTab != "wifi" {
		t.Fatalf("default tab = %q, want wifi", h.networkTab)
	}
}

// The panel must survive a registry with no network service at all: that is
// the state on a machine without NetworkManager, and a nil dereference here
// paints nothing while the service still reads active.
func TestNetworkTreeToleratesAnAbsentService(t *testing.T) {
	h := &PanelHost{id: PanelNetwork}
	if got := networkTree(&Registry{}, h); got == nil {
		t.Fatal("networkTree returned nil with no service; it must still paint")
	}
}
