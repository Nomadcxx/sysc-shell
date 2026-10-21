package shell

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const panelBluetoothAction = "panel:bluetooth"

// buildBluetoothWidget is only a route to the shared Bluetooth body. It does
// not retain adapter or device state of its own.
func buildBluetoothWidget() textWidget {
	icon := &ui.Node{
		Kind: ui.KindIcon, Icon: "bluetooth_disabled", IconSize: DefaultTheme().Metrics.IconNormal,
	}
	battery := &ui.Node{Kind: ui.KindText, Tabular: true}
	row := &ui.Node{
		Kind:   ui.KindRow,
		Action: panelBluetoothAction, Name: "Bluetooth", Role: "button",
		Children: []*ui.Node{icon, battery},
	}
	return textWidget{
		node:    row,
		tooltip: "Bluetooth",
		refresh: func(v barView) bool { return refreshBluetoothWidget(row, icon, battery, v) },
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

func bluetoothBatteryText(state services.BluetoothState) string {
	if !state.Available || !state.Adapter.Powered {
		return ""
	}
	var lowest uint8
	found := false
	for _, device := range state.Devices {
		if !device.Connected || device.Battery == nil {
			continue
		}
		if !found || *device.Battery < lowest {
			lowest, found = *device.Battery, true
		}
	}
	if !found {
		return ""
	}
	return fmt.Sprintf("%d%%", lowest)
}

func refreshBluetoothWidget(row, icon, battery *ui.Node, view barView) bool {
	changed := false
	if name := bluetoothGlyph(view.Bluetooth); icon.Icon != name {
		icon.Icon = name
		changed = true
	}
	if text := bluetoothBatteryText(view.Bluetooth); battery.Text != text {
		battery.Text = text
		changed = true
	}
	gap := 0
	if battery.Text != "" {
		gap = theme.MarginXS
	}
	if row.Gap != gap {
		row.Gap = gap
		changed = true
	}
	return changed
}
