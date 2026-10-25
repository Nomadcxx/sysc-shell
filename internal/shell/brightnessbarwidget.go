package shell

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const panelBrightnessAction = "panel:brightness"

// buildBrightnessWidget shows the newest known display brightness. Without a
// brightness service there is nothing to show and no bar space to reserve.
func buildBrightnessWidget() textWidget {
	icon := &ui.Node{
		Kind: ui.KindIcon, Icon: "brightness_high", IconSize: DefaultTheme().Metrics.IconNormal,
	}
	text := &ui.Node{Kind: ui.KindText, Tabular: true}
	row := &ui.Node{
		Kind:   ui.KindRow,
		Action: panelBrightnessAction, Name: "Brightness", Role: "button",
		Children: []*ui.Node{icon, text},
	}
	w := textWidget{
		node:           row,
		tooltip:        "Brightness",
		hideWhenAbsent: true,
		refresh: func(v barView) bool {
			return refreshBrightnessWidget(row, icon, text, v)
		},
	}
	return w
}

func brightnessWidgetText(state services.BrightnessState) string {
	return fmt.Sprintf("%d%%", state.Level)
}

func refreshBrightnessWidget(row, icon, text *ui.Node, v barView) bool {
	changed := false
	if absent := !v.BrightOK; row.Absent != absent {
		row.Absent = absent
		changed = true
	}
	if v.BrightOK {
		if value := brightnessWidgetText(v.Brightness); text.Text != value {
			text.Text = value
			changed = true
		}
	}
	if gap := widgetTextGap(text.Text); row.Gap != gap {
		row.Gap = gap
		changed = true
	}
	return changed
}

// widgetTextGap is the shared icon-to-label gap for row widgets.
func widgetTextGap(text string) int {
	if text == "" {
		return 0
	}
	return theme.MarginXS
}
