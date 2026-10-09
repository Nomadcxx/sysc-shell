package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	panelMediaAction     = "panel:media"
	mediaPlayPauseAction = "media:playpause"
	mediaBarArtSize      = 20
)

func hasMediaItem(items []config.Item) bool {
	for _, item := range items {
		if item.ID == "media" || (item.ID == "group" && hasMediaItem(item.Items)) {
			return true
		}
	}
	return false
}

// buildMediaWidget is the art (or state glyph), the active track's title and a
// play/pause button, absent while no player is on the bus. Hovering or clicking
// the art and title opens the media strip (2026-10-08 bar media strip design).
func buildMediaWidget(items ...config.Item) textWidget {
	maxWidth := 0
	if len(items) > 0 {
		maxWidth = items[0].MaxWidth
	}
	row := &ui.Node{Kind: ui.KindRow, Gap: groupGap, Action: panelMediaAction, Absent: true,
		Name: "Media", Role: "button",
		Children: []*ui.Node{
			{Kind: ui.KindIcon, Key: "media-art", Icon: "music_note", IconSize: DefaultTheme().Metrics.IconNormal},
			{Kind: ui.KindText, Key: "media-title", MaxWidth: maxWidth, Marquee: true},
			{Kind: ui.KindIcon, Key: "media-play", Icon: "play_arrow", IconSize: DefaultTheme().Metrics.IconNormal,
				Action: mediaPlayPauseAction, Name: "Play or pause", Role: "button"},
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
	leading := row.Children[0]
	if v.Media.Available && v.MediaArt != nil {
		if leading.Kind != ui.KindImage || leading.Image != v.MediaArt {
			leading.Kind = ui.KindImage
			leading.Image = v.MediaArt
			leading.ImageSize = mediaBarArtSize
			leading.Shape = ui.ShapeSmall
			leading.Icon = ""
			changed = true
		}
	} else {
		if leading.Kind != ui.KindIcon || leading.Icon != icon || leading.Image != nil {
			leading.Kind = ui.KindIcon
			leading.Image = nil
			leading.ImageSize = 0
			leading.Shape = ui.ShapeInherit
			leading.Icon = icon
			changed = true
		}
	}
	if absent := !v.Media.Available; row.Absent != absent {
		row.Absent = absent
		changed = true
	}
	if !v.Media.Available {
		return changed
	}
	button := row.Children[2]
	playIcon := "play_arrow"
	if v.Media.Status == services.PlaybackPlaying {
		playIcon = "pause"
	}
	tone := ui.ToneNormal
	if mediaPlayDisabled(v.Media) {
		tone = ui.ToneSubtle
	}
	if button.Icon != playIcon || button.Tone != tone {
		button.Icon, button.Tone = playIcon, tone
		changed = true
	}
	if text := row.Children[1]; text.Text != v.Media.Title {
		text.Text = v.Media.Title
		changed = true
	}
	return changed
}
