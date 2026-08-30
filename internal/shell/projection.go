package shell

import (
	"strconv"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

// outputState is the Niri-derived text for one connector.
//
// A missing workspace renders "-" and a missing window renders empty. Both are
// stable: an empty text node measures zero-wide, so a section with no window
// simply shrinks rather than reserving space.
type outputState struct {
	Workspace string
	Title     string
}

// noWorkspace is the label shown before the first snapshot arrives, or for a
// connector Niri has not reported.
const noWorkspace = "-"

// projectOutputs reduces one snapshot to per-connector text.
//
// The title join is output → that output's active workspace → its
// active_window_id → that window. Niri's globally focused window is
// deliberately not consulted: each bar reports what is active on its own
// monitor, so moving focus to another output does not blank this one.
func projectOutputs(s niri.Snapshot) map[string]outputState {
	// A focused workspace outranks a merely active one on the same output.
	chosen := make(map[string]niri.Workspace, len(s.Workspaces))
	focused := make(map[string]bool, len(s.Workspaces))
	for _, w := range s.Workspaces {
		if w.Output == "" || !(w.Focused || w.Active) {
			continue
		}
		if focused[w.Output] && !w.Focused {
			continue
		}
		chosen[w.Output] = w
		if w.Focused {
			focused[w.Output] = true
		}
	}

	titles := make(map[uint64]string, len(s.Windows))
	for _, window := range s.Windows {
		titles[window.ID] = window.Title
	}

	out := make(map[string]outputState, len(chosen))
	for connector, w := range chosen {
		label := w.Name
		if label == "" {
			label = strconv.Itoa(w.Index)
		}
		state := outputState{Workspace: label}
		if w.HasActiveWindow {
			// A missing entry means the window closed and the follow-up event
			// has not arrived. Empty is correct; stale would be wrong.
			state.Title = titles[w.ActiveWindowID]
		}
		out[connector] = state
	}
	return out
}
