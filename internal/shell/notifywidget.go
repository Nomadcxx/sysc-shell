package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Hit-testing uses the single Action plus the pointer button: left opens the
// centre, middle toggles DND, right is the duration menu (Task 12).
const (
	panelNotificationsAction = "panel:notifications"
	notifyDNDAction          = "notify:dnd"
	notifyDNDMenuAction      = "notify:dnd-menu"
	notifyBadgeSize          = 6
)

func buildNotifyWidget() textWidget {
	row := &ui.Node{
		Kind:     ui.KindRow,
		Action:   panelNotificationsAction,
		Children: []*ui.Node{{Kind: ui.KindText, Text: notifyGlyph(false)}},
	}
	return textWidget{
		node:    row,
		tooltip: "Notifications",
		refresh: func(v barView) bool { return refreshNotifyWidget(row, v) },
	}
}

func notifyGlyph(dnd bool) string {
	name := "notifications"
	if dnd {
		name = "notifications-off"
	}
	r, _ := render.IconByName(name)
	return string(r)
}

func refreshNotifyWidget(row *ui.Node, v barView) bool {
	text := notifyGlyph(v.DND)
	wantBadge := v.Unread > 0
	var glyph *ui.Node
	if len(row.Children) > 0 {
		glyph = row.Children[0]
	}
	hasBadge := len(row.Children) > 1 &&
		row.Children[1] != nil &&
		row.Children[1].Kind == ui.KindCapsule &&
		row.Children[1].Fill == ui.FillError &&
		row.Children[1].Width == notifyBadgeSize
	if glyph != nil && glyph.Kind == ui.KindText && glyph.Text == text && wantBadge == hasBadge {
		return false
	}
	children := []*ui.Node{{Kind: ui.KindText, Text: text}}
	if wantBadge {
		children = append(children, &ui.Node{
			Kind: ui.KindCapsule, Fill: ui.FillError, Width: notifyBadgeSize, Shape: ui.ShapeCircle,
		})
	}
	row.Children = children
	return true
}
