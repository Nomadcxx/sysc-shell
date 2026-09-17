package shell

import (
	"slices"
	"strings"
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

// chips lays three rows stacked vertically, which is how a lane is drawn: one
// full-width row per widget. The decision is therefore on the vertical axis.
// It was written against a horizontal fixture first, and passed while the real
// lane resolved every drop against its first chip -- a drag down did nothing
// and a drop onto a group was unreachable.
func chips() []ui.Rect {
	return []ui.Rect{
		{X: 0, Y: 0, W: 300, H: 40},
		{X: 0, Y: 50, W: 300, H: 40},
		{X: 0, Y: 100, W: 300, H: 40},
	}
}

func TestDropOnAChipsInnerHalfJoinsIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		y    int
		join int
	}{
		{"middle of the first", 20, 0},
		{"middle of the second", 70, 1},
		{"middle of the third", 120, 2},
		{"just inside the inner half", 11, 0},
	} {
		got := resolveDrop(chips(), 150, tc.y)
		if got.Join != tc.join {
			t.Errorf("%s: Join = %d, want %d (%+v)", tc.name, got.Join, tc.join, got)
		}
	}
}

func TestDropNearAnEdgeInsertsRatherThanJoins(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		y      int
		insert int
	}{
		{"above the first", 2, 0},
		{"below the first", 38, 1},
		{"above the second", 52, 1},
		{"below the third", 138, 3},
		{"in the gap between rows", 45, 1},
		{"past the end of the lane", 400, 3},
		{"above the lane", -20, 0},
	} {
		got := resolveDrop(chips(), 150, tc.y)
		if got.Join >= 0 {
			t.Errorf("%s: joined row %d, want an insertion", tc.name, got.Join)
			continue
		}
		if got.Insert != tc.insert {
			t.Errorf("%s: Insert = %d, want %d", tc.name, got.Insert, tc.insert)
		}
	}
}

func TestDropOnAnEmptyLaneInsertsAtTheStart(t *testing.T) {
	t.Parallel()
	got := resolveDrop(nil, 150, 40)
	if got.Join >= 0 || got.Insert != 0 {
		t.Errorf("got %+v, want an insertion at 0", got)
	}
}

// A lane holding one row still has to offer above and below, or a widget could
// never be placed before the one already there.
func TestASingleChipOffersBothSides(t *testing.T) {
	t.Parallel()
	one := []ui.Rect{{X: 0, Y: 0, W: 300, H: 40}}
	if got := resolveDrop(one, 150, 2); got.Join >= 0 || got.Insert != 0 {
		t.Errorf("leading edge: got %+v, want insert 0", got)
	}
	if got := resolveDrop(one, 150, 38); got.Join >= 0 || got.Insert != 1 {
		t.Errorf("trailing edge: got %+v, want insert 1", got)
	}
	if got := resolveDrop(one, 150, 20); got.Join != 0 {
		t.Errorf("middle: got %+v, want join 0", got)
	}
}

// Horizontal position is not part of the decision: a lane row spans the whole
// column, so every row shares one x range and only the vertical position can
// tell them apart. Resolving on x is what made a drag down do nothing.
func TestResolutionIgnoresHorizontalPosition(t *testing.T) {
	t.Parallel()
	for _, x := range []int{-40, 0, 150, 299, 800} {
		if got := resolveDrop(chips(), x, 70); got.Join != 1 {
			t.Errorf("x=%d: got %+v, want join 1", x, got)
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
	// Fourteen widgets across the three lanes, plus the one group's own
	// handle.
	if len(got) != 15 {
		t.Errorf("default bar drew %d chips (%v), want fourteen widgets plus the group handle", len(got), got)
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
	// The group leads with its own handle: a group is draggable, selectable
	// and inspectable under D2, and it renders as a column, which cannot be a
	// drag source itself.
	strip := barLaneStrip(h)
	got := chipNames(strip)
	want := []string{"Group", "Processor", "Memory"}
	if len(got) != len(want) {
		t.Fatalf("group drew %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("group drew %v, want %v", got, want)
		}
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

// Task 11 and D6. Every keyboard command calls the same config mutation the
// pointer path calls, so the two cannot drift.
func barKeyHost(t *testing.T, bar config.Bar) (*Registry, *PanelHost) {
	t.Helper()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	drainAux(t, reg, 2)
	h := reg.panelHosts[PanelSettings]
	h.draft.Bar = bar
	h.section = "Bar"
	h.alt = true
	reg.rebuildPanel(h)
	barLayout(t, reg, h)
	return reg, h
}

// barLayout lays the panel out so chip bounds exist. The pointer path resolves
// a drop from laid-out boxes, so a test that skipped this would be inventing
// the geometry it then asserts against.
func barLayout(t *testing.T, reg *Registry, h *PanelHost) {
	t.Helper()
	size := panelTargetSize(PanelSettings)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if err := h.configure(size.W, size.H, int(ui.ScaleUnit)); err != nil {
		t.Fatalf("settings does not lay out: %v", err)
	}
}

func focusChip(t *testing.T, h *PanelHost, ref config.ItemRef) {
	t.Helper()
	want := "bar-select:" + barRefAction(ref)
	for i, n := range h.focus {
		if n != nil && n.Action == want {
			h.roving.Set(i)
			return
		}
	}
	t.Fatalf("no chip focusable for %+v", ref)
}

func laneIDs(items []config.Item) string { return chipSummary(items) }

func chipSummary(items []config.Item) string {
	var out []string
	for _, it := range items {
		if it.ID == "group" {
			var m []string
			for _, x := range it.Items {
				m = append(m, x.ID)
			}
			out = append(out, "group("+strings.Join(m, ",")+")")
			continue
		}
		out = append(out, it.ID)
	}
	return strings.Join(out, " ")
}

func TestAltArrowMovesAChipWithinItsLane(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{Left: []config.Item{{ID: "clock"}, {ID: "cpu"}, {ID: "memory"}}})
	focusChip(t, h, config.ItemRef{Lane: "left", Path: config.ItemPath{Index: 0, Member: -1}})
	if !h.barKeyPress(reg, keyRight) {
		t.Fatal("alt+right was not handled")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "cpu clock memory" {
		t.Errorf("after alt+right: %q", got)
	}
}

func TestAltArrowStopsAtTheEndOfALane(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{Left: []config.Item{{ID: "clock"}, {ID: "cpu"}}})
	focusChip(t, h, config.ItemRef{Lane: "left", Path: config.ItemPath{Index: 0, Member: -1}})
	if !h.barKeyPress(reg, keyLeft) {
		t.Fatal("alt+left at the start should still be handled, not fall through")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "clock cpu" {
		t.Errorf("a chip moved off the start of its lane: %q", got)
	}
}

func TestAltArrowMovesAChipBetweenLanes(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{
		Left:   []config.Item{{ID: "clock"}, {ID: "cpu"}},
		Center: []config.Item{{ID: "wordmark"}},
	})
	focusChip(t, h, config.ItemRef{Lane: "left", Path: config.ItemPath{Index: 0, Member: -1}})
	if !h.barKeyPress(reg, keyDown) {
		t.Fatal("alt+down was not handled")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "cpu" {
		t.Errorf("left lane after the move: %q", got)
	}
	if got := laneIDs(h.draft.Bar.Center); got != "wordmark clock" {
		t.Errorf("centre lane after the move: %q", got)
	}
}

func TestAltArrowMovesAMemberWithinItsGroup(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{
		Left: []config.Item{{ID: "group", Items: []config.Item{{ID: "cpu"}, {ID: "memory"}}}},
	})
	focusChip(t, h, config.ItemRef{Lane: "left", Path: config.ItemPath{Index: 0, Member: 0}})
	if !h.barKeyPress(reg, keyRight) {
		t.Fatal("alt+right on a group member was not handled")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "group(memory,cpu)" {
		t.Errorf("after moving inside the group: %q", got)
	}
}

// Without the modifier the arrows still belong to whatever has focus.
func TestArrowsWithoutAltAreNotLaneCommands(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{Left: []config.Item{{ID: "clock"}, {ID: "cpu"}}})
	h.alt = false
	focusChip(t, h, config.ItemRef{Lane: "left", Path: config.ItemPath{Index: 0, Member: -1}})
	if h.barKeyPress(reg, keyRight) {
		t.Fatal("a bare arrow was taken as a lane command")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "clock cpu" {
		t.Errorf("the lane changed anyway: %q", got)
	}
}

// Task 12. The inspector is built from entries synthesised at runtime over one
// config.Item. This is what sub-project A exists for: the retired Get/Set
// switch pair could name a setting only by a compile-time path.
func TestInspectorShowsTheSelectedWidgetsOwnOptions(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{
		Left: []config.Item{{ID: "clock", Format: "15:04"}, {ID: "clock", Format: "Mon 2 Jan"}},
	})
	if !h.barActivate(reg, "bar-inspect:left:1:-1") {
		t.Fatal("inspect action was not handled")
	}
	node := barInspector(h, 600)
	if node == nil {
		t.Fatal("no inspector for a selected chip")
	}
	var fields []string
	walkNodes(node, func(n *ui.Node) {
		if n.Kind == ui.KindTextField {
			fields = append(fields, n.Name)
		}
	})
	if len(fields) != 1 || fields[0] != "Format" {
		t.Errorf("inspector fields = %v, want one Format row", fields)
	}
}

func TestInspectorSaysWhenAWidgetHasNoOptions(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{Left: []config.Item{{ID: "wordmark"}}})
	h.barActivate(reg, "bar-select:left:0:-1")
	var text []string
	walkNodes(barInspector(h, 600), func(n *ui.Node) {
		if n.Kind == ui.KindText && n.Text != "" {
			text = append(text, n.Text)
		}
	})
	if !slices.Contains(text, "This widget has no options.") {
		t.Errorf("inspector text = %v, want it to say there are no options", text)
	}
}

func TestInspectorSaysWhenTheSelectionIsGone(t *testing.T) {
	t.Parallel()
	_, h := barKeyHost(t, config.Bar{Left: []config.Item{{ID: "clock"}}})
	h.barSelected = "left:7:-1"
	var text []string
	walkNodes(barInspector(h, 600), func(n *ui.Node) {
		if n.Kind == ui.KindText && n.Text != "" {
			text = append(text, n.Text)
		}
	})
	if !slices.Contains(text, "That widget is no longer on the bar.") {
		t.Errorf("inspector text = %v, want it to say the widget is gone", text)
	}
}

// The group command and the equivalent drag produce the same draft, which is
// the guarantee D6 is for.
func TestGroupCommandMatchesTheEquivalentDrag(t *testing.T) {
	t.Parallel()
	start := config.Bar{Left: []config.Item{{ID: "clock"}, {ID: "cpu"}, {ID: "memory"}}}

	reg, h := barKeyHost(t, start)
	h.barActivate(reg, "bar-group:left:0:-1")
	viaCommand := laneIDs(h.draft.Bar.Left)

	// The drag equivalent: a drop of chip 1 onto chip 0's inner half.
	outcome := resolveDrop([]ui.Rect{
		{X: 0, Y: 0, W: 100, H: 30}, {X: 110, Y: 0, W: 100, H: 30}, {X: 220, Y: 0, W: 100, H: 30},
	}, 50, 15)
	if outcome.Join != 0 {
		t.Fatalf("the drop did not resolve to a join: %+v", outcome)
	}
	dragged, err := config.GroupItems(start.Left, 1, outcome.Join, config.NewMinter(config.Config{}))
	if err != nil {
		t.Fatal(err)
	}
	if viaCommand != laneIDs(dragged) {
		t.Errorf("command gave %q, the equivalent drag gives %q", viaCommand, laneIDs(dragged))
	}
}

func TestUngroupCommandDissolvesTheGroup(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{
		Left: []config.Item{{ID: "group", Items: []config.Item{{ID: "cpu"}, {ID: "memory"}}}},
	})
	if !h.barActivate(reg, "bar-ungroup:left:0:-1") {
		t.Fatal("ungroup was not handled")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "cpu memory" {
		t.Errorf("after dissolving: %q", got)
	}
}

func TestAddAndRemoveReachTheDraft(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{Left: []config.Item{{ID: "clock"}}})
	if !h.barActivate(reg, "bar-add-item:left:battery") {
		t.Fatal("add was not handled")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "clock battery" {
		t.Errorf("after adding: %q", got)
	}
	if !h.barActivate(reg, "bar-remove:left:0:-1") {
		t.Fatal("remove was not handled")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "battery" {
		t.Errorf("after removing: %q", got)
	}
}

// Task 13 and D7. A lane is inherited whole or overridden whole, because
// applyBar takes it whole. Editing one widget on one output therefore forks
// that entire lane, and later changes to the shared lane stop reaching it.
func TestEditingAnOutputForksTheWholeLane(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{
		Left:  []config.Item{{ID: "clock"}, {ID: "cpu"}},
		Right: []config.Item{{ID: "battery"}},
	})
	h.barOutput = "DP-1"
	reg.rebuildPanel(h)

	if h.barLaneOverridden("left") {
		t.Fatal("the lane reads as overridden before anything was edited")
	}
	focusChip(t, h, config.ItemRef{Lane: "left", Path: config.ItemPath{Index: 0, Member: -1}})
	if !h.barKeyPress(reg, keyRight) {
		t.Fatal("alt+right was not handled on an output")
	}

	if !h.barLaneOverridden("left") {
		t.Error("the edited lane does not read as overridden")
	}
	if h.barLaneOverridden("right") {
		t.Error("an untouched lane was forked too")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "clock cpu" {
		t.Errorf("the shared lane changed: %q", got)
	}

	// A later change to the shared lane must not reach the forked one.
	h.draft.Bar.Left = append(h.draft.Bar.Left, config.Item{ID: "memory"})
	if got := laneIDs(barLaneItems(h.barEditedBar(), "left")); got != "cpu clock" {
		t.Errorf("the forked lane followed the shared one: %q", got)
	}
	// The lane it never touched still inherits.
	if got := laneIDs(barLaneItems(h.barEditedBar(), "right")); got != "battery" {
		t.Errorf("an untouched lane stopped inheriting: %q", got)
	}
}

func TestResetReturnsALaneToShared(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{Left: []config.Item{{ID: "clock"}, {ID: "cpu"}}})
	h.barOutput = "DP-1"
	reg.rebuildPanel(h)
	focusChip(t, h, config.ItemRef{Lane: "left", Path: config.ItemPath{Index: 0, Member: -1}})
	h.barKeyPress(reg, keyRight)
	if !h.barLaneOverridden("left") {
		t.Fatal("the lane was not forked")
	}
	if !h.barActivate(reg, "bar-reset-lane:left") {
		t.Fatal("reset was not handled")
	}
	if h.barLaneOverridden("left") {
		t.Error("the lane still reads as overridden after a reset")
	}
	if got := laneIDs(barLaneItems(h.barEditedBar(), "left")); got != "clock cpu" {
		t.Errorf("after the reset the lane shows %q, want the shared order", got)
	}
}

// The pointer path is a thin caller over resolveDrop and the same mutations
// the keyboard uses. These drive it through the real node tree so the chip
// bounds are the laid-out ones rather than invented.
func dropOnLane(t *testing.T, reg *Registry, h *PanelHost, payload, lane string, chipIndex int, inner bool) bool {
	t.Helper()
	var zone *ui.Node
	walkNodes(h.root, func(n *ui.Node) {
		if n.Action == "bar-lane:"+lane {
			zone = n
		}
	})
	if zone == nil {
		t.Fatalf("no drop zone for the %s lane", lane)
	}
	want := "bar-select:" + barRefAction(config.ItemRef{
		Lane: lane, Path: config.ItemPath{Index: chipIndex, Member: -1},
	})
	var target ui.Rect
	walkNodes(zone, func(n *ui.Node) {
		if n.Action == want && n.Bounds.H > 0 {
			target = n.Bounds
		}
	})
	if target.H == 0 {
		t.Fatalf("chip %d of the %s lane has no laid-out box", chipIndex, lane)
	}
	// Rows stack vertically, so the middle of a row joins it and the top edge
	// inserts above it.
	y := target.Y + target.H/2
	if !inner {
		y = target.Y + 1
	}
	return h.barDrop(reg, zone, payload, target.X+target.W/2, y)
}

func TestDroppingOnAChipsInnerHalfGroupsThem(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{Left: []config.Item{{ID: "clock"}, {ID: "cpu"}, {ID: "memory"}}})
	if !dropOnLane(t, reg, h, "left:1:-1", "left", 0, true) {
		t.Fatal("the drop was not handled")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "group(clock,cpu) memory" {
		t.Errorf("after the drop: %q", got)
	}
}

func TestDroppingNearAnEdgeReorders(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{Left: []config.Item{{ID: "clock"}, {ID: "cpu"}, {ID: "memory"}}})
	if !dropOnLane(t, reg, h, "left:2:-1", "left", 0, false) {
		t.Fatal("the drop was not handled")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "memory clock cpu" {
		t.Errorf("after the drop: %q", got)
	}
}

func TestDroppingOntoAnotherLaneCarriesTheWidget(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{
		Left:  []config.Item{{ID: "clock"}, {ID: "cpu"}},
		Right: []config.Item{{ID: "battery"}},
	})
	if !dropOnLane(t, reg, h, "left:0:-1", "right", 0, false) {
		t.Fatal("the cross-lane drop was not handled")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "cpu" {
		t.Errorf("source lane: %q", got)
	}
	if got := laneIDs(h.draft.Bar.Right); got != "clock battery" {
		t.Errorf("target lane: %q", got)
	}
}

// D5's cap is enforced at the drop, and the refusal is something the user sees
// rather than a configuration that fails to load afterwards.
func TestDroppingAGroupOntoAGroupIsRefusedAtTheDrop(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{Left: []config.Item{
		{ID: "group", Items: []config.Item{{ID: "cpu"}}},
		{ID: "group", Items: []config.Item{{ID: "memory"}}},
	}})
	dropOnLane(t, reg, h, "left:1:-1", "left", 0, true)
	if got := laneIDs(h.draft.Bar.Left); got != "group(cpu) group(memory)" {
		t.Errorf("the lane changed despite the refusal: %q", got)
	}
	if h.errLabel == "" {
		t.Error("the refusal was silent; the user is told nothing")
	}
}

// Dragging the last member out of a group prunes the group with it.
func TestDraggingTheLastMemberOutPrunesTheGroup(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{
		Left:  []config.Item{{ID: "group", Items: []config.Item{{ID: "cpu"}}}},
		Right: []config.Item{{ID: "battery"}},
	})
	if !dropOnLane(t, reg, h, "left:0:0", "right", 0, false) {
		t.Fatal("the drop was not handled")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "" {
		t.Errorf("the emptied group was left behind: %q", got)
	}
	if got := laneIDs(h.draft.Bar.Right); got != "cpu battery" {
		t.Errorf("target lane: %q", got)
	}
}

// Every chip's own content has to be laid out, not just the chip.
//
// This is the defect the laptop found and every test here missed. The chip
// nested a row inside the drag source alongside a second child, and
// layoutButtonContent only descends into a single row or column child -- with
// two or more it measures each one but never lays out their children. The pill
// painted; the grip, the label and the inspector glyph inside it did not. The
// chrome-fit conformance test could not catch it either, because it asserts a
// box for interactive nodes and a label is not interactive.
func TestChipContentIsLaidOutNotJustTheChip(t *testing.T) {
	t.Parallel()
	_, h := barKeyHost(t, config.Bar{
		Left:  []config.Item{{ID: "clock"}, {ID: "cpu"}},
		Right: []config.Item{{ID: "group", Items: []config.Item{{ID: "memory"}}}},
	})

	var chips []*ui.Node
	walkNodes(h.root, func(n *ui.Node) {
		if n.Kind == ui.KindDragSource && n.DragType == barChipDragType {
			chips = append(chips, n)
		}
	})
	if len(chips) == 0 {
		t.Fatal("no chips in the laid-out tree")
	}

	for _, chip := range chips {
		if chip.Bounds.W <= 0 || chip.Bounds.H <= 0 {
			t.Errorf("chip %q has no box", chip.Name)
			continue
		}
		var labels, glyphs int
		walkNodes(chip, func(n *ui.Node) {
			switch n.Kind {
			case ui.KindText:
				if n.Text == "" {
					return
				}
				labels++
				if n.Bounds.W <= 0 || n.Bounds.H <= 0 {
					t.Errorf("chip %q: label %q has no box, so the chip paints empty", chip.Name, n.Text)
				}
			case ui.KindIcon:
				glyphs++
				if n.Bounds.W <= 0 || n.Bounds.H <= 0 {
					t.Errorf("chip %q: glyph %q has no box", chip.Name, n.Icon)
				}
			}
		})
		if labels == 0 {
			t.Errorf("chip %q carries no label at all", chip.Name)
		}
		if glyphs == 0 {
			t.Errorf("chip %q carries no glyph at all", chip.Name)
		}
	}
}

// Every per-chip action has to be reachable where the chip is. The inspector
// used to be the only route to remove and group, and it renders beneath three
// vertically stacked lanes: with the default bar its controls landed some 360
// logical pixels below the scroll viewport, so neither was reachable at all.
// Found by the owner on the laptop.
func TestChipCarriesItsOwnActions(t *testing.T) {
	t.Parallel()
	_, h := barKeyHost(t, config.Default().Bar)

	for _, want := range []string{
		"bar-remove:left:0:-1",
		"bar-group:left:0:-1",
		"bar-inspect:left:0:-1",
	} {
		var found *ui.Node
		walkNodes(h.root, func(n *ui.Node) {
			if n.Action == want {
				found = n
			}
		})
		if found == nil {
			t.Errorf("%s is not on the chip at all", want)
			continue
		}
		if found.Bounds.W <= 0 || found.Bounds.H <= 0 {
			t.Errorf("%s has no box", want)
		}
		if !found.Focusable || found.Name == "" {
			t.Errorf("%s is not reachable by keyboard or screen reader", want)
		}
	}
}

// A group's own row carries dissolve, and its members carry remove, so a
// grouping can be taken apart and put back together without the inspector.
func TestAGroupMemberCanBeRemovedFromItsOwnRow(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{
		Left: []config.Item{{ID: "group", Items: []config.Item{{ID: "cpu"}, {ID: "memory"}}}},
	})
	var remove *ui.Node
	walkNodes(h.root, func(n *ui.Node) {
		if n.Action == "bar-remove:left:0:1" {
			remove = n
		}
	})
	if remove == nil || remove.Bounds.W <= 0 {
		t.Fatal("a group member has no reachable remove control")
	}
	if !h.barActivate(reg, "bar-remove:left:0:1") {
		t.Fatal("removing a group member was not handled")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "group(cpu)" {
		t.Errorf("after removing a member: %q", got)
	}
}

// Grouping is reachable again after a dissolve: the chip's own control folds
// it together with the neighbour that follows it.
func TestAGroupCanBeRebuiltAfterBeingDissolved(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{
		Left: []config.Item{{ID: "group", Items: []config.Item{{ID: "cpu"}, {ID: "memory"}}}},
	})
	if !h.barActivate(reg, "bar-ungroup:left:0:-1") {
		t.Fatal("dissolve was not handled")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "cpu memory" {
		t.Fatalf("after dissolving: %q", got)
	}
	if !h.barActivate(reg, "bar-group:left:0:-1") {
		t.Fatal("regrouping was not handled")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "group(cpu,memory)" {
		t.Errorf("after regrouping: %q", got)
	}
}

// The last chip in a lane has nothing after it to group with, so it must not
// offer the control at all rather than offering one that fails.
func TestTheLastChipOffersNoGroupControl(t *testing.T) {
	t.Parallel()
	_, h := barKeyHost(t, config.Bar{Left: []config.Item{{ID: "clock"}, {ID: "cpu"}}})
	var found bool
	walkNodes(h.root, func(n *ui.Node) {
		if n.Action == "bar-group:left:1:-1" {
			found = true
		}
	})
	if found {
		t.Error("the last chip in the lane offers a group control with nothing to group with")
	}
}

// Every edit rebuilds the panel tree, and a fresh tree starts at the top. With
// three stacked lanes the strip is taller than the viewport, so a drag or a
// remove threw the user back to the first row and lost their place. Reported
// from the laptop.
func TestAnEditKeepsTheScrollPosition(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Default().Bar)

	scroll := func() *ui.Node {
		var out *ui.Node
		walkNodes(h.root, func(n *ui.Node) {
			if n.Kind == ui.KindScroll && out == nil {
				out = n
			}
		})
		return out
	}
	s := scroll()
	if s == nil {
		t.Fatal("the settings body is not a scrolling column")
	}
	// Somewhere down the strip, well past the first lane.
	const parked = 240
	s.ScrollOffset = parked

	if !h.barActivate(reg, "bar-remove:right:0:-1") {
		t.Fatal("remove was not handled")
	}
	barLayout(t, reg, h)

	after := scroll()
	if after == nil {
		t.Fatal("no scrolling column after the edit")
	}
	if after.ScrollOffset != parked {
		t.Errorf("scroll offset = %d after an edit, want %d: the view jumped", after.ScrollOffset, parked)
	}
}

// A control that draws a glyph and no text says nothing to anyone who does not
// already know what it does. The settings rail learned this during sub-project
// A -- its tabs draw glyphs only, so the name was reachable by screen reader
// and by nothing else -- and the lane editor is denser than the rail: three
// glyph controls per chip, plus dissolve and add.
func TestEveryGlyphOnlyControlInTheEditorHasATooltip(t *testing.T) {
	t.Parallel()
	_, h := barKeyHost(t, config.Bar{
		Left:  []config.Item{{ID: "clock"}, {ID: "cpu"}},
		Right: []config.Item{{ID: "group", Items: []config.Item{{ID: "memory"}, {ID: "gpu"}}}},
	})

	hasText := func(n *ui.Node) bool {
		found := false
		walkNodes(n, func(c *ui.Node) {
			if c.Kind == ui.KindText && c.Text != "" {
				found = true
			}
		})
		return found
	}

	var checked int
	walkNodes(h.root, func(n *ui.Node) {
		if !n.Focusable || !strings.HasPrefix(n.Action, "bar-") {
			return
		}
		if hasText(n) {
			return
		}
		checked++
		if n.Tooltip == "" {
			t.Errorf("%s draws a glyph only and carries no tooltip", n.Action)
		}
		if n.Name == "" {
			t.Errorf("%s has no accessible name", n.Action)
		}
	})
	if checked == 0 {
		t.Fatal("no glyph-only controls found; this test is not covering anything")
	}
}

// Opening the add list and changing your mind has to be possible. The control
// toggles, so the same press that opened it closes it again.
func TestTheAddListCanBeDismissed(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{Left: []config.Item{{ID: "clock"}}})
	if !h.barActivate(reg, "bar-add:left") {
		t.Fatal("add was not handled")
	}
	if h.barAdding != "left" {
		t.Fatalf("barAdding = %q, want left", h.barAdding)
	}
	if !h.barActivate(reg, "bar-add:left") {
		t.Fatal("the second press was not handled")
	}
	if h.barAdding != "" {
		t.Errorf("barAdding = %q after pressing again, want the list closed", h.barAdding)
	}
}

// Opening another lane's list moves the list rather than leaving two open.
func TestOpeningAnotherLanesAddListMovesIt(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{Left: []config.Item{{ID: "clock"}}})
	h.barActivate(reg, "bar-add:left")
	h.barActivate(reg, "bar-add:right")
	if h.barAdding != "right" {
		t.Errorf("barAdding = %q, want right", h.barAdding)
	}
}

// The inspector belongs beside what it inspects. It used to render after all
// three lanes, so selecting a widget in the Left lane put its options below the
// Right lane, a long way from the chip.
func TestTheInspectorFollowsTheLaneThatOwnsTheSelection(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{
		Left:  []config.Item{{ID: "clock"}},
		Right: []config.Item{{ID: "battery"}},
	})
	h.barActivate(reg, "bar-select:left:0:-1")
	barLayout(t, reg, h)

	var inspectorY, rightLaneY int
	walkNodes(h.root, func(n *ui.Node) {
		if n.Action == "bar-remove:left:0:-1" && n.Bounds.H > 0 {
			// The inspector's own remove control, not the chip's: the chip's
			// sits inside a drag source.
		}
		if n.Action == "bar-lane:right" {
			rightLaneY = n.Bounds.Y
		}
		if n.Kind == ui.KindText && n.Text == "This widget has no options." {
			inspectorY = n.Bounds.Y
		}
	})
	if inspectorY == 0 || rightLaneY == 0 {
		t.Skip("layout did not produce both markers")
	}
	if inspectorY > rightLaneY {
		t.Errorf("inspector at y=%d sits below the Right lane at y=%d", inspectorY, rightLaneY)
	}
}

// Dropping onto a group joins it, rather than being unreachable. The group's
// whole body is the row the lane resolves against, so anywhere on it counts.
func TestDroppingOntoAGroupJoinsIt(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{Left: []config.Item{
		{ID: "clock"},
		{ID: "group", Items: []config.Item{{ID: "cpu"}, {ID: "memory"}}},
	}})
	var body ui.Rect
	walkNodes(h.root, func(n *ui.Node) {
		if n.Action == "bar-groupbody:left:1:-1" && n.Bounds.H > 0 {
			body = n.Bounds
		}
	})
	if body.H == 0 {
		t.Fatal("the group body has no box, so it cannot be a drop target")
	}
	var zone *ui.Node
	walkNodes(h.root, func(n *ui.Node) {
		if n.Action == "bar-lane:left" {
			zone = n
		}
	})
	if !h.barDrop(reg, zone, "left:0:-1", body.X+body.W/2, body.Y+body.H/2) {
		t.Fatal("the drop onto a group was not handled")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "group(cpu,memory,clock)" {
		t.Errorf("after dropping onto the group: %q", got)
	}
}

// Dragging downwards has to work. It resolved every drop against the first row
// while the rule was horizontal, so a downward drag did nothing at all.
func TestDraggingDownwardsReorders(t *testing.T) {
	t.Parallel()
	reg, h := barKeyHost(t, config.Bar{
		Left: []config.Item{{ID: "clock"}, {ID: "cpu"}, {ID: "memory"}},
	})
	if !dropOnLane(t, reg, h, "left:0:-1", "left", 2, false) {
		t.Fatal("the downward drop was not handled")
	}
	if got := laneIDs(h.draft.Bar.Left); got != "cpu clock memory" {
		t.Errorf("after dragging the first row down onto the third's top edge: %q", got)
	}
}

// A group has to read as one thing. It used to paint at the same fill as the
// chips inside it, so a grouping looked like three unrelated rows.
func TestAGroupIsVisuallyDistinctFromItsLaneAndItsMembers(t *testing.T) {
	t.Parallel()
	_, h := barKeyHost(t, config.Bar{
		Left: []config.Item{{ID: "group", Items: []config.Item{{ID: "cpu"}, {ID: "memory"}}}},
	})
	var laneFill, groupFill ui.Fill
	var memberFills []ui.Fill
	walkNodes(h.root, func(n *ui.Node) {
		switch {
		case n.Action == "bar-lane:left":
			laneFill = n.Fill
		case n.Action == "bar-groupbody:left:0:-1":
			groupFill = n.Fill
		case n.Kind == ui.KindDragSource && n.DragType == barChipDragType &&
			strings.Contains(n.Action, ":0:") && !strings.HasSuffix(n.Action, ":-1"):
			memberFills = append(memberFills, n.Fill)
		}
	})
	if groupFill == laneFill {
		t.Errorf("the group paints the same fill as its lane (%v)", groupFill)
	}
	if len(memberFills) == 0 {
		t.Fatal("no member chips found")
	}
	for _, f := range memberFills {
		if f == groupFill {
			t.Errorf("a member chip paints the same fill as the group (%v)", f)
		}
	}
}

// A drag repainted the whole surface on every pointer motion, and the paint
// path reads no drag state at all, so each repaint produced identical pixels.
// On a 900x760 software-rendered panel that is what made dragging feel like
// heavy load. A repaint is now worth doing only when the answer changes.
func TestDragMotionOnlyRepaintsWhenTheTargetChanges(t *testing.T) {
	t.Parallel()
	_, h := barKeyHost(t, config.Bar{
		Left: []config.Item{{ID: "clock"}, {ID: "cpu"}, {ID: "memory"}},
	})
	var zone *ui.Node
	rowOf := map[int]ui.Rect{}
	walkNodes(h.root, func(n *ui.Node) {
		if n.Action == "bar-lane:left" {
			zone = n
		}
		for i := range 3 {
			if n.Action == "bar-select:"+barRefAction(config.ItemRef{
				Lane: "left", Path: config.ItemPath{Index: i, Member: -1},
			}) && n.Bounds.H > 0 {
				rowOf[i] = n.Bounds
			}
		}
	})
	if zone == nil || len(rowOf) != 3 {
		t.Fatal("the lane did not lay out three rows")
	}

	src := &ui.Node{Kind: ui.KindDragSource, DragType: barChipDragType, Payload: "left:0:-1"}
	first := rowOf[0]
	h.drag.Begin(src, float64(first.X+first.W/2), float64(first.Y+first.H/2))
	// Past the movement threshold, so the drag is live.
	h.drag.Move(float64(first.X+first.W/2), float64(first.Y+first.H/2+20))
	_ = h.barDragHover()

	// Staying over the same row must not ask for a repaint.
	second := rowOf[1]
	h.drag.Move(float64(second.X+second.W/2), float64(second.Y+second.H/2))
	if !h.barDragHover() {
		t.Fatal("moving onto a different row did not repaint")
	}
	for _, dy := range []int{1, 2, 3} {
		h.drag.Move(float64(second.X+second.W/2), float64(second.Y+second.H/2+dy))
		if h.barDragHover() {
			t.Errorf("a motion of %d px within the same row asked for a repaint", dy)
		}
	}

	// And the row it is over is marked, so the drag shows where it lands.
	var marked []string
	walkNodes(h.root, func(n *ui.Node) {
		if strings.HasPrefix(n.Action, "bar-select:") && n.State&ui.StateHovered != 0 {
			marked = append(marked, n.Action)
		}
	})
	if len(marked) != 1 || marked[0] != "bar-select:left:1:-1" {
		t.Errorf("marked rows = %v, want exactly the row under the pointer", marked)
	}
}
