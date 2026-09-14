package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const panelMediaAction = "panel:media"

// buildMediaWidget is one glyph and the active track's title, absent while no
// player is on the bus. The design's D7 scope: no player picker, no seek bar,
// no volume — the control centre's Media page is the one picker, and volume
// is already its own widget.
func buildMediaWidget() textWidget {
	row := &ui.Node{Kind: ui.KindRow, Gap: groupGap, Action: panelMediaAction,
		Name: "Media", Role: "button",
		Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: "music_note", IconSize: DefaultTheme().Metrics.IconNormal},
			{Kind: ui.KindText},
		}}
	return textWidget{
		node:    row,
		tooltip: "Media",
		refresh: func(v barView) bool { return refreshMediaWidget(row, v) },
	}
}

// refreshMediaWidget marks the widget absent with no player and swaps the
// title as tracks change. The glyph is deliberately constant: the font subset
// carries one media glyph, and the design asks for one.
func refreshMediaWidget(row *ui.Node, v barView) bool {
	changed := false
	if absent := !v.Media.Available; row.Absent != absent {
		row.Absent = absent
		changed = true
	}
	if !v.Media.Available {
		return changed
	}
	if text := row.Children[1]; text.Text != v.Media.Title {
		text.Text = v.Media.Title
		changed = true
	}
	return changed
}
