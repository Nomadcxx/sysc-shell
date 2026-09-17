package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Task 9. Drop resolution is a pure function of chip bounds and a drop point,
// so the rule is testable without synthesising pointer events and the pointer
// path is a thin caller.
//
// Each lane has exactly one drop zone and resolves the target itself, because
// FindDropZone tests a node before descending into it: an outer lane zone
// always wins over a nested group zone, which would make a group drop target
// unreachable. Groups are consequently not drop zones.

// chips lays three 100-wide chips at x = 0, 110 and 220, all 30 tall.
func chips() []ui.Rect {
	return []ui.Rect{
		{X: 0, Y: 0, W: 100, H: 30},
		{X: 110, Y: 0, W: 100, H: 30},
		{X: 220, Y: 0, W: 100, H: 30},
	}
}

func TestDropOnAChipsInnerHalfJoinsIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		x    int
		join int
	}{
		{"centre of the first", 50, 0},
		{"centre of the second", 160, 1},
		{"centre of the third", 270, 2},
		{"just inside the inner half", 26, 0},
	} {
		got := resolveDrop(chips(), tc.x, 15)
		if got.Join != tc.join {
			t.Errorf("%s: Join = %d, want %d (%+v)", tc.name, got.Join, tc.join, got)
		}
	}
}

func TestDropNearAnEdgeInsertsRatherThanJoins(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		x      int
		insert int
	}{
		{"before the first", 5, 0},
		{"after the first", 95, 1},
		{"before the second", 115, 1},
		{"after the third", 315, 3},
		{"in the gap between chips", 105, 1},
		{"past the end of the lane", 400, 3},
		{"before the lane starts", -20, 0},
	} {
		got := resolveDrop(chips(), tc.x, 15)
		if got.Join >= 0 {
			t.Errorf("%s: joined chip %d, want an insertion", tc.name, got.Join)
			continue
		}
		if got.Insert != tc.insert {
			t.Errorf("%s: Insert = %d, want %d", tc.name, got.Insert, tc.insert)
		}
	}
}

func TestDropOnAnEmptyLaneInsertsAtTheStart(t *testing.T) {
	t.Parallel()
	got := resolveDrop(nil, 40, 15)
	if got.Join >= 0 || got.Insert != 0 {
		t.Errorf("got %+v, want an insertion at 0", got)
	}
}

// A lane holding one chip still has to offer both edges, or a widget could
// never be placed before the one already there.
func TestASingleChipOffersBothSides(t *testing.T) {
	t.Parallel()
	one := []ui.Rect{{X: 0, Y: 0, W: 100, H: 30}}
	if got := resolveDrop(one, 5, 15); got.Join >= 0 || got.Insert != 0 {
		t.Errorf("leading edge: got %+v, want insert 0", got)
	}
	if got := resolveDrop(one, 95, 15); got.Join >= 0 || got.Insert != 1 {
		t.Errorf("trailing edge: got %+v, want insert 1", got)
	}
	if got := resolveDrop(one, 50, 15); got.Join != 0 {
		t.Errorf("centre: got %+v, want join 0", got)
	}
}

// Vertical position is deliberately not part of the decision. The lane's own
// zone has already been resolved by FindDropZone with its drop slop, and a
// pointer a few pixels above or below a chip is still on that chip as far as
// the lane is concerned.
func TestResolutionIgnoresVerticalPosition(t *testing.T) {
	t.Parallel()
	for _, y := range []int{-40, 0, 15, 29, 80} {
		if got := resolveDrop(chips(), 160, y); got.Join != 1 {
			t.Errorf("y=%d: got %+v, want join 1", y, got)
		}
	}
}

// Task 10. The strip renders three lanes with a chip per widget.
func barHost(t *testing.T, bar config.Bar) *PanelHost {
	t.Helper()
	h := newSettingsHost()
	h.draft.Bar = bar
	h.section = "Bar"
	return h
}

func chipNames(n *ui.Node) []string {
	var out []string
	walkNodes(n, func(c *ui.Node) {
		if c.Kind == ui.KindDragSource {
			out = append(out, c.Name)
		}
	})
	return out
}

func TestLaneStripDrawsAChipPerWidget(t *testing.T) {
	t.Parallel()
	h := barHost(t, config.Bar{
		Left:   []config.Item{{ID: "clock"}, {ID: "cpu"}},
		Center: []config.Item{{ID: "wordmark"}},
		Right:  []config.Item{{ID: "battery"}},
	})
	got := chipNames(barLaneStrip(h))
	want := []string{"Clock", "Processor", "Wordmark", "Battery"}
	if len(got) != len(want) {
		t.Fatalf("chips = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chips = %v, want %v", got, want)
		}
	}
}

// Each lane has exactly one drop zone. More than one, or a zone nested inside
// a group, would be unreachable behind the outer one.
func TestEachLaneHasExactlyOneDropZone(t *testing.T) {
	t.Parallel()
	h := barHost(t, config.Bar{
		Left: []config.Item{{ID: "group", Items: []config.Item{{ID: "cpu"}, {ID: "memory"}}}},
	})
	zones := 0
	walkNodes(barLaneStrip(h), func(n *ui.Node) {
		if n.Kind == ui.KindDropZone {
			zones++
		}
	})
	if zones != len(config.LaneNames()) {
		t.Errorf("drop zones = %d, want one per lane (%d)", zones, len(config.LaneNames()))
	}
}

// Every column states its own width. A's clipping defect was a node with no
// width inside a row that right-pins, and this strip goes into that same pane.
func TestEveryLaneColumnCarriesAWidth(t *testing.T) {
	t.Parallel()
	h := barHost(t, config.Bar{Left: []config.Item{{ID: "clock"}}})
	walkNodes(barLaneStrip(h), func(n *ui.Node) {
		if n.Kind == ui.KindDropZone && n.Width <= 0 {
			t.Errorf("lane %q has no width, so it cannot bound its chips", n.Name)
		}
	})
	if strip := barLaneStrip(h); strip.Width <= 0 {
		t.Error("the strip itself has no width")
	}
}

// A chip's extent comes from the density ladder, never a literal.
//
// The design specified a lane as one horizontal row at a fixed height. That
// does not survive the default configuration -- the right lane carries six
// placements including a group of four -- and internal/ui offers neither a
// wrapping row nor a horizontal scroll, so a strip could only have clipped.
// Lanes are vertical stacks instead, and it is the chip that takes the ladder.
func TestChipHeightFollowsTheDensityLadder(t *testing.T) {
	t.Parallel()
	h := barHost(t, config.Bar{})
	if got, want := barChipHeight(h), h.metrics().StandardControl; got != want {
		t.Errorf("chip height = %d, want %d", got, want)
	}
}

// The whole default bar has to lay out without clipping, which is the case
// that forced the vertical stack.
func TestTheDefaultBarFitsTheStrip(t *testing.T) {
	t.Parallel()
	h := barHost(t, config.Default().Bar)
	got := chipNames(barLaneStrip(h))
	if len(got) != 14 {
		t.Errorf("default bar drew %d chips (%v), want one per widget including group members", len(got), got)
	}
}

func TestChipsCarryAccessibleNamesAndRoles(t *testing.T) {
	t.Parallel()
	h := barHost(t, config.Bar{Left: []config.Item{{ID: "window-title"}}})
	var chip *ui.Node
	walkNodes(barLaneStrip(h), func(n *ui.Node) {
		if n.Kind == ui.KindDragSource && chip == nil {
			chip = n
		}
	})
	if chip == nil {
		t.Fatal("no chip rendered")
	}
	if chip.Name != "Window title" || chip.Role != "button" || !chip.Focusable {
		t.Errorf("chip = name %q role %q focusable %v", chip.Name, chip.Role, chip.Focusable)
	}
	if chip.DragType != barChipDragType {
		t.Errorf("chip drag type = %q, want %q", chip.DragType, barChipDragType)
	}
}

// A group draws as a bounded run of its members with a dissolve control.
func TestAGroupDrawsItsMembersAndADissolveControl(t *testing.T) {
	t.Parallel()
	h := barHost(t, config.Bar{
		Left: []config.Item{{ID: "group", Items: []config.Item{{ID: "cpu"}, {ID: "memory"}}}},
	})
	strip := barLaneStrip(h)
	if got := chipNames(strip); len(got) != 2 || got[0] != "Processor" || got[1] != "Memory" {
		t.Errorf("group members = %v", got)
	}
	found := false
	walkNodes(strip, func(n *ui.Node) {
		if n.Action == "bar-ungroup:left:0:-1" {
			found = true
		}
	})
	if !found {
		t.Error("the group has no dissolve control")
	}
}

// An address has to survive the trip through an action string.
func TestARefRoundTripsThroughItsAction(t *testing.T) {
	t.Parallel()
	for _, ref := range []config.ItemRef{
		{Lane: "left", Path: config.ItemPath{Index: 0, Member: -1}},
		{Lane: "center", Path: config.ItemPath{Index: 3, Member: 2}},
		{Lane: "right", Path: config.ItemPath{Index: 11, Member: -1}},
	} {
		got, ok := barParseRef(barRefAction(ref))
		if !ok || got != ref {
			t.Errorf("round trip of %+v gave %+v (ok=%v)", ref, got, ok)
		}
	}
	for _, bad := range []string{"", "left", "left:0", "nowhere:0:-1", "left:x:-1", "left:0:y"} {
		if _, ok := barParseRef(bad); ok {
			t.Errorf("barParseRef(%q) accepted a malformed address", bad)
		}
	}
}
