package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// networkTree builds PanelNetwork.
//
// Wi-Fi is the default tab, seeded here rather than at open so every entry
// point agrees: IPC, the bar glyph and a keybind all land on the same page.
//
// The header and the two tab bodies arrive in the tasks after this one. What
// this slice establishes is the panel's identity, its geometry and its
// trigger.
func networkTree(r *Registry, h *PanelHost) *ui.Node {
	if h.networkTab == "" {
		h.networkTab = "wifi"
	}
	m := h.metrics()
	return &ui.Node{
		Kind: ui.KindColumn, Gap: 12, Padding: m.PanelPadding,
		Children: []*ui.Node{
			{Kind: ui.KindText, Text: "Network"},
		},
	}
}
