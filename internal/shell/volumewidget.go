package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const panelAudioAction = "panel:audio"

func buildVolumeWidget() textWidget {
	node := &ui.Node{Kind: ui.KindRow, Action: panelAudioAction,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "volume_up"}}}
	return textWidget{
		node:    node,
		tooltip: "Volume",
		refresh: func(v barView) bool { return refreshVolumeWidget(node.Children[0], v) },
	}
}

// refreshVolumeWidget swaps the ligature as level and mute change. A muted
// sink reads volume_off; the bar path reports only the default sink (D13).
func refreshVolumeWidget(n *ui.Node, v barView) bool {
	icon := "volume_up"
	if v.Audio.Muted || v.Audio.Level == 0 {
		icon = "volume_off"
	}
	if n.Icon == icon {
		return false
	}
	n.Icon = icon
	return true
}

func (b *Bar) actionCenterX(action string) int {
	r := b.actionBounds(action)
	if r.W == 0 {
		return 0
	}
	return r.X + r.W/2
}
