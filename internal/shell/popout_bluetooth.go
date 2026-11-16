package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// bluetoothTree adds only the standalone panel chrome around bluetoothBody.
// The control centre calls bluetoothBody directly so the two hosts cannot grow
// separate device-management trees.
func bluetoothTree(r *Registry, h *PanelHost) *ui.Node {
	m := DefaultTheme().Metrics
	if h != nil {
		m = h.metrics()
	}
	panelH := panelTargetSize(PanelBluetooth).H
	if h != nil && h.place.Panel.H > 0 {
		panelH = h.place.Panel.H
	}
	tr := hostT(h)
	rowH := h.headlineRowHeight(cardT(tr, "bt.title", "Bluetooth"))
	header := bluetoothHeader(r, tr, m, rowH)
	viewportH := max(panelH-2*m.PanelPadding-rowH-theme.MarginL, 0)
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginL, Padding: m.PanelPadding, Children: []*ui.Node{
		header,
		{Kind: ui.KindScroll, Height: viewportH, Children: []*ui.Node{bluetoothBody(r, h)}},
	}}
}

func bluetoothHeader(r *Registry, tr translateFunc, m theme.Metrics, rowH int) *ui.Node {
	state := services.BluetoothState{}
	if r != nil {
		state = r.bluetoothState
	}
	icon := &ui.Node{
		Kind: ui.KindCapsule, Width: m.StandardControl, Height: m.StandardControl,
		Fill: ui.FillContainerHighest, Shape: ui.ShapeMedium,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: bluetoothGlyph(state), IconSize: m.IconNormal}},
	}
	title := &ui.Node{Kind: ui.KindText, Text: cardT(tr, "bt.title", "Bluetooth"), TextRole: theme.RoleHeadline, Name: cardT(tr, "bt.title", "Bluetooth"), Role: "heading"}
	close := &ui.Node{
		Kind: ui.KindButton, Action: "bluetooth-close", Name: cardT(tr, "notify.close", "Close"), Role: "button",
		Focusable: true, Width: m.CompactControl, Height: m.CompactControl, Shape: ui.ShapeCircle,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "close", IconSize: m.IconNormal}},
	}
	return &ui.Node{Kind: ui.KindRow, Height: rowH, Gap: theme.MarginL, PinEnd: true,
		Children: []*ui.Node{
			{Kind: ui.KindRow, Height: rowH, Gap: theme.MarginL, Children: []*ui.Node{icon, title}},
			close,
		}}
}
