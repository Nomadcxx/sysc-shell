package plugin

import (
	"slices"
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

// A plugin that hands off to a desktop app (xdg-open of a game, a folder, a
// config file) must pass on the display session: without DISPLAY an X11 app
// such as Steam, started through Lutris, died with "Unable to open a
// connection to X".
func TestPluginEnvironmentCarriesTheDisplaySession(t *testing.T) {
	session := map[string]string{
		"DISPLAY":                  ":0",
		"XAUTHORITY":               "/run/user/1000/xauth",
		"DBUS_SESSION_BUS_ADDRESS": "unix:path=/run/user/1000/bus",
		"XDG_CURRENT_DESKTOP":      "niri",
		"XDG_SESSION_TYPE":         "wayland",
	}
	for k, v := range session {
		t.Setenv(k, v)
	}
	got := pluginEnvironment()
	for k, v := range session {
		if !slices.Contains(got, k+"="+v) {
			t.Errorf("%s missing from the plugin environment", k)
		}
	}
}
