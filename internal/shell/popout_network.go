package shell

import (
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// absent is what a figure with no value renders as. An absent metric is not a
// zero: "0.0.0.0" and "no address" are different facts, and the dash keeps the
// column's space so the row does not reflow when a value arrives.
const absent = "—"

// networkFigureRowH is the label-over-value row under the header's rule. It is
// named so the scroll viewport can subtract it without guessing.
const (
	networkFigureRowH = 34
	networkFigureGap  = 8
)

// networkTree builds PanelNetwork: the status header, the tab row, and
// whichever tab body is selected.
//
// Wi-Fi is the default tab, seeded here rather than at open so every entry
// point agrees: IPC, the bar glyph and a keybind all land on the same page.
//
// Every read here is a cached one. This runs under Registry.mu and on the
// Wayland owner, where a bus round trip would stall every bar on the machine.
func networkTree(r *Registry, h *PanelHost) *ui.Node {
	if h.networkTab == "" {
		h.networkTab = "wifi"
	}
	m := h.metrics()

	st := services.NetworkState{}
	snap := services.Snapshot{}
	var aps []services.AccessPoint
	if r != nil && r.network != nil {
		st = r.network.CachedState()
		aps = r.network.CachedAccessPoints()
	}
	if r != nil {
		snap = r.sample
	}

	body := networkWifiTree(aps, st, h, m)
	if h.networkTab == "ethernet" {
		body = networkEthernetTree(st, m)
	}

	panelH := h.place.Panel.H
	if panelH <= 0 {
		panelH = panelTargetSize(PanelNetwork).H
	}
	viewportH := max(panelH-2*m.PanelPadding-networkHeaderHeight(m)-m.StandardControl-2*theme.MarginL, 0)

	children := []*ui.Node{
		networkHeaderCard(h, st, snap, m),
		networkTabs(h, m),
		{Kind: ui.KindScroll, Height: viewportH, Children: []*ui.Node{body}},
	}
	if h.errLabel != "" {
		children = append(children, &ui.Node{
			Kind: ui.KindText, Text: h.errLabel, Tone: ui.ToneError,
			TextRole: theme.RoleCaption,
		})
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginL, Padding: m.PanelPadding, Children: children}
}

func networkHeaderHeight(m theme.Metrics) int {
	// card padding, the status row, two column gaps, the rule, and the figures
	return 2*m.CardPadding + m.StandardControl + theme.MarginL + 1 + theme.MarginL + networkFigureRowH
}

// networkHeaderCard is Direction B's status block: the active connection is
// promoted above the tabs, so "what am I on?" is answered without reading the
// list below it.
func networkHeaderCard(h *PanelHost, st services.NetworkState, snap services.Snapshot, m theme.Metrics) *ui.Node {
	well := m.StandardControl
	closeSz := m.CompactControl
	wired := h != nil && h.networkTab == "ethernet"

	icon := &ui.Node{
		Kind: ui.KindCapsule, Width: well, Height: well, Shape: ui.ShapeMedium,
		Fill:     ui.FillContainerHighest,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: wifiGlyph(st), IconSize: m.IconNormal}},
	}

	title := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
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

	trailing := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Height: well}
	if !wired {
		// The wired link has no radio to switch, so the toggle belongs to the
		// Wi-Fi tab alone.
		trailing.Children = append(trailing.Children, &ui.Node{
			Kind: ui.KindToggle, Value: toggleValue(st.WirelessEnabled),
			Action: "network-radio", Name: "Wi-Fi", Role: "switch", Focusable: true,
		})
	}
	trailing.Children = append(trailing.Children, closeBtn)

	top := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginL, Height: well, PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindRow, Gap: theme.MarginL, Height: well, Children: []*ui.Node{icon, title}},
		trailing,
	}}

	return &ui.Node{
		Kind: ui.KindCapsule, Padding: m.CardPadding, Height: networkHeaderHeight(m),
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginL, Children: []*ui.Node{
			top,
			{Kind: ui.KindSeparator},
			networkFigures(st, snap, (panelTargetSize(PanelNetwork).W-2*m.PanelPadding-2*m.CardPadding-2*networkFigureGap)/3),
		}}},
	}
}

// networkTabs is the Wi-Fi / Ethernet segmented control, the same component
// the audio panel's tabs use. Wi-Fi is first because it is the default.
func networkTabs(h *PanelHost, m theme.Metrics) *ui.Node {
	tab := h.networkTab
	return &ui.Node{
		Kind: ui.KindSegmented, Key: "network-tab", Gap: theme.MarginXXS, Height: m.StandardControl,
		Children: []*ui.Node{
			networkSegment(m, "network-tab:wifi", "Wi-Fi", tab != "ethernet"),
			networkSegment(m, "network-tab:ethernet", "Ethernet", tab == "ethernet"),
		},
	}
}

func networkSegment(m theme.Metrics, action, label string, selected bool) *ui.Node {
	n := &ui.Node{
		Kind: ui.KindButton, Action: action, Name: label, Role: "tab",
		Focusable: true, Height: m.CompactControl,
		Children: []*ui.Node{{Kind: ui.KindText, Text: label}},
	}
	if selected {
		n.State |= ui.StateSelected
	}
	return n
}

// networkWifiTree is the access-point list, or the one state that explains why
// there is no list.
//
// Rows arrive already sorted by band from the service. The panel does not
// re-sort: two orderings would drift, and the list would reshuffle under the
// pointer.
func networkWifiTree(aps []services.AccessPoint, st services.NetworkState, h *PanelHost, m theme.Metrics) *ui.Node {
	if h != nil && h.pendingSSID != "" {
		return networkPasswordCard(h)
	}
	switch {
	case !st.WirelessEnabled:
		// Not an empty list: an empty list claims "no networks here", which is
		// a different and wrong statement.
		return networkNotice("Wi-Fi is off", "Turn the radio on to scan for networks.", m)
	case st.Scanning && len(aps) == 0:
		return networkNotice("Searching for networks…", orAbsent(st.Interface), m)
	case len(aps) == 0:
		return networkNotice("No networks found", "Nothing is in range on this adapter.", m)
	}

	rows := make([]*ui.Node, 0, len(aps))
	for _, ap := range aps {
		rows = append(rows, networkAPRow(ap, m))
	}
	return &ui.Node{Kind: ui.KindCapsule, Padding: m.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: rows}},
	}
}

func networkPasswordCard(h *PanelHost) *ui.Node {
	m := h.metrics()
	if h.password == nil {
		h.password = ui.NewField("")
		h.password.Masked = true
	}
	field := h.password.Node("Password")
	field.Key = "network-password"
	field.Height = m.StandardControl
	revealIcon, revealName := "visibility", "Show password"
	if !h.password.Masked {
		revealIcon, revealName = "visibility_off", "Hide password"
	}
	reveal := &ui.Node{
		Kind: ui.KindButton, Action: "network-password-reveal", Name: revealName,
		Role: "button", Focusable: true, Width: m.StandardControl, Height: m.StandardControl,
		Shape:    ui.ShapeCircle,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: revealIcon, IconSize: m.IconNormal}},
	}
	buttons := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindButton, Action: "network-password-cancel", Name: "Cancel", Role: "button", Focusable: true,
			Height: m.StandardControl, Children: []*ui.Node{{Kind: ui.KindText, Text: "Cancel"}}},
		{Kind: ui.KindButton, Action: "network-password-submit", Name: "Connect", Role: "button", Focusable: true,
			Height: m.StandardControl, Fill: ui.FillAccent, Children: []*ui.Node{{Kind: ui.KindText, Text: "Connect"}}},
	}}
	return &ui.Node{Kind: ui.KindCapsule, Padding: m.CardPadding, Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginL, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "Join " + h.pendingSSID, TextRole: theme.RoleTitle},
			{Kind: ui.KindRow, Gap: theme.MarginM, PinEnd: true, Children: []*ui.Node{field, reveal}},
			buttons,
		}}},
	}
}

// networkAPRow is one access point.
//
// The connected row carries a trailing check and no fill: the status block
// above owns the filled highlight, and two primary-filled elements would
// state the same fact twice and fight for the same focal point.
func networkAPRow(ap services.AccessPoint, m theme.Metrics) *ui.Node {
	leading := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginL, Children: []*ui.Node{
		{Kind: ui.KindIcon, Icon: wifiBandGlyph(ap.Strength), IconSize: m.IconNormal},
		{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: ap.SSID},
			{Kind: ui.KindText, Text: apDetail(ap), TextRole: theme.RoleCaption, Tabular: true},
		}},
	}}
	trailing := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM}
	if ap.Secured {
		trailing.Children = append(trailing.Children, &ui.Node{Kind: ui.KindIcon, Icon: "lock", IconSize: m.IconSmall})
	}
	if ap.Active {
		trailing.Children = append(trailing.Children, &ui.Node{Kind: ui.KindIcon, Icon: "check", IconSize: m.IconNormal})
	}
	content := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginL, PinEnd: true, Children: []*ui.Node{leading}}
	if len(trailing.Children) > 0 {
		content.Children = append(content.Children, trailing)
	}
	return &ui.Node{
		Kind: ui.KindButton, Action: "network-ap:" + ap.SSID, Name: ap.SSID,
		Role: "button", Focusable: true, Height: m.StandardControl, Padding: m.ButtonPadding, Shape: ui.ShapeSmall,
		Children: []*ui.Node{content},
	}
}

// apDetail says what tapping the row will do, which is the question the user
// is actually asking: connect, reconnect, or be asked for a password.
func apDetail(ap services.AccessPoint) string {
	state := "Open"
	switch {
	case ap.Active:
		state = "Connected"
	case ap.Saved:
		state = "Saved"
	case ap.Secured:
		state = "Secured"
	}
	return state + "  " + itoa(int(ap.Strength)) + "%"
}

// networkEthernetTree is the wired tab: one interface, or the absence of one.
func networkEthernetTree(st services.NetworkState, m theme.Metrics) *ui.Node {
	if st.Kind != services.ConnWired || !st.Connected {
		return networkNotice("No wired connection", "Plug in a cable to use this tab.", m)
	}
	row := &ui.Node{
		Kind: ui.KindRow, Gap: theme.MarginL, PinEnd: true, Padding: m.CardPadding,
		Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: "lan", IconSize: m.IconLarge},
			{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
				{Kind: ui.KindText, Text: orAbsent(st.Interface)},
				{Kind: ui.KindText, Text: "Connected  " + orAbsent(st.IPv4),
					TextRole: theme.RoleCaption, Tabular: true},
			}},
			{Kind: ui.KindIcon, Icon: "check", IconSize: m.IconNormal},
		},
	}
	return &ui.Node{Kind: ui.KindCapsule, Padding: m.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{row},
	}
}

// networkNotice is the shared empty state: a title that names the situation
// and a line that says what to do about it.
func networkNotice(title, hint string, m theme.Metrics) *ui.Node {
	return &ui.Node{Kind: ui.KindCapsule, Padding: m.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: title, TextRole: theme.RoleLabel},
			{Kind: ui.KindText, Text: hint, TextRole: theme.RoleCaption},
		}}},
	}
}

// applyNetworkControl consumes the panel's own actions. It returns false for
// anything it does not own, including "network-close", which the shared close
// path handles.
//
// Every write goes through scheduleControl: this runs under Registry.mu, and a
// bus round trip taken here would stall the owner.
func (h *PanelHost) applyNetworkControl(r *Registry, n *ui.Node) bool {
	switch {
	case n.Action == "network-password-reveal":
		if h.password == nil {
			h.password = ui.NewField("")
			h.password.Masked = true
		} else {
			h.password.Masked = !h.password.Masked
		}
		r.rebuildPanel(h)
		return true
	case n.Action == "network-password-submit":
		psk := ""
		if h.password != nil {
			psk = h.password.Text
		}
		h.clearNetworkSecret()
		if r.network != nil {
			r.network.SubmitSecret(psk)
		}
		psk = ""
		r.rebuildPanel(h)
		return true
	case n.Action == "network-password-cancel":
		h.clearNetworkSecret()
		if r.network != nil {
			r.network.CancelSecret()
		}
		r.rebuildPanel(h)
		return true
	case n.Action == "network-tab:wifi":
		h.networkTab = "wifi"
		r.rebuildPanel(h)
		return true
	case n.Action == "network-tab:ethernet":
		h.networkTab = "ethernet"
		r.rebuildPanel(h)
		return true
	case n.Action == "network-radio":
		svc := r.network
		if svc == nil {
			return true
		}
		want := !svc.CachedState().WirelessEnabled
		r.scheduleControl(h, func() error { return svc.SetWirelessEnabled(want) })
		return true
	}

	ssid, ok := strings.CutPrefix(n.Action, "network-ap:")
	if !ok {
		return false
	}
	svc := r.network
	if svc == nil {
		return true
	}
	var target services.AccessPoint
	for _, ap := range svc.CachedAccessPoints() {
		if ap.SSID == ssid {
			target = ap
			break
		}
	}
	if target.SSID == "" {
		return true
	}
	r.scheduleControl(h, func() error { return svc.Activate(target) })
	return true
}

func (h *PanelHost) clearNetworkSecret() {
	if h.password != nil {
		h.password.Clear()
		h.password.Masked = true
	}
	h.pendingSSID = ""
}

// networkFigures is the three-column row under the separator. Every value is
// tabular so the row does not jitter as figures change.
//
// Throughput stays with the existing metrics owner rather than expanding
// NetworkState. A missing sample or interface is an absent figure, not zero.

func networkFigures(st services.NetworkState, snap services.Snapshot, columnW int) *ui.Node {
	down, up := absent, absent
	if rate, ok := snap.Rate(services.Selector{Source: services.SourceNetwork, Subject: st.Interface, Direction: "rx"}); ok {
		down = formatRate(rate)
	}
	if rate, ok := snap.Rate(services.Selector{Source: services.SourceNetwork, Subject: st.Interface, Direction: "tx"}); ok {
		up = formatRate(rate)
	}
	return &ui.Node{Kind: ui.KindRow, Gap: networkFigureGap, Height: networkFigureRowH, Children: []*ui.Node{
		networkFigure("IPv4", orAbsent(st.IPv4), columnW),
		networkFigure("Down", down, columnW),
		networkFigure("Up", up, columnW),
	}}
}

func networkFigure(label, value string, width int) *ui.Node {
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXXS, Width: width, Children: []*ui.Node{
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

// itoa avoids pulling strconv in for two call sites.
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
