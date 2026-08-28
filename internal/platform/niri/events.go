// Package niri streams typed workspace state from the compositor's JSON IPC
// socket. It connects to $NIRI_SOCKET directly and never shells out to
// `niri msg`.
//
// Niri emits no output event. Workspaces carry an output name only; output
// existence, identity, scale, mode, transform and hotplug all come from
// Wayland, so this package never projects output state.
package niri

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Workspace is one Niri workspace.
type Workspace struct {
	ID      uint64
	Index   int
	Name    string
	Output  string
	Active  bool
	Focused bool
}

// Snapshot is an immutable view of workspace state.
type Snapshot struct {
	Workspaces []Workspace
	// FocusedOutput is derived from the workspace whose is_focused is true.
	// Niri has no dedicated event or field for it.
	FocusedOutput string
}

// wireWorkspace decodes one workspace. Pointer fields distinguish a missing
// field from its zero value; Niri may add fields, which are ignored.
type wireWorkspace struct {
	ID        *uint64 `json:"id"`
	Index     *int    `json:"idx"`
	Name      *string `json:"name"`
	Output    *string `json:"output"`
	IsActive  *bool   `json:"is_active"`
	IsFocused *bool   `json:"is_focused"`
}

// project validates the required fields and maps nullable ones to empty
// strings.
func (w wireWorkspace) project() (Workspace, error) {
	switch {
	case w.ID == nil:
		return Workspace{}, fmt.Errorf("niri: workspace is missing id")
	case w.Index == nil:
		return Workspace{}, fmt.Errorf("niri: workspace %d is missing idx", *w.ID)
	case w.IsActive == nil:
		return Workspace{}, fmt.Errorf("niri: workspace %d is missing is_active", *w.ID)
	case w.IsFocused == nil:
		return Workspace{}, fmt.Errorf("niri: workspace %d is missing is_focused", *w.ID)
	}

	out := Workspace{ID: *w.ID, Index: *w.Index, Active: *w.IsActive, Focused: *w.IsFocused}
	if w.Name != nil {
		out.Name = *w.Name
	}
	if w.Output != nil {
		out.Output = *w.Output
	}
	return out, nil
}

type wireWorkspacesChanged struct {
	Workspaces []wireWorkspace `json:"workspaces"`
}

type wireWorkspaceActivated struct {
	ID      *uint64 `json:"id"`
	Focused *bool   `json:"focused"`
}

// state accumulates workspace events into snapshots.
type state struct {
	workspaces []Workspace
}

// apply decodes one event line. It reports whether a new snapshot should be
// published. Unknown top-level events are ignored; a malformed known event is
// an error and leaves the state untouched.
func (s *state) apply(line []byte) (bool, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(line, &envelope); err != nil {
		return false, fmt.Errorf("niri: decode event: %w", err)
	}

	if payload, ok := envelope["WorkspacesChanged"]; ok {
		var changed wireWorkspacesChanged
		if err := json.Unmarshal(payload, &changed); err != nil {
			return false, fmt.Errorf("niri: decode WorkspacesChanged: %w", err)
		}
		// Build the whole set before replacing state, so a malformed member
		// cannot publish a partial snapshot.
		next := make([]Workspace, 0, len(changed.Workspaces))
		for _, w := range changed.Workspaces {
			projected, err := w.project()
			if err != nil {
				return false, err
			}
			next = append(next, projected)
		}
		s.workspaces = next
		return true, nil
	}

	if payload, ok := envelope["WorkspaceActivated"]; ok {
		var activated wireWorkspaceActivated
		if err := json.Unmarshal(payload, &activated); err != nil {
			return false, fmt.Errorf("niri: decode WorkspaceActivated: %w", err)
		}
		if activated.ID == nil || activated.Focused == nil {
			return false, fmt.Errorf("niri: WorkspaceActivated is missing id or focused")
		}
		return true, s.activate(*activated.ID, *activated.Focused)
	}

	return false, nil
}

// activate makes one workspace active on its output, and focused across all
// outputs when the event says so.
func (s *state) activate(id uint64, focused bool) error {
	target := slices.IndexFunc(s.workspaces, func(w Workspace) bool { return w.ID == id })
	if target < 0 {
		// WorkspacesChanged always precedes activation, so an unknown id means
		// the stream and this state have diverged.
		return fmt.Errorf("niri: WorkspaceActivated names unknown workspace %d", id)
	}

	output := s.workspaces[target].Output
	for i := range s.workspaces {
		if s.workspaces[i].Output == output {
			s.workspaces[i].Active = false
		}
		if focused {
			s.workspaces[i].Focused = false
		}
	}
	s.workspaces[target].Active = true
	s.workspaces[target].Focused = focused
	return nil
}

// snapshot copies the state into a stable, immutable view.
func (s *state) snapshot() Snapshot {
	workspaces := slices.Clone(s.workspaces)
	slices.SortFunc(workspaces, func(a, b Workspace) int {
		if c := strings.Compare(a.Output, b.Output); c != 0 {
			return c
		}
		if c := a.Index - b.Index; c != 0 {
			return c
		}
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		}
		return 0
	})

	snap := Snapshot{Workspaces: workspaces}
	for _, w := range workspaces {
		if w.Focused {
			snap.FocusedOutput = w.Output
			break
		}
	}
	return snap
}
