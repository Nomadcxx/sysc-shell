package shell

import (
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const panelLockKeysAction = "panel:lock-keys"

// buildLockKeysWidget names the locks that are on. Without a lock-key service
// the widget is absent; with one, an idle keyboard shows the dimmed icon so
// the surface keeps a place to read the state from.
func buildLockKeysWidget() textWidget {
	icon := &ui.Node{
		Kind: ui.KindIcon, Icon: "keyboard", IconSize: DefaultTheme().Metrics.IconNormal,
	}
	text := &ui.Node{Kind: ui.KindText}
	row := &ui.Node{
		Kind:   ui.KindRow,
		Action: panelLockKeysAction, Name: "Lock keys", Role: "button",
		Children: []*ui.Node{icon, text},
	}
	w := textWidget{
		node:           row,
		tooltip:        "Lock keys",
		hideWhenAbsent: true,
		refresh: func(v barView) bool {
			return refreshLockKeysWidget(row, icon, text, v)
		},
	}
	return w
}

// lockKeysWidgetText spells the active locks in a fixed order.
func lockKeysWidgetText(state services.LockState) string {
	var names []string
	if state.Caps {
		names = append(names, "Caps")
	}
	if state.Num {
		names = append(names, "Num")
	}
	return strings.Join(names, " ")
}

func lockKeysWidgetTone(state services.LockState) ui.Tone {
	if state.Caps || state.Num {
		return ui.ToneNormal
	}
	return ui.ToneSubtle
}

func refreshLockKeysWidget(row, icon, text *ui.Node, v barView) bool {
	changed := false
	if absent := !v.LockKeysOK; row.Absent != absent {
		row.Absent = absent
		changed = true
	}
	if value := lockKeysWidgetText(v.LockKeys); text.Text != value {
		text.Text = value
		changed = true
	}
	if tone := lockKeysWidgetTone(v.LockKeys); icon.Tone != tone {
		icon.Tone = tone
		changed = true
	}
	if gap := widgetTextGap(text.Text); row.Gap != gap {
		row.Gap = gap
		changed = true
	}
	return changed
}
