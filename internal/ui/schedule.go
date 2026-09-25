package ui

import (
	"fmt"
	"sort"
	"time"
)

const (
	scheduleAxisWidth         = 48
	scheduleHeaderHeight      = 30
	scheduleAllDayRowHeight   = 24
	scheduleMinEventHeight    = 20
	scheduleMinColumnWidth    = 64
	scheduleTimelineMinHeight = 160
)

type ScheduleLayout struct {
	Start time.Time
	Now   time.Time
	Zone  *time.Location
	Days  int
}

type ScheduleItem struct {
	Start     time.Time
	End       time.Time
	DayIndex  int
	AllDay    bool
	StartDate string
	EndDate   string
}

type ScheduleGeometry struct {
	AxisWidth    int
	HeaderHeight int
	TimelineY    int
	TimelineH    int
	ColumnWidth  int
	Days         int
}

type allDayPlacement struct {
	node       *Node
	start, end int
	lane       int
}

type timedPlacement struct {
	node       *Node
	start, end time.Time
	lane       int
}

func layoutScheduleGrid(root *Node, bounds Rect, measure MeasureText) error {
	if root.Schedule == nil || root.Schedule.Zone == nil || root.Schedule.Days < 1 || root.Schedule.Days > 7 {
		return fmt.Errorf("schedule grid has invalid range data")
	}
	if bounds.W < scheduleAxisWidth+root.Schedule.Days*scheduleMinColumnWidth {
		return fmt.Errorf("schedule grid width %d is too narrow for %d days", bounds.W, root.Schedule.Days)
	}
	root.Bounds = bounds
	start := localScheduleDay(root.Schedule.Start, root.Schedule.Zone)
	allDay := make([]allDayPlacement, 0)
	timedByDay := make([][]timedPlacement, root.Schedule.Days)
	for _, child := range root.Children {
		if child == nil || child.Kind != KindButton || child.ScheduleItem == nil {
			return fmt.Errorf("schedule grid children must be event buttons")
		}
		item := child.ScheduleItem
		if item.AllDay {
			first, errFirst := time.ParseInLocation("2006-01-02", item.StartDate, root.Schedule.Zone)
			last, errLast := time.ParseInLocation("2006-01-02", item.EndDate, root.Schedule.Zone)
			if errFirst != nil || errLast != nil || !first.Before(last) {
				return fmt.Errorf("schedule event %q has invalid all-day bounds", child.Action)
			}
			firstIndex, lastIndex := scheduleDayIndex(start, first, root.Schedule.Days), scheduleDayIndex(start, last, root.Schedule.Days)
			if firstIndex < 0 && lastIndex < 0 {
				continue
			}
			if firstIndex < 0 {
				firstIndex = 0
			}
			if lastIndex < 0 {
				lastIndex = root.Schedule.Days
			}
			allDay = append(allDay, allDayPlacement{node: child, start: firstIndex, end: lastIndex})
			continue
		}
		if item.DayIndex < 0 || item.DayIndex >= root.Schedule.Days || !item.Start.Before(item.End) {
			return fmt.Errorf("schedule event %q has invalid timed bounds", child.Action)
		}
		dayStart, dayEnd := scheduleDayBounds(root.Schedule, item.DayIndex)
		segStart, segEnd := item.Start.In(root.Schedule.Zone), item.End.In(root.Schedule.Zone)
		if segStart.Before(dayStart) {
			segStart = dayStart
		}
		if segEnd.After(dayEnd) {
			segEnd = dayEnd
		}
		if segStart.Before(segEnd) {
			timedByDay[item.DayIndex] = append(timedByDay[item.DayIndex], timedPlacement{node: child, start: segStart, end: segEnd})
		}
	}

	sort.SliceStable(allDay, func(i, j int) bool {
		if allDay[i].start == allDay[j].start {
			return allDay[i].end > allDay[j].end
		}
		return allDay[i].start < allDay[j].start
	})
	laneEnds := make([]int, 0)
	for i := range allDay {
		lane := 0
		for lane < len(laneEnds) && laneEnds[lane] > allDay[i].start {
			lane++
		}
		if lane == len(laneEnds) {
			laneEnds = append(laneEnds, allDay[i].end)
		} else {
			laneEnds[lane] = allDay[i].end
		}
		allDay[i].lane = lane
	}
	geometry := ScheduleGeometry{
		AxisWidth: scheduleAxisWidth, HeaderHeight: scheduleHeaderHeight,
		TimelineY:   scheduleHeaderHeight + len(laneEnds)*scheduleAllDayRowHeight + 6,
		ColumnWidth: (bounds.W - scheduleAxisWidth) / root.Schedule.Days,
		Days:        root.Schedule.Days,
	}
	geometry.TimelineH = bounds.H - geometry.TimelineY
	if geometry.TimelineH < scheduleTimelineMinHeight {
		return fmt.Errorf("schedule grid height %d leaves too little time range", bounds.H)
	}
	root.ScheduleGeometry = geometry
	for _, placement := range allDay {
		x := bounds.X + geometry.AxisWidth + placement.start*geometry.ColumnWidth + 2
		w := (placement.end-placement.start)*geometry.ColumnWidth - 4
		child := placement.node
		child.Bounds = Rect{X: x, Y: bounds.Y + geometry.HeaderHeight + placement.lane*scheduleAllDayRowHeight,
			W: w, H: scheduleMinEventHeight}
		if err := layoutButtonContent(child, measure, true); err != nil {
			return err
		}
	}
	for day, items := range timedByDay {
		placeTimedScheduleDay(root, day, items, geometry, measure)
	}
	return nil
}

func placeTimedScheduleDay(root *Node, day int, items []timedPlacement, geometry ScheduleGeometry, measure MeasureText) {
	if len(items) == 0 {
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].start.Equal(items[j].start) {
			return items[i].node.Action < items[j].node.Action
		}
		return items[i].start.Before(items[j].start)
	})
	laneEnds := make([]time.Time, 0)
	for i := range items {
		lane := 0
		for lane < len(laneEnds) && laneEnds[lane].After(items[i].start) {
			lane++
		}
		if lane == len(laneEnds) {
			laneEnds = append(laneEnds, items[i].end)
		} else {
			laneEnds[lane] = items[i].end
		}
		items[i].lane = lane
	}
	colW := geometry.ColumnWidth
	laneW := colW / len(laneEnds)
	dayStart, dayEnd := scheduleDayBounds(root.Schedule, day)
	dayDuration := dayEnd.Sub(dayStart)
	for _, item := range items {
		top := int(float64(geometry.TimelineH) * float64(item.start.Sub(dayStart)) / float64(dayDuration))
		bottom := int(float64(geometry.TimelineH) * float64(item.end.Sub(dayStart)) / float64(dayDuration))
		height := max(bottom-top, scheduleMinEventHeight)
		if top+height > geometry.TimelineH {
			top = max(geometry.TimelineH-height, 0)
		}
		width := laneW - 4
		if item.lane == len(laneEnds)-1 {
			width = colW - item.lane*laneW - 4
		}
		item.node.Bounds = Rect{
			X: root.Bounds.X + geometry.AxisWidth + day*colW + item.lane*laneW + 2,
			Y: root.Bounds.Y + geometry.TimelineY + top,
			W: width, H: height,
		}
		_ = layoutButtonContent(item.node, measure, true)
	}
}

func scheduleDayBounds(schedule *ScheduleLayout, index int) (time.Time, time.Time) {
	start := localScheduleDay(schedule.Start, schedule.Zone).AddDate(0, 0, index)
	return start, start.AddDate(0, 0, 1)
}

func localScheduleDay(date time.Time, zone *time.Location) time.Time {
	date = date.In(zone)
	year, month, day := date.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, zone)
}

func scheduleDayIndex(start, date time.Time, days int) int {
	key := date.In(start.Location()).Format("2006-01-02")
	for i := 0; i < days; i++ {
		if start.AddDate(0, 0, i).Format("2006-01-02") == key {
			return i
		}
	}
	return -1
}

func scheduleNowY(schedule *ScheduleLayout, now time.Time, geometry ScheduleGeometry) (int, bool) {
	zone := schedule.Zone
	if zone == nil {
		return 0, false
	}
	local := now.In(zone)
	index := scheduleDayIndex(localScheduleDay(schedule.Start, zone), local, schedule.Days)
	if index < 0 {
		return 0, false
	}
	dayStart, dayEnd := scheduleDayBounds(schedule, index)
	if local.Before(dayStart) || !local.Before(dayEnd) {
		return 0, false
	}
	y := int(float64(geometry.TimelineH) * float64(local.Sub(dayStart)) / float64(dayEnd.Sub(dayStart)))
	return geometry.TimelineY + y, true
}

func (schedule *ScheduleLayout) DayBounds(index int) (time.Time, time.Time) {
	if schedule == nil || schedule.Zone == nil {
		return time.Time{}, time.Time{}
	}
	return scheduleDayBounds(schedule, index)
}

func (schedule *ScheduleLayout) DayIndex(date time.Time) int {
	if schedule == nil || schedule.Zone == nil {
		return -1
	}
	return scheduleDayIndex(localScheduleDay(schedule.Start, schedule.Zone), date, schedule.Days)
}
