package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
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
		if node.Width != node.Height || node.Width != DefaultTheme().CompactHeight {
			t.Errorf("%s is %dx%d, want a square %d", tc.action, node.Width, node.Height,
				DefaultTheme().CompactHeight)
		}
	}
}
