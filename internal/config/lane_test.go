package config

import (
	"strings"
	"testing"
)

// D1: lane mutations are pure functions over []Item, so the rules are written
// once and both the pointer and the keyboard path call them. These tests need
// no PanelHost, which is the point: the invariants that matter -- the
// one-level cap, empty group pruning, lazy id minting, instance uniqueness --
// would otherwise be written inside pointer-event handling.

func ids(lane []Item) string {
	var parts []string
	for _, it := range lane {
		if it.ID == "group" {
			var members []string
			for _, m := range it.Items {
				members = append(members, m.ID)
			}
			parts = append(parts, "group("+strings.Join(members, ",")+")")
			continue
		}
		parts = append(parts, it.ID)
	}
	return strings.Join(parts, " ")
}

func lane() []Item {
	return []Item{{ID: "clock"}, {ID: "cpu"}, {ID: "memory"}}
}

func TestMoveItemWithinALane(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		from, to int
		want     string
	}{
		{"forward", 0, 2, "cpu memory clock"},
		{"back", 2, 0, "memory clock cpu"},
		{"adjacent", 0, 1, "cpu clock memory"},
		{"onto itself", 1, 1, "clock cpu memory"},
	} {
		got, err := MoveItem(lane(), tc.from, tc.to)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if ids(got) != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, ids(got), tc.want)
		}
	}
}

func TestMoveItemRefusesAnIndexOutsideTheLane(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ from, to int }{{-1, 0}, {0, 3}, {5, 0}} {
		if _, err := MoveItem(lane(), tc.from, tc.to); err == nil {
			t.Errorf("MoveItem(%d, %d) succeeded, want a refusal", tc.from, tc.to)
		}
	}
}

// A mutation must not write through to the caller's slice: the settings pane
// holds a draft it may discard, and an in-place edit would have already
// changed it.
func TestMutationsDoNotAliasTheInput(t *testing.T) {
	t.Parallel()
	src := lane()
	if _, err := MoveItem(src, 0, 2); err != nil {
		t.Fatal(err)
	}
	if ids(src) != "clock cpu memory" {
		t.Errorf("input lane was mutated: %q", ids(src))
	}
}

func TestInsertItemAtAPosition(t *testing.T) {
	t.Parallel()
	got, err := InsertItem(lane(), 1, Item{ID: "battery"})
	if err != nil {
		t.Fatal(err)
	}
	if ids(got) != "clock battery cpu memory" {
		t.Errorf("got %q", ids(got))
	}
	if got, err = InsertItem(lane(), 3, Item{ID: "battery"}); err != nil {
		t.Fatal(err)
	}
	if ids(got) != "clock cpu memory battery" {
		t.Errorf("append: got %q", ids(got))
	}
}

func TestRemoveItemFromALane(t *testing.T) {
	t.Parallel()
	got, err := RemoveItem(lane(), ItemPath{Index: 1, Member: -1})
	if err != nil {
		t.Fatal(err)
	}
	if ids(got) != "clock memory" {
		t.Errorf("got %q", ids(got))
	}
}

func TestGroupItemsCreatesAGroupHoldingBoth(t *testing.T) {
	t.Parallel()
	got, err := GroupItems(lane(), 1, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ids(got) != "clock group(cpu,memory)" {
		t.Errorf("got %q", ids(got))
	}
}

// D5: the one-level cap is enforced at the drop, never silently flattened.
func TestGroupItemsRefusesAGroupInsideAGroup(t *testing.T) {
	t.Parallel()
	start := []Item{
		{ID: "group", Items: []Item{{ID: "cpu"}}},
		{ID: "memory"},
	}
	_, err := GroupItems(start, 0, 1, nil)
	if err == nil {
		t.Fatal("grouping onto a group succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "group") {
		t.Errorf("error = %q, want it to say why", err)
	}
}

func TestUngroupItemSplicesMembersBackIntoTheLane(t *testing.T) {
	t.Parallel()
	start := []Item{
		{ID: "clock"},
		{ID: "group", Instance: "g1", Items: []Item{{ID: "cpu"}, {ID: "memory"}}},
		{ID: "battery"},
	}
	got, err := UngroupItem(start, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ids(got) != "clock cpu memory battery" {
		t.Errorf("got %q", ids(got))
	}
}

// D5: dragging the last member out removes the group and its placement.
func TestRemovingTheLastMemberPrunesTheGroup(t *testing.T) {
	t.Parallel()
	start := []Item{
		{ID: "clock"},
		{ID: "group", Instance: "g1", Items: []Item{{ID: "cpu"}}},
	}
	got, err := RemoveItem(start, ItemPath{Index: 1, Member: 0})
	if err != nil {
		t.Fatal(err)
	}
	if ids(got) != "clock" {
		t.Errorf("got %q, want the emptied group pruned with its placement", ids(got))
	}
}

func TestRemovingOneOfTwoMembersKeepsTheGroup(t *testing.T) {
	t.Parallel()
	start := []Item{{ID: "group", Instance: "g1", Items: []Item{{ID: "cpu"}, {ID: "memory"}}}}
	got, err := RemoveItem(start, ItemPath{Index: 0, Member: 1})
	if err != nil {
		t.Fatal(err)
	}
	if ids(got) != "group(cpu)" {
		t.Errorf("got %q", ids(got))
	}
}

// D3: an id appears only when something addresses the widget.
func TestGroupingMintsIdsOnlyForWhatItAddresses(t *testing.T) {
	t.Parallel()
	m := NewMinter(Config{})
	got, err := GroupItems(lane(), 1, 2, m)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Instance != "" {
		t.Errorf("untouched clock gained id %q", got[0].Instance)
	}
	group := got[1]
	if group.Instance == "" {
		t.Error("the new group was minted no id, so nothing can address it")
	}
	for _, m := range group.Items {
		if m.Instance == "" {
			t.Errorf("member %s was minted no id", m.ID)
		}
	}
}

// A minted id must not collide with one the configuration already carries, or
// consistentInstances refuses the result of the editor's own edit.
func TestMintedIdsAvoidWhatTheConfigurationAlreadyUses(t *testing.T) {
	t.Parallel()
	cfg := Config{Bar: Bar{Left: []Item{{ID: "cpu", Instance: "cpu-1"}}}}
	m := NewMinter(cfg)
	first := m.Ensure(&Item{ID: "cpu"})
	second := m.Ensure(&Item{ID: "cpu"})
	for _, got := range []string{first, second} {
		if got == "cpu-1" {
			t.Fatalf("minted %q, which the configuration already uses", got)
		}
	}
	if first == second {
		t.Fatalf("minted %q twice in one session", first)
	}
}

// The whole point of the pure functions: what they produce has to be something
// the loader will accept. This is the round-trip guard applied to mutations.
func TestAMutatedLaneSurvivesTheLoader(t *testing.T) {
	t.Parallel()
	m := NewMinter(Config{})
	grouped, err := GroupItems(lane(), 1, 2, m)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.Bar.Left, cfg.Bar.Center, cfg.Bar.Right = grouped, nil, nil

	path := t.TempDir() + "/config.json"
	if err := Write(path, cfg); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("the editor produced a configuration the loader refuses: %v", err)
	}
	if ids(got.Bar.Left) != "clock group(cpu,memory)" {
		t.Errorf("after a round trip: %q", ids(got.Bar.Left))
	}
}

// Task 6. The whole-file guarantee behind D3: loading a document and writing
// it back must not invent ids for widgets nobody touched, or every existing
// configuration grows identity it never asked for on the first save.
func TestAWriteWithoutAnEditGrowsNoIds(t *testing.T) {
	t.Parallel()
	cfg := Default()
	path := t.TempDir() + "/config.json"
	if err := Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range [][]Item{got.Bar.Left, got.Bar.Center, got.Bar.Right} {
		for _, it := range flattenItems(section) {
			if it.Instance != "" && it.ID != "plugin" {
				t.Errorf("%s gained instance %q on a plain round trip", it.ID, it.Instance)
			}
		}
	}
}

// Task 7. Addressing one item out of a whole bar is what lets a per-widget
// option write reach one widget instead of every widget of that type.
func TestBarItemRefsReachEveryItemIncludingGroupMembers(t *testing.T) {
	t.Parallel()
	b := Bar{
		Left:   []Item{{ID: "clock"}, {ID: "group", Items: []Item{{ID: "cpu"}, {ID: "memory"}}}},
		Center: []Item{{ID: "wordmark"}},
		Right:  []Item{{ID: "battery"}},
	}
	refs := BarItemRefs(b)
	var got []string
	for _, ref := range refs {
		it := b.ItemAt(ref)
		if it == nil {
			t.Fatalf("ref %v resolved to nothing", ref)
		}
		got = append(got, ref.Lane+":"+it.ID)
	}
	want := []string{
		"left:clock", "left:group", "left:cpu", "left:memory",
		"center:wordmark", "right:battery",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestItemAtReturnsAPointerIntoTheBar(t *testing.T) {
	t.Parallel()
	b := Bar{Left: []Item{{ID: "group", Items: []Item{{ID: "clock"}}}}}
	ref := ItemRef{Lane: "left", Path: ItemPath{Index: 0, Member: 0}}
	it := b.ItemAt(ref)
	if it == nil {
		t.Fatal("ItemAt resolved to nothing")
	}
	it.Format = "15:04"
	if b.Left[0].Items[0].Format != "15:04" {
		t.Error("ItemAt returned a copy; a write through it does not reach the bar")
	}
}

func TestItemAtRefusesAnAddressThatNoLongerResolves(t *testing.T) {
	t.Parallel()
	b := Bar{Left: []Item{{ID: "clock"}}}
	for _, ref := range []ItemRef{
		{Lane: "left", Path: ItemPath{Index: 4, Member: -1}},
		{Lane: "right", Path: ItemPath{Index: 0, Member: -1}},
		{Lane: "left", Path: ItemPath{Index: 0, Member: 2}},
		{Lane: "nowhere", Path: ItemPath{Index: 0, Member: -1}},
	} {
		if got := b.ItemAt(ref); got != nil {
			t.Errorf("ItemAt(%v) = %+v, want nil", ref, got)
		}
	}
}

func TestAddToGroupPutsAnItemInside(t *testing.T) {
	t.Parallel()
	start := []Item{{ID: "clock"}, {ID: "group", Items: []Item{{ID: "cpu"}}}}
	got, err := AddToGroup(start, 1, Item{ID: "memory"}, NewMinter(Config{}))
	if err != nil {
		t.Fatal(err)
	}
	if ids(got) != "clock group(cpu,memory)" {
		t.Errorf("got %q", ids(got))
	}
	if ids(start) != "clock group(cpu)" {
		t.Errorf("the input lane was mutated: %q", ids(start))
	}
}

func TestAddToGroupRefusesAGroupAndANonGroupTarget(t *testing.T) {
	t.Parallel()
	start := []Item{{ID: "clock"}, {ID: "group", Items: []Item{{ID: "cpu"}}}}
	if _, err := AddToGroup(start, 1, Item{ID: "group"}, nil); err == nil {
		t.Error("a group was added inside a group")
	}
	if _, err := AddToGroup(start, 0, Item{ID: "memory"}, nil); err == nil {
		t.Error("an item was added to something that is not a group")
	}
}
