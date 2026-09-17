package config

import (
	"fmt"
	"slices"
	"strings"
)

// Lane mutations live here, beside resolveItem, rather than in the settings
// panel or in a package of their own.
//
// They are pure functions over []Item so the drag path and the keyboard path
// call the same rules and cannot drift, and so the invariants that matter --
// the one-level nesting cap, empty group pruning, lazy id minting -- are
// testable without a live host. They sit next to the loader because what they
// produce has to be something the loader accepts; a separate package would be
// a third home for bar rules that already live here.
//
// Every one returns a new slice. The settings pane edits a draft it may
// discard, so an in-place edit would have already changed it.

// ItemPath addresses one item within a lane. Member is the position inside the
// group at Index, or -1 for a top-level item, which is what lets one address
// name both a chip in a lane and a chip inside a group.
type ItemPath struct {
	Index  int
	Member int
}

func (p ItemPath) String() string {
	if p.Member < 0 {
		return fmt.Sprintf("[%d]", p.Index)
	}
	return fmt.Sprintf("[%d].items[%d]", p.Index, p.Member)
}

// cloneLane copies a lane and every group's members, so a mutation of the copy
// cannot reach the caller's items through a shared backing array.
func cloneLane(lane []Item) []Item {
	out := make([]Item, len(lane))
	copy(out, lane)
	for i := range out {
		if len(out[i].Items) > 0 {
			out[i].Items = slices.Clone(out[i].Items)
		}
	}
	return out
}

// MoveItem reorders one top-level item. A move onto its own index is a no-op
// rather than an error: a drag that ends where it began is a normal gesture,
// not a failure.
func MoveItem(lane []Item, from, to int) ([]Item, error) {
	if from < 0 || from >= len(lane) {
		return nil, fmt.Errorf("config: move from %d: outside a lane of %d", from, len(lane))
	}
	if to < 0 || to >= len(lane) {
		return nil, fmt.Errorf("config: move to %d: outside a lane of %d", to, len(lane))
	}
	out := cloneLane(lane)
	if from == to {
		return out, nil
	}
	it := out[from]
	out = slices.Delete(out, from, from+1)
	return slices.Insert(out, to, it), nil
}

// InsertItem places an item at a position. An index equal to the length
// appends, which is what the add control at a lane's end resolves to.
func InsertItem(lane []Item, at int, it Item) ([]Item, error) {
	if at < 0 || at > len(lane) {
		return nil, fmt.Errorf("config: insert at %d: outside a lane of %d", at, len(lane))
	}
	return slices.Insert(cloneLane(lane), at, it), nil
}

// RemoveItem deletes the addressed item. Removing a group's last member prunes
// the group and its placement: an empty capsule is not a thing the bar can
// draw, and leaving one behind would need its own cleanup pass later.
func RemoveItem(lane []Item, p ItemPath) ([]Item, error) {
	if p.Index < 0 || p.Index >= len(lane) {
		return nil, fmt.Errorf("config: remove %s: outside a lane of %d", p, len(lane))
	}
	out := cloneLane(lane)
	if p.Member < 0 {
		return slices.Delete(out, p.Index, p.Index+1), nil
	}
	group := out[p.Index]
	if group.ID != "group" {
		return nil, fmt.Errorf("config: remove %s: %q is not a group", p, group.ID)
	}
	if p.Member >= len(group.Items) {
		return nil, fmt.Errorf("config: remove %s: the group holds %d", p, len(group.Items))
	}
	group.Items = slices.Delete(group.Items, p.Member, p.Member+1)
	if len(group.Items) == 0 {
		return slices.Delete(out, p.Index, p.Index+1), nil
	}
	out[p.Index] = group
	return out, nil
}

// GroupItems folds the item at src into a new group with the item at dst,
// which is what a drop on a chip's inner half means. The result sits where dst
// was.
//
// The one-level cap is enforced here, at the drop, rather than left to the
// loader: a refusal the user sees as "that is not allowed" is a different
// thing from a configuration that fails to load afterwards, and silently
// flattening would lose what they asked for.
func GroupItems(lane []Item, src, dst int, m *Minter) ([]Item, error) {
	if src < 0 || src >= len(lane) {
		return nil, fmt.Errorf("config: group from %d: outside a lane of %d", src, len(lane))
	}
	if dst < 0 || dst >= len(lane) {
		return nil, fmt.Errorf("config: group onto %d: outside a lane of %d", dst, len(lane))
	}
	if src == dst {
		return nil, fmt.Errorf("config: group %d onto itself", src)
	}
	if lane[src].ID == "group" || lane[dst].ID == "group" {
		return nil, fmt.Errorf("config: a group may not contain a group")
	}

	// The members keep the order they had in the lane, and the group takes the
	// earlier of the two positions. A group is a visible run of chips, so the
	// arrangement the user could see before the drop is the one they get
	// after it; ordering by which chip happened to be dragged would reshuffle
	// the pair for no reason they could name.
	first, second := min(src, dst), max(src, dst)
	out := cloneLane(lane)
	a, b := out[first], out[second]
	m.Ensure(&a)
	m.Ensure(&b)
	group := Item{ID: "group", Items: []Item{a, b}}
	m.Ensure(&group)

	out[first] = group
	return slices.Delete(out, second, second+1), nil
}

// UngroupItem dissolves a group, splicing its members back into the lane where
// the group stood so the arrangement the user could see is preserved.
func UngroupItem(lane []Item, at int) ([]Item, error) {
	if at < 0 || at >= len(lane) {
		return nil, fmt.Errorf("config: ungroup %d: outside a lane of %d", at, len(lane))
	}
	if lane[at].ID != "group" {
		return nil, fmt.Errorf("config: ungroup %d: %q is not a group", at, lane[at].ID)
	}
	out := cloneLane(lane)
	members := out[at].Items
	out = slices.Delete(out, at, at+1)
	return slices.Insert(out, at, members...), nil
}

// Minter hands out instance ids that do not collide with one another or with
// anything the configuration already carries.
//
// D3 mints lazily: an id appears only when something addresses the widget, so
// an existing document keeps working untouched and a file grows ids only for
// the widgets the user actually customised, which keeps diffs readable.
type Minter struct{ taken map[string]bool }

// NewMinter reads every id already in use, across the shared bar and every
// output override. A minted id that collided with one of those would make
// consistentInstances refuse the result of the editor's own edit.
func NewMinter(cfg Config) *Minter {
	m := &Minter{taken: map[string]bool{}}
	bars := append([]Bar{cfg.Bar}, func() []Bar {
		out := make([]Bar, 0, len(cfg.Outputs))
		for _, o := range cfg.Outputs {
			out = append(out, o.Bar)
		}
		return out
	}()...)
	for _, b := range bars {
		for _, section := range [][]Item{b.Left, b.Center, b.Right} {
			for _, it := range flattenItems(section) {
				if it.Instance != "" {
					m.taken[it.Instance] = true
				}
			}
		}
	}
	return m
}

// Ensure gives an item an id if it has none, and returns the id it ends up
// with. An item that already carries one keeps it: minting is how identity
// starts, never how it changes.
//
// A nil Minter is usable and mints nothing, so a caller that only reorders --
// which addresses positions rather than widgets -- need not build one.
func (m *Minter) Ensure(it *Item) string {
	if it == nil {
		return ""
	}
	if it.Instance != "" {
		return it.Instance
	}
	if m == nil {
		return ""
	}
	// The id is derived from the widget it names, so a configuration file
	// stays readable: "clock-2" says what it is, where a counter would not.
	// A plugin placement is named for its entry rather than the literal
	// "plugin", for the same reason.
	stem := it.ID
	if it.ID == "plugin" && it.Entry != "" {
		stem = it.Entry
	}
	stem = strings.TrimSuffix(stem, "-")
	for n := 1; ; n++ {
		candidate := fmt.Sprintf("%s-%d", stem, n)
		if !m.taken[candidate] {
			m.taken[candidate] = true
			it.Instance = candidate
			return candidate
		}
	}
}
