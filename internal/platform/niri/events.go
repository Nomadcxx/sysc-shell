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
	// Urgent mirrors Niri's is_urgent: the workspace holds a window that
	// demanded attention. It is optional on the wire and false when absent.
	Urgent bool
	// ActiveWindowID is meaningful only when HasActiveWindow is set. It is the
	// workspace's own active window, which is what makes a per-output focused
	// title possible; Niri's global focus is a separate concept the shell does
	// not project.
	ActiveWindowID  uint64
	HasActiveWindow bool
}

// Window is one Niri window. Only the fields the shell projects are decoded.
// Tile geometry comes from the compositor layout, so a window that fills its
// output can hold the wallpaper paused behind it.
type Window struct {
	ID    uint64
	Title string
	AppID string
	// WorkspaceID is meaningful only when HasWorkspace is set. Niri sends
	// workspace_id as null for a window on no workspace, which is distinct
	// from workspace zero.
	WorkspaceID    uint64
	HasWorkspace   bool
	Focused        bool
	FocusTimestamp int64 // monotonic ns; 0 when null or omitted
	Pid            int   // 0 when Niri sends null
	// Floating reports is_floating. A floating window carries a view
	// position on every Niri release, so the evaluator judges it by
	// geometry like any other tile.
	Floating bool
	// HasLayout and the tile rectangle are set from the compositor layout.
	// A window without a layout cannot cover anything.
	HasLayout bool
	// TileX, TileY, TileW and TileH are the tile rectangle in workspace
	// view coordinates, from tile_pos_in_workspace_view and tile_size.
	// The size is always set when HasLayout is; the position is not.
	TileX, TileY float64
	TileW, TileH float64
	// TileInView reports that the tile position was present. Niri 26.04
	// sends none for tiled windows, fullscreen included, so its absence
	// does not mean the window is outside the workspace view: the
	// wallpaper evaluator falls back to the tile size there.
	TileInView bool
}

// KeyboardLayouts is niri's configured keyboard layout list and active index.
type KeyboardLayouts struct {
	Names   []string
	Current int
}

// Cast is a Niri screencast. TargetKnown distinguishes a recognised target
// from a future or malformed one; TargetSet is false for the explicit Nothing
// target. Unknown targets never imply that screen sharing is inactive.
type Cast struct {
	StreamID    uint64
	Active      bool
	Target      CastTarget
	TargetSet   bool
	TargetKnown bool
	PWNodeID    uint32
	HasPWNodeID bool
}

// CastTarget is Niri's selected cast destination. Kind is "nothing", "output",
// "window", or "unknown".
type CastTarget struct {
	Kind     string
	Name     string
	WindowID uint64
}

// Snapshot is an immutable view of workspace, window, and keyboard state.
type Snapshot struct {
	Workspaces   []Workspace
	Windows      []Window
	Layouts      KeyboardLayouts
	Casts        []Cast
	CastsKnown   bool
	OverviewOpen bool
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
	IsUrgent  *bool   `json:"is_urgent"`

	ActiveWindowID *uint64 `json:"active_window_id"`
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
	if w.ActiveWindowID != nil {
		out.ActiveWindowID, out.HasActiveWindow = *w.ActiveWindowID, true
	}
	if w.IsUrgent != nil {
		out.Urgent = *w.IsUrgent
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

type wireWorkspaceActiveWindowChanged struct {
	WorkspaceID    *uint64 `json:"workspace_id"`
	ActiveWindowID *uint64 `json:"active_window_id"`
}

// wireWindow decodes one window. Only id is required; Niri sends title,
// app_id and workspace_id as null in legitimate states.
type wireWindow struct {
	ID             *uint64 `json:"id"`
	Title          *string `json:"title"`
	AppID          *string `json:"app_id"`
	WorkspaceID    *uint64 `json:"workspace_id"`
	IsFocused      *bool   `json:"is_focused"`
	Pid            *int    `json:"pid"`
	FocusTimestamp *struct {
		Secs  uint64 `json:"secs"`
		Nanos uint64 `json:"nanos"`
	} `json:"focus_timestamp"`
	IsFloating *bool             `json:"is_floating"`
	Layout     *wireWindowLayout `json:"layout"`
}

// wireWindowLayout decodes the slice of the compositor layout the shell
// consumes: where the window sits and how large its tile is.
type wireWindowLayout struct {
	TileSize *[2]float64 `json:"tile_size"`
	TilePos  *[2]float64 `json:"tile_pos_in_workspace_view"`
}

// applyWindowLayout copies a decoded layout onto a projected window. A null
// layout, or one without a tile size, clears the rectangle.
func applyWindowLayout(out *Window, layout *wireWindowLayout) {
	out.HasLayout, out.TileInView = false, false
	out.TileX, out.TileY, out.TileW, out.TileH = 0, 0, 0, 0
	if layout == nil || layout.TileSize == nil {
		return
	}
	out.HasLayout = true
	out.TileW, out.TileH = layout.TileSize[0], layout.TileSize[1]
	if layout.TilePos != nil {
		out.TileX, out.TileY = layout.TilePos[0], layout.TilePos[1]
		out.TileInView = true
	}
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
	if w.IsFocused != nil {
		out.Focused = *w.IsFocused
	}
	if w.Pid != nil && *w.Pid > 0 {
		out.Pid = *w.Pid
	}
	if w.FocusTimestamp != nil {
		out.FocusTimestamp = int64(w.FocusTimestamp.Secs)*1e9 + int64(w.FocusTimestamp.Nanos)
	}
	if w.IsFloating != nil {
		out.Floating = *w.IsFloating
	}
	applyWindowLayout(&out, w.Layout)
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

// wireWindowLayoutChange is one [id, layout] pair from WindowLayoutsChanged.
// The layout entry is null when the compositor takes the window out of its
// layout.
type wireWindowLayoutChange struct {
	ID     uint64
	Layout *wireWindowLayout
}

func (c *wireWindowLayoutChange) UnmarshalJSON(data []byte) error {
	var pair []json.RawMessage
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
	}
	if len(pair) != 2 {
		return fmt.Errorf("niri: WindowLayoutsChanged change has %d entries, want 2", len(pair))
	}
	if err := json.Unmarshal(pair[0], &c.ID); err != nil {
		return err
	}
	if string(pair[1]) == "null" {
		return nil
	}
	var layout wireWindowLayout
	if err := json.Unmarshal(pair[1], &layout); err != nil {
		return err
	}
	c.Layout = &layout
	return nil
}

type wireWindowLayoutsChanged struct {
	Changes []wireWindowLayoutChange `json:"changes"`
}

type wireKeyboardLayoutsChanged struct {
	KeyboardLayouts struct {
		Names      []string `json:"names"`
		CurrentIdx *int     `json:"current_idx"`
	} `json:"keyboard_layouts"`
}

type wireKeyboardLayoutSwitched struct {
	Idx *int `json:"idx"`
}

type wireOverviewOpenedOrClosed struct {
	IsOpen *bool `json:"is_open"`
}

type wireCast struct {
	StreamID *uint64         `json:"stream_id"`
	IsActive *bool           `json:"is_active"`
	Target   json.RawMessage `json:"target"`
	PWNodeID *uint32         `json:"pw_node_id"`
}

func (c wireCast) project() (Cast, error) {
	if c.StreamID == nil {
		return Cast{}, fmt.Errorf("niri: cast is missing stream_id")
	}
	if c.IsActive == nil {
		return Cast{}, fmt.Errorf("niri: cast %d is missing is_active", *c.StreamID)
	}
	out := Cast{StreamID: *c.StreamID, Active: *c.IsActive}
	out.Target, out.TargetSet, out.TargetKnown = projectCastTarget(c.Target)
	if c.PWNodeID != nil && *c.PWNodeID > 0 {
		out.PWNodeID, out.HasPWNodeID = *c.PWNodeID, true
	}
	return out, nil
}

// projectCastTarget handles Niri's externally tagged CastTarget enum. Unknown
// variants from a newer compositor stay representable as unknown state.

func projectCastTarget(data json.RawMessage) (target CastTarget, set, known bool) {
	unknown := CastTarget{Kind: "unknown"}
	if len(data) == 0 || string(data) == "null" {
		return unknown, false, false
	}
	var variants map[string]json.RawMessage
	if err := json.Unmarshal(data, &variants); err != nil || len(variants) != 1 {
		return unknown, false, false
	}
	for kind, payload := range variants {
		switch kind {
		case "Nothing":
			var empty map[string]json.RawMessage
			if err := json.Unmarshal(payload, &empty); err != nil || empty == nil || len(empty) != 0 {
				return unknown, false, false
			}
			return CastTarget{Kind: "nothing"}, false, true
		case "Output":
			var output struct {
				Name *string `json:"name"`
			}
			if err := json.Unmarshal(payload, &output); err != nil || output.Name == nil {
				return unknown, false, false
			}
			if strings.TrimSpace(*output.Name) == "" {
				return unknown, false, false
			}
			return CastTarget{Kind: "output", Name: *output.Name}, true, true
		case "Window":
			var window struct {
				ID *uint64 `json:"id"`
			}
			if err := json.Unmarshal(payload, &window); err != nil || window.ID == nil || *window.ID == 0 {
				return unknown, false, false
			}
			return CastTarget{Kind: "window", WindowID: *window.ID}, true, true
		default:
			return unknown, false, false
		}
	}
	return unknown, false, false
}

type wireCastsChanged struct {
	Casts *[]wireCast `json:"casts"`
}

type wireCastStartedOrChanged struct {
	Cast *wireCast `json:"cast"`
}

type wireCastStopped struct {
	StreamID *uint64 `json:"stream_id"`
}

// state accumulates workspace and window events into snapshots.
//
// last is the most recently published snapshot. Comparing against it is what
// keeps an event that changes no projected field from waking the shell.
type state struct {
	workspaces   []Workspace
	windows      []Window
	layouts      KeyboardLayouts
	casts        map[uint64]Cast
	castsKnown   bool
	overviewOpen bool
	last         Snapshot
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
		return s.publishIfChanged(), nil
	}

	if payload, ok := envelope["WorkspaceActivated"]; ok {
		var activated wireWorkspaceActivated
		if err := json.Unmarshal(payload, &activated); err != nil {
			return false, fmt.Errorf("niri: decode WorkspaceActivated: %w", err)
		}
		if activated.ID == nil || activated.Focused == nil {
			return false, fmt.Errorf("niri: WorkspaceActivated is missing id or focused")
		}
		if err := s.activate(*activated.ID, *activated.Focused); err != nil {
			return false, err
		}
		return s.publishIfChanged(), nil
	}

	if payload, ok := envelope["WorkspaceActiveWindowChanged"]; ok {
		var changed wireWorkspaceActiveWindowChanged
		if err := json.Unmarshal(payload, &changed); err != nil {
			return false, fmt.Errorf("niri: decode WorkspaceActiveWindowChanged: %w", err)
		}
		if changed.WorkspaceID == nil {
			return false, fmt.Errorf("niri: WorkspaceActiveWindowChanged is missing workspace_id")
		}
		i := slices.IndexFunc(s.workspaces, func(w Workspace) bool { return w.ID == *changed.WorkspaceID })
		if i < 0 {
			// WorkspacesChanged always precedes this event, so an unknown id
			// means the stream and this state have diverged. There is nowhere
			// to record the active window, so the title would go stale
			// silently.
			return false, fmt.Errorf(
				"niri: WorkspaceActiveWindowChanged names unknown workspace %d", *changed.WorkspaceID)
		}
		if changed.ActiveWindowID != nil {
			s.workspaces[i].ActiveWindowID = *changed.ActiveWindowID
			s.workspaces[i].HasActiveWindow = true
		} else {
			s.workspaces[i].ActiveWindowID, s.workspaces[i].HasActiveWindow = 0, false
		}
		return s.publishIfChanged(), nil
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
		return s.publishIfChanged(), nil
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
		return s.publishIfChanged(), nil
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
		return s.publishIfChanged(), nil
	}

	if payload, ok := envelope["WindowLayoutsChanged"]; ok {
		var changed wireWindowLayoutsChanged
		if err := json.Unmarshal(payload, &changed); err != nil {
			return false, fmt.Errorf("niri: decode WindowLayoutsChanged: %w", err)
		}
		for _, change := range changed.Changes {
			i := slices.IndexFunc(s.windows, func(w Window) bool { return w.ID == change.ID })
			if i < 0 {
				// A window the snapshot never saw has nothing to update.
				continue
			}
			applyWindowLayout(&s.windows[i], change.Layout)
		}
		return s.publishIfChanged(), nil
	}

	if payload, ok := envelope["WindowFocusChanged"]; ok {
		var changed struct {
			ID *uint64 `json:"id"`
		}
		if err := json.Unmarshal(payload, &changed); err != nil {
			return false, fmt.Errorf("niri: decode WindowFocusChanged: %w", err)
		}
		if changed.ID == nil {
			for i := range s.windows {
				s.windows[i].Focused = false
			}
			return s.publishIfChanged(), nil
		}
		for i := range s.windows {
			s.windows[i].Focused = s.windows[i].ID == *changed.ID
		}
		return s.publishIfChanged(), nil
	}

	if payload, ok := envelope["KeyboardLayoutsChanged"]; ok {
		var changed wireKeyboardLayoutsChanged
		if err := json.Unmarshal(payload, &changed); err != nil {
			return false, fmt.Errorf("niri: decode KeyboardLayoutsChanged: %w", err)
		}
		if changed.KeyboardLayouts.CurrentIdx == nil {
			return false, fmt.Errorf("niri: KeyboardLayoutsChanged is missing current_idx")
		}
		s.layouts = KeyboardLayouts{
			Names:   slices.Clone(changed.KeyboardLayouts.Names),
			Current: *changed.KeyboardLayouts.CurrentIdx,
		}
		return s.publishIfChanged(), nil
	}

	if payload, ok := envelope["KeyboardLayoutSwitched"]; ok {
		var switched wireKeyboardLayoutSwitched
		if err := json.Unmarshal(payload, &switched); err != nil {
			return false, fmt.Errorf("niri: decode KeyboardLayoutSwitched: %w", err)
		}
		if switched.Idx == nil {
			return false, fmt.Errorf("niri: KeyboardLayoutSwitched is missing idx")
		}
		s.layouts.Current = *switched.Idx
		return s.publishIfChanged(), nil
	}

	if payload, ok := envelope["OverviewOpenedOrClosed"]; ok {
		var changed wireOverviewOpenedOrClosed
		if err := json.Unmarshal(payload, &changed); err != nil {
			return false, fmt.Errorf("niri: decode OverviewOpenedOrClosed: %w", err)
		}
		if changed.IsOpen == nil {
			return false, fmt.Errorf("niri: OverviewOpenedOrClosed is missing is_open")
		}
		s.overviewOpen = *changed.IsOpen
		return s.publishIfChanged(), nil
	}

	if payload, ok := envelope["CastsChanged"]; ok {
		var changed wireCastsChanged
		if err := json.Unmarshal(payload, &changed); err != nil {
			return false, fmt.Errorf("niri: decode CastsChanged: %w", err)
		}
		if changed.Casts == nil {
			return false, fmt.Errorf("niri: CastsChanged is missing casts")
		}
		next := make(map[uint64]Cast, len(*changed.Casts))
		for _, wire := range *changed.Casts {
			cast, err := wire.project()
			if err != nil {
				return false, err
			}
			if _, exists := next[cast.StreamID]; exists {
				return false, fmt.Errorf("niri: CastsChanged repeats stream_id %d", cast.StreamID)
			}
			next[cast.StreamID] = cast
		}
		s.casts, s.castsKnown = next, true
		return s.publishIfChanged(), nil
	}

	if payload, ok := envelope["CastStartedOrChanged"]; ok {
		var changed wireCastStartedOrChanged
		if err := json.Unmarshal(payload, &changed); err != nil {
			return false, fmt.Errorf("niri: decode CastStartedOrChanged: %w", err)
		}
		if changed.Cast == nil {
			return false, fmt.Errorf("niri: CastStartedOrChanged is missing cast")
		}
		cast, err := changed.Cast.project()
		if err != nil {
			return false, err
		}
		if s.casts == nil {
			s.casts = make(map[uint64]Cast)
		}
		s.casts[cast.StreamID] = cast
		return s.publishIfChanged(), nil
	}

	if payload, ok := envelope["CastStopped"]; ok {
		var stopped wireCastStopped
		if err := json.Unmarshal(payload, &stopped); err != nil {
			return false, fmt.Errorf("niri: decode CastStopped: %w", err)
		}
		if stopped.StreamID == nil {
			return false, fmt.Errorf("niri: CastStopped is missing stream_id")
		}
		delete(s.casts, *stopped.StreamID)
		return s.publishIfChanged(), nil
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

	casts := make([]Cast, 0, len(s.casts))
	for _, cast := range s.casts {
		casts = append(casts, cast)
	}
	slices.SortFunc(casts, func(a, b Cast) int {
		switch {
		case a.StreamID < b.StreamID:
			return -1
		case a.StreamID > b.StreamID:
			return 1
		default:
			return 0
		}
	})

	snap := Snapshot{
		Workspaces:   workspaces,
		Windows:      windows,
		Casts:        casts,
		CastsKnown:   s.castsKnown,
		OverviewOpen: s.overviewOpen,
		Layouts: KeyboardLayouts{
			Names:   slices.Clone(s.layouts.Names),
			Current: s.layouts.Current,
		},
	}
	for _, w := range workspaces {
		if w.Focused {
			snap.FocusedOutput = w.Output
			break
		}
	}
	return snap
}

// publishIfChanged records and reports a new snapshot only when it differs
// from the last published one. Workspace and Window contain only comparable
// fields, so slices.Equal is an exact comparison.
func (s *state) publishIfChanged() bool {
	next := s.snapshot()
	if next.FocusedOutput == s.last.FocusedOutput &&
		next.OverviewOpen == s.last.OverviewOpen &&
		next.CastsKnown == s.last.CastsKnown &&
		slices.Equal(next.Workspaces, s.last.Workspaces) &&
		slices.Equal(next.Windows, s.last.Windows) &&
		slices.Equal(next.Casts, s.last.Casts) &&
		slices.Equal(next.Layouts.Names, s.last.Layouts.Names) &&
		next.Layouts.Current == s.last.Layouts.Current {
		return false
	}
	s.last = next
	return true
}
