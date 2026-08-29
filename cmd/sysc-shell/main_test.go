package main

import (
	"context"
	"strings"
	"testing"
)

// The --output flag is gone: every connected output receives a bar, so there
// is no command line left to parse. What remains worth asserting is that the
// environment is validated before Wayland is opened, so the startup error names
// the missing variable rather than surfacing as a connection failure.
func TestRunRequiresNiriSocket(t *testing.T) {
	t.Setenv("NIRI_SOCKET", "")

	err := run(context.Background())
	if err == nil {
		t.Fatal("run succeeded with NIRI_SOCKET unset")
	}
	if !strings.Contains(err.Error(), "NIRI_SOCKET") {
		t.Fatalf("error %q does not name the missing variable", err)
	}
}
