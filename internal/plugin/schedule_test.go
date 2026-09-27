package plugin

import (
	"strings"
	"testing"
	"time"
)

func TestSchedulePublishesAtMostThirtyHz(t *testing.T) {
	now := time.Unix(0, 0)
	s := NewSchedule(func() time.Time { return now })
	if !s.Due("a") {
		t.Fatal("first publication must pass")
	}
	if s.Due("a") {
		t.Fatal("publication in the same instant must be gated")
	}
	now = now.Add(publishInterval - time.Millisecond)
	if s.Due("a") {
		t.Fatal("publication inside the window must be gated")
	}
	now = now.Add(time.Millisecond)
	if !s.Due("a") {
		t.Fatal("publication an interval after the last pass must pass")
	}
	if !s.Due("b") {
		t.Fatal("view ids must gate independently")
	}
}

func TestPluginEnvironmentIsAllowlisted(t *testing.T) {
	t.Setenv("PATH", "/bin")
	t.Setenv("WAYLAND_DISPLAY", "wayland-1")
	t.Setenv("MY_SESSION_TOKEN", "secret")
	t.Setenv("SYSC_HELPER_IMAGE", "/tmp/art.png")
	got := strings.Join(pluginEnvironment(), "\n")
	if !strings.Contains(got, "PATH=/bin") || !strings.Contains(got, "WAYLAND_DISPLAY=wayland-1") {
		t.Fatalf("allowlisted vars missing: %q", got)
	}
	if !strings.Contains(got, "SYSC_HELPER_IMAGE=/tmp/art.png") {
		t.Fatalf("SYSC_ config namespace must pass through: %q", got)
	}
	if strings.Contains(got, "MY_SESSION_TOKEN") {
		t.Fatalf("non-allowlisted var leaked: %q", got)
	}
}
