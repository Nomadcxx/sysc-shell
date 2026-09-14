package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Hit-testing uses the single Action plus the pointer button: left opens the
// centre, middle toggles DND, right is the duration menu (Task 12).
const (
	panelNotificationsAction = "panel:notifications"
	notifyDNDAction          = "notify:dnd"
	notifyDNDMenuAction      = "notify:dnd-menu"
)

func buildNotifyWidget(m theme.Metrics) textWidget {
	icon := &ui.Node{
		Kind: ui.KindIcon, Icon: "notifications", IconSize: m.IconNormal,
		Action: panelNotificationsAction,
	}
	return textWidget{
		node:    icon,
		tooltip: "Notifications",
		refresh: func(v barView) bool { return refreshNotifyWidget(icon, v) },
	}
}

func notifyIcon(dnd bool) string {
	name := "notifications"
	if dnd {
		name = "do_not_disturb_on"
	}
	return name
}

func refreshNotifyWidget(icon *ui.Node, v barView) bool {
	name := notifyIcon(v.DND)
	fill := ui.FillNone
	if v.Unread > 0 {
		fill = ui.FillError
	}
	if icon.Icon == name && icon.Fill == fill {
		return false
	}
	icon.Icon = name
	icon.Fill = fill
	return true
}
