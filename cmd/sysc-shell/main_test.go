package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
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

func TestPumpNiriDrainsTerminalErrorAfterSnapshotsClose(t *testing.T) {
	wantErr := errors.New("terminal stream failure")
	snapshots := make(chan niri.Snapshot)
	errs := make(chan error, 1)
	errs <- wantErr
	close(snapshots)
	close(errs)

	err := pumpNiri(snapshots, errs, func(niri.Snapshot) {})
	if !errors.Is(err, wantErr) {
		t.Fatalf("pumpNiri error = %v, want %v", err, wantErr)
	}
}

func TestPumpNiriForwardsSnapshotsBeforeClosure(t *testing.T) {
	snapshots := make(chan niri.Snapshot, 1)
	errs := make(chan error)
	snapshots <- niri.Snapshot{}
	close(snapshots)
	close(errs)

	updates := 0
	if err := pumpNiri(snapshots, errs, func(niri.Snapshot) { updates++ }); err != nil {
		t.Fatalf("pumpNiri error = %v, want nil", err)
	}
	if updates != 1 {
		t.Fatalf("updates = %d, want 1", updates)
	}
}
