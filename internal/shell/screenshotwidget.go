package shell

import "github.com/Nomadcxx/sysc-shell/internal/ui"

const panelScreenshotAction = "panel:screenshot"

// buildScreenshotWidget is only a route to the screenshot panel; it keeps no
// state of its own. It is opt-in, like wallpaper and bluetooth.
func buildScreenshotWidget() textWidget {
	icon := &ui.Node{
		Kind: ui.KindIcon, Icon: "desktop_windows", IconSize: DefaultTheme().Metrics.IconNormal,
		Action: panelScreenshotAction, Name: "Screenshot", Role: "button",
	}
	return textWidget{node: icon, tooltip: "Screenshot", refresh: func(barView) bool { return false }}
}
