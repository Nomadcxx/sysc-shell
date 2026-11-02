package compositor

import (
	"path/filepath"
	"testing"
)

func TestDetect(t *testing.T) {
	for _, tc := range []struct {
		name, sysc, niri string
		want             Kind
	}{
		{"sysc", "/run/control.sock", "/run/user/1000/sysc-compositor.wayland-1.7.sock", SyscCompositor},
		{"stale", "/run/control.sock", "/run/user/1000/niri.wayland-1.7.sock", Niri},
		{"unset", "", "", Niri},
		{"missing control", "", "/run/sysc-compositor.wayland-1.7.sock", Niri},
		{"parent only", "/run/control.sock", "/run/sysc-compositor.session/niri.sock", Niri},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"SYSC_SOCKET": tc.sysc, "NIRI_SOCKET": tc.niri}
			if got := Detect(func(key string) string { return env[key] }); got != tc.want {
				t.Fatalf("Detect = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	for _, tc := range []struct {
		kind Kind
		dir  string
	}{{Niri, "niri"}, {SyscCompositor, "sysc-compositor"}} {
		if got, want := ConfigDir(home, tc.kind), filepath.Join(home, ".config", tc.dir); got != want {
			t.Fatalf("ConfigDir = %q, want %q", got, want)
		}
	}
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if got, want := ConfigDir(home, SyscCompositor), filepath.Join(xdg, "sysc-compositor"); got != want {
		t.Fatalf("ConfigDir = %q, want %q", got, want)
	}
	t.Setenv("XDG_CONFIG_HOME", "relative")
	if got, want := ConfigDir(home, Niri), filepath.Join(home, ".config", "niri"); got != want {
		t.Fatalf("relative XDG_CONFIG_HOME = %q, want %q", got, want)
	}
}
