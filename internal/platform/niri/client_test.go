package niri

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Wire fixtures captured from `niri msg -j event-stream` on Niri 26.04. They
// carry no window titles or user data.
const (
	replyOK         = `{"Ok":"Handled"}`
	replyErr        = `{"Err":"error parsing request"}`
	replyUnexpected = `{"Ok":"Unexpected"}`

	workspacesChanged = `{"WorkspacesChanged":{"workspaces":[` +
		`{"id":1,"idx":1,"name":null,"output":"DP-1","is_urgent":false,"is_active":true,"is_focused":true,"active_window_id":null},` +
		`{"id":3,"idx":2,"name":null,"output":"DP-1","is_urgent":false,"is_active":false,"is_focused":false,"active_window_id":null}]}}`

	workspaceActivated = `{"WorkspaceActivated":{"id":3,"focused":true}}`

	// Named workspaces on a second output, to prove nullable handling and the
	// multi-output projection.
	namedWorkspaces = `{"WorkspacesChanged":{"workspaces":[` +
		`{"id":5,"idx":1,"name":"code","output":"DP-3","is_urgent":false,"is_active":true,"is_focused":true,"active_window_id":80},` +
		`{"id":6,"idx":2,"name":null,"output":null,"is_urgent":false,"is_active":false,"is_focused":false,"active_window_id":null}]}}`

	// Events the shell does not model and must ignore. WindowsChanged left this
	// set when window state became modelled state.
	unknownEvents = `{"KeyboardLayoutsChanged":{"keyboard_layouts":{"names":["English (US)"],"current_idx":0}}}` + "\n" +
		`{"OverviewOpenedOrClosed":{"is_open":false}}` + "\n" +
		`{"ConfigLoaded":{"failed":false}}`
)

// fakeNiri is a scripted Unix socket server standing in for the compositor.
type fakeNiri struct {
	path     string
	requests chan string
}

// startFakeNiri serves one connection, recording the request line and writing
// the supplied lines in order.
func startFakeNiri(t *testing.T, lines ...string) *fakeNiri {
	t.Helper()

	// Keep the path short; a Unix socket path is limited to ~108 bytes.
	dir, err := os.MkdirTemp("", "niri")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	f := &fakeNiri{path: filepath.Join(dir, "s"), requests: make(chan string, 1)}
	listener, err := net.Listen("unix", f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		request, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			return
		}
		f.requests <- request

		for _, line := range lines {
			if _, err := fmt.Fprintln(conn, line); err != nil {
				return
			}
		}
		// Hold the connection open so the client sees a live stream.
		<-time.After(2 * time.Second)
	}()
	return f
}

// nextSnapshot waits for one published snapshot.
func nextSnapshot(t *testing.T, snapshots <-chan Snapshot, errs <-chan error) Snapshot {
	t.Helper()
	select {
	case snap := <-snapshots:
		return snap
	case err := <-errs:
		t.Fatalf("stream failed: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a snapshot")
	}
	return Snapshot{}
}

// nextError waits for one stream error.
func nextError(t *testing.T, snapshots <-chan Snapshot, errs <-chan error) error {
	t.Helper()
	for {
		select {
		case err := <-errs:
			if err != nil {
				return err
			}
		case snap, ok := <-snapshots:
			if ok {
				t.Fatalf("stream published %+v instead of failing", snap)
			}
			snapshots = nil
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for an error")
		}
	}
}

func TestStreamSendsEventStreamRequest(t *testing.T) {
	t.Parallel()

	f := startFakeNiri(t, replyOK, workspacesChanged)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	snapshots, errs := Stream(ctx, f.path)
	nextSnapshot(t, snapshots, errs)

	select {
	case got := <-f.requests:
		if got != "\"EventStream\"\n" {
			t.Fatalf("request = %q, want %q", got, "\"EventStream\"\n")
		}
	case <-time.After(time.Second):
		t.Fatal("the client sent no request")
	}
}

func TestStreamRejectsErrorReply(t *testing.T) {
	t.Parallel()

	// The Err reply arrives where the Ok reply would. A client that treated it
	// as an unknown event would discard it and wait forever.
	f := startFakeNiri(t, replyErr, workspacesChanged)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	snapshots, errs := Stream(ctx, f.path)
	err := nextError(t, snapshots, errs)
	if !strings.Contains(err.Error(), "error parsing request") {
		t.Fatalf("error = %v, want it to quote the compositor's reply", err)
	}
}

func TestStreamRejectsUnexpectedOKReply(t *testing.T) {
	t.Parallel()

	f := startFakeNiri(t, replyUnexpected, workspacesChanged)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	snapshots, errs := Stream(ctx, f.path)
	if err := nextError(t, snapshots, errs); err == nil {
		t.Fatal("the client accepted an unexpected Ok reply")
	}
}

func TestStreamRejectsMissingReply(t *testing.T) {
	t.Parallel()

	// An event where the reply belongs must not be accepted as the handshake.
	f := startFakeNiri(t, workspacesChanged)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	snapshots, errs := Stream(ctx, f.path)
	if err := nextError(t, snapshots, errs); err == nil {
		t.Fatal("the client accepted an event as its handshake reply")
	}
}

func TestStreamPublishesInitialSnapshot(t *testing.T) {
	t.Parallel()

	f := startFakeNiri(t, replyOK, workspacesChanged)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	snapshots, errs := Stream(ctx, f.path)
	snap := nextSnapshot(t, snapshots, errs)
	if len(snap.Workspaces) != 2 {
		t.Fatalf("initial snapshot holds %d workspaces, want the complete set of 2", len(snap.Workspaces))
	}
	if snap.FocusedOutput != "DP-1" {
		t.Fatalf("focused output = %q, want DP-1", snap.FocusedOutput)
	}
	first := snap.Workspaces[0]
	if first.ID != 1 || first.Index != 1 || first.Output != "DP-1" || !first.Active || !first.Focused {
		t.Fatalf("first workspace = %+v, want the active focused workspace 1 on DP-1", first)
	}
	if first.Name != "" {
		t.Fatalf("null name projected to %q, want the empty string", first.Name)
	}
}

func TestStreamAppliesWorkspaceActivated(t *testing.T) {
	t.Parallel()

	f := startFakeNiri(t, replyOK, workspacesChanged, workspaceActivated)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	snapshots, errs := Stream(ctx, f.path)
	nextSnapshot(t, snapshots, errs) // the initial snapshot

	deadline := time.After(2 * time.Second)
	for {
		snap := nextSnapshot(t, snapshots, errs)
		byID := map[uint64]Workspace{}
		for _, w := range snap.Workspaces {
			byID[w.ID] = w
		}
		if byID[3].Active && byID[3].Focused && !byID[1].Active && !byID[1].Focused {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("activation never applied: %+v", snap.Workspaces)
		default:
		}
	}
}

func TestStreamAcceptsNullableNameAndOutput(t *testing.T) {
	t.Parallel()

	f := startFakeNiri(t, replyOK, namedWorkspaces)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	snapshots, errs := Stream(ctx, f.path)
	snap := nextSnapshot(t, snapshots, errs)
	var named, orphan Workspace
	for _, w := range snap.Workspaces {
		switch w.ID {
		case 5:
			named = w
		case 6:
			orphan = w
		}
	}
	if named.Name != "code" || named.Output != "DP-3" {
		t.Fatalf("named workspace = %+v, want name code on DP-3", named)
	}
	if orphan.Name != "" || orphan.Output != "" {
		t.Fatalf("null name and output projected to %+v, want empty strings", orphan)
	}
	if snap.FocusedOutput != "DP-3" {
		t.Fatalf("focused output = %q, want DP-3", snap.FocusedOutput)
	}
}

func TestStreamIgnoresUnknownEvents(t *testing.T) {
	t.Parallel()

	// The unknown events arrive between two known ones; the stream must survive.
	f := startFakeNiri(t, replyOK, unknownEvents, workspacesChanged)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	snapshots, errs := Stream(ctx, f.path)
	snap := nextSnapshot(t, snapshots, errs)
	if len(snap.Workspaces) != 2 {
		t.Fatalf("snapshot after unknown events holds %d workspaces, want 2", len(snap.Workspaces))
	}
}

func TestStreamRejectsMalformedKnownEvent(t *testing.T) {
	t.Parallel()

	malformed := []struct {
		name string
		line string
	}{
		{"missing id", `{"WorkspacesChanged":{"workspaces":[{"idx":1,"name":null,"output":"DP-1","is_active":true,"is_focused":true}]}}`},
		{"missing idx", `{"WorkspacesChanged":{"workspaces":[{"id":1,"name":null,"output":"DP-1","is_active":true,"is_focused":true}]}}`},
		{"missing is_active", `{"WorkspacesChanged":{"workspaces":[{"id":1,"idx":1,"name":null,"output":"DP-1","is_focused":true}]}}`},
		{"mistyped id", `{"WorkspacesChanged":{"workspaces":[{"id":"one","idx":1,"name":null,"output":"DP-1","is_active":true,"is_focused":true}]}}`},
		{"mistyped is_focused", `{"WorkspacesChanged":{"workspaces":[{"id":1,"idx":1,"name":null,"output":"DP-1","is_active":true,"is_focused":"yes"}]}}`},
	}
	for _, tc := range malformed {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := startFakeNiri(t, replyOK, tc.line)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// No partial snapshot may be published before the failure.
			snapshots, errs := Stream(ctx, f.path)
			if err := nextError(t, snapshots, errs); err == nil {
				t.Fatal("a malformed known event was accepted")
			}
		})
	}
}

func TestStreamRejectsOversizedLine(t *testing.T) {
	t.Parallel()

	huge := `{"WorkspacesChanged":{"workspaces":[],"pad":"` + strings.Repeat("x", 1<<20) + `"}}`
	f := startFakeNiri(t, replyOK, huge)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	snapshots, errs := Stream(ctx, f.path)
	if err := nextError(t, snapshots, errs); err == nil {
		t.Fatal("a line larger than 1 MiB was accepted")
	}
}

func TestStreamStopsOnContextCancellation(t *testing.T) {
	t.Parallel()

	f := startFakeNiri(t, replyOK, workspacesChanged)
	ctx, cancel := context.WithCancel(context.Background())

	snapshots, errs := Stream(ctx, f.path)
	nextSnapshot(t, snapshots, errs)

	cancel()

	// Both channels must close, which proves the reader goroutine returned.
	for snapshots != nil || errs != nil {
		select {
		case _, ok := <-snapshots:
			if !ok {
				snapshots = nil
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation reported %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("the reader goroutine did not return after cancellation")
		}
	}
}

func TestStreamHandshakeStopsOnContextCancellation(t *testing.T) {
	f := startFakeNiri(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Stream(ctx, f.path)
		close(done)
	}()

	select {
	case <-f.requests:
	case <-time.After(time.Second):
		t.Fatal("client did not send the request")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Stream remained blocked in handshake after cancellation")
	}
}

func TestStreamFailsOnMissingSocket(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	snapshots, errs := Stream(ctx, "/nonexistent/niri.sock")
	if err := nextError(t, snapshots, errs); err == nil {
		t.Fatal("Stream accepted a socket path that does not exist")
	}
}

// A full initial burst, as the compositor sends it: workspaces, then windows.
const initialBurst = `{"WorkspacesChanged":{"workspaces":[` +
	`{"id":5,"idx":1,"name":"code","output":"DP-9","is_urgent":false,` +
	`"is_active":true,"is_focused":true,"active_window_id":80}]}}` + "\n" +
	`{"WindowsChanged":{"windows":[` +
	`{"id":80,"title":"Fixture One","app_id":"fixture.one","pid":1000,"workspace_id":5,` +
	`"is_focused":true,"is_floating":false,"is_urgent":false,"layout":{},"focus_timestamp":null}]}}`

func TestTheStreamDeliversWindowState(t *testing.T) {
	t.Parallel()
	server := startFakeNiri(t, replyOK, initialBurst)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Each event publishes once, so the workspace snapshot arrives before the
	// window one. Draining to channel close instead would assert on the fake
	// server hanging up, which the client correctly reports as a failure.
	snapshots, errs := Stream(ctx, server.path)
	nextSnapshot(t, snapshots, errs)
	last := nextSnapshot(t, snapshots, errs)

	if len(last.Windows) != 1 || last.Windows[0].Title != "Fixture One" {
		t.Fatalf("windows = %+v, want one titled Fixture One", last.Windows)
	}
	if len(last.Workspaces) != 1 || !last.Workspaces[0].HasActiveWindow {
		t.Fatalf("workspaces = %+v, want one with an active window", last.Workspaces)
	}
}
