package main

import (
	"testing"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
	"github.com/Nomadcxx/sysc-shell/plugins/reference/worldclock"
)

func TestDropAfterRenderedRowReordersAfterThatZone(t *testing.T) {
	zones := []string{"UTC", "Europe/Paris", "Asia/Tokyo"}
	readings := []worldclock.Reading{
		{Zone: zones[0]},
		{Zone: zones[1]},
		{Zone: zones[2]},
	}
	tree := worldclock.PanelTree(readings, "", "", "")
	var list *v1.Node
	for _, child := range tree.Children {
		if child.Kind == v1.KindList {
			list = child
			break
		}
	}
	if list == nil || len(list.Children) < 3 {
		t.Fatalf("zone list = %+v, want a gap after the first row", list)
	}
	dropNode := list.Children[2].ID
	if dropNode != "drop:1" {
		t.Fatalf("gap after the first row = %q, want drop:1", dropNode)
	}

	insertBefore, ok := dropInsertBefore(dropNode, len(zones))
	if !ok {
		t.Fatalf("invalid drop node %q", dropNode)
	}
	clock := worldclock.New()
	clock.Restore(zones)
	if err := clock.Reorder(2, insertBefore); err != nil {
		t.Fatal(err)
	}
	want := []string{"UTC", "Asia/Tokyo", "Europe/Paris"}
	if got := clock.Zones(); !equalStrings(got, want) {
		t.Fatalf("zones after drop = %v, want %v", got, want)
	}
}

func TestPanelOpenParamsKeepPlacementAndOutputGeneration(t *testing.T) {
	event := &v1.InputEvent{ViewID: "bar-view", Output: "DP-1", Generation: 42}
	got := panelOpenParams("world-clock@DP-1", event)
	want := v1.PanelParams{Entry: "panel", Output: "DP-1", Generation: 42, Instance: "world-clock@DP-1"}
	if got != want {
		t.Fatalf("panel params = %+v, want %+v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
