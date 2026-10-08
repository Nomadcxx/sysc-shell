package polkit

import (
	"fmt"
	"os"

	"github.com/godbus/dbus/v5"
)

// sessionID is the subject a registered agent belongs to. polkitd keys
// agents per session, so registering as the session's own agent is what lets
// "one agent per session" work: a second registration collides and the agent
// goes passive instead of competing.
//
// XDG_SESSION_ID comes from the graphical session and is present in the
// shell's systemd user service environment. logind is the fallback for a
// shell started outside that environment.
func sessionID(conn *dbus.Conn) (string, error) {
	if id := os.Getenv("XDG_SESSION_ID"); id != "" {
		return id, nil
	}
	var path dbus.ObjectPath
	call := conn.Object(logindName, logindPath).Call(logindManager+".GetSessionByPID", 0, uint32(os.Getpid()))
	if call.Err != nil {
		return "", fmt.Errorf("polkit: session of pid %d: %w", os.Getpid(), call.Err)
	}
	if err := call.Store(&path); err != nil {
		return "", fmt.Errorf("polkit: session of pid %d: %w", os.Getpid(), err)
	}
	var id string
	if err := conn.Object(logindName, path).Call(logindSession+".GetId", 0).Store(&id); err != nil {
		return "", fmt.Errorf("polkit: session id: %w", err)
	}
	return id, nil
}

// logind is only the fallback for a shell with no XDG_SESSION_ID, so its
// wire names are constants rather than a client.
const (
	logindName    = "org.freedesktop.login1"
	logindPath    = "/org/freedesktop/login1"
	logindManager = "org.freedesktop.login1.Manager"
	logindSession = "org.freedesktop.login1.Session"
)

// subject is the polkit (sa{sv}) this agent registers as. unix-session needs
// only the session id; unix-process would also need pid and start-time and
// would key the agent to one process instead of the session.
func subject(id string) struct {
	Kind   string
	Values map[string]dbus.Variant
} {
	return struct {
		Kind   string
		Values map[string]dbus.Variant
	}{
		Kind:   "unix-session",
		Values: map[string]dbus.Variant{"session-id": dbus.MakeVariant(id)},
	}
}
