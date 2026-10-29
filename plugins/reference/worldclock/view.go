package worldclock

import (
	"strconv"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func BarTree(first Reading) *v1.Node {
	return barReadout(first.Clock)
}

func barReadout(clock string) *v1.Node {
	return &v1.Node{
		Kind: v1.KindButton, ID: "open", Key: "time", Text: clock, Tabular: true, Name: "Open world clock", Role: "button",
		Events: []v1.EventKind{v1.EventActivate},
	}
}

func BarTimePatch(first Reading) []v1.Replacement {
	return []v1.Replacement{{
		Key:  "time",
		Node: barReadout(first.Clock),
	}}
}

func TimePatch(readings []Reading) []v1.Replacement {
	out := make([]v1.Replacement, 0, len(readings)*2)
	for _, r := range readings {
		timeKey := "time:" + r.Zone
		out = append(out, v1.Replacement{
			Key:  timeKey,
			Node: &v1.Node{Kind: v1.KindText, Key: timeKey, Text: r.Clock, Tabular: true},
		})
		offsetKey := "offset:" + r.Zone
		out = append(out, v1.Replacement{
			Key:  offsetKey,
			Node: &v1.Node{Kind: v1.KindText, Key: offsetKey, Text: r.Offset},
		})
	}
	return out
}

func PanelTree(readings []Reading, pendingAdd, pendingRemove, draft string) *v1.Node {
	var addControl *v1.Node
	switch {
	case pendingAdd != "":
		addControl = &v1.Node{Kind: v1.KindRow, Gap: 8, Children: []*v1.Node{
			{Kind: v1.KindText, Text: "Add " + pendingAdd + "?"},
			{Kind: v1.KindButton, ID: "confirm-add", Text: "Add", Name: "Confirm add", Role: "button", Events: []v1.EventKind{v1.EventActivate}},
			{Kind: v1.KindButton, ID: "cancel", Text: "Cancel", Name: "Cancel", Role: "button", Events: []v1.EventKind{v1.EventActivate}},
		}}
	default:
		addControl = &v1.Node{Kind: v1.KindTextInput, ID: "zone", Text: draft, Name: "Add a city", Role: "textbox",
			Events: []v1.EventKind{v1.EventChange, v1.EventSubmit}}
	}

	rows := make([]*v1.Node, 0, 2*len(readings)+1)
	if len(readings) == 0 {
		rows = append(rows, &v1.Node{Kind: v1.KindText, Text: "No cities", Tone: v1.ToneSubtle})
	} else {
		for i, r := range readings {
			rows = append(rows, dropZone(i))
			children := []*v1.Node{
				{
					Kind: v1.KindDragSource, ID: "drag:" + r.Zone, Key: "drag:" + r.Zone,
					Name: "Reorder " + r.Zone, Role: "button", DragType: "zone", Payload: r.Zone,
					Events: []v1.EventKind{v1.EventPointer},
				},
				{Kind: v1.KindText, Text: r.Zone},
				{Kind: v1.KindText, Key: "time:" + r.Zone, Text: r.Clock, Tabular: true},
				{Kind: v1.KindText, Key: "offset:" + r.Zone, Text: r.Offset},
			}
			if pendingRemove == r.Zone {
				children = append(children,
					&v1.Node{Kind: v1.KindButton, ID: "confirm-remove", Text: "Remove", Name: "Confirm remove " + r.Zone, Role: "button", Tone: v1.ToneError, Events: []v1.EventKind{v1.EventActivate}},
					&v1.Node{Kind: v1.KindButton, ID: "cancel", Text: "Cancel", Name: "Cancel remove " + r.Zone, Role: "button", Events: []v1.EventKind{v1.EventActivate}},
				)
			} else {
				children = append(children, &v1.Node{
					Kind: v1.KindButton, ID: "rm:" + r.Zone, Text: "Remove", Name: "Remove " + r.Zone, Role: "button",
					Events: []v1.EventKind{v1.EventActivate},
				})
			}
			rows = append(rows, &v1.Node{Kind: v1.KindRow, Gap: 8, Padding: 8, Key: "row:" + r.Zone, Children: children})
		}
		rows = append(rows, dropZone(len(readings)))
	}
	return &v1.Node{Kind: v1.KindColumn, Gap: 8, Children: []*v1.Node{
		addControl,
		{Kind: v1.KindList, Height: 240, Children: rows},
	}}
}

func dropZone(index int) *v1.Node {
	return &v1.Node{
		Kind: v1.KindDropZone, ID: "drop:" + strconv.Itoa(index), Height: 3, Accept: []string{"zone"},
		Events: []v1.EventKind{v1.EventDrop},
	}
}
