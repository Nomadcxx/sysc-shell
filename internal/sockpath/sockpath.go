// Package sockpath gives tests a directory short enough to hold a Unix
// socket.
//
// A Unix socket path is limited to 108 bytes on Linux (sun_path). A longer
// path cannot be bound: bind fails with "invalid argument", which reads as a
// broken component rather than a broken test.
//
// Both ways Go makes a temporary directory inherit TMPDIR. t.TempDir adds
// the test's own name to it, so a long test name under a long TMPDIR
// overruns the limit. os.MkdirTemp leaves the test name out but keeps the
// TMPDIR prefix. A directory with a short name of its own does not depend
// on either, so Dir makes one under a short base and checks the length
// before handing it back.
package sockpath

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

// Limit is the length of sun_path on Linux. A socket path longer than this
// cannot be bound.
const Limit = 107

// Reserve is the room Dir leaves for the file name a caller adds to the
// directory it returns.
const Reserve = 32

// bases are the directories Dir tries, in order. /dev/shm is short and a
// Linux session has it; /tmp is the fallback. os.TempDir is the last resort,
// and the length check rejects it when TMPDIR is long.
var bases = []string{"/dev/shm", "/tmp"}

// Dir returns a new directory whose path is short enough to hold a Unix
// socket, and removes it when the test ends.
//
// The prefix names the directory, so a caller can tell whose socket it is.
// The socket file name a caller adds to the path must fit in Reserve bytes.
func Dir(t testing.TB, prefix string) string {
	t.Helper()
	dir, err := makeDir(prefix)
	if err != nil {
		t.Fatalf("no directory short enough for a socket path: %v", err)
		return ""
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// makeDir creates a directory whose path is short enough to hold a Unix
// socket.
func makeDir(prefix string) (string, error) {
	var lastErr error
	for _, base := range append(append([]string{}, bases...), os.TempDir()) {
		dir, err := os.MkdirTemp(base, prefix)
		if err != nil {
			lastErr = err
			continue
		}
		if len(dir)+Reserve > Limit {
			_ = os.RemoveAll(dir)
			lastErr = fmt.Errorf("%s leaves no room for a socket name", dir)
			continue
		}
		return dir, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no base directory was tried")
	}
	return "", lastErr
}
