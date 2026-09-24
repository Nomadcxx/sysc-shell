package shell

import (
	"path/filepath"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// primaryInterface is the non-loopback interface that has carried the most
// traffic. Cumulative bytes rather than the current rate, so the pick does not
// follow a momentary burst on a bridge.
func primaryInterface(snap services.Snapshot) string {
	if snap.Network == nil {
		return ""
	}
	best, bestBytes := "", uint64(0)
	for _, i := range snap.Network.Interfaces {
		if i.Name == "lo" {
			continue
		}
		total := i.ReceiveBytes + i.TransmitBytes
		if best == "" || total > bestBytes || (total == bestBytes && i.Name < best) {
			best, bestBytes = i.Name, total
		}
	}
	return best
}

// primaryBlockDevice is the device backing /, or failing that the busiest
// real device. Loop, RAM and zram devices are never chosen.
func primaryBlockDevice(snap services.Snapshot, resolve func(string) (string, error)) string {
	if snap.Block == nil {
		return ""
	}
	listed := make(map[string]bool, len(snap.Block.Devices))
	for _, d := range snap.Block.Devices {
		listed[d.Name] = true
	}
	if snap.Filesystem != nil {
		for _, fs := range snap.Filesystem.Filesystems {
			if fs.MountPoint != "/" {
				continue
			}
			if path, err := resolve(fs.Source); err == nil && listed[filepath.Base(path)] {
				return filepath.Base(path)
			}
		}
	}
	best, bestBytes := "", uint64(0)
	for _, d := range snap.Block.Devices {
		if strings.HasPrefix(d.Name, "loop") || strings.HasPrefix(d.Name, "ram") || strings.HasPrefix(d.Name, "zram") {
			continue
		}
		total := d.ReadBytes + d.WriteBytes
		if best == "" || total > bestBytes || (total == bestBytes && d.Name < best) {
			best, bestBytes = d.Name, total
		}
	}
	return best
}

func resolveDevicePath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
