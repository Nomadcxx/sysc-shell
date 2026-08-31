package shell

import (
	"testing"
	"time"
)

func TestCalendarGridSevenColumns(t *testing.T) {
	t.Parallel()
	g := calendarGrid(time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC))
	if len(g.Weeks) < 4 || len(g.Weeks[0]) != 7 {
		t.Fatalf("grid shape wrong: %+v", g)
	}
	if !g.Weeks[0][0].InMonth && g.Weeks[0][0].Day == 0 {
		t.Fatal("leading cells must be blanks or previous-month days")
	}
	found := false
	for _, week := range g.Weeks {
		if week[0].Day == 30 && week[0].InMonth {
			found = true
		}
	}
	if !found {
		t.Fatal("30 Aug 2026 is a Sunday and must sit in column 0")
	}
}

func TestCalendarMarksToday(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 30, 15, 4, 0, 0, time.UTC)
	g := calendarGrid(now)
	var marked int
	for _, week := range g.Weeks {
		for _, cell := range week {
			if cell.Today {
				marked++
				if cell.Day != 30 || !cell.InMonth {
					t.Fatalf("today cell = %+v", cell)
				}
			}
		}
	}
	if marked != 1 {
		t.Fatalf("today marks = %d, want 1", marked)
	}
}
