package shell

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
)

func TestPluginHostScreenshotCallsReachTheRegistry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Screenshots")
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	r.BindNotifications(&pluginToastRecorder{})
	r.screenshotDir = func() string { return dir }
	seen := make(chan any, 1)
	r.niriScreenshot = func(_ context.Context, action any, _ string) error {
		seen <- action
		return nil
	}
	h := &pluginHost{r: r}

	got, err := h.screenshotDirectory(context.Background())
	if err != nil || got != dir {
		t.Fatalf("directory = %q, %v; want %q", got, err, dir)
	}
	if err := h.screenshotStart(context.Background(), "screen"); err != nil {
		t.Fatal(err)
	}
	select {
	case action := <-seen:
		if _, ok := action.(niri.ScreenshotScreen); !ok {
			t.Fatalf("action = %#v, want a screen capture", action)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the registry never started the capture")
	}
	if err := h.screenshotStart(context.Background(), "scroll"); err == nil {
		t.Fatal("an unknown mode must be an error")
	}
}

func TestHostPluginCapsIncludeScreenshot(t *testing.T) {
	for _, c := range hostPluginCaps {
		if c == plugin.CapScreenshot {
			return
		}
	}
	t.Fatal("the host does not grant the screenshot capability")
}
