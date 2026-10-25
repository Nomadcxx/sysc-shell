package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const panelPowerProfileAction = "panel:power-profile"

// buildPowerProfileWidget names the active power profile. Without the
// power-profiles service the widget is absent: a bar on a machine without
// powerprofilesctl reserves no gap for it.
func buildPowerProfileWidget() textWidget {
	icon := &ui.Node{
		Kind: ui.KindIcon, Icon: "speed", IconSize: DefaultTheme().Metrics.IconNormal,
	}
	text := &ui.Node{Kind: ui.KindText}
	row := &ui.Node{
		Kind:   ui.KindRow,
		Action: panelPowerProfileAction, Name: "Power profile", Role: "button",
		Children: []*ui.Node{icon, text},
	}
	w := textWidget{
		node:           row,
		tooltip:        "Power profile",
		hideWhenAbsent: true,
		refresh: func(v barView) bool {
			return refreshPowerProfileWidget(row, icon, text, v)
		},
	}
	return w
}

// powerProfileWidgetText keeps the profile's own name; powerprofilesctl names
// are short enough that no translation table is needed here.
func powerProfileWidgetText(active string, ok bool) string {
	if !ok || active == "" {
		return ""
	}
	return active
}

// powerProfileWidgetIcon follows the session pane's mapping so the bar and
// the pane agree on what a profile looks like.
func powerProfileWidgetIcon(active string, ok bool) string {
	if !ok {
		return "speed"
	}
	if icon := sessionProfileIcon(active); icon != "" {
		return icon
	}
	return "speed"
}

func refreshPowerProfileWidget(row, icon, text *ui.Node, v barView) bool {
	changed := false
	absent := !v.ProfilesOK || len(v.Profiles) == 0
	if row.Absent != absent {
		row.Absent = absent
		changed = true
	}
	if absent {
		return changed
	}
	if name := powerProfileWidgetIcon(v.ProfileActive, v.ProfilesOK); icon.Icon != name {
		icon.Icon = name
		changed = true
	}
	if value := powerProfileWidgetText(v.ProfileActive, v.ProfilesOK); text.Text != value {
		text.Text = value
		changed = true
	}
	if gap := widgetTextGap(text.Text); row.Gap != gap {
		row.Gap = gap
		changed = true
	}
	return changed
}

// nextPowerProfile cycles the ordered list, wrapping at the end. An empty
// list has nothing to cycle to.
func nextPowerProfile(profiles []string, active string) string {
	for i, name := range profiles {
		if name == active {
			return profiles[(i+1)%len(profiles)]
		}
	}
	if len(profiles) > 0 {
		return profiles[0]
	}
	return ""
}
