package plugin

import (
	"slices"
	"testing"
)

func TestDeclaredSocketsResolveFromTheSession(t *testing.T) {
	t.Parallel()
	session := map[string]string{
		"XDG_RUNTIME_DIR":          "/run/user/1000",
		"DBUS_SESSION_BUS_ADDRESS": "unix:path=/run/user/1000/bus,guid=abc",
		"NIRI_SOCKET":              "/run/user/1000/niri.wayland-1.5879.sock",
		"WAYLAND_DISPLAY":          "wayland-1",
	}
	for _, tc := range []struct {
		name       string
		env        map[string]string
		sockets    []string
		paths      []string
		pass       []string
		unresolved []string
	}{
		{"nothing declared", session, nil, nil, nil, nil},
		{"session bus from its address", session, []string{SocketSessionBus},
			[]string{"/run/user/1000/bus"}, []string{"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus"}, nil},
		{"session bus from the runtime dir", map[string]string{"XDG_RUNTIME_DIR": "/run/user/7"}, []string{SocketSessionBus},
			[]string{"/run/user/7/bus"}, []string{"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/7/bus"}, nil},
		{"the first unix:path of several", map[string]string{"DBUS_SESSION_BUS_ADDRESS": "unix:abstract=/tmp/x;unix:path=/run/b"}, []string{SocketSessionBus},
			[]string{"/run/b"}, []string{"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/b"}, nil},
		{"an abstract-only bus does not resolve", map[string]string{"DBUS_SESSION_BUS_ADDRESS": "unix:abstract=/tmp/x"}, []string{SocketSessionBus},
			nil, nil, []string{SocketSessionBus}},
		{"system bus default", session, []string{SocketSystemBus},
			[]string{"/run/dbus/system_bus_socket"}, []string{"DBUS_SYSTEM_BUS_ADDRESS=unix:path=/run/dbus/system_bus_socket"}, nil},
		{"compositor", session, []string{SocketCompositor},
			[]string{"/run/user/1000/niri.wayland-1.5879.sock"}, []string{"NIRI_SOCKET=/run/user/1000/niri.wayland-1.5879.sock"}, nil},
		{"no compositor running", map[string]string{}, []string{SocketCompositor}, nil, nil, []string{SocketCompositor}},
		{"wayland made absolute", session, []string{SocketWayland},
			[]string{"/run/user/1000/wayland-1"}, []string{"WAYLAND_DISPLAY=/run/user/1000/wayland-1"}, nil},
		{"a path is itself", session, []string{"/run/docker.sock"}, []string{"/run/docker.sock"}, nil, nil},
	} {
		paths, pass, unresolved := resolveSockets(tc.sockets, func(k string) string { return tc.env[k] })
		if !slices.Equal(paths, tc.paths) || !slices.Equal(pass, tc.pass) || !slices.Equal(unresolved, tc.unresolved) {
			t.Errorf("%s: paths %q env %q unresolved %q, want %q %q %q", tc.name, paths, pass, unresolved, tc.paths, tc.pass, tc.unresolved)
		}
	}
}
