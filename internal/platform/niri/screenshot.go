package niri

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// ScreenshotTimeout bounds the wait for the compositor to write a capture.
const ScreenshotTimeout = 5 * time.Second

// Screenshot sends a screenshot action and waits until the compositor reports
// the file at path written.
//
// The compositor replies to the action before it writes the file, and the
// reply carries no path, so the only completion signal is the
// ScreenshotCaptured event. The event stream is therefore subscribed -- its
// handshake read and checked -- before the action exists, or a fast capture
// could be announced to nobody. The event is transient, so it never enters
// Snapshot.
func Screenshot(ctx context.Context, socketPath string, action any, path string) error {
	return screenshot(ctx, socketPath, action, path, ScreenshotTimeout)
}

func screenshot(ctx context.Context, socketPath string, action any, path string, timeout time.Duration) error {
	conn, scanner, err := handshake(ctx, socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancel()

	if err := Action(ctx, socketPath, action); err != nil {
		return err
	}
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("niri: screenshot deadline: %w", err)
	}
	for scanner.Scan() {
		var event struct {
			ScreenshotCaptured *struct {
				Path string `json:"path"`
			} `json:"ScreenshotCaptured"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		if event.ScreenshotCaptured != nil && event.ScreenshotCaptured.Path == path {
			return nil
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return fmt.Errorf("niri: timed out after %s waiting for the screenshot", timeout)
		}
		return fmt.Errorf("niri: screenshot event stream: %w", err)
	}
	return errors.New("niri: the compositor closed the event stream before the screenshot was written")
}
