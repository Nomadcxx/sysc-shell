package trayclient

import "testing"

// The service binds this name in internal/presenter/server.go, which is
// unexported and cannot be imported, so the contract is restated here rather
// than derived from socketName. Deriving it is what hid the original defect:
// every other test in this package builds its fake server's path from
// socketName, so the suite agreed with itself while the shell dialled a path
// nothing served. Change this literal only against the service's source.
const presenterSocket = "/run/user/1000/sysc-tray/presenter.v1.sock"

func TestSocketPathIsTheSocketTheServiceBinds(t *testing.T) {
	if got := SocketPath("/run/user/1000"); got != presenterSocket {
		t.Fatalf("SocketPath = %q, want %q", got, presenterSocket)
	}
}
