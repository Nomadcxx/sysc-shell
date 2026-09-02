package recorder

import v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"

const (
	nodeRecord = "record"
)

func BarTree(snap Snapshot, cfg Config) *v1.Node {
	if snap.Mode == Idle && cfg.HideInactive {
		return &v1.Node{Kind: v1.KindRow}
	}
	label, tone := presentation(snap)
	return &v1.Node{Kind: v1.KindRow, Gap: 8, Children: []*v1.Node{
		{
			Kind: v1.KindButton, ID: nodeRecord, Key: "status",
			Text: label, Name: label, Role: "button", Tone: tone,
			Events: []v1.EventKind{v1.EventActivate, v1.EventPointer},
		},
	}}
}

func TooltipTree(snap Snapshot) *v1.Node {
	label, _ := presentation(snap)
	children := []*v1.Node{{Kind: v1.KindText, Text: label}}
	if snap.Artifact != "" {
		children = append(children, &v1.Node{Kind: v1.KindText, Text: snap.Artifact})
	}
	if snap.Err != "" {
		children = append(children, &v1.Node{Kind: v1.KindText, Text: snap.Err, Tone: v1.ToneError})
	}
	if snap.Logs != "" {
		children = append(children, &v1.Node{Kind: v1.KindText, Text: snap.Logs})
	}
	return &v1.Node{Kind: v1.KindColumn, Gap: 4, Children: children}
}

func presentation(snap Snapshot) (string, v1.Tone) {
	switch snap.Mode {
	case Unavailable:
		return "Recorder unavailable", v1.ToneError
	case Recording, Adopted:
		return "Recording", v1.ToneNormal
	case ReplayActive:
		return "Replay", v1.ToneNormal
	case Stopping:
		return "Stopping", v1.ToneNormal
	case Failed:
		return "Recorder failed", v1.ToneError
	default:
		return "Record", v1.ToneNormal
	}
}

func HandleInput(ev *v1.InputEvent) (record, replay, save bool) {
	if ev == nil || ev.Node != nodeRecord {
		return false, false, false
	}
	if ev.Event == v1.EventActivate {
		return true, false, false
	}
	if ev.Event != v1.EventPointer {
		return false, false, false
	}
	switch ev.Button {
	case v1.ButtonPrimary:
		return true, false, false
	case v1.ButtonSecondary:
		return false, true, false
	case v1.ButtonMiddle:
		return false, false, true
	}
	return false, false, false
}
