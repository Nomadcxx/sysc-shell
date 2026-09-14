package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestRegistryBluetoothRelayPublishesOneSnapshotToVisibleBars(t *testing.T) {
	svc := &services.Bluetooth{}
	icon := &ui.Node{Kind: ui.KindIcon, Icon: "bluetooth_disabled"}
	bar := &Bar{right: []textWidget{{
		node: icon, inner: icon,
		refresh: func(view barView) bool {
			want := "bluetooth_disabled"
			if view.Bluetooth.Available && view.Bluetooth.Adapter.Powered {
				want = "bluetooth"
			}
			if icon.Icon == want {
				return false
			}
			icon.Icon = want
			return true
		},
	}}}
	r := &Registry{
		bluetooth:      svc,
		bluetoothState: services.BluetoothState{},
		bars:           map[uint32]*Bar{7: bar},
		metrics:        services.NewMetrics(),
		notify:         newNotifyState(),
		invalidations:  make(chan wayland.Invalidation, 1),
		closed:         make(chan struct{}),
	}
	t.Cleanup(r.metrics.Close)

	r.publishBluetoothSnapshot(svc, services.BluetoothState{
		Available: true,
		Adapter:   services.BluetoothAdapter{Powered: true},
	})

	if !r.bluetoothState.Available || !r.bluetoothState.Adapter.Powered {
		t.Fatalf("registry state = %+v, want powered snapshot", r.bluetoothState)
	}
	if icon.Icon != "bluetooth" {
		t.Fatalf("bar icon = %q, want bluetooth", icon.Icon)
	}
	select {
	case invalidation := <-r.invalidations:
		if invalidation.Global != 7 {
			t.Fatalf("invalidation global = %d, want 7", invalidation.Global)
		}
	default:
		t.Fatal("Bluetooth snapshot did not invalidate the changed bar")
	}
}
