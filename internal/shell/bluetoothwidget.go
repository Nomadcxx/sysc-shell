package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const panelBluetoothAction = "panel:bluetooth"

// buildBluetoothWidget is only a route to the shared Bluetooth body. It does
// not retain adapter or device state of its own.
//
// It takes the density row like its neighbours rather than reading
// DefaultTheme(): a widget that resolves its own default ignores the configured
// density, which is the whole defect the metrics row was threaded to fix.
func buildBluetoothWidget(m theme.Metrics) textWidget {
	icon := &ui.Node{
		Kind: ui.KindIcon, Icon: "bluetooth_disabled", IconSize: m.IconNormal,
		Action: panelBluetoothAction, Name: "Bluetooth", Role: "button",
	}
	return textWidget{
		node:    icon,
		tooltip: "Bluetooth",
		refresh: func(v barView) bool { return refreshBluetoothWidget(icon, v) },
	}
}

func bluetoothGlyph(state services.BluetoothState) string {
	if !state.Available || !state.Adapter.Powered {
		return "bluetooth_disabled"
	}
	for _, device := range state.Devices {
		if device.Connected {
			return "bluetooth_connected"
		}
	}
	return "bluetooth"
}

func refreshBluetoothWidget(icon *ui.Node, view barView) bool {
	name := bluetoothGlyph(view.Bluetooth)
	if icon.Icon == name {
		return false
	}
	icon.Icon = name
	return true
}
