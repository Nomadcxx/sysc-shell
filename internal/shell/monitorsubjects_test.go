package shell

import (
	"testing"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/services"
)

func TestPrimaryInterface(t *testing.T) {
	iface := func(name string, rx, tx uint64) metrics.NetworkInterface {
		return metrics.NetworkInterface{Name: name, ReceiveBytes: rx, TransmitBytes: tx}
	}
	for _, tc := range []struct {
		name string
		ifs  []metrics.NetworkInterface
		want string
	}{
		{"this desktop", []metrics.NetworkInterface{
			iface("br-07deea98ce79", 10, 10), iface("docker0", 50, 50), iface("enp7s0", 9e9, 1e9),
			iface("lo", 9e12, 9e12), iface("tailscale0", 1e6, 1e6), iface("veth0e02b9d", 100, 100),
		}, "enp7s0"},
		{"loopback only", []metrics.NetworkInterface{iface("lo", 5, 5)}, ""},
		{"tie breaks by name", []metrics.NetworkInterface{iface("wlan0", 5, 5), iface("eth0", 5, 5)}, "eth0"},
		{"none", nil, ""},
	} {
		snap := services.Snapshot{Network: &metrics.NetworkSnapshot{Interfaces: tc.ifs}}
		if got := primaryInterface(snap); got != tc.want {
			t.Errorf("%s: primaryInterface = %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := primaryInterface(services.Snapshot{}); got != "" {
		t.Errorf("nil network = %q", got)
	}
}

func TestPrimaryBlockDevice(t *testing.T) {
	dev := func(name string, r, w uint64) metrics.BlockDevice {
		return metrics.BlockDevice{Name: name, ReadBytes: r, WriteBytes: w}
	}
	snap := services.Snapshot{
		Filesystem: &metrics.FilesystemSnapshot{Filesystems: []metrics.Filesystem{
			{MountPoint: "/", Source: "/dev/mapper/ArchinstallVg-root"},
			{MountPoint: "/boot", Source: "/dev/nvme0n1p1"},
		}},
		Block: &metrics.BlockSnapshot{Devices: []metrics.BlockDevice{
			dev("dm-0", 10, 10), dev("nvme0n1", 500, 500), dev("zram0", 9e9, 9e9), dev("loop0", 8e9, 0),
		}},
	}
	if got := primaryBlockDevice(snap, "dm-0"); got != "dm-0" {
		t.Errorf("root device = %q, want dm-0", got)
	}
	if got := primaryBlockDevice(snap, ""); got != "nvme0n1" {
		t.Errorf("fallback = %q, want the busiest real device, not zram or loop", got)
	}
	if got := primaryBlockDevice(snap, "sdz"); got != "nvme0n1" {
		t.Errorf("unlisted root device = %q, want the fallback", got)
	}
	if got := primaryBlockDevice(services.Snapshot{}, ""); got != "" {
		t.Errorf("no block snapshot = %q", got)
	}
}
