package plugin

import (
	"path/filepath"
	"strings"
)

// resolveSockets turns a declaration's sockets into the paths the boundary
// grants and the environment that tells the plugin where they are. A named
// endpoint is read from the session the shell runs in, at each start, because
// a compositor socket's name changes with every session. An endpoint that does
// not resolve to a filesystem path grants nothing and is returned so the
// caller can say so; the plugin still starts.
func resolveSockets(sockets []string, getenv func(string) string) (paths, env, unresolved []string) {
	runtime := getenv("XDG_RUNTIME_DIR")
	for _, socket := range sockets {
		var path, key, value string
		switch socket {
		case SocketSessionBus:
			path = busPath(getenv("DBUS_SESSION_BUS_ADDRESS"))
			if path == "" && getenv("DBUS_SESSION_BUS_ADDRESS") == "" && filepath.IsAbs(runtime) {
				path = filepath.Join(runtime, "bus")
			}
			key, value = "DBUS_SESSION_BUS_ADDRESS", "unix:path="+path
		case SocketSystemBus:
			path = busPath(getenv("DBUS_SYSTEM_BUS_ADDRESS"))
			if path == "" && getenv("DBUS_SYSTEM_BUS_ADDRESS") == "" {
				path = "/run/dbus/system_bus_socket"
			}
			key, value = "DBUS_SYSTEM_BUS_ADDRESS", "unix:path="+path
		case SocketCompositor:
			if p := getenv("NIRI_SOCKET"); filepath.IsAbs(p) {
				path = p
			}
			key, value = "NIRI_SOCKET", path
		case SocketWayland:
			// libwayland accepts an absolute display name, which keeps the
			// socket reachable when the plugin's runtime directory is its own.
			display := getenv("WAYLAND_DISPLAY")
			switch {
			case filepath.IsAbs(display):
				path = display
			case display != "" && filepath.IsAbs(runtime):
				path = filepath.Join(runtime, display)
			}
			key, value = "WAYLAND_DISPLAY", path
		default:
			path = socket
		}
		if path == "" {
			unresolved = append(unresolved, socket)
			continue
		}
		paths = append(paths, path)
		if key != "" {
			env = append(env, key+"="+value)
		}
	}
	return paths, env, unresolved
}

// busPath is the first unix:path= address in a D-Bus address list. Abstract
// and TCP addresses are not filesystem sockets a rule can grant.
func busPath(address string) string {
	for _, entry := range strings.Split(address, ";") {
		transport, params, ok := strings.Cut(entry, ":")
		if !ok || transport != "unix" {
			continue
		}
		for _, kv := range strings.Split(params, ",") {
			if p, ok := strings.CutPrefix(kv, "path="); ok && filepath.IsAbs(p) {
				return p
			}
		}
	}
	return ""
}
