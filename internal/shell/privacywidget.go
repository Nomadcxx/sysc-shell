package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const panelPrivacyAction = "panel:privacy"

type privacyWidgetIcon struct {
	name        string
	label       string
	tone        ui.Tone
	projectIcon bool
	active      bool
}

func buildPrivacyWidget(pad int, m theme.Metrics) textWidget {
	state := privacyState{
		microphone:  privacyUnknown,
		camera:      privacyUnknown,
		screenShare: privacyUnknown,
	}
	icons := privacyWidgetIcons(state)
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS}
	row.Children = privacyIconNodes(icons, m)
	node := &ui.Node{
		Kind: ui.KindCapsule, Padding: max(pad, 0), Shape: ui.ShapeMedium,
		Action: panelPrivacyAction, Name: privacyAccessibleName(state), Role: "button",
		Tooltip: privacyTooltip(state), Children: []*ui.Node{row},
	}
	w := textWidget{node: node, hideWhenAbsent: true}
	var previous privacyState
	initialized := false
	w.refresh = func(view barView) bool {
		if initialized && previous == view.Privacy {
			return false
		}
		initialized = true
		previous = view.Privacy
		icons := privacyWidgetIcons(view.Privacy)
		row.Children = privacyIconNodes(icons, m)
		node.Absent = len(icons) == 0
		node.Name = privacyAccessibleName(view.Privacy)
		node.Tooltip = privacyTooltip(view.Privacy)
		return true
	}
	return w
}

func privacyIconNodes(icons []privacyWidgetIcon, m theme.Metrics) []*ui.Node {
	nodes := make([]*ui.Node, 0, len(icons))
	for _, icon := range icons {
		node := &ui.Node{
			Kind: ui.KindIcon, Icon: icon.name, IconSize: m.IconNormal,
			IconProjectFirst: icon.projectIcon, Name: icon.label, Role: "img", Tone: icon.tone,
		}
		if icon.active {
			node.Fill = ui.FillError
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func privacyWidgetIcons(state privacyState) []privacyWidgetIcon {
	var icons []privacyWidgetIcon
	if privacyHasWarning(state) {
		label := "Privacy monitoring unavailable"
		if state.microphoneIncomplete || state.cameraIncomplete || state.screenShareIncomplete {
			label = "Privacy monitoring incomplete"
		}
		icons = append(icons, privacyWidgetIcon{
			name: "gpp_bad", label: label, tone: ui.ToneError,
		})
	}
	if state.microphone == privacyActive {
		icons = append(icons, privacyWidgetIcon{name: "mic", label: "Microphone in use", tone: ui.ToneNormal, active: true})
	}
	if state.camera == privacyActive {
		icons = append(icons, privacyWidgetIcon{name: "camera", label: "Camera in use", tone: ui.ToneNormal, projectIcon: true, active: true})
	}
	if state.screenShare == privacyActive {
		icons = append(icons, privacyWidgetIcon{name: "desktop_windows", label: "Screen sharing in use", tone: ui.ToneNormal, active: true})
	}
	return icons
}
