package plugin

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// The shell's main loop calls Runtime.Send. If a plugin stops draining its
// standard input, the pipe fills and a synchronous Send would wedge the
// whole desktop — Send must queue, report overflow, and never block the
// caller. FIFO order across the queue is what keeps views coherent.
func TestRuntimeSendStalledPlugin(t *testing.T) {
	t.Parallel()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})
	r := NewRuntime(Candidate{}, RuntimeOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel) // do not call r.Stop: this handcrafted session has no cmd
	sess := &Session{enc: v1.NewEncoder(pw)}
	q := make(chan outItem, sendQueueDepth)
	r.mu.Lock()
	r.session = sess
	r.out = q
	r.mu.Unlock()
	go r.pumpOut(ctx, sess, q)

	const total = 2000
	type outcome struct {
		overflows int
	}
	done := make(chan outcome, 1)
	go func() {
		var o outcome
		for i := 0; i < total; i++ {
			if err := r.Send(&v1.ViewResync{ViewID: fmt.Sprintf("v%04d", i)}); err != nil {
				o.overflows++
			}
		}
		done <- o
	}()
	var res outcome
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Runtime.Send blocked its caller on a stalled plugin pipe")
	}
	if res.overflows == 0 {
		t.Error("an unread pipe plus a bounded queue must surface overflow, got none")
	}
	if res.overflows == total {
		t.Fatal("every Send failed: the queue and pump are not wired up")
	}

	// Accepted messages must reach the pipe in order; overflowed ones are
	// dropped, so the stream is strictly increasing with gaps allowed.
	accepted := total - res.overflows
	_ = pr.SetReadDeadline(time.Now().Add(5 * time.Second))
	dec := v1.NewDecoder(pr, v1.ToPlugin)
	prev := -1
	for i := 0; i < accepted; i++ {
		m, err := dec.Decode()
		if err != nil {
			t.Fatalf("frame %d of %d: %v", i, accepted, err)
		}
		vr, ok := m.(*v1.ViewResync)
		if !ok {
			t.Fatalf("frame %d: unexpected %T", i, m)
		}
		var n int
		if _, err := fmt.Sscanf(vr.ViewID, "v%d", &n); err != nil || n <= prev || (i == 0 && n != 0) {
			t.Fatalf("frame %d out of order: %s after v%04d", i, vr.ViewID, prev)
		}
		prev = n
	}
}

// A session that ends on its own must retire its queue with it: Send has to
// report "not running" rather than quietly filling a channel no pump drains
// (gh #208).
func TestSendAfterSessionEndsReportsNotRunning(t *testing.T) {
	r := NewRuntime(Candidate{Manifest: installHelper(t, "ok")}, helperOptions())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}
	_ = r.Send(&v1.HostShutdown{})
	waitRuntimeState(t, r, StateDisabled)

	err := r.Send(&v1.ViewResync{ViewID: "x"})
	if err == nil {
		t.Fatal("Send to an ended session returned nil")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Fatalf("Send after session end = %v, want \"plugin: not running\"", err)
	}
}

// Stop used to flush into that dead queue and wait out stopFlushTimeout for
// a barrier no pump would ever close. Retry and disable call Stop, so the
// cost landed on every teardown after a plugin had exited by itself (#208).
func TestStopAfterSessionEndsDoesNotWaitForFlush(t *testing.T) {
	r := NewRuntime(Candidate{Manifest: installHelper(t, "ok")}, helperOptions())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}
	_ = r.Send(&v1.HostShutdown{})
	waitRuntimeState(t, r, StateDisabled)

	start := time.Now()
	r.Stop()
	if elapsed := time.Since(start); elapsed >= 200*time.Millisecond {
		t.Fatalf("Stop after session end took %v; want well under the %v flush timeout", elapsed, stopFlushTimeout)
	}
}
