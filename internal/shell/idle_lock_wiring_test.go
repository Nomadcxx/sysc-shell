package shell

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The whole idle-lock chain, minus the compositor: choosing "Lock" after 5m
// in Settings arms a 5 minute ext-idle-notify object, and the compositor's
// idled verdict for it spawns the configured locker. Main wires the executor
// the same way (cmd/sysc-shell/main.go).
func TestSettingsIdleLockArmsTimerAndLocks(t *testing.T) {
	r, h, _ := newOpenIdleSettings(t, readyWallsSnapshot(), "swaylock")
	var spawns atomic.Int32
	exits := make(chan int, 1)
	t.Cleanup(func() { exits <- 0 })
	r.mu.Lock()
	r.lockerSpawn = func([]string) (io.ReadCloser, <-chan int, error) {
		spawns.Add(1)
		return io.NopCloser(&io.LimitedReader{}), exits, nil
	}
	r.mu.Unlock()

	svc := services.NewIdleService(services.IdleOptions{
		Execs: services.IdleExecutors{Lock: func() {
			if err := r.LockTracked(); err != nil {
				t.Errorf("idle lock: %v", err)
			}
		}},
		ReadBattery: func() (metrics.BatterySnapshot, error) { return metrics.BatterySnapshot{}, errors.New("no battery") },
	})
	r.SetIdleService(svc)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go svc.Run(ctx)

	r.mu.Lock()
	applyIdleForTest(t, r, h, &ui.Node{Kind: ui.KindMenu, Action: "set:idle.after", Text: "lock"})
	applyIdleForTest(t, r, h, &ui.Node{Kind: ui.KindTextField, Action: "set:idle.delay", Text: "5m"})
	if r.cfg.Idle.Lock != 5*time.Minute {
		r.mu.Unlock()
		t.Fatalf("cfg.Idle.Lock = %v, want 5m", r.cfg.Idle.Lock)
	}
	r.mu.Unlock()

	want := wayland.IdleRequest{ID: uint64(services.IdleLock), TimeoutMS: 300_000}
	deadline := time.After(2 * time.Second)
	for armed := false; !armed; {
		select {
		case req := <-svc.Requests():
			armed = req == want
		case <-deadline:
			t.Fatalf("no %+v arm request after choosing Lock in Settings", want)
		}
	}

	svc.Events() <- wayland.IdleEvent{ID: uint64(services.IdleLock), Idled: true}
	waitFor(t, func() bool { return spawns.Load() == 1 })
}
