package shell

import (
	"slices"
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
	// Pills are this output's workspaces in index order. Workspace stays the
	// focused label: it is what a text-only workspace widget renders and what
	// the held-state tests assert.
	Pills []workspacePill
}

// workspacePill is one workspace as the bar draws it. Occupied and Focused are
// separate because a focused workspace can be empty.
type workspacePill struct {
	Index    int
	Occupied bool
	Focused  bool
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
	populated := make(map[uint64]bool, len(s.Windows))
	for _, window := range s.Windows {
		titles[window.ID] = window.Title
		if window.HasWorkspace {
			populated[window.WorkspaceID] = true
		}
	}

	// Every workspace on an output gets a pill, not just the chosen one, so a
	// bar shows the whole set and marks which is focused.
	pills := make(map[string][]workspacePill, len(chosen))
	for _, w := range s.Workspaces {
		if w.Output == "" {
			continue
		}
		pills[w.Output] = append(pills[w.Output], workspacePill{
			Index:    w.Index,
			Occupied: w.HasActiveWindow || populated[w.ID],
			Focused:  w.Focused || w.Active,
		})
	}
	for connector := range pills {
		slices.SortFunc(pills[connector], func(a, b workspacePill) int { return a.Index - b.Index })
	}

	out := make(map[string]outputState, len(chosen))
	for connector, w := range chosen {
		label := w.Name
		if label == "" {
			label = strconv.Itoa(w.Index)
		}
		state := outputState{Workspace: label, Pills: pills[connector]}
		if w.HasActiveWindow {
			// A missing entry means the window closed and the follow-up event
			// has not arrived. Empty is correct; stale would be wrong.
			state.Title = titles[w.ActiveWindowID]
		}
		out[connector] = state
	}
	// An output whose workspaces are all inactive has no chosen entry above,
	// but it still has pills to draw.
	for connector, p := range pills {
		if _, ok := out[connector]; !ok {
			out[connector] = outputState{Workspace: noWorkspace, Pills: p}
		}
	}
	return out
}
