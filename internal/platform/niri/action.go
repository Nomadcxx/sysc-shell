package niri

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
)

// ToggleOverview toggles Niri's compositor-rendered overview.
type ToggleOverview struct{}

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

// SwitchLayout asks Niri to select one reported keyboard layout index.
type SwitchLayout struct{ Index uint8 }

// ToggleWindowFloating toggles one exact window ID between floating and tiling.
type ToggleWindowFloating struct{ ID uint64 }

// ActionRefusalError reports an explicit Err reply from Niri for an action.
type ActionRefusalError struct{ Reason string }

func (e *ActionRefusalError) Error() string { return fmt.Sprintf("niri: action: %s", e.Reason) }

// ActionNotSentError reports a failure known to happen before Niri could
// receive the action request.
type ActionNotSentError struct{ Cause error }

func (e *ActionNotSentError) Error() string { return fmt.Sprintf("niri: action not sent: %v", e.Cause) }

func (e *ActionNotSentError) Unwrap() error { return e.Cause }

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
		return &ActionNotSentError{Cause: err}
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return &ActionNotSentError{Cause: fmt.Errorf("niri: connect to %s: %w", socketPath, err)}
	}
	defer conn.Close()
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancel()

	request := append(payload, '\n')
	written, err := conn.Write(request)
	if err != nil {
		cause := fmt.Errorf("niri: send action: %w", err)
		if written == 0 {
			return &ActionNotSentError{Cause: cause}
		}
		return cause
	}
	if written != len(request) {
		return fmt.Errorf("niri: short action write: %d of %d bytes", written, len(request))
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 4096), maxLine)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("niri: read action reply: %w", err)
		}
		return fmt.Errorf("niri: no action reply")
	}
	var reply wireReply
	if err := json.Unmarshal(scanner.Bytes(), &reply); err != nil {
		return fmt.Errorf("niri: decode action reply: %w", err)
	}
	if reply.Err != nil {
		return &ActionRefusalError{Reason: *reply.Err}
	}
	if reply.Ok == nil {
		return fmt.Errorf("niri: action reply carried no result")
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
	case ToggleWindowFloating:
		inner = map[string]any{"ToggleWindowFloating": map[string]any{"id": v.ID}}
	case SwitchLayout:
		inner = map[string]any{"SwitchLayout": map[string]any{
			"layout": map[string]any{"Index": v.Index},
		}}
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
