package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const panelThemeModeAction = "panel:theme-mode"

// buildThemeModeWidget shows the appearance mode and toggles it, the same
// switch the appearance pane owns. The mode always has a value, so the widget
// is always present.
func buildThemeModeWidget() textWidget {
	icon := &ui.Node{
		Kind: ui.KindIcon, Icon: "palette", IconSize: DefaultTheme().Metrics.IconNormal,
	}
	text := &ui.Node{Kind: ui.KindText}
	row := &ui.Node{
		Kind:   ui.KindRow,
		Action: panelThemeModeAction, Name: "Theme mode", Role: "button",
		Children: []*ui.Node{icon, text},
	}
	w := textWidget{
		node:    row,
		tooltip: "Theme mode",
		refresh: func(v barView) bool {
			return refreshThemeModeWidget(row, icon, text, v)
		},
	}
	return w
}

// themeModeWidgetText capitalises the stored mode; an empty or unknown value
// falls back to the default dark presentation.
func themeModeWidgetText(mode string) string {
	switch mode {
	case "light":
		return "Light"
	default:
		return "Dark"
	}
}

func refreshThemeModeWidget(row, icon, text *ui.Node, v barView) bool {
	changed := false
	if value := themeModeWidgetText(v.ThemeMode); text.Text != value {
		text.Text = value
		changed = true
	}
	if gap := widgetTextGap(text.Text); row.Gap != gap {
		row.Gap = gap
		changed = true
	}
	return changed
}

// themeModeCycle flips the stored mode; a value outside the pair goes dark.
func themeModeCycle(mode string) string {
	if mode == "light" {
		return "dark"
	}
	return "light"
}
