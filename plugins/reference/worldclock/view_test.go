package worldclock

import (
	"testing"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func TestPanelTreeHasDragHandlesAndTimeKeys(t *testing.T) {
	t.Parallel()
	root := PanelTree([]Reading{{Zone: "UTC", Clock: "15:04", Offset: "UTC+0"}}, "", "", "")
	if err := v1.Validate(root, v1.ViewPanel); err != nil {
		t.Fatal(err)
	}
	var drag, timeKey, drop bool
	walk(root, func(n *v1.Node) {
		if n.Kind == v1.KindDragSource && n.ID == "drag:UTC" {
			drag = true
		}
		if n.Key == "time:UTC" {
			timeKey = true
		}
		if n.Kind == v1.KindDropZone {
			drop = true
		}
	})
	if !drag || !timeKey || !drop {
		t.Fatalf("drag=%v time=%v drop=%v", drag, timeKey, drop)
	}
}

func TestTimePatchUpdatesExistingPanelTimeAndOffsetNodes(t *testing.T) {
	t.Parallel()
	root := PanelTree([]Reading{{Zone: "UTC", Clock: "15:00", Offset: "UTC+0"}}, "", "", "")
	keys := map[string]bool{}
	walk(root, func(n *v1.Node) {
		if n.Key != "" {
			keys[n.Key] = true
		}
	})
	patch := TimePatch([]Reading{{Zone: "UTC", Clock: "16:00", Offset: "UTC+1"}})
	got := map[string]string{}
	for _, r := range patch {
		if !keys[r.Key] {
			t.Errorf("replacement key %q does not exist in panel tree", r.Key)
		}
		got[r.Key] = r.Node.Text
	}
	if len(got) != 2 || got["time:UTC"] != "16:00" || got["offset:UTC"] != "UTC+1" {
		t.Fatalf("patch values = %v", got)
	}
	for _, r := range patch {
		if r.Node.Kind != v1.KindText {
			t.Fatalf("patched %s, want text", r.Node.Kind)
		}
	}
}

func TestBarTimePatchTouchesOnlyTheBarTimeNode(t *testing.T) {
	t.Parallel()
	patch := BarTimePatch(Reading{Zone: "UTC", Clock: "16:00", Offset: "UTC+0"})
	if len(patch) != 1 || patch[0].Key != "time" || patch[0].Node.Text != "16:00" {
		t.Fatalf("bar patch = %+v", patch)
	}
}

func TestBarTreeIsARow(t *testing.T) {
	t.Parallel()
	root := BarTree(Reading{Zone: "UTC", Clock: "15:04"})
	if err := v1.Validate(root, v1.ViewBar); err != nil {
		t.Fatal(err)
	}
}

func walk(n *v1.Node, fn func(*v1.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, c := range n.Children {
		walk(c, fn)
	}
}
