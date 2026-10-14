package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"strings"
	"testing"
	"time"
)

func TestCalendarGridSevenColumns(t *testing.T) {
	t.Parallel()
	aug := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	g := calendarGrid(aug, aug)
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
	g := calendarGrid(now, now)
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

func TestCalendarNavigatesWithCompactChevronButtons(t *testing.T) {
	t.Parallel()
	tree := clockTree(time.Date(2026, 9, 3, 14, 30, 0, 0, time.UTC), 0, DefaultTheme())

	for _, tc := range []struct{ action, name, icon string }{
		{"cal-prev", "Previous month", "chevron_left"},
		{"cal-next", "Next month", "chevron_right"},
	} {
		node := findNode(tree, func(n *ui.Node) bool { return n.Action == tc.action })
		if node == nil {
			t.Fatalf("calendar lost its %q control", tc.action)
		}
		// The direction has to survive in the accessible name: the glyph is a
		// ligature and reads as nothing.
		if node.Name != tc.name {
			t.Errorf("%s name = %q, want %q", tc.action, node.Name, tc.name)
		}
		if node.Text != "" {
			t.Errorf("%s still carries the ASCII label %q", tc.action, node.Text)
		}
		icon := findNode(node, func(n *ui.Node) bool { return n.Kind == ui.KindIcon })
		if icon == nil || icon.Icon != tc.icon {
			t.Fatalf("%s icon = %v, want %q", tc.action, icon, tc.icon)
		}
		if !render.ValidMaterialIcon(icon.Icon) {
			t.Errorf("%s names %q, which is outside the embedded subset", tc.action, icon.Icon)
		}
		// A square compact button clamps to a circle; it needs no geometry of
		// its own beyond being square.
		if node.Width != node.Height || node.Width != DefaultTheme().Metrics.CompactControl {
			t.Errorf("%s is %dx%d, want a square %d", tc.action, node.Width, node.Height,
				DefaultTheme().Metrics.CompactControl)
		}
	}
}

func TestCalendarMonthStepsOneMonthFromAnyDay(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		now       time.Time
		delta     int
		wantYear  int
		wantMonth time.Month
	}{
		{time.Date(2026, 1, 29, 12, 0, 0, 0, time.UTC), 1, 2026, time.February},
		{time.Date(2026, 1, 30, 12, 0, 0, 0, time.UTC), 1, 2026, time.February},
		{time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC), 1, 2026, time.February},
		{time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC), -1, 2025, time.December},
		{time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC), -13, 2024, time.December},
		{time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC), 1, 2026, time.April},
		{time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC), 25, 2028, time.April},
		{time.Date(2025, 12, 31, 12, 0, 0, 0, time.UTC), 1, 2026, time.January},
		{time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC), 0, 2026, time.May},
	} {
		got := calendarMonth(tc.now, tc.delta)
		if got.Year() != tc.wantYear || got.Month() != tc.wantMonth {
			t.Errorf("calendarMonth(%s, %d) = %s, want %04d-%02d",
				tc.now.Format("2006-01-02"), tc.delta, got.Format("2006-01-02"), tc.wantYear, tc.wantMonth)
		}
	}
}

func TestCalendarGridMarksTodayOnlyInTheCurrentMonth(t *testing.T) {
	t.Parallel()
	today := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)
	marked := func(g Calendar) []int {
		var days []int
		for _, week := range g.Weeks {
			for _, cell := range week {
				if cell.Today {
					days = append(days, cell.Day)
				}
			}
		}
		return days
	}
	if got := marked(calendarGrid(calendarMonth(today, 0), today)); len(got) != 1 || got[0] != 31 {
		t.Fatalf("January marks %v, want [31]", got)
	}
	for _, delta := range []int{1, 2, -1} {
		if got := marked(calendarGrid(calendarMonth(today, delta), today)); len(got) != 0 {
			t.Errorf("delta %d marks %v, want no today cell", delta, got)
		}
	}
}

func TestClockPopoutStepsFromEndOfJanuaryIntoFebruary(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)
	if got := renderText(clockTree(now, 1, DefaultTheme())); !strings.Contains(got, "February 2026") {
		t.Fatalf("next month from 31 Jan reads %q, want February 2026", got)
	}
	if got := renderText(clockTree(now, 2, DefaultTheme())); !strings.Contains(got, "March 2026") {
		t.Fatalf("two months on from 31 Jan reads %q, want March 2026", got)
	}
	if got := renderText(clockTree(now, -1, DefaultTheme())); !strings.Contains(got, "December 2025") {
		t.Fatalf("previous month from 31 Jan reads %q, want December 2025", got)
	}
}
