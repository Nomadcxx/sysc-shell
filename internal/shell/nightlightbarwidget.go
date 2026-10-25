package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const panelNightLightAction = "panel:nightlight"

// buildNightLightWidget routes the bar to the night light toggle. The widget
// is absent until the service sees a gamma control on some output, so a bar
// without the capability reserves no gap.
func buildNightLightWidget() textWidget {
	icon := &ui.Node{
		Kind: ui.KindIcon, Icon: "bedtime", IconSize: DefaultTheme().Metrics.IconNormal,
	}
	row := &ui.Node{
		Kind:   ui.KindRow,
		Action: panelNightLightAction, Name: "Night light", Role: "button",
		Children: []*ui.Node{icon},
	}
	w := textWidget{
		node:           row,
		tooltip:        "Night light",
		hideWhenAbsent: true,
		refresh:        func(v barView) bool { return refreshNightLightWidget(row, icon, v) },
	}
	return w
}

// refreshNightLightWidget paints the schedule state through the icon's tone:
// dimmed while the warm tint is off, accent while it is on. The node stays
// absent until the service supports gamma, and an unavailable control keeps
// the last state rather than silently vanishing, mirroring the control centre
// tile.
func refreshNightLightWidget(row, icon *ui.Node, v barView) bool {
	changed := false
	if absent := !v.NightLight.Supported; row.Absent != absent {
		row.Absent = absent
		changed = true
	}
	if tone := nightLightGlyphTone(v.NightLight); icon.Tone != tone {
		icon.Tone = tone
		changed = true
	}
	return changed
}

func nightLightGlyphTone(state services.NightLightState) ui.Tone {
	if state.Active {
		return ui.ToneAccent
	}
	return ui.ToneSubtle
}
