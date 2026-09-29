package shell

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

// waitProducer waits for the recorder to hold n commands.
func waitProducer(t *testing.T, rec *pluginToastRecorder, n int) []protocol.Command {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := rec.commands(); len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("producer commands = %d, want %d", len(rec.commands()), n)
	return nil
}

func TestStillScreenshotsAskNiriAndToastThePath(t *testing.T) {
	cases := []struct {
		mode    string
		sendErr error
		check   func(t *testing.T, action any, path string)
		summary string
	}{
		{mode: "screen", summary: "Screenshot saved", check: func(t *testing.T, action any, path string) {
			a, ok := action.(niri.ScreenshotScreen)
			if !ok || !a.WriteToDisk || a.Path != path {
				t.Fatalf("screen action = %#v for %q", action, path)
			}
		}},
		{mode: "window", summary: "Screenshot saved", check: func(t *testing.T, action any, path string) {
			a, ok := action.(niri.ScreenshotWindow)
			if !ok || a.ID != nil || !a.WriteToDisk || a.Path != path {
				t.Fatalf("window action = %#v for %q", action, path)
			}
		}},
		{mode: "window", sendErr: errors.New("niri: action: no focused window"), summary: "Screenshot failed"},
	}
	for _, tc := range cases {
		t.Run(tc.mode+"/"+tc.summary, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "Screenshots")
			r := NewRegistry(config.Default())
			rec := &pluginToastRecorder{}
			r.BindNotifications(rec)
			r.screenshotDir = func() string { return dir }
			var gotAction any
			var gotPath string
			r.niriScreenshot = func(_ context.Context, action any, path string) error {
				gotAction, gotPath = action, path
				return tc.sendErr
			}

			if err := r.Screenshot(tc.mode); err != nil {
				t.Fatalf("Screenshot(%q): %v", tc.mode, err)
			}
			got := waitProducer(t, rec, 1)
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				t.Fatalf("the directory was not created: %v", err)
			}
			if filepath.Dir(gotPath) != dir || !strings.HasPrefix(filepath.Base(gotPath), "screenshot_") {
				t.Fatalf("path = %q", gotPath)
			}
			if tc.check != nil {
				tc.check(t, gotAction, gotPath)
			}
			p := got[0].Producer
			if got[0].Kind != protocol.CommandProducerPublish || p == nil || p.Summary != tc.summary {
				t.Fatalf("toast = %+v", got[0])
			}
			if tc.sendErr == nil && p.Body != gotPath {
				t.Fatalf("toast body = %q, want the path %q", p.Body, gotPath)
			}
			if tc.sendErr != nil && (p.Urgency != protocol.UrgencyCritical || !strings.Contains(p.Body, "no focused window")) {
				t.Fatalf("failure toast = %+v", p)
			}
		})
	}
}

func TestScreenshotRejectsAnUnknownMode(t *testing.T) {
	r := NewRegistry(config.Default())
	if err := r.Screenshot("scroll"); err == nil {
		t.Fatal("an unknown mode was accepted")
	}
}
