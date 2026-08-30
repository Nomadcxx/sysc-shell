package shell

import (
	"fmt"
	"time"

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

func clockTree(now time.Time, monthDelta int) *ui.Node {
	view := now.AddDate(0, monthDelta, 0)
	g := calendarGrid(view)
	header := view.Format("January 2006")
	col := &ui.Node{Kind: ui.KindColumn, Gap: 8, Padding: 12, Children: []*ui.Node{
		{Kind: ui.KindText, Text: now.Format("15:04"), Name: "time"},
		{Kind: ui.KindText, Text: now.Format("Mon 2 Jan 2006")},
		{Kind: ui.KindRow, Gap: 8, Children: []*ui.Node{
			{Kind: ui.KindButton, Text: "<", Action: "cal-prev", Name: "Previous month", Role: "button", Focusable: true},
			{Kind: ui.KindText, Text: header},
			{Kind: ui.KindButton, Text: ">", Action: "cal-next", Name: "Next month", Role: "button", Focusable: true},
		}},
	}}
	weekdays := &ui.Node{Kind: ui.KindRow, Gap: 4}
	for _, d := range []string{"S", "M", "T", "W", "T", "F", "S"} {
		weekdays.Children = append(weekdays.Children, &ui.Node{Kind: ui.KindText, Text: d})
	}
	col.Children = append(col.Children, weekdays)
	for _, week := range g.Weeks {
		row := &ui.Node{Kind: ui.KindRow, Gap: 4}
		for _, cell := range week {
			label := ""
			if cell.Day != 0 {
				label = fmt.Sprintf("%d", cell.Day)
			}
			n := &ui.Node{Kind: ui.KindText, Text: label}
			if cell.Today {
				n.Tone = ui.ToneNormal
				n.Action = "today"
			}
			row.Children = append(row.Children, n)
		}
		col.Children = append(col.Children, row)
	}
	return col
}
