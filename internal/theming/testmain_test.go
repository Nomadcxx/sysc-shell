package theming

import (
	"os"
	"testing"
)

// Template mechanism checks must not signal desktop or greeter processes.
// The signal-specific check supplies its own recorded process directory.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sysc-theming-test-proc-")
	if err != nil {
		panic(err)
	}
	procRoot = dir
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
