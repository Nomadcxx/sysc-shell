package shell

import (
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

// syncControlCentreSubjectsLocked keeps one interface and one device leased.
// A subject is re-resolved only when the snapshot no longer carries it.
// Caller holds r.mu.
func (r *Registry) syncControlCentreSubjectsLocked(h *PanelHost, snap services.Snapshot) {
	if h == nil || r.metrics == nil {
		return
	}
	iface, device := h.ccIface, h.ccDevice
	if !snapshotHasInterface(snap, iface) {
		iface = primaryInterface(snap)
	}
	if !snapshotHasDevice(snap, device) {
		device = primaryBlockDevice(snap, resolveDevicePath)
	}
	if iface == h.ccIface && device == h.ccDevice {
		return
	}
	var leases []*services.Lease
	for _, sel := range ccRateSelectors(iface, device) {
		lease, err := r.metrics.Acquire(sel, time.Second)
		if err != nil {
			releaseAll(leases)
			return
		}
		leases = append(leases, lease)
	}
	releaseAll(h.subjectLeases)
	h.subjectLeases, h.ccIface, h.ccDevice = leases, iface, device
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
