package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// terminalArtTree is the Terminal Art panel: sysc-Go effects on the wallpaper
// layer. It reads the same wallpaper service snapshot as the Wallpaper panel
// and keeps its state in the same per-host fields, so the two panels are
// separate chrome over one assignment table.
func terminalArtTree(r *Registry, h *PanelHost) *ui.Node {
	return &ui.Node{
		Kind: ui.KindColumn, Padding: wallpaperPadding, Gap: wallpaperGridGap,
		Children: []*ui.Node{
			{Kind: ui.KindText, Text: "Terminal Art", TextRole: theme.RoleTitle},
		},
	}
}
