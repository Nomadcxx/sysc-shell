package shell

import (
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// pluginPlaceholderWidth is the reserved bar slot for a missing or failed
// plugin view. A fixed width is what keeps a crash from rearranging built-ins.
const pluginPlaceholderWidth = 48

const pluginActionPrefix = "plugin:"

// pluginFrame is one prepared view as the bar reads it.
type pluginFrame struct {
	Root     *ui.Node
	Tooltip  *ui.Node
	Revision uint64
	Failed   bool
	Name     string
	Label    string
	ViewID   string
}

type pluginHit struct {
	ViewID string
	Node   string
}

func parsePluginAction(action string) (pluginHit, bool) {
	rest, ok := strings.CutPrefix(action, pluginActionPrefix)
	if !ok {
		return pluginHit{}, false
	}
	viewID, node, ok := strings.Cut(rest, ":")
	if !ok || viewID == "" || node == "" {
		return pluginHit{}, false
	}
	return pluginHit{ViewID: viewID, Node: node}, true
}

func stampPluginActions(n *ui.Node, viewID string) {
	if n == nil {
		return
	}
	if n.Action != "" && !strings.HasPrefix(n.Action, pluginActionPrefix) {
		n.Action = pluginActionPrefix + viewID + ":" + n.Action
	}
	for _, c := range n.Children {
		stampPluginActions(c, viewID)
	}
}

func buildPluginWidget(item config.Item) textWidget {
	row := &ui.Node{Kind: ui.KindRow, Width: pluginPlaceholderWidth}
	applyPluginPlaceholder(row, item.Plugin, "starting", false, "")
	rev := new(uint64)
	*rev = ^uint64(0)
	lastName := item.Plugin
	tip := new(*ui.Node)
	return textWidget{
		node:           row,
		tooltip:        item.Plugin,
		tip:            tip,
		hideWhenAbsent: true,
		refresh: func(v barView) bool {
			if off := v.PluginsOff[item.Plugin]; off != row.Absent {
				row.Absent = off
				*rev = ^uint64(0)
				if off {
					*tip = nil
					return true
				}
			} else if off {
				return false
			}
			frame, ok := v.Plugins[item.Instance]
			id := uint64(0)
			if ok {
				id = frame.Revision
				if frame.Failed {
					id = ^id
				}
			}
			name := item.Plugin
			if ok && frame.Name != "" {
				name = frame.Name
			}
			if ok && !frame.Failed {
				*tip = frame.Tooltip
			} else {
				*tip = nil
			}
			if id == *rev && name == lastName {
				return false
			}
			*rev = id
			lastName = name
			if !ok || frame.Failed || frame.Root == nil {
				applyPluginPlaceholder(row, name, statusFor(frame, ok), ok && frame.Failed, frame.ViewID)
				return true
			}
			adoptPluginNode(row, frame.Root)
			return true
		},
	}
}

func statusFor(frame pluginFrame, ok bool) string {
	if !ok {
		return "starting"
	}
	if frame.Label != "" {
		return frame.Label
	}
	if frame.Failed {
		return "failed"
	}
	return "starting"
}

func applyPluginPlaceholder(row *ui.Node, name, status string, failed bool, viewID string) {
	accessibleName := name
	if status != "" {
		accessibleName += ": " + status
	}
	row.Kind = ui.KindRow
	row.Width = pluginPlaceholderWidth
	row.Gap = 0
	row.Action = ""
	row.Tooltip = accessibleName
	row.Name = accessibleName
	row.Role = "status"
	mark := &ui.Node{
		Kind: ui.KindText, Text: name, MaxWidth: pluginPlaceholderWidth, Tone: ui.ToneSubtle,
		Name: accessibleName, Role: "status", Tooltip: accessibleName,
	}
	if failed {
		mark.Tone = ui.ToneError
		if viewID != "" {
			mark.Kind = ui.KindButton
			mark.Width = pluginPlaceholderWidth
			mark.Fill = ui.FillError
			mark.Action = pluginActionPrefix + viewID + ":" + "camera"
			mark.Role = "button"
			mark.Focusable = true
		}
	}
	row.Children = []*ui.Node{mark}
}

func adoptPluginNode(dst, src *ui.Node) {
	if src == nil {
		return
	}
	children := src.Children
	*dst = *src
	dst.Children = children
	dst.Width = 0
	clearPluginBounds(dst)
}

func clearPluginBounds(n *ui.Node) {
	if n == nil {
		return
	}
	n.Bounds = ui.Rect{}
	for _, c := range n.Children {
		clearPluginBounds(c)
	}
}

func (w textWidget) tooltipTree() *ui.Node {
	if w.tip == nil {
		return nil
	}
	return *w.tip
}

func pointerButton(button uint32) v1.PointerButton {
	switch button {
	case 273:
		return v1.ButtonSecondary
	case 274:
		return v1.ButtonMiddle
	default:
		return v1.ButtonPrimary
	}
}
