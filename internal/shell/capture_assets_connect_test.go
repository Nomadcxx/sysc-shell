package shell

import (
	"testing"
	"time"

	clipboardprotocol "github.com/Nomadcxx/sysc-clipboard/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// The connectivity and clipboard scenes. Every name below is invented: no
// network, device or clipboard entry comes from a real machine.

func TestAssetNetwork(t *testing.T) {
	backend := &shellNetworkBackend{
		state: services.NetworkState{
			Kind: services.ConnWireless, Connected: true, WirelessEnabled: true,
			SSID: "Harbour Fibre", IPv4: "192.168.1.24", Interface: "wlan0", Strength: 86,
		},
		aps: []services.AccessPoint{
			{Path: "/ap/1", SSID: "Harbour Fibre", Strength: 86, Secured: true, Active: true, Saved: true},
			{Path: "/ap/2", SSID: "Harbour Fibre 5G", Strength: 71, Secured: true, Saved: true},
			{Path: "/ap/3", SSID: "Studio Mesh", Strength: 54, Secured: true},
			{Path: "/ap/4", SSID: "Library Guest", Strength: 38},
			{Path: "/ap/5", SSID: "Corner Cafe", Strength: 22, Secured: true},
		},
	}
	captureAssetPanelWith(t, PanelNetwork, "network", "wifi",
		func(reg *Registry) {
			assetBase(t, reg)
			reg.setNetwork(services.NewNetwork(backend))
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) && len(reg.network.CachedAccessPoints()) < len(backend.aps) {
				time.Sleep(time.Millisecond)
			}
		}, nil)
}

func TestAssetBluetooth(t *testing.T) {
	battery := func(v uint8) *uint8 { return &v }
	captureAssetPanel(t, PanelBluetooth, "bluetooth", "default", func(reg *Registry, h *PanelHost) {
		assetBase(t, reg)
		reg.bluetoothState = services.BluetoothState{
			Available: true, AgentReady: true,
			Adapter: services.BluetoothAdapter{Powered: true},
			Devices: []services.BluetoothDevice{
				{ID: "headphones", Alias: "Studio Headphones", Address: "AA:00:00:00:00:01", Icon: "audio-headphones", Paired: true, Trusted: true, Connected: true, Battery: battery(78)},
				{ID: "keyboard", Alias: "Keyboard", Address: "AA:00:00:00:00:02", Icon: "input-keyboard", Paired: true, Trusted: true},
				{ID: "trackball", Alias: "Trackball", Address: "AA:00:00:00:00:03", Icon: "input-mouse", Paired: true, Trusted: true},
				{ID: "speaker", Alias: "Living Room Speaker", Address: "AA:00:00:00:00:04", Icon: "audio-speakers"},
			},
		}
	})
}

func TestAssetClipboard(t *testing.T) {
	entry := func(id, preview string, ago time.Duration, pinned bool) clipboardprotocol.Entry {
		return clipboardprotocol.Entry{
			ID: id, Kind: clipboardprotocol.KindText, MIME: "text/plain", OfferedMIME: []string{"text/plain"},
			Size: uint64(len(preview)), SHA256: id, CapturedAt: assetNow.Add(-ago), Preview: preview, Pinned: pinned,
		}
	}
	captureAssetPanel(t, PanelClipboard, "clipboard", "default", func(reg *Registry, h *PanelHost) {
		assetBase(t, reg)
		reg.clipboard = clipboardProjection{Connected: true, Snapshot: clipboardprotocol.Snapshot{
			Persistence: clipboardprotocol.PersistenceDurable, Wayland: clipboardprotocol.WaylandReady,
			Entries: []clipboardprotocol.Entry{
				entry("a1", "git rebase --onto main feature~3 feature", 2*time.Minute, true),
				entry("a2", "https://nomadcxx.github.io/sysc/docs/", 9*time.Minute, false),
				entry("a3", "Lunch on Thursday at 12:30? I booked the corner table.", 24*time.Minute, false),
				entry("a4", "Order 48213 confirmed. Delivery on Monday between 9 and 12.", 71*time.Minute, false),
				entry("a5", "sudo systemctl --user restart sysc-shell.service", 3*time.Hour, false),
				entry("a6", "Design review notes: tighten the bar spacing, keep the cat.", 5*time.Hour, false),
			},
		}}
	})
}
