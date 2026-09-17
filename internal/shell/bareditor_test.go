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
	x := target.X + target.W/2
	if !inner {
		x = target.X + 1
	}
	return h.barDrop(reg, zone, payload, x, target.Y+target.H/2)
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
