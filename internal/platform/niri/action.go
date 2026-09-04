package niri

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
)

// FocusWindow asks the compositor to focus one window by id.
type FocusWindow struct {
	ID uint64 `json:"id"`
}

// CloseWindow asks the compositor to close one window by id.
type CloseWindow struct {
	ID uint64 `json:"id"`
}

// Action sends one compositor request on a short-lived connection. It does
// not use the EventStream socket.
func Action(ctx context.Context, socketPath string, body any) error {
	payload, err := marshalAction(body)
	if err != nil {
		return err
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("niri: connect to %s: %w", socketPath, err)
	}
	defer conn.Close()
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancel()

	if _, err := conn.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("niri: send action: %w", err)
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
		return fmt.Errorf("niri: action: %s", *reply.Err)
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
	default:
		return nil, fmt.Errorf("niri: unknown action %T", body)
	}
	return json.Marshal(map[string]any{"Action": inner})
}
