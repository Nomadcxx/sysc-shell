package shell

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/notifyclient"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

// regionRecorder captures every aux request the toast host emits, keeping the
// target output so an input region can be checked against that output's real
// size. The stock hostHarness drops the output and cannot do that.
type regionRecorder struct {
	mu      sync.Mutex
	updates []wayland.AuxRequest
	closes  []string
	opens   int
}

func (rr *regionRecorder) request(req wayland.AuxRequest) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	switch {
	case req.Open != nil:
		rr.opens++
	case req.Update != nil:
		rr.updates = append(rr.updates, req)
	default:
		rr.closes = append(rr.closes, req.ID)
	}
}

func (rr *regionRecorder) inputRegions() []wayland.AuxRequest {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	var out []wayland.AuxRequest
	for _, u := range rr.updates {
		if u.Update.SetInputRegion {
			out = append(out, u)
		}
	}
	return out
}

// TestToastInputRegionsStayInBoundsUnderBurst drives the plugin path's shape --
// one unique notification id per alert, hundreds in a burst -- through the
// toast host while outputs disappear and the daemon connection drops and
// resyncs, and asserts every input region the host publishes stays a valid,
// in-bounds rectangle for its output.
//
// The captured Wayland error names wl_compositor.create_region, whose request
// carries only a new object ID. Rectangle coordinates arrive later through
// wl_region.add. This test checks the toast host's bounds contract under the
// burst the ai-usage plugin produces; it does not explain the object-ID error.
func TestToastInputRegionsStayInBoundsUnderBurst(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	keepInvalidationsDrained(t, r)
	rec := &regionRecorder{}
	h := newToastHost(r, &hostHarness{})
	h.request = rec.request
	r.toasts = h

	const outputGlobal uint32 = 5
	const outputW, outputH = 1920, 1080
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": outputGlobal})
	if err := h.configure("eDP-1", outputW, outputH, 120); err != nil {
		t.Fatal(err)
	}

	check := func(stage string) {
		t.Helper()
		for _, u := range rec.inputRegions() {
			if u.Output != outputGlobal {
				continue // an output we have not sized; nothing to check against
			}
			for _, rect := range u.Update.InputRects {
				switch {
				case rect.W <= 0 || rect.H <= 0:
					t.Fatalf("%s: emitted an empty/negative input rect %+v", stage, rect)
				case rect.X < 0 || rect.Y < 0:
					t.Fatalf("%s: emitted a negative-origin input rect %+v", stage, rect)
				case rect.X+rect.W > outputW || rect.Y+rect.H > outputH:
					t.Fatalf("%s: input rect %+v leaves the %dx%d output", stage, rect, outputW, outputH)
				}
			}
		}
	}

	// Phase 1: a burst of unique notifications, the shape the plugin path uses.
	seq := uint64(1)
	notes := make([]protocol.Notification, 0, 300)
	for i := 1; i <= 300; i++ {
		notes = append(notes, note(uint32(i), fmt.Sprintf("alert %d", i)))
	}
	r.applyNotify(snap(seq, notes...))
	check("snapshot burst")

	// Phase 2: churn closes and additions on top of the live stack, so the
	// layout height changes every step and cards move between visible/queued.
	for i := 0; i < 300; i++ {
		seq++
		if i%2 == 0 {
			r.applyNotify(delta(1, seq, protocol.Delta{Kind: protocol.DeltaClosed, ID: uint32(i + 1)}))
		} else {
			id := uint32(1000 + i)
			r.applyNotify(delta(1, seq, protocol.Delta{
				Kind:         protocol.DeltaAdded,
				Notification: ptr(note(id, fmt.Sprintf("alert %d", id))),
				Lifetime:     &protocol.Lifetime{ID: id, DurationMS: 5000, RemainingMS: 5000, Running: true},
			}))
		}
	}
	check("churn")

	// Phase 3: the output goes away and comes back mid-burst. A surface that
	// is gone must not keep receiving sized regions.
	h.syncOutputs(map[string]uint32{})
	check("output lost")
	h.syncOutputs(map[string]uint32{"eDP-1": outputGlobal})
	if got := rec.opens; got != 2 {
		t.Fatalf("surface opens = %d, want one initial and one after the output returned", got)
	}

	// Phase 4: the daemon connection drops and a fresh generation resyncs, so
	// the projection is cleared and rebuilt under the host.
	r.applyNotify(notifyclient.Message{Generation: 1, Kind: notifyclient.KindDisconnected})
	r.applyNotify(snap(1, note(7, "after resync")))
	check("resync")

	// Phase 5: suppression (DND) with the stack still populated, then lift it.
	r.notify.mu.Lock()
	r.notify.dnd = true
	r.notify.mu.Unlock()
	h.recompute()
	check("dnd on")
	r.notify.mu.Lock()
	r.notify.dnd = false
	r.notify.mu.Unlock()
	h.recompute()
	check("dnd off")

	// Every recorded region update must still target a live surface, and the
	// host must not have leaked per-output state through all that churn.
	if len(h.outputs) != 1 {
		t.Fatalf("open outputs = %v, want just eDP-1", h.outputs)
	}
}

// TestToastHostBurstWithLeaseRenewStaysResponsive runs the pump, the toast
// callbacks, output bookkeeping, and the presentation lease renewer at once
// under a burst, and fails if the host wedges. It extends the lock-order test
// with the renew goroutine, which publishes presentation while holding
// Registry.mu.
func TestToastHostBurstWithLeaseRenewStaysResponsive(t *testing.T) {
	r, h, _ := wiredToast(t)
	keepInvalidationsDrained(t, r)
	r.applyNotify(snap(1, note(1, "one")))
	callbacks := h.harness().opens[0].Callbacks
	if err := callbacks.Configure(1920, 1080, 120); err != nil {
		t.Fatal(err)
	}
	h.startLeaseRenew(5 * time.Millisecond)
	t.Cleanup(h.stopLeaseRenew)

	deadline := time.Now().Add(time.Second)
	var wg sync.WaitGroup
	pumpDone := make(chan struct{})
	go func() {
		defer close(pumpDone)
		for i := 0; time.Now().Before(deadline); i++ {
			id := uint32(100 + i)
			r.applyNotify(delta(1, uint64(i+2), protocol.Delta{
				Kind:         protocol.DeltaAdded,
				Notification: ptr(note(id, "alert")),
				Lifetime:     &protocol.Lifetime{ID: id, DurationMS: 5000, RemainingMS: 5000, Running: true},
			}))
			r.applyNotify(delta(1, uint64(i+3), protocol.Delta{Kind: protocol.DeltaClosed, ID: id}))
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 1920*1080*4)
		for time.Now().Before(deadline) {
			if err := callbacks.Render(buf, 1920, 1080, 1920*4); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-pumpDone:
	case <-time.After(10 * time.Second):
		t.Fatal("toast host wedged under burst with lease renew")
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("toast render wedged under burst with lease renew")
	}
}
