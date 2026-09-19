package trayclient

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A service that is not there has to say so once. Before this, session()
// dropped the dial error and Run retried forever, so a shell that could never
// reach the tray looked exactly like a tray with no items: sysc-411 lived for
// weeks behind that silence. The report is per transition, not per attempt,
// because the retry loop runs every couple of seconds forever.
func TestUnreachableServiceIsReportedOnceNotPerRetry(t *testing.T) {
	dir := runtimeDir(t)
	out := make(chan Message, 32)
	client := New(dir, out)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = client.Run(ctx) }()

	// Long enough for several backoff rounds: the first two are 100ms and
	// 200ms, so this covers at least four attempts.
	time.Sleep(900 * time.Millisecond)
	cancel()
	<-done

	var failures []Message
	for {
		select {
		case m := <-out:
			if m.Err != nil {
				failures = append(failures, m)
			}
			continue
		default:
		}
		break
	}
	if len(failures) == 0 {
		t.Fatal("an unreachable service was never reported; the shell cannot tell it apart from an empty tray")
	}
	if len(failures) != 1 {
		t.Fatalf("reported %d failures, want exactly one for the transition: %v", len(failures), failures)
	}
}

// A service that comes back and goes away again is named again. Reporting
// only the first failure for the life of the process would mean a tray that
// dies at noon is as silent as one that never started.
func TestUnreachableIsReportedAgainAfterAConnection(t *testing.T) {
	out := make(chan Message, 8)
	client := New(runtimeDir(t), out)

	client.reportUnreachable(errFake)
	client.reportUnreachable(errFake)
	if got := len(out); got != 1 {
		t.Fatalf("messages after two failures = %d, want one", got)
	}

	// A completed handshake re-arms the report.
	client.clearUnreachable()
	client.reportUnreachable(errFake)
	if got := len(out); got != 2 {
		t.Fatalf("messages after a reconnect and a second failure = %d, want two", got)
	}
	for range 2 {
		if m := <-out; m.Kind != KindDisconnected || m.Err == nil {
			t.Fatalf("message = %+v, want a disconnect carrying the reason", m)
		}
	}
}

var errFake = errors.New("trayclient: dial refused")
