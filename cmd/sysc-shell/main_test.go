package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	clipboardclient "github.com/Nomadcxx/sysc-clipboard/client"
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

func TestPumpClipboardForwardsDisconnectAndReconnectBeforeClosure(t *testing.T) {
	updates := make(chan clipboardclient.Update, 2)
	updates <- clipboardclient.Update{Connected: false}
	updates <- clipboardclient.Update{Connected: true}
	close(updates)

	var states []bool
	pumpClipboard(context.Background(), updates, func(update clipboardclient.Update) {
		states = append(states, update.Connected)
	})
	if len(states) != 2 || states[0] || !states[1] {
		t.Fatalf("clipboard connection states = %v, want [false true]", states)
	}
}

func TestPumpClipboardStopsWhenRegistryContextCloses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	updates := make(chan clipboardclient.Update)
	done := make(chan struct{})
	go func() {
		pumpClipboard(ctx, updates, func(clipboardclient.Update) {})
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("clipboard pump did not stop after context cancellation")
	}
}

// The exit code has to come from the envelope, not from a substring: nested
// per-item "error" keys are data on a reply the server marked successful.
func TestReplyFailedReadsTheEnvelope(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want bool
	}{
		{"nested error on a healthy store reply", `{"id":1,"ok":true,"sources":[{"name":"official","error":""}]}`, false},
		{"nested error with a message still succeeds", `{"id":1,"ok":true,"listings":[{"error":"404 not found"}]}`, false},
		{"ok with no error key", `{"id":1,"ok":true}`, false},
		{"top level error", `{"id":1,"error":"unknown method"}`, true},
		{"empty top level error", `{"id":1,"error":""}`, true},
		{"no ok and no error", `{"id":1}`, true},
		{"not json", `not json`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := replyFailed(tc.out); got != tc.want {
				t.Errorf("replyFailed(%s) = %v, want %v", tc.out, got, tc.want)
			}
		})
	}
}
