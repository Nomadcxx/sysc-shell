package shell

import (
	"fmt"
	"strconv"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The media strip is a fixed card: art and text over a seek row and the
// transport. TestMediaStripTreeStates holds the content inside it.
const (
	mediaStripW     = 372
	mediaStripH     = 200
	mediaStripGap   = 6
	mediaStripPad   = 14
	mediaStripArt   = 88
	mediaStripTimeW = 56
)

// mediaStripPlacement puts the strip off the bar's inner edge, centred on the
// pill along the bar, and clamps it inside the work area.
func mediaStripPlacement(edge string, anchor, body, work ui.Rect) ui.Rect {
	if work.W <= 0 || work.H <= 0 {
		return ui.Rect{}
	}
	w, h := min(mediaStripW, work.W), min(mediaStripH, work.H)
	cx, cy := anchor.X+anchor.W/2, anchor.Y+anchor.H/2
	x, y := cx-w/2, body.Y+body.H+mediaStripGap
	switch edge {
	case "bottom":
		y = body.Y - h - mediaStripGap
	case "left":
		x, y = body.X+body.W+mediaStripGap, cy-h/2
	case "right":
		x, y = body.X-w-mediaStripGap, cy-h/2
	}
	x = max(work.X, min(x, work.X+work.W-w))
	y = max(work.Y, min(y, work.Y+work.H-h))
	return ui.Rect{X: x, Y: y, W: w, H: h}
}

// mediaStripTree is the strip's content for one snapshot. position is the
// pending seek when one is in flight, so the thumb does not jump back.
func mediaStripTree(state services.MediaState, players []services.Player, position int64, art *ui.Image) *ui.Node {
	textW := mediaStripW - 2*mediaStripPad - mediaStripArt - theme.MarginL
	artNode := &ui.Node{Kind: ui.KindCapsule, Width: mediaStripArt, Height: mediaStripArt,
		Fill: ui.FillContainerHighest, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "music_note"}}}
	if art != nil {
		artNode = &ui.Node{Kind: ui.KindImage, Width: mediaStripArt, Height: mediaStripArt,
			ImageSize: mediaStripArt, Image: art, Shape: ui.ShapeCard}
	}
	meta := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
		mediaStripPlayerChip(state, players, textW),
		{Kind: ui.KindText, Text: ccText(state.Title), TextRole: theme.RoleTitle, MaxWidth: textW},
		{Kind: ui.KindText, Text: ccText(state.Artist), TextRole: theme.RoleCaption, MaxWidth: textW},
	}}
	return &ui.Node{Kind: ui.KindColumn, Padding: mediaStripPad, Gap: theme.MarginM,
		Name: "Media controls", Role: "group", Children: []*ui.Node{
			{Kind: ui.KindRow, Height: mediaStripArt, Gap: theme.MarginL, Children: []*ui.Node{artNode, meta}},
			mediaStripSeek(state, position),
			{Kind: ui.KindRow, Gap: theme.MarginL, CenterY: true, Children: mediaTransportButtons(state, true)},
		}}
}

// mediaStripPlayerChip names the player. With two or more it is a button that
// prefers the next one in bus order.
func mediaStripPlayerChip(state services.MediaState, players []services.Player, maxW int) *ui.Node {
	name := ccText(state.Identity)
	if len(players) < 2 {
		return &ui.Node{Kind: ui.KindText, Text: name, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle, MaxWidth: maxW}
	}
	next := players[0].Bus
	for i, p := range players {
		if p.Bus == state.Player {
			next = players[(i+1)%len(players)].Bus
			break
		}
	}
	return &ui.Node{Kind: ui.KindButton, Action: "media:player:" + next, Name: "Switch player", Role: "button",
		Focusable: true, Shape: ui.ShapeStadium, Fill: ui.FillContainerHighest, Padding: theme.MarginXS,
		Children: []*ui.Node{{Kind: ui.KindText, TextRole: theme.RoleCaption, MaxWidth: maxW - 2*theme.MarginXS,
			Text: fmt.Sprintf("%s · %d players", name, len(players))}}}
}

// mediaStripSeek is elapsed, the seek control and the length. A player that
// cannot seek, or reports no length, gets a read-only meter.
func mediaStripSeek(state services.MediaState, position int64) *ui.Node {
	w := mediaStripW - 2*mediaStripPad - 2*mediaStripTimeW - 2*theme.MarginM
	var control *ui.Node
	if state.CanSeek && state.LengthUS > 0 {
		control = &ui.Node{Kind: ui.KindSlider, Action: mediaSeekAction + strconv.FormatInt(position, 10),
			Name: "Position", Role: "slider", Focusable: true, Width: w,
			Value: float64(position), Min: 0, Max: float64(state.LengthUS), Step: 1_000_000,
			ValueText: mediaTime(position) + " of " + mediaTime(state.LengthUS)}
	} else {
		value := 0.0
		if state.LengthUS > 0 {
			value = float64(position) / float64(state.LengthUS)
		}
		control = &ui.Node{Kind: ui.KindMeter, Width: w, Value: value, Absent: state.LengthUS <= 0, Name: "Position"}
	}
	return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, CenterY: true, Children: []*ui.Node{
		{Kind: ui.KindText, Text: mediaTime(position), TextRole: theme.RoleCaption, Tabular: true, Width: mediaStripTimeW},
		control,
		{Kind: ui.KindText, Text: mediaTime(state.LengthUS), TextRole: theme.RoleCaption, Tabular: true, Width: mediaStripTimeW},
	}}
}
