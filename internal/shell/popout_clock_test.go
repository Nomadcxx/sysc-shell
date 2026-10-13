package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"testing"
	"time"
)

func TestCalendarGridSevenColumns(t *testing.T) {
	t.Parallel()
	d := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	g := calendarGrid(d, d)
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

// AddDate keeps the day-of-month and normalises, so stepping from the 29th-31st
// skipped or repeated months. Every step has to move exactly one month.
func TestCalendarMonthStepsOneMonthFromAnyDay(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		now  time.Time
	}{
		{"Jan 29", time.Date(2026, 1, 29, 12, 0, 0, 0, time.UTC)},
		{"Jan 30", time.Date(2026, 1, 30, 12, 0, 0, 0, time.UTC)},
		{"Jan 31", time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)},
		{"Mar 31", time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)},
		{"May 31", time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)},
		{"Dec 31", time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC)},
		{"Feb 29 leap", time.Date(2028, 2, 29, 12, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		for delta := -13; delta <= 25; delta++ {
			got := calendarMonth(tc.now, delta)
			want := time.Date(tc.now.Year(), tc.now.Month()+time.Month(delta), 1, 0, 0, 0, 0, time.UTC)
			if !got.Equal(want) {
				t.Errorf("%s delta %+d = %s, want %s", tc.name, delta, got.Format("2006-01-02"), want.Format("2006-01-02"))
			}
			if got.Day() != 1 {
				t.Errorf("%s delta %+d kept day %d, want 1", tc.name, delta, got.Day())
			}
		}
	}
}

func TestCalendarMonthConsecutiveStepsNeverRepeat(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)
	prev := calendarMonth(now, 0)
	for delta := 1; delta <= 14; delta++ {
		cur := calendarMonth(now, delta)
		if monthsApart(prev, cur) != 1 {
			t.Fatalf("step %d went %s -> %s, want exactly one month", delta, prev.Format("2006-01"), cur.Format("2006-01"))
		}
		prev = cur
	}
}

func monthsApart(a, b time.Time) int {
	return (b.Year()-a.Year())*12 + int(b.Month()-a.Month())
}

// Today is the real current day, not a day of whatever month the view landed on.
func TestCalendarTodayOnlyInCurrentMonth(t *testing.T) {
	t.Parallel()
	today := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)
	for _, delta := range []int{-1, 1, 2} {
		month := calendarMonth(today, delta)
		var marked []int
		for _, week := range calendarGrid(month, today).Weeks {
			for _, cell := range week {
				if cell.Today {
					marked = append(marked, cell.Day)
				}
			}
		}
		if len(marked) != 0 {
			t.Errorf("viewing %s marked days %v as Today, want none", month.Format("January 2006"), marked)
		}
	}
	var marked []int
	for _, week := range calendarGrid(calendarMonth(today, 0), today).Weeks {
		for _, cell := range week {
			if cell.Today {
				marked = append(marked, cell.Day)
			}
		}
	}
	if len(marked) != 1 || marked[0] != 31 {
		t.Errorf("viewing the current month marked %v, want [31]", marked)
	}
}

// February has to be reachable from the end of January in both directions.
func TestCalendarMonthReachesFebruary(t *testing.T) {
	t.Parallel()
	for _, day := range []int{28, 29, 30, 31} {
		now := time.Date(2026, 1, day, 12, 0, 0, 0, time.UTC)
		if got := calendarMonth(now, 1); got.Month() != time.February {
			t.Errorf("from Jan %d, next month is %s, want February", day, got.Format("January 2006"))
		}
		if got := calendarMonth(now, -1); got.Month() != time.December || got.Year() != 2025 {
			t.Errorf("from Jan %d, previous month is %s, want December 2025", day, got.Format("January 2006"))
		}
	}
}

// A leap February must render 29 cells from either neighbouring month.
func TestCalendarGridLeapFebruaryLength(t *testing.T) {
	t.Parallel()
	today := time.Date(2028, 1, 31, 12, 0, 0, 0, time.UTC)
	feb := calendarGrid(calendarMonth(today, 1), today)
	inMonth := 0
	for _, week := range feb.Weeks {
		for _, cell := range week {
			if cell.InMonth {
				inMonth++
			}
		}
	}
	if inMonth != 29 {
		t.Fatalf("Feb 2028 rendered %d cells, want 29", inMonth)
	}
	// March 2028 has 31 days and must come back from February's end.
	mar := calendarMonth(time.Date(2028, 2, 29, 12, 0, 0, 0, time.UTC), 1)
	if mar.Month() != time.March || mar.Day() != 1 {
		t.Fatalf("from Feb 29 2028 got %s, want March 1 2028", mar.Format("2006-01-02"))
	}
}

// The header has to name the month the grid is showing.
func TestClockTreeHeaderFollowsNavigatedMonth(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 31, 14, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		delta int
		want  string
	}{
		{-1, "December 2025"},
		{0, "January 2026"},
		{1, "February 2026"},
		{2, "March 2026"},
	} {
		tree := clockTree(now, tc.delta, DefaultTheme())
		node := findNode(tree, func(n *ui.Node) bool { return n.Text == tc.want })
		if node == nil {
			t.Errorf("delta %+d: clock popout has no %q header", tc.delta, tc.want)
		}
	}
}

// Tree-level check on the same thing the grid test covers: a Today marker is an
// action on the cell node, so a paged-to month must not carry one.
func TestClockTreeMarksTodayOnlyInCurrentMonth(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 31, 14, 30, 0, 0, time.UTC)
	for _, delta := range []int{-1, 1, 2} {
		var marked []string
		walkNodes(clockTree(now, delta, DefaultTheme()), func(n *ui.Node) {
			if n.Action == "today" {
				marked = append(marked, n.Text)
			}
		})
		if len(marked) != 0 {
			t.Errorf("delta %+d marked %v as today, want none", delta, marked)
		}
	}
	var marked []string
	walkNodes(clockTree(now, 0, DefaultTheme()), func(n *ui.Node) {
		if n.Action == "today" {
			marked = append(marked, n.Text)
		}
	})
	if len(marked) != 1 || marked[0] != "31" {
		t.Errorf("the current month marked %v, want [31]", marked)
	}
}
