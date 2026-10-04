package niri

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
)

// ToggleOverview toggles Niri's compositor-rendered overview.
type ToggleOverview struct{}

// LoadConfigFile asks the compositor to reload its configuration, which is how
// a published native root takes effect without waiting for the file watcher.
// An empty Path reloads the current root; a set Path must be absolute, because
// a relative one would resolve against whatever cwd niri happened to have.
type LoadConfigFile struct {
	Path string
}

// CloseOverview closes Niri's compositor-rendered overview.
type CloseOverview struct{}

// MoveWindowToWorkspace moves one exact window ID to a workspace ID.
type MoveWindowToWorkspace struct {
	WindowID    uint64
	WorkspaceID uint64
	Focus       bool
}

// MoveColumnToWorkspace moves the focused column to a workspace ID.
type MoveColumnToWorkspace struct {
	WorkspaceID uint64
	Focus       bool
}

// FocusWindow asks the compositor to focus one window by id.
type FocusWindow struct {
	ID uint64 `json:"id"`
}

// CloseWindow asks the compositor to close one window by id.
type CloseWindow struct {
	ID uint64 `json:"id"`
}

// FocusWorkspace asks the compositor to focus one workspace by id. Niri's
// reference is an id, an index or a name; the id is the only one that is
// stable while workspaces are created and destroyed, so it is the only one
// this package sends.
type FocusWorkspace struct {
	ID uint64
}

// ScreenshotScreen asks the compositor to capture the focused output. The
// compositor always copies the capture to its own clipboard; WriteToDisk also
// saves it, to Path when set. Path must be absolute.
type ScreenshotScreen struct {
	WriteToDisk bool   `json:"write_to_disk"`
	ShowPointer bool   `json:"show_pointer"`
	Path        string `json:"path"`
}

// ScreenshotWindow asks the compositor to capture one window; a nil ID is the
// focused window. Clipboard and disk behave as for ScreenshotScreen.
type ScreenshotWindow struct {
	ID          *uint64 `json:"id"`
	WriteToDisk bool    `json:"write_to_disk"`
	ShowPointer bool    `json:"show_pointer"`
	Path        string  `json:"path"`
}

// PowerOffMonitors asks the compositor to DPMS-off every output.
type PowerOffMonitors struct{}

// PowerOnMonitors asks the compositor to DPMS-on every output.
type PowerOnMonitors struct{}

// Action sends one compositor request on a short-lived connection. It does
// not use the EventStream socket.
func Action(ctx context.Context, socketPath string, body any) error {
	payload, err := marshalAction(body)
	if err != nil {
		return err
	}
	ok, err := call(ctx, socketPath, payload)
	if err != nil {
		return err
	}
	// An Action acknowledges with a string. An object means the reply belongs
	// to a different request kind, so it is not read as a handled Action.
	var handled *string
	if err := json.Unmarshal(ok, &handled); err != nil {
		return fmt.Errorf("niri: decode action reply: %w", err)
	}
	return nil
}

func marshalAction(body any) ([]byte, error) {
	var inner map[string]any
	switch v := body.(type) {
	case FocusWindow:
		inner = map[string]any{"FocusWindow": v}
	case CloseWindow:
		inner = map[string]any{"CloseWindow": v}
	case ScreenshotScreen:
		inner = map[string]any{"ScreenshotScreen": v}
	case ScreenshotWindow:
		inner = map[string]any{"ScreenshotWindow": v}
	case FocusWorkspace:
		// WorkspaceReferenceArg is an externally tagged enum, so the id
		// reference is the object {"Id": n} rather than a bare number.
		inner = map[string]any{"FocusWorkspace": map[string]any{
			"reference": map[string]any{"Id": v.ID},
		}}
	case PowerOffMonitors:
		// Fieldless variants still serialize as an empty object.
		inner = map[string]any{"PowerOffMonitors": map[string]any{}}
	case PowerOnMonitors:
		inner = map[string]any{"PowerOnMonitors": map[string]any{}}
	case ToggleOverview:
		inner = map[string]any{"ToggleOverview": map[string]any{}}
	case LoadConfigFile:
		if v.Path != "" && !filepath.IsAbs(v.Path) {
			return nil, fmt.Errorf("niri: config path %q is not absolute", v.Path)
		}
		// An unset path reloads the current root, which niri represents as
		// null rather than as an empty string.
		var path any
		if v.Path != "" {
			path = v.Path
		}
		inner = map[string]any{"LoadConfigFile": map[string]any{"path": path}}
	case CloseOverview:
		inner = map[string]any{"CloseOverview": map[string]any{}}
	case MoveWindowToWorkspace:
		inner = map[string]any{"MoveWindowToWorkspace": map[string]any{
			"window_id": v.WindowID,
			"reference": map[string]any{"Id": v.WorkspaceID},
			"focus":     v.Focus,
		}}
	case MoveColumnToWorkspace:
		inner = map[string]any{"MoveColumnToWorkspace": map[string]any{
			"reference": map[string]any{"Id": v.WorkspaceID},
			"focus":     v.Focus,
		}}
	default:
		return nil, fmt.Errorf("niri: unknown action %T", body)
	}
	return json.Marshal(map[string]any{"Action": inner})
}
