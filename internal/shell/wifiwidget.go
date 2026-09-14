package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Hit-testing uses the single Action plus the pointer button: left opens the
// network panel, right toggles the radio.
//
// The widget owns no popup of its own. The device list, the access points and
// the password prompt exist once, in PanelNetwork, and this glyph is a route
// to them.
const panelWifiAction = "panel:wifi"

func buildWifiWidget(m theme.Metrics) textWidget {
	icon := &ui.Node{
		Kind: ui.KindIcon, Icon: "wifi_off", IconSize: m.IconNormal,
		Action: panelWifiAction,
	}
	return textWidget{
		node:    icon,
		tooltip: "Network",
		refresh: func(v barView) bool { return refreshWifiWidget(icon, v) },
	}
}

// wifiGlyph ports the reference shell's glyph logic: a wired link wins, the
// radio being off is its own glyph, and otherwise the signal band chooses.
//
// Being switched on with no association is band zero, not "off": the two are
// different facts and the user acts on them differently.
func wifiGlyph(st services.NetworkState) string {
	if st.Kind == services.ConnWired && st.Connected {
		return "lan"
	}
	if !st.WirelessEnabled {
		return "wifi_off"
	}
	if st.Kind == services.ConnWireless && st.Connected {
		return wifiBandGlyph(st.Strength)
	}
	return "signal_wifi_0_bar"
}

// wifiBandGlyph maps a band to its glyph. Banding rather than the raw percent
// is what stops the bar glyph flickering between two icons while a scan runs.
func wifiBandGlyph(strength uint8) string {
	switch services.SignalBand(strength) {
	case 4:
		return "signal_wifi_4_bar"
	case 3:
		return "network_wifi_3_bar"
	case 2:
		return "network_wifi_2_bar"
	case 1:
		return "network_wifi_1_bar"
	default:
		return "signal_wifi_0_bar"
	}
}

// refreshWifiWidget reports whether anything changed, so Bar.apply can skip a
// repaint. The node holds the last glyph, as every other widget does.
func refreshWifiWidget(icon *ui.Node, v barView) bool {
	name := wifiGlyph(v.Network)
	if icon.Icon == name {
		return false
	}
	icon.Icon = name
	return true
}
