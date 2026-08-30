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

// Window is one Niri window. Only the fields the shell projects are decoded.
// Layout, focus timestamp, urgency and floating state have no consumer, so an
// event carrying them decodes and ignores them.
type Window struct {
	ID    uint64
	Title string
	AppID string
	// WorkspaceID is meaningful only when HasWorkspace is set. Niri sends
	// workspace_id as null for a window on no workspace, which is distinct
	// from workspace zero.
	WorkspaceID  uint64
	HasWorkspace bool
}

// Snapshot is an immutable view of workspace and window state.
type Snapshot struct {
	Workspaces []Workspace
	Windows    []Window
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

// wireWindow decodes one window. Only id is required; Niri sends title,
// app_id and workspace_id as null in legitimate states.
type wireWindow struct {
	ID          *uint64 `json:"id"`
	Title       *string `json:"title"`
	AppID       *string `json:"app_id"`
	WorkspaceID *uint64 `json:"workspace_id"`
}

func (w wireWindow) project() (Window, error) {
	if w.ID == nil {
		return Window{}, fmt.Errorf("niri: window is missing id")
	}
	out := Window{ID: *w.ID}
	if w.Title != nil {
		out.Title = *w.Title
	}
	if w.AppID != nil {
		out.AppID = *w.AppID
	}
	if w.WorkspaceID != nil {
		out.WorkspaceID, out.HasWorkspace = *w.WorkspaceID, true
	}
	return out, nil
}

type wireWindowsChanged struct {
	Windows []wireWindow `json:"windows"`
}

type wireWindowOpenedOrChanged struct {
	Window wireWindow `json:"window"`
}

type wireWindowClosed struct {
	ID *uint64 `json:"id"`
}

// state accumulates workspace and window events into snapshots.
type state struct {
	workspaces []Workspace
	windows    []Window
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

	if payload, ok := envelope["WindowsChanged"]; ok {
		var changed wireWindowsChanged
		if err := json.Unmarshal(payload, &changed); err != nil {
			return false, fmt.Errorf("niri: decode WindowsChanged: %w", err)
		}
		// Build the whole set before replacing state, so a malformed member
		// cannot publish a partial snapshot.
		next := make([]Window, 0, len(changed.Windows))
		for _, w := range changed.Windows {
			projected, err := w.project()
			if err != nil {
				return false, err
			}
			next = append(next, projected)
		}
		s.windows = next
		return true, nil
	}

	if payload, ok := envelope["WindowOpenedOrChanged"]; ok {
		var opened wireWindowOpenedOrChanged
		if err := json.Unmarshal(payload, &opened); err != nil {
			return false, fmt.Errorf("niri: decode WindowOpenedOrChanged: %w", err)
		}
		projected, err := opened.Window.project()
		if err != nil {
			return false, err
		}
		if i := slices.IndexFunc(s.windows, func(w Window) bool { return w.ID == projected.ID }); i >= 0 {
			s.windows[i] = projected
		} else {
			s.windows = append(s.windows, projected)
		}
		return true, nil
	}

	if payload, ok := envelope["WindowClosed"]; ok {
		var closed wireWindowClosed
		if err := json.Unmarshal(payload, &closed); err != nil {
			return false, fmt.Errorf("niri: decode WindowClosed: %w", err)
		}
		if closed.ID == nil {
			return false, fmt.Errorf("niri: WindowClosed is missing id")
		}
		i := slices.IndexFunc(s.windows, func(w Window) bool { return w.ID == *closed.ID })
		if i < 0 {
			// The desired post-state — this window absent — already holds.
			// There is nothing to diverge from, so this is not an error.
			return false, nil
		}
		s.windows = slices.Delete(s.windows, i, i+1)
		return true, nil
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

	windows := slices.Clone(s.windows)
	slices.SortFunc(windows, func(a, b Window) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		}
		return 0
	})

	snap := Snapshot{Workspaces: workspaces, Windows: windows}
	for _, w := range workspaces {
		if w.Focused {
			snap.FocusedOutput = w.Output
			break
		}
	}
	return snap
}
