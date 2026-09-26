package shell

import (
	"path/filepath"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// ccRateSelectors are the rate rings the Monitor page charts: receive and
// transmit for the interface, read and write for the device.
func ccRateSelectors(iface, device string) []services.Selector {
	var out []services.Selector
	if iface != "" {
		out = append(out,
			services.Selector{Source: services.SourceNetwork, Subject: iface, Direction: "rx"},
			services.Selector{Source: services.SourceNetwork, Subject: iface, Direction: "tx"})
	}
	if device != "" {
		out = append(out,
			services.Selector{Source: services.SourceBlock, Subject: device},
			services.Selector{Source: services.SourceBlock, Subject: device, Direction: "write"})
	}
	return out
}

func rootFilesystemSource(snap services.Snapshot) string {
	if snap.Filesystem == nil {
		return ""
	}
	for _, fs := range snap.Filesystem.Filesystems {
		if fs.MountPoint == "/" {
			return fs.Source
		}
	}
	return ""
}

// rateSubjectPanels are the hosts that chart one interface and one device:
// the Control Centre's Monitor page and the system monitor's System page.
var rateSubjectPanels = [...]PanelID{PanelControlCenter, PanelMonitor}

// updateRootDevice caches the root filesystem's backing device on each host
// that charts one, resolving the source off Registry.mu and only when it
// changes. UpdateMetrics calls this from its sampling goroutine.
func (r *Registry) updateRootDevice(snap services.Snapshot, resolve func(string) (string, error)) {
	if snap.Filesystem == nil {
		return
	}
	source := rootFilesystemSource(snap)
	var stale []*PanelHost
	r.mu.Lock()
	for _, id := range rateSubjectPanels {
		if h := r.panelHosts[id]; h != nil && h.ccRootSource != source {
			h.ccRootSource = source
			stale = append(stale, h)
		}
	}
	r.mu.Unlock()
	if len(stale) == 0 {
		return
	}

	device := ""
	if source != "" {
		if path, err := resolve(source); err == nil {
			device = filepath.Base(path)
		}
	}

	r.mu.Lock()
	for _, h := range stale {
		if r.panelHosts[h.id] == h && h.ccRootSource == source {
			h.ccRootDevice = device
		}
	}
	r.mu.Unlock()
}

// syncRateSubjectsLocked keeps one interface and one device leased for a host
// in rateSubjectPanels. A subject changes when it disappears or the root
// filesystem source changes. Caller holds r.mu.
func (r *Registry) syncRateSubjectsLocked(h *PanelHost, snap services.Snapshot, interval time.Duration) {
	if h == nil || r.metrics == nil {
		return
	}
	// A nil source is a failed pass, not a missing subject. Re-resolving on it
	// would release the rings and restart the chart from nothing.
	iface, device := h.ccIface, h.ccDevice
	if snap.Network != nil && !snapshotHasInterface(snap, iface) {
		iface = primaryInterface(snap)
	}
	rootSourceChanged := h.ccRootSource != h.ccRootSelectionSource
	if snap.Block != nil && (rootSourceChanged || !snapshotHasDevice(snap, device)) {
		device = primaryBlockDevice(snap, h.ccRootDevice)
	}
	if iface == h.ccIface && device == h.ccDevice {
		if snap.Block != nil {
			h.ccRootSelectionSource = h.ccRootSource
		}
		return
	}
	var leases []*services.Lease
	for _, sel := range ccRateSelectors(iface, device) {
		lease, err := r.metrics.Acquire(sel, interval)
		if err != nil {
			releaseAll(leases)
			return
		}
		leases = append(leases, lease)
	}
	releaseAll(h.subjectLeases)
	h.subjectLeases, h.ccIface, h.ccDevice = leases, iface, device
	if snap.Block != nil {
		h.ccRootSelectionSource = h.ccRootSource
	}
}

func snapshotHasInterface(snap services.Snapshot, name string) bool {
	if name == "" || snap.Network == nil {
		return false
	}
	for _, i := range snap.Network.Interfaces {
		if i.Name == name {
			return true
		}
	}
	return false
}

func snapshotHasDevice(snap services.Snapshot, name string) bool {
	if name == "" || snap.Block == nil {
		return false
	}
	for _, d := range snap.Block.Devices {
		if d.Name == name {
			return true
		}
	}
	return false
}
