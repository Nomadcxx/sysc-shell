package plugin

import (
	"fmt"
	"os"
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
	r.mu.Lock()
	r.session = &Session{enc: v1.NewEncoder(pw)}
	r.mu.Unlock()

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
