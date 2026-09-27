package shell

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	osdAudio      = "audio"
	osdBrightness = "brightness"
	osdMedia      = "media"
	osdCapsLock   = "caps lock"
	osdNumLock    = "num lock"
	osdLayout     = "layout"
	osdDND        = "do not disturb"

	osdPad    = 12
	osdInnerW = osdWidth - 16 - 2*osdPad
	osdIconSz = 20
	osdGap    = 10

	osdFadeFrom = 85
	osdFadeTo   = 95
	osdHandleSz = 14
)

// OSDView is one OSD payload. Level is 0..100 for metered kinds; Muted is
// audio mute or a stopped player; Text is a media title or a layout name;
// On is a toggle's state or a playing player.
type OSDView struct {
	Kind  string
	Level int
	Muted bool
	Text  string
	On    bool
}

func osdMetered(kind string) bool {
	return kind == osdAudio || kind == osdBrightness || kind == osdMedia
}

func osdIcon(v OSDView) string {
	switch v.Kind {
	case osdAudio:
		if v.Muted {
			return "volume_off"
		}
		return "volume_up"
	case osdBrightness:
		return "brightness_high"
	case osdMedia:
		switch {
		case v.Muted:
			return "music_note"
		case v.On:
			return "play_arrow"
		default:
			return "pause"
		}
	case osdCapsLock, osdNumLock:
		return "keyboard"
	case osdDND:
		if v.On {
			return "do_not_disturb_on"
		}
		return "notifications"
	}
	return ""
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func osdLabel(v OSDView) string {
	switch v.Kind {
	case osdAudio:
		if v.Muted {
			return "Muted"
		}
		return "Volume"
	case osdBrightness:
		return "Brightness"
	case osdMedia:
		if v.Muted {
			return "Stopped"
		}
		if v.Text != "" {
			return v.Text
		}
		return "Media"
	case osdCapsLock:
		return "Caps Lock " + onOff(v.On)
	case osdNumLock:
		return "Num Lock " + onOff(v.On)
	case osdDND:
		return "Do not disturb " + onOff(v.On)
	case osdLayout:
		return v.Text
	}
	return v.Kind
}

// osdTree is the OSD's content for one view, laid out inside the body the
// manager paints. Metered kinds put the icon beside a label over a meter;
// media leads with its title; toggles are icon and label; layout is a
// centred name.
func osdTree(v OSDView) *ui.Node {
	label := &ui.Node{Kind: ui.KindText, Text: osdLabel(v), MaxWidth: osdInnerW - osdIconSz - osdGap}
	icon := &ui.Node{Kind: ui.KindIcon, Icon: osdIcon(v), IconSize: osdIconSz}
	meter := func(w int) *ui.Node {
		tone := ui.ToneNormal
		if v.Muted {
			tone = ui.ToneSubtle
		}
		return &ui.Node{Kind: ui.KindMeter, Key: "osd-meter", Value: float64(v.Level) / 100, Width: w, Height: 6, Tone: tone}
	}
	root := &ui.Node{Kind: ui.KindColumn, Padding: osdPad, Gap: 6}
	switch {
	case v.Kind == osdLayout:
		label.MaxWidth = osdInnerW
		label.CenterX = true
		label.TextRole = theme.RoleTitle
		root.Children = []*ui.Node{label}
	case v.Kind == osdMedia:
		root.Children = []*ui.Node{
			{Kind: ui.KindRow, Gap: osdGap, Children: []*ui.Node{icon, label}},
			meter(osdInnerW),
		}
	case osdMetered(v.Kind):
		root.Children = []*ui.Node{{Kind: ui.KindRow, Gap: osdGap, Children: []*ui.Node{
			icon,
			{Kind: ui.KindColumn, Gap: 6, Children: []*ui.Node{label, meter(osdInnerW - osdIconSz - osdGap)}},
		}}}
	default:
		root.Children = []*ui.Node{{Kind: ui.KindRow, Gap: osdGap, Children: []*ui.Node{icon, label}}}
	}
	return root
}

// x is the handle's centre on the track; iconOpacity and textOpacity are
// percent, 0 meaning "do not paint".
func osdHandle(level, trackX, trackW int) (x int, iconOpacity, textOpacity uint8) {
	level = min(max(level, 0), 100)
	x = trackX + trackW*level/100
	switch {
	case level <= osdFadeFrom:
		return x, 100, 0
	case level >= osdFadeTo:
		return x, 0, 100
	}
	text := uint8((level - osdFadeFrom) * 100 / (osdFadeTo - osdFadeFrom))
	return x, 100 - text, text
}

// osdHandleNodes are painted over a laid-out meter: the kind's icon at the
// fill edge, crossfading to the percentage near full. Opacity zero means
// "unset" to the painter, so a fully faded node is left out instead.
func osdHandleNodes(v OSDView, meter *ui.Node) []*ui.Node {
	if !osdMetered(v.Kind) || meter == nil {
		return nil
	}
	x, iconOp, textOp := osdHandle(v.Level, meter.Bounds.X, meter.Bounds.W)
	cy := meter.Bounds.Y + meter.Bounds.H/2
	var out []*ui.Node
	if iconOp > 0 {
		out = append(out, &ui.Node{Kind: ui.KindIcon, Icon: osdIcon(v), IconSize: osdHandleSz, Opacity: iconOp,
			Bounds: ui.Rect{X: x - osdHandleSz/2, Y: cy - osdHandleSz/2, W: osdHandleSz, H: osdHandleSz}})
	}
	if textOp > 0 {
		out = append(out, &ui.Node{Kind: ui.KindText, Text: fmt.Sprintf("%d%%", min(max(v.Level, 0), 100)),
			TextRole: theme.RoleCaption, Tabular: true, Opacity: textOp,
			Bounds: ui.Rect{X: x - 16, Y: cy - 8, W: 32, H: 16}})
	}
	return out
}

func osdFindKey(n *ui.Node, key string) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Key == key {
		return n
	}
	for _, child := range n.Children {
		if found := osdFindKey(child, key); found != nil {
			return found
		}
	}
	return nil
}
