package niri

import (
	"bufio"
	"context"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeScreenshotNiri serves one event stream and one action connection. The
// stream answers the handshake, then writes events once the action arrived;
// the action connection records whether the stream was already subscribed.
type fakeScreenshotNiri struct {
	socket string
	// events are written on the stream after the action; closeStream ends the
	// stream after them.
	events      []string
	closeStream bool
	actionReply string

	mu               sync.Mutex
	subscribedFirst  bool
	sawAction        bool
	actionDelivered  chan struct{}
	streamSubscribed chan struct{}
}

func newFakeScreenshotNiri(t *testing.T, events []string, closeStream bool, actionReply string) *fakeScreenshotNiri {
	t.Helper()
	f := &fakeScreenshotNiri{
		socket:           filepath.Join(t.TempDir(), "niri.sock"),
		events:           events,
		closeStream:      closeStream,
		actionReply:      actionReply,
		actionDelivered:  make(chan struct{}),
		streamSubscribed: make(chan struct{}),
	}
	ln, err := net.Listen("unix", f.socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

func (f *fakeScreenshotNiri) serve(conn net.Conn) {
	sc := bufio.NewScanner(conn)
	if !sc.Scan() {
		conn.Close()
		return
	}
	if sc.Text() == request {
		// Record the subscription before the reply: handshake consumes the reply
		// before screenshot() sends the action, so the reply write is what
		// orders the two. Closing the channel after the write left the
		// ordering check racing the write on a loaded runner.
		close(f.streamSubscribed)
		_, _ = conn.Write([]byte("{\"Ok\":\"Handled\"}\n"))
		<-f.actionDelivered
		for _, e := range f.events {
			_, _ = conn.Write([]byte(e + "\n"))
		}
		if f.closeStream {
			conn.Close()
			return
		}
		// Hold the stream open until the client hangs up.
		_, _ = conn.Read(make([]byte, 1))
		conn.Close()
		return
	}
	f.mu.Lock()
	select {
	case <-f.streamSubscribed:
		f.subscribedFirst = true
	default:
	}
	f.sawAction = true
	f.mu.Unlock()
	_, _ = conn.Write([]byte(f.actionReply + "\n"))
	conn.Close()
	close(f.actionDelivered)
}

func TestScreenshotWaitsForItsOwnCapture(t *testing.T) {
	t.Parallel()
	const path = "/p/shot.png"
	cases := []struct {
		name        string
		events      []string
		closeStream bool
		reply       string
		timeout     time.Duration
		wantErr     string
	}{
		{
			name: "matching path after unrelated events",
			events: []string{
				`{"WorkspacesChanged":{"workspaces":[]}}`,
				`{"ScreenshotCaptured":{"path":"/p/other.png"}}`,
				`{"ScreenshotCaptured":{"path":"/p/shot.png"}}`,
			},
			reply: `{"Ok":"Handled"}`,
		},
		{
			name:    "action refused",
			reply:   `{"Err":"no focused window"}`,
			wantErr: "no focused window",
		},
		{
			name:        "stream closed first",
			events:      []string{`{"ScreenshotCaptured":{"path":"/p/other.png"}}`},
			closeStream: true,
			reply:       `{"Ok":"Handled"}`,
			wantErr:     "closed",
		},
		{
			name:    "deadline",
			reply:   `{"Ok":"Handled"}`,
			timeout: 50 * time.Millisecond,
			wantErr: "timed out",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFakeScreenshotNiri(t, tc.events, tc.closeStream, tc.reply)
			timeout := tc.timeout
			if timeout == 0 {
				timeout = 2 * time.Second
			}
			err := screenshot(context.Background(), f.socket, ScreenshotScreen{WriteToDisk: true, Path: path}, path, timeout)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Screenshot: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Screenshot error = %v, want one containing %q", err, tc.wantErr)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if !f.sawAction {
				t.Fatal("the action was never sent")
			}
			if !f.subscribedFirst {
				t.Fatal("the action was sent before the event stream was subscribed")
			}
		})
	}
}
