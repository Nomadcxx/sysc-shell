package wayland

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/sockpath"
)

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
}
