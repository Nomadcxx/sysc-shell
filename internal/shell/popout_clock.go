package shell

import (
	"fmt"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

type calCell struct {
	Day     int
	InMonth bool
	Today   bool
}

type Calendar struct {
	Weeks [][]calCell
}

func calendarGrid(now time.Time) Calendar {
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	days := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, now.Location()).Day()
	lead := int(first.Weekday())
	prevDays := time.Date(now.Year(), now.Month(), 0, 0, 0, 0, 0, now.Location()).Day()

	var cells []calCell
	for i := lead; i > 0; i-- {
		cells = append(cells, calCell{Day: prevDays - i + 1})
	}
	for d := 1; d <= days; d++ {
		cells = append(cells, calCell{
			Day:     d,
			InMonth: true,
			Today:   d == now.Day() && now.Month() == first.Month() && now.Year() == first.Year(),
		})
	}
	for len(cells)%7 != 0 {
		cells = append(cells, calCell{Day: len(cells) - lead - days + 1})
	}
	weeks := make([][]calCell, 0, len(cells)/7)
	for i := 0; i < len(cells); i += 7 {
		weeks = append(weeks, cells[i:i+7])
	}
	return Calendar{Weeks: weeks}
}

func clockTree(now time.Time, monthDelta int, th Theme) *ui.Node {
	view := now.AddDate(0, monthDelta, 0)
	g := calendarGrid(view)
	header := view.Format("January 2006")
	col := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Padding: th.Metrics.PanelPadding, Children: []*ui.Node{
		{Kind: ui.KindText, Text: now.Format("15:04"), Name: "time"},
		{Kind: ui.KindText, Text: now.Format("Mon 2 Jan 2006")},
		{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{
			calendarArrow("chevron_left", "cal-prev", "Previous month", th),
			{Kind: ui.KindText, Text: header},
			calendarArrow("chevron_right", "cal-next", "Next month", th),
		}},
	}}
	// Seven columns and six gaps span the popout's padded width.
	cellW := max((panelTargetSize(PanelClock).W-2*th.Metrics.PanelPadding-6*theme.MarginXS)/7, 0)
	weekdays := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS}
	for _, d := range []string{"S", "M", "T", "W", "T", "F", "S"} {
		weekdays.Children = append(weekdays.Children, calendarCell(cellW,
			&ui.Node{Kind: ui.KindText, Text: d, Tone: ui.ToneSubtle, CenterX: true}))
	}
	col.Children = append(col.Children, weekdays)
	for _, week := range g.Weeks {
		row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS}
		for _, cell := range week {
			n := &ui.Node{Kind: ui.KindText, Text: fmt.Sprintf("%d", cell.Day), CenterX: true, Tabular: true}
			switch {
			case cell.Today:
				n.Tone = ui.ToneAccent
				n.Action = "today"
			case !cell.InMonth:
				n.Tone = ui.ToneSubtle
			}
			row.Children = append(row.Children, calendarCell(cellW, n))
		}
		col.Children = append(col.Children, row)
	}
	return col
}

// calendarCell is one grid column: a fixed-width box holding its content
// centred in both axes. Every weekday and day uses it, with or without
// events, so the seven columns line up across the header and every week.
func calendarCell(width int, content ...*ui.Node) *ui.Node {
	return &ui.Node{Kind: ui.KindColumn, Width: width, Children: []*ui.Node{
		{Kind: ui.KindColumn, CenterY: true, Gap: ccCalendarDotGap, Children: content},
	}}
}

// calendarArrow is a compact circular icon button. A square button clamps to a
// stadium, which at equal width and height is a circle, so month navigation
// needs no geometry of its own. The accessible name carries the direction: the
// glyph is a ligature and reads as nothing.
func calendarArrow(icon, action, name string, th Theme) *ui.Node {
	return &ui.Node{
		Kind: ui.KindButton, Action: action, Name: name, Role: "button", Focusable: true,
		Width: th.Metrics.CompactControl, Height: th.Metrics.CompactControl,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: icon, IconSize: th.Metrics.IconNormal}},
	}
}
