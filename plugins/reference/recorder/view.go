package recorder

import v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"

const (
	nodeCamera = "camera"
	nodeRecord = "record"
	nodeStop   = "stop"
)

func BarTree(snap Snapshot, cfg Config) *v1.Node {
	children := []*v1.Node{cameraButton(snap)}
	if !(snap.Mode == Idle && cfg.HideInactive) {
		children = append(children,
			&v1.Node{
				Kind: v1.KindButton, ID: nodeRecord, Key: nodeRecord,
				Text: "Record", Name: "Record", Role: "button",
				Events: []v1.EventKind{v1.EventActivate},
			},
			&v1.Node{
				Kind: v1.KindButton, ID: nodeStop, Key: nodeStop,
				Text: "Stop", Name: "Stop", Role: "button",
				Events: []v1.EventKind{v1.EventActivate},
			},
		)
	}
	return &v1.Node{Kind: v1.KindRow, Gap: 8, Children: children}
}

func cameraButton(snap Snapshot) *v1.Node {
	icon, tone := cameraPresentation(snap)
	return &v1.Node{
		Kind: v1.KindButton, ID: nodeCamera, Key: nodeCamera,
		Icon: icon, Name: "Open screen recorder", Role: "button", Tone: tone,
		Events: []v1.EventKind{v1.EventActivate},
	}
}

func cameraPresentation(snap Snapshot) (string, v1.Tone) {
	switch snap.Mode {
	case Unavailable, Failed:
		return "camera-off", v1.ToneError
	case Recording, Adopted:
		return "camera", v1.ToneError
	case ReplayActive:
		return "replay", v1.ToneNormal
	default:
		return "camera", v1.ToneNormal
	}
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

func HandleInput(ev *v1.InputEvent, mode Mode) (open, record, stop, replay, save bool) {
	if ev == nil || ev.Event != v1.EventActivate {
		return false, false, false, false, false
	}
	switch ev.Node {
	case nodeCamera:
		return true, false, false, false, false
	case nodeRecord:
		if mode == Idle {
			return false, true, false, false, false
		}
	case nodeStop:
		switch mode {
		case Recording, Adopted, Stopping:
			return false, false, true, false, false
		}
	}
	return false, false, false, false, false
}
