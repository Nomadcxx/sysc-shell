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
}

func buildPrivacyWidget(padding int, m theme.Metrics) textWidget {
	state := privacyState{
		microphone:  privacyUnknown,
		camera:      privacyUnknown,
		screenShare: privacyUnknown,
	}
	icons := privacyWidgetIcons(state)
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS}
	for _, icon := range icons {
		row.Children = append(row.Children, &ui.Node{
			Kind: ui.KindIcon, Icon: icon.name, IconSize: m.IconNormal,
			IconProjectFirst: icon.projectIcon, Name: icon.label, Role: "img", Tone: icon.tone,
		})
	}
	node := &ui.Node{
		Kind: ui.KindCapsule, Padding: padding, Shape: ui.ShapeMedium,
		Action: panelPrivacyAction, Name: privacyAccessibleName(state), Role: "button",
		Tooltip: privacyTooltip(state), Children: []*ui.Node{row},
	}
	w := textWidget{node: node}
	var previous privacyState
	initialized := false
	w.refresh = func(view barView) bool {
		if initialized && previous == view.Privacy {
			return false
		}
		initialized = true
		previous = view.Privacy
		icons := privacyWidgetIcons(view.Privacy)
		for i, icon := range icons {
			child := row.Children[i]
			child.Icon, child.IconProjectFirst = icon.name, icon.projectIcon
			child.Name, child.Tone = icon.label, icon.tone
		}
		node.Name = privacyAccessibleName(view.Privacy)
		node.Tooltip = privacyTooltip(view.Privacy)
		return true
	}
	return w
}

func privacyWidgetIcons(state privacyState) [3]privacyWidgetIcon {
	return [3]privacyWidgetIcon{
		privacySourceIcon("Microphone", state.microphone, "mic", "mic_off", false),
		privacySourceIcon("Camera", state.camera, "camera", "camera-off", true),
		privacySourceIcon("Screen sharing", state.screenShare, "desktop_windows", "desktop_windows", false),
	}
}

func privacySourceIcon(label string, state privacyActivity, activeIcon, inactiveIcon string, projectIcon bool) privacyWidgetIcon {
	icon := privacyWidgetIcon{tone: ui.ToneSubtle}
	switch state {
	case privacyActive:
		icon.name, icon.label, icon.tone = activeIcon, label+" in use", ui.ToneNormal
	case privacyInactive:
		icon.name, icon.label = inactiveIcon, label+" not in use"
	default:
		icon.name, icon.label, icon.tone = "gpp_bad", label+" unavailable", ui.ToneError
		projectIcon = false
	}
	icon.projectIcon = projectIcon
	return icon
}
