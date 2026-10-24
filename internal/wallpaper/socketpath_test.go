package wallpaper

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/sockpath"
)

// unixSocketPathLimit is the length of sun_path on Linux. A socket path
// longer than this cannot be bound: the bind fails with "invalid argument",
// which reads as a broken engine rather than a broken test.
const unixSocketPathLimit = 107

// TestShortTempDirLeavesRoomForASocket pins the property the helper exists
// for: the directory it returns, and the sockets the engine binds under it,
// are short enough for the limit, whatever TMPDIR says.
func TestShortTempDirLeavesRoomForASocket(t *testing.T) {
	dir := sockpath.Dir(t, "wc")
	for _, path := range []string{
		terminalSocketPath(dir, "DP-1"),
		socketPath(dir, "DP-1"),
		filepath.Join(dir, "replacement.sock"),
		filepath.Join(dir, "g.sock"),
	} {
		if len(path) > unixSocketPathLimit {
			t.Fatalf("socket path is %d bytes: %s; the limit is %d", len(path), path, unixSocketPathLimit)
		}
	}
	h := newEngineHarness(t)
	for _, path := range []string{h.socket("DP-1"), terminalSocketPath(h.eng.dir, "DP-1")} {
		if len(path) > unixSocketPathLimit {
			t.Fatalf("harness socket path is %d bytes: %s; the limit is %d", len(path), path, unixSocketPathLimit)
		}
	}
}

// TestEngineSocketDirIsNotInTheTestTempRoot pins where the engine's socket
// directory may live.
//
// t.TempDir names its directory after the test, so the test's own name sits
// in every socket path the engine binds. A long test name under a long
// TMPDIR overruns the socket limit. A directory outside the test's temp root
// keeps that name out of the path.
func TestEngineSocketDirIsNotInTheTestTempRoot(t *testing.T) {
	root := filepath.Dir(t.TempDir())
	h := newEngineHarness(t)
	if strings.HasPrefix(h.eng.dir, root) {
		t.Fatalf("the engine's socket dir is %s, inside the test's temp root %s; use sockpath.Dir(t, ...)", h.eng.dir, root)
	}
}

// TestSocketListensAvoidLongTempDirs fails a test that binds a unix socket
// under a directory t.TempDir made. That directory is named after the test,
// so the path overruns the socket limit under a long TMPDIR and the bind
// fails with "invalid argument" instead of the test's own failure.
//
// The check reads the package's own test sources, so it holds under any load
// and needs no socket of its own.
func TestSocketListensAvoidLongTempDirs(t *testing.T) {
	if err := sockpath.CheckSocketPaths("."); err != nil {
		t.Fatal(err)
	}
	binds, err := sockpath.CountSocketBinds(".")
	if err != nil {
		t.Fatal(err)
	}
	if binds == 0 {
		t.Fatal("the check saw no socket bind: it no longer reads this package's own tests")
	}
}
