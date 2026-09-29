package shell

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/screenshot"
)

// screenshotToastKey is the producer key for screenshot results. One key, so
// a new result replaces the last one's toast rather than stacking.
const screenshotToastKey = "sysc-shell:screenshot"

// Screenshot starts a capture: "screen" and "window" through niri, "region"
// through the shell's own selector. It returns once the capture has started;
// the result is a toast.
func (r *Registry) Screenshot(mode string) error {
	switch mode {
	case "screen", "window":
		return r.stillScreenshot(mode)
	case "region":
		return r.openRegionSelector()
	}
	return fmt.Errorf("unknown screenshot mode %q", mode)
}

// stillScreenshot asks niri for a screen or window capture. niri writes the
// file and copies it to its own clipboard, so the shell only names the file,
// waits for it, and reports it.
func (r *Registry) stillScreenshot(mode string) error {
	dir := r.screenshotDirectory()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("screenshot directory: %w", err)
	}
	path := screenshot.NextPath(dir, time.Now(), screenshot.Exists)
	var action any = niri.ScreenshotScreen{WriteToDisk: true, ShowPointer: true, Path: path}
	if mode == "window" {
		action = niri.ScreenshotWindow{WriteToDisk: true, ShowPointer: true, Path: path}
	}
	send := r.niriScreenshot
	if send == nil {
		socket := os.Getenv("NIRI_SOCKET")
		if socket == "" {
			return errors.New("NIRI_SOCKET is unset")
		}
		send = func(ctx context.Context, action any, path string) error {
			return niri.Screenshot(ctx, socket, action, path)
		}
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), niri.ScreenshotTimeout+time.Second)
		defer cancel()
		r.screenshotToast(path, send(ctx, action, path))
	}()
	return nil
}

func (r *Registry) screenshotDirectory() string {
	if r.screenshotDir != nil {
		return r.screenshotDir()
	}
	return screenshot.Dir()
}

// screenshotToast reports one capture: the saved path, or why it failed.
func (r *Registry) screenshotToast(path string, err error) {
	summary, body, urgency := "Screenshot saved", path, protocol.UrgencyNormal
	if err != nil {
		log.Print("screenshot: ", err)
		summary, body, urgency = "Screenshot failed", err.Error(), protocol.UrgencyCritical
	}
	if _, sendErr := r.publishToast(screenshotToastKey, summary, body, urgency, -1); sendErr != nil {
		log.Printf("screenshot toast: %v", sendErr)
	}
}
