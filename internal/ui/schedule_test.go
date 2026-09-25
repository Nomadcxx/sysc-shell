package ui

import (
	"testing"
	"time"
)

func TestScheduleGridPlacesDatesAllDayAndOverlaps(t *testing.T) {
	zone, err := time.LoadLocation("Australia/Melbourne")
	if err != nil {
		t.Fatal(err)
	}
	for _, days := range []int{1, 4, 7} {
		start := time.Date(2026, 9, 14, 0, 0, 0, 0, zone)
		grid := &Node{Kind: KindScheduleGrid, Schedule: &ScheduleLayout{Start: start, Days: days, Zone: zone, Now: start.Add(12 * time.Hour)}}
		grid.Children = []*Node{
			{Kind: KindButton, Action: "a", ScheduleItem: &ScheduleItem{Start: start.Add(9 * time.Hour), End: start.Add(11 * time.Hour)}},
			{Kind: KindButton, Action: "b", ScheduleItem: &ScheduleItem{Start: start.Add(10 * time.Hour), End: start.Add(12 * time.Hour)}},
		}
		if days >= 4 {
			grid.Children = append(grid.Children, &Node{Kind: KindButton, Action: "c", ScheduleItem: &ScheduleItem{AllDay: true, StartDate: "2026-09-15", EndDate: "2026-09-17"}})
		}
		if err := layoutScheduleGrid(grid, Rect{X: 8, Y: 12, W: 1000, H: 700}, fakeMeasure); err != nil {
			t.Fatalf("%d-day layout: %v", days, err)
		}
		for _, item := range grid.Children {
			if item.Bounds.W <= 0 || item.Bounds.H < scheduleMinEventHeight {
				t.Fatalf("%d-day item %q bounds = %+v", days, item.Action, item.Bounds)
			}
			if item.Bounds.X < grid.Bounds.X || item.Bounds.X+item.Bounds.W > grid.Bounds.X+grid.Bounds.W || item.Bounds.Y+item.Bounds.H > grid.Bounds.Y+grid.Bounds.H {
				t.Fatalf("%d-day item %q escaped the grid: %+v", days, item.Action, item.Bounds)
			}
		}
		if grid.Children[0].Bounds.W >= 1000 || grid.Children[1].Bounds.W != grid.Children[0].Bounds.W {
			t.Fatalf("overlap lanes not split: %v and %v", grid.Children[0].Bounds, grid.Children[1].Bounds)
		}
		if days >= 4 && grid.Children[2].Bounds.W < 2*scheduleMinColumnWidth {
			t.Fatalf("multi-day all-day event did not span columns: %+v", grid.Children[2].Bounds)
		}
	}
}

func TestScheduleGridUsesElapsedTimeAcrossDSTAndClipsNow(t *testing.T) {
	zone, err := time.LoadLocation("Australia/Melbourne")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 4, 0, 0, 0, 0, zone)
	grid := &Node{Kind: KindScheduleGrid, Schedule: &ScheduleLayout{Start: start, Days: 1, Zone: zone, Now: start.Add(12 * time.Hour)}}
	grid.Children = []*Node{{Kind: KindButton, Action: "noon", ScheduleItem: &ScheduleItem{
		Start: time.Date(2026, 10, 4, 12, 0, 0, 0, zone), End: time.Date(2026, 10, 4, 13, 0, 0, 0, zone),
	}}}
	if err := layoutScheduleGrid(grid, Rect{W: 600, H: 700}, fakeMeasure); err != nil {
		t.Fatal(err)
	}
	_, dayEnd := scheduleDayBounds(grid.Schedule, 0)
	if dayEnd.Sub(start) != 23*time.Hour {
		t.Fatalf("DST day duration = %v, want 23h", dayEnd.Sub(start))
	}
	position := grid.Children[0].Bounds.Y - (grid.Bounds.Y + grid.ScheduleGeometry.TimelineY)
	want := grid.ScheduleGeometry.TimelineH * int(grid.Children[0].ScheduleItem.Start.Sub(start)) / int(dayEnd.Sub(start))
	if position < want-2 || position > want+2 {
		t.Fatalf("noon top = %d, want elapsed-time position near %d", position, want)
	}
	if _, ok := scheduleNowY(grid.Schedule, start.AddDate(0, 0, -1), grid.ScheduleGeometry); ok {
		t.Fatal("off-range current-time marker was not clipped")
	}
}
