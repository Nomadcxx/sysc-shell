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

func TestControlCentreResolvesRootDeviceOncePerSource(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	snap := ccSubjectSnapshot("enp7s0")
	snap.Block.Devices = append(snap.Block.Devices, metrics.BlockDevice{Name: "dm-0"})
	snap.Filesystem = &metrics.FilesystemSnapshot{Filesystems: []metrics.Filesystem{{
		MountPoint: "/", Source: "/dev/mapper/root",
	}}}
	calls, resolvedUnlocked := 0, true
	resolve := func(source string) (string, error) {
		calls++
		if r.mu.TryLock() {
			r.mu.Unlock()
		} else {
			resolvedUnlocked = false
		}
		if source == "/dev/mapper/root" {
			return "/dev/dm-0", nil
		}
		return "/dev/nvme0n1", nil
	}
	for i := 0; i < 3; i++ {
		r.updateControlCentreRootDevice(snap, resolve)
		r.mu.Lock()
		h := r.panelHosts[PanelControlCenter]
		r.syncControlCentreSubjectsLocked(h, snap)
		r.mu.Unlock()
	}
	if calls != 1 || !resolvedUnlocked {
		t.Fatalf("resolve calls = %d, unlocked = %v; want one call outside Registry.mu", calls, resolvedUnlocked)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	got := h.ccDevice
	r.mu.Unlock()
	if got != "dm-0" {
		t.Fatalf("root device = %q, want dm-0", got)
	}
	failedFilesystem := snap
	failedFilesystem.Filesystem = nil
	r.updateControlCentreRootDevice(failedFilesystem, resolve)
	if calls != 1 {
		t.Fatalf("a failed filesystem pass re-resolved the root %d times", calls)
	}

	snap.Filesystem.Filesystems[0].Source = "/dev/mapper/new-root"
	r.updateControlCentreRootDevice(snap, resolve)
	r.mu.Lock()
	r.syncControlCentreSubjectsLocked(h, snap)
	got = h.ccDevice
	r.mu.Unlock()
	if calls != 2 || got != "nvme0n1" {
		t.Fatalf("changed root source: resolve calls = %d, device = %q; want 2 and nvme0n1", calls, got)
	}
}
