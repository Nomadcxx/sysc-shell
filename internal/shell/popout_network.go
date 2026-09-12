package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// absent is what a figure with no value renders as. An absent metric is not a
// zero: "0.0.0.0" and "no address" are different facts, and the dash keeps the
// column's space so the row does not reflow when a value arrives.
const absent = "—"

// networkTree builds PanelNetwork.
//
// Wi-Fi is the default tab, seeded here rather than at open so every entry
// point agrees: IPC, the bar glyph and a keybind all land on the same page.
func networkTree(r *Registry, h *PanelHost) *ui.Node {
	if h.networkTab == "" {
		h.networkTab = "wifi"
	}
	m := h.metrics()
	st := services.NetworkState{}
	if r != nil && r.network != nil {
		st = r.network.CachedState()
	}
	return &ui.Node{
		Kind: ui.KindColumn, Gap: 12, Padding: m.PanelPadding,
		Children: []*ui.Node{
			networkHeaderCard(h, st, m),
		},
	}
}

// networkHeaderCard is Direction B's status block: the active connection is
// promoted above the tabs, so "what am I on?" is answered without reading the
// list below it.
func networkHeaderCard(h *PanelHost, st services.NetworkState, m theme.Metrics) *ui.Node {
	well := m.StandardControl
	closeSz := m.CompactControl
	wired := h != nil && h.networkTab == "ethernet"

	icon := &ui.Node{
		Kind: ui.KindCapsule, Width: well, Height: well, Shape: ui.ShapeMedium,
		Fill:     ui.FillContainerHighest,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: wifiGlyph(st), IconSize: m.IconNormal}},
	}

	title := &ui.Node{Kind: ui.KindColumn, Gap: 2, Children: []*ui.Node{
		{
			Kind: ui.KindText, Text: headerTitle(st, wired),
			TextRole: theme.RoleTitle, Name: "Network", Role: "heading",
		},
		{Kind: ui.KindText, Text: headerDetail(st, wired), TextRole: theme.RoleCaption},
	}}

	closeBtn := &ui.Node{
		Kind: ui.KindButton, Action: "network-close", Name: "Close", Role: "button",
		Focusable: true, Width: closeSz, Height: closeSz, Shape: ui.ShapeCircle,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "close", IconSize: m.IconNormal}},
	}

	trailing := &ui.Node{Kind: ui.KindRow, Gap: 8, Height: well}
	if !wired {
		// The wired link has no radio to switch, so the toggle belongs to the
		// Wi-Fi tab alone.
		trailing.Children = append(trailing.Children, &ui.Node{
			Kind: ui.KindToggle, Value: toggleValue(st.WirelessEnabled),
			Action: "network-radio", Name: "Wi-Fi", Role: "switch", Focusable: true,
		})
	}
	trailing.Children = append(trailing.Children, closeBtn)

	top := &ui.Node{Kind: ui.KindRow, Gap: 12, Height: well, PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindRow, Gap: 12, Height: well, Children: []*ui.Node{icon, title}},
		trailing,
	}}

	return &ui.Node{
		Kind: ui.KindCapsule, Padding: m.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: 12, Children: []*ui.Node{
			top,
			{Kind: ui.KindSeparator},
			networkFigures(st),
		}}},
	}
}

// networkFigures is the three-column row under the separator. Every value is
// tabular so the row does not jitter as figures change.
//
// Down and Up are dashes today: services.NetworkState carries no throughput,
// and inventing a number here would be worse than admitting there is none.
// Wiring them to the rate source is its own slice.
func networkFigures(st services.NetworkState) *ui.Node {
	return &ui.Node{Kind: ui.KindRow, Gap: 8, Children: []*ui.Node{
		networkFigure("IPv4", orAbsent(st.IPv4)),
		networkFigure("Down", absent),
		networkFigure("Up", absent),
	}}
}

func networkFigure(label, value string) *ui.Node {
	return &ui.Node{Kind: ui.KindColumn, Gap: 2, Children: []*ui.Node{
		{Kind: ui.KindText, Text: label, TextRole: theme.RoleCaption},
		{Kind: ui.KindText, Text: value, TextRole: theme.RoleLabel, Tabular: true},
	}}
}

// headerTitle names what the panel is looking at: the network, the interface,
// or the reason there is neither.
func headerTitle(st services.NetworkState, wired bool) string {
	if wired {
		if st.Connected {
			return "Wired connection"
		}
		return "No wired link"
	}
	if !st.WirelessEnabled {
		return "Wi-Fi is off"
	}
	if st.Connected && st.SSID != "" {
		return st.SSID
	}
	if st.Resolving {
		return "Connecting…"
	}
	return "Not connected"
}

// headerDetail is the caption under the title: the interface, and the signal
// when there is one to report.
func headerDetail(st services.NetworkState, wired bool) string {
	if wired {
		return orAbsent(st.Interface)
	}
	if !st.WirelessEnabled {
		return "Radio off"
	}
	iface := orAbsent(st.Interface)
	if st.Connected {
		return iface + " · " + itoa(int(st.Strength)) + "% signal"
	}
	return iface
}

// toggleValue maps a flag onto ui.Node.Value, which is a float64 the renderer
// reads for every control kind, as the settings panel's toggles do.
func toggleValue(on bool) float64 {
	if on {
		return 1
	}
	return 0
}

func orAbsent(s string) string {
	if s == "" {
		return absent
	}
	return s
}

// itoa avoids pulling strconv in for one call site.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for v > 0 && i > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
