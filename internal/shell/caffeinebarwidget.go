package shell

import (
	"fmt"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const panelIdleInhibitorAction = "panel:idle-inhibitor"

// buildIdleInhibitorWidget is always visible: an inhibitor that is off still
// needs a place to be switched on from.
func buildIdleInhibitorWidget() textWidget {
	icon := &ui.Node{
		Kind: ui.KindIcon, Icon: "coffee", IconSize: DefaultTheme().Metrics.IconNormal,
	}
	text := &ui.Node{Kind: ui.KindText, Tabular: true}
	row := &ui.Node{
		Kind:   ui.KindRow,
		Action: panelIdleInhibitorAction, Name: "Idle inhibitor", Role: "button",
		Children: []*ui.Node{icon, text},
	}
	w := textWidget{
		node:    row,
		tooltip: "Idle inhibitor",
		refresh: func(v barView) bool {
			return refreshIdleInhibitorWidget(row, icon, text, v)
		},
	}
	return w
}

// caffeineWidgetTone keeps the inactive cup dimmed so the bar does not read
// as if the session were being held awake.
func caffeineWidgetTone(wanted bool) ui.Tone {
	if wanted {
		return ui.ToneAccent
	}
	return ui.ToneSubtle
}

// caffeineWidgetText shows the bound inhibitor's remaining hold, so a
// fifteen-minute coffee does not outlive the operator's intent unnoticed.
func caffeineWidgetText(until, now time.Time) string {
	if !wantedUntilSet(until) {
		return ""
	}
	if minutes := caffeineRemainingMinutes(until, now); minutes > 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return ""
}

func wantedUntilSet(until time.Time) bool {
	return !until.IsZero()
}

func refreshIdleInhibitorWidget(row, icon, text *ui.Node, v barView) bool {
	changed := false
	if tone := caffeineWidgetTone(v.CaffeineWanted); icon.Tone != tone {
		icon.Tone = tone
		changed = true
	}
	value := caffeineWidgetText(v.CaffeineUntil, v.Now)
	if text.Text != value {
		text.Text = value
		changed = true
	}
	gap := 0
	if value != "" {
		gap = theme.MarginXS
	}
	if row.Gap != gap {
		row.Gap = gap
		changed = true
	}
	return changed
}
