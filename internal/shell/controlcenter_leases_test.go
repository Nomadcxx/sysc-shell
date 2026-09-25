package shell

import (
	"testing"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/services"
)

func ccSubjectSnapshot(ifaces ...string) services.Snapshot {
	snap := services.Snapshot{
		Network: &metrics.NetworkSnapshot{},
		Block:   &metrics.BlockSnapshot{Devices: []metrics.BlockDevice{{Name: "nvme0n1", ReadBytes: 1}}},
	}
	for i, name := range ifaces {
		snap.Network.Interfaces = append(snap.Network.Interfaces,
			metrics.NetworkInterface{Name: name, ReceiveBytes: uint64(1000 - i)})
	}
	return snap
}

func TestControlCentreLeasesStorageNetworkAndBlock(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	for _, sel := range []services.Selector{
		{Source: services.SourceFilesystem, Subject: "/"},
		{Source: services.SourceNetwork},
		{Source: services.SourceBlock},
	} {
		if !r.metrics.Leased(sel) {
			t.Errorf("control centre does not lease %v", sel)
		}
	}
}

func TestControlCentreResolvesSubjectsOnceAndKeepsThem(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	r.syncControlCentreSubjectsLocked(h, ccSubjectSnapshot("enp7s0", "wlan0"))
	r.mu.Unlock()
	for _, sel := range ccRateSelectors("enp7s0", "nvme0n1") {
		if !r.metrics.Leased(sel) {
			t.Errorf("rate selector %v is not leased", sel)
		}
	}

	// A new, busier interface appearing does not move the chart.
	r.mu.Lock()
	busier := ccSubjectSnapshot("enp7s0")
	busier.Network.Interfaces = append(busier.Network.Interfaces,
		metrics.NetworkInterface{Name: "tailscale0", ReceiveBytes: 1 << 40})
	r.syncControlCentreSubjectsLocked(h, busier)
	got := h.ccIface
	r.mu.Unlock()
	if got != "enp7s0" {
		t.Fatalf("interface moved to %q while enp7s0 still exists", got)
	}

	// The chosen interface disappearing re-resolves and releases the old leases.
	r.mu.Lock()
	r.syncControlCentreSubjectsLocked(h, ccSubjectSnapshot("wlan0"))
	got = h.ccIface
	r.mu.Unlock()
	if got != "wlan0" {
		t.Fatalf("interface = %q after enp7s0 vanished, want wlan0", got)
	}
	if r.metrics.Leased(services.Selector{Source: services.SourceNetwork, Subject: "enp7s0", Direction: "rx"}) {
		t.Error("the vanished interface is still leased")
	}

	r.ClosePanel(PanelControlCenter)
	for _, sel := range ccRateSelectors("wlan0", "nvme0n1") {
		if r.metrics.Leased(sel) {
			t.Errorf("%v is still leased after close", sel)
		}
	}
}

// A failed network or block pass leaves that source nil. That is no
// information, not a missing subject: the chart keeps its subject and its
// ring rather than blanking and restarting from nothing.
func TestControlCentreKeepsSubjectsThroughAFailedPass(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.panelHosts[PanelControlCenter]
	r.syncControlCentreSubjectsLocked(h, ccSubjectSnapshot("enp7s0"))
	r.syncControlCentreSubjectsLocked(h, services.Snapshot{})
	if h.ccIface != "enp7s0" || h.ccDevice != "nvme0n1" {
		t.Fatalf("subjects after a failed pass = %q, %q; want enp7s0, nvme0n1", h.ccIface, h.ccDevice)
	}
	for _, sel := range ccRateSelectors("enp7s0", "nvme0n1") {
		if !r.metrics.Leased(sel) {
			t.Errorf("%v was released by a failed pass", sel)
		}
	}
}
