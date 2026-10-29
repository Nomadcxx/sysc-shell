package worldclock

import (
	"strconv"
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

func TestBarTreeIsAnAccessibleActivationButton(t *testing.T) {
	t.Parallel()
	root := BarTree(Reading{Zone: "UTC", Clock: "15:04"})
	if err := v1.Validate(root, v1.ViewBar); err != nil {
		t.Fatal(err)
	}
	if root.Kind != v1.KindButton || root.ID != "open" || root.Name != "Open world clock" || root.Role != "button" ||
		len(root.Events) != 1 || root.Events[0] != v1.EventActivate {
		t.Fatalf("bar readout = %+v, want one accessible activation button", root)
	}
}

func TestBarTreeShowsOnlyFirstCityReadout(t *testing.T) {
	t.Parallel()
	root := BarTree(Reading{Zone: "UTC", Clock: "15:04"})
	if root.Key != "time" || root.Text != "15:04" || !root.Tabular {
		t.Fatalf("bar readout = %+v, want first city's time", root)
	}
}

func TestPanelTreeUsesDropGapsBetweenPaddedRows(t *testing.T) {
	t.Parallel()
	zones := []Reading{
		{Zone: "UTC", Clock: "15:04", Offset: "UTC+0"},
		{Zone: "America/New_York", Clock: "11:04", Offset: "UTC-4"},
	}
	root := PanelTree(zones, "", "", "")
	if err := v1.Validate(root, v1.ViewPanel); err != nil {
		t.Fatal(err)
	}
	var list *v1.Node
	walk(root, func(n *v1.Node) {
		if n.Kind == v1.KindList {
			list = n
		}
	})
	if list == nil || len(list.Children) != 5 {
		t.Fatalf("zone list = %+v, want gap/row/gap/row/gap", list)
	}
	for i, reading := range zones {
		gap, row := list.Children[2*i], list.Children[2*i+1]
		if gap.Kind != v1.KindDropZone || gap.ID != "drop:"+strconv.Itoa(i) || gap.Height != 3 || len(gap.Children) != 0 {
			t.Errorf("gap %d = %+v", i, gap)
		}
		if row.Kind != v1.KindRow || row.Key != "row:"+reading.Zone || row.Padding < 6 {
			t.Errorf("row %q = %+v, want padded zone row", reading.Zone, row)
			continue
		}
		if len(row.Children) == 0 || row.Children[0].Kind != v1.KindDragSource || row.Children[0].ID != "drag:"+reading.Zone || row.Children[0].Text != "" {
			t.Errorf("drag handle for %q = %+v, want unlabeled host-drawn handle", reading.Zone, row.Children)
		}
		for _, child := range row.Children {
			if child.Kind == v1.KindDropZone {
				t.Errorf("drop gap for %q is nested in its row", reading.Zone)
			}
		}
	}
	lastGap := list.Children[4]
	if lastGap.Kind != v1.KindDropZone || lastGap.ID != "drop:2" || lastGap.Height != 3 {
		t.Errorf("final gap = %+v", lastGap)
	}
}

func TestPanelTreeConfirmsRemovalInline(t *testing.T) {
	t.Parallel()
	root := PanelTree([]Reading{
		{Zone: "UTC", Clock: "15:04", Offset: "UTC+0"},
		{Zone: "Australia/Sydney", Clock: "01:04", Offset: "UTC+10"},
	}, "", "Australia/Sydney", "")
	if err := v1.Validate(root, v1.ViewPanel); err != nil {
		t.Fatal(err)
	}
	var row *v1.Node
	walk(root, func(n *v1.Node) {
		if n.Key == "row:Australia/Sydney" {
			row = n
		}
	})
	if row == nil || len(row.Children) < 2 {
		t.Fatalf("Sydney row = %+v", row)
	}
	confirm, cancel := row.Children[len(row.Children)-2], row.Children[len(row.Children)-1]
	if confirm.Kind != v1.KindButton || confirm.ID != "confirm-remove" || confirm.Tone != v1.ToneError || confirm.Name == "" {
		t.Errorf("inline confirmation = %+v, want accessible destructive button", confirm)
	}
	if cancel.Kind != v1.KindButton || cancel.ID != "cancel" {
		t.Errorf("inline cancel = %+v", cancel)
	}
	for _, child := range row.Children {
		if child.ID == "rm:Australia/Sydney" {
			t.Error("pending row still shows the ordinary remove control")
		}
	}
}

func TestPanelTreeKeepsAddControlOutsideAnEmptyList(t *testing.T) {
	t.Parallel()
	root := PanelTree(nil, "", "", "")
	if len(root.Children) != 2 || root.Children[0].Kind != v1.KindTextInput || root.Children[0].ID != "zone" {
		t.Fatalf("empty panel controls = %+v, want add field followed by list", root.Children)
	}
	list := root.Children[1]
	if list.Kind != v1.KindList || len(list.Children) != 1 {
		t.Fatalf("empty list = %+v, want one empty-state line", list)
	}
	hint := list.Children[0]
	if hint.Kind != v1.KindText || hint.Tone != v1.ToneSubtle || hint.Text == "" {
		t.Errorf("empty-state hint = %+v, want muted text", hint)
	}
}

func TestPanelTreeKeepsPendingAddControlsOutsideTheList(t *testing.T) {
	t.Parallel()
	root := PanelTree(nil, "Europe/Paris", "", "")
	if err := v1.Validate(root, v1.ViewPanel); err != nil {
		t.Fatal(err)
	}
	if len(root.Children) != 2 || root.Children[0].Kind != v1.KindRow {
		t.Fatalf("pending-add controls = %+v, want confirmation followed by list", root.Children)
	}
	confirmRow := root.Children[0]
	if len(confirmRow.Children) != 3 || confirmRow.Children[1].ID != "confirm-add" || confirmRow.Children[2].ID != "cancel" {
		t.Errorf("pending-add row = %+v", confirmRow)
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
