package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const panelMediaAction = "panel:media"

// buildMediaWidget is one glyph and the active track's title, absent while no
// player is on the bus. The design's D7 scope: no player picker, no seek bar,
// no volume — the control centre's Media page is the one picker, and volume
// is already its own widget.
func buildMediaWidget(items ...config.Item) textWidget {
	maxWidth := 0
	if len(items) > 0 {
		maxWidth = items[0].MaxWidth
	}
	row := &ui.Node{Kind: ui.KindRow, Gap: groupGap, Action: panelMediaAction,
		Name: "Media", Role: "button",
		Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: "music_note", IconSize: DefaultTheme().Metrics.IconNormal},
			{Kind: ui.KindText, Key: "media-title", MaxWidth: maxWidth, Marquee: true},
		}}
	return textWidget{
		node:           row,
		hideWhenAbsent: true,
		tooltip:        "Media",
		refresh:        func(v barView) bool { return refreshMediaWidget(row, v) },
	}
}

// refreshMediaWidget marks the widget absent with no player, swaps the state
// glyph, and updates the title as tracks change.
func refreshMediaWidget(row *ui.Node, v barView) bool {
	changed := false
	icon := "music_note"
	switch v.Media.Status {
	case services.PlaybackPlaying:
		icon = "pause"
	case services.PlaybackPaused:
		icon = "play_arrow"
	}
	if row.Children[0].Icon != icon {
		row.Children[0].Icon = icon
		changed = true
	}
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
