package shell

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

// The lock order is Registry.mu, then notifyState.mu: every path that holds
// Registry.mu may take the projection lock, and every projection-lock holder
// releases it before taking Registry.mu (GH #37). This drives the notify pump,
// the toast surface callbacks, centre open/close and output sync at once, so
// an inverted path deadlocks here instead of on a desktop.
func TestNotifyAndToastCallbacksDoNotDeadlock(t *testing.T) {
	r, h, _ := wiredToast(t)
	keepInvalidationsDrained(t, r)
	r.applyNotify(snap(1, note(1, "one"), note(2, "two")))
	callbacks := h.harness().opens[0].Callbacks
	const width, height = 1200, 800
	stride := width * 4
	if err := callbacks.Configure(width, height, 120); err != nil {
		t.Fatal(err)
	}

	run := 2 * time.Second
	if testing.Short() {
		run = 200 * time.Millisecond
	}
	watchdog := time.AfterFunc(run+10*time.Second, func() {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		panic(fmt.Sprintf("notify/toast lock-order deadlock:\n%s", buf[:n]))
	})
	defer watchdog.Stop()

	deadline := time.Now().Add(run)
	var wg sync.WaitGroup
	loop := func(body func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; time.Now().Before(deadline); i++ {
				body(i)
			}
		}()
	}

	loop(func(i int) { // the notify pump
		seq := uint64(i*3 + 2)
		r.applyNotify(snap(seq, note(1, "one"), note(2, "two")))
		r.applyNotify(delta(1, seq+1, protocol.Delta{Kind: protocol.DeltaAdded, Notification: ptr(note(3, "three")),
			Lifetime: &protocol.Lifetime{ID: 3, DurationMS: 5000, RemainingMS: 5000, Running: true}}))
		r.applyNotify(delta(1, seq+2, protocol.Delta{Kind: protocol.DeltaClosed, ID: 3}))
	})
	loop(func(i int) { // the Wayland owner painting and routing the pointer
		buf := make([]byte, stride*height)
		if err := callbacks.Configure(width, height, 120); err != nil {
			t.Error(err)
		}
		if err := callbacks.Render(buf, width, height, stride); err != nil {
			t.Error(err)
		}
		callbacks.Handle(wayland.Event{Kind: wayland.EventPointerMotion, X: float64(width - 100 - i%50), Y: float64(40 + i%80)})
	})
	loop(func(i int) { // the centre opening and closing, under Registry.mu
		r.mu.Lock()
		r.setCenterOpen(i%2 == 0)
		r.mu.Unlock()
	})
	loop(func(int) { // output bookkeeping
		r.SyncToastOutputs(map[string]uint32{"eDP-1": 5})
	})
	wg.Wait()
}
