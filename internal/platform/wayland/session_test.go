package wayland

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestSessionEndReportsTheEndOfAStoppedSession(t *testing.T) {
	t.Parallel()

	reset := fmt.Errorf("wayland: dispatch: unable to read msg: %w", &net.OpError{
		Op:  "readmsg",
		Err: os.NewSyscallError("recvmsg", syscall.ECONNRESET),
	})
	real := errors.New("wayland: reload: the config is invalid")

	for _, tc := range []struct {
		name     string
		stopping bool
		err      error
		wantNil  bool
		wantHas  error
	}{
		{
			name:     "a stopped session with a lost connection",
			stopping: true,
			err:      reset,
			wantNil:  true,
		},
		{
			name:     "a stopped session with a lost connection and a shutdown failure",
			stopping: true,
			err:      errors.Join(reset, fmt.Errorf("wayland: shutdown roundtrip: %w", io.EOF)),
			wantNil:  true,
		},
		{
			name:     "a stopped session with a real failure",
			stopping: true,
			err:      real,
			wantHas:  real,
		},
		{
			name:     "a stopped session with a lost connection and a real failure",
			stopping: true,
			err:      errors.Join(reset, real),
			wantHas:  real,
		},
		{
			name:     "a running session with a lost connection",
			stopping: false,
			err:      reset,
			wantHas:  reset,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := sessionEnd(tc.stopping, tc.err)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("session end = %v, want no failure", got)
				}
				return
			}
			if !errors.Is(got, tc.wantHas) {
				t.Fatalf("session end = %v, want it to carry %v", got, tc.wantHas)
			}
		})
	}
}

func TestShutdownAgainstAGoneCompositorEndsTheSession(t *testing.T) {
	t.Parallel()

	o, peer := newDispatchTestOwner(t)
	// The compositor leaves: the socket closes under the shell's feet.
	if err := peer.Close(); err != nil {
		t.Fatal(err)
	}
	err := o.shutdown()
	if err == nil {
		t.Fatal("shutdown against a gone compositor reported no failure")
	}
	if !connectionGone(err) {
		t.Fatalf("shutdown error = %v, want a lost connection", err)
	}
	if got := sessionEnd(true, err); got != nil {
		t.Fatalf("a stopped session reported %v, want no failure", got)
	}
}

func TestAwaitStopEndsTheSessionWhenTheStopArrives(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := awaitStop(ctx, time.Second, errors.New("connection reset by peer")); got != nil {
		t.Fatalf("a session that was stopped reported %v, want no failure", got)
	}
}

func TestAwaitStopKeepsTheFailureWhenNoStopArrives(t *testing.T) {
	t.Parallel()

	lost := errors.New("connection reset by peer")
	start := time.Now()
	got := awaitStop(context.Background(), 10*time.Millisecond, lost)
	if !errors.Is(got, lost) {
		t.Fatalf("a session without a stop reported %v, want %v", got, lost)
	}
	if elapsed := time.Since(start); elapsed < 10*time.Millisecond {
		t.Fatalf("awaitStop returned after %v, want it to wait for the stop", elapsed)
	}
}
