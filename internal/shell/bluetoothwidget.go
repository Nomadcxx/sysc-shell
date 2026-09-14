package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const panelBluetoothAction = "panel:bluetooth"

// buildBluetoothWidget is only a route to the shared Bluetooth body. It does
// not retain adapter or device state of its own.
func buildBluetoothWidget() textWidget {
	icon := &ui.Node{
		Kind: ui.KindIcon, Icon: "bluetooth_disabled", IconSize: DefaultTheme().Metrics.IconNormal,
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
