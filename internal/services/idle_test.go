package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"
	wayland "github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

func fmtDecisions(ds []IdleDecision) string {
	if len(ds) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(ds))
	for _, d := range ds {
		name := "blank"
		if d.Behavior == IdleSuspend {
			name = "suspend"
		}
		switch {
		case d.Arm != nil && *d.Arm == 0:
			parts = append(parts, name+":disarm")
		case d.Arm != nil:
			parts = append(parts, name+":arm="+d.Arm.String())
		case d.Action == IdleActionBlank:
			parts = append(parts, name+":blank")
		case d.Action == IdleActionUnblank:
			parts = append(parts, name+":unblank")
		case d.Action == IdleActionSuspend:
			parts = append(parts, name+":suspend")
		}
	}
	return strings.Join(parts, ",")
}

type idleStep struct {
	desc string
	run  func(m *idleMachine) []IdleDecision
	want string
}

func runIdleSteps(t *testing.T, onAC bool, set IdleSettings, steps []idleStep) {
	t.Helper()
	m := &idleMachine{settings: set, onAC: onAC}
	for _, s := range steps {
		if got := fmtDecisions(s.run(m)); got != s.want {
			t.Errorf("%s:\n got %s\nwant %s", s.desc, got, s.want)
		}
	}
}

func TestIdleMachine(t *testing.T) {
	tests := []struct {
		name  string
		onAC  bool
		set   IdleSettings
		steps []idleStep
	}{
		{
			name: "inhibit held disarms everything and release re-arms",
			onAC: true,
			set:  IdleSettings{BlankAc: 10 * time.Minute, SuspendAc: 30 * time.Minute, MediaExempt: true},
			steps: []idleStep{
				{"config", func(m *idleMachine) []IdleDecision { return m.setSettings(m.settings) },
					"blank:arm=10m0s,suspend:arm=30m0s"},
				{"caffeine on", func(m *idleMachine) []IdleDecision { return m.setInhibit(true) },
					"blank:disarm,suspend:disarm"},
				{"caffeine off", func(m *idleMachine) []IdleDecision { return m.setInhibit(false) },
					"blank:arm=10m0s,suspend:arm=30m0s"},
			},
		},
		{
			name: "media exemption removes the timers while playing",
			onAC: true,
			set:  IdleSettings{BlankAc: 10 * time.Minute, SuspendAc: 30 * time.Minute, MediaExempt: true},
			steps: []idleStep{
				{"config", func(m *idleMachine) []IdleDecision { return m.setSettings(m.settings) },
					"blank:arm=10m0s,suspend:arm=30m0s"},
				{"play", func(m *idleMachine) []IdleDecision { return m.setMedia(true) },
					"blank:disarm,suspend:disarm"},
				{"stop", func(m *idleMachine) []IdleDecision { return m.setMedia(false) },
					"blank:arm=10m0s,suspend:arm=30m0s"},
			},
		},
		{
			name: "media exemption off ignores playback",
			onAC: true,
			set:  IdleSettings{BlankAc: 10 * time.Minute},
			steps: []idleStep{
				{"config", func(m *idleMachine) []IdleDecision { return m.setSettings(m.settings) },
					"blank:arm=10m0s"},
				{"play", func(m *idleMachine) []IdleDecision { return m.setMedia(true) }, "-"},
			},
		},
		{
			name: "power source switch re-arms with the other timeout",
			onAC: true,
			set: IdleSettings{
				BlankAc: 10 * time.Minute, BlankBattery: 4 * time.Minute,
				SuspendAc: 30 * time.Minute, SuspendBattery: 10 * time.Minute,
			},
			steps: []idleStep{
				{"config", func(m *idleMachine) []IdleDecision { return m.setSettings(m.settings) },
					"blank:arm=10m0s,suspend:arm=30m0s"},
				{"unplug", func(m *idleMachine) []IdleDecision { return m.setPower(false) },
					"blank:arm=4m0s,suspend:arm=10m0s"},
				{"plug in", func(m *idleMachine) []IdleDecision { return m.setPower(true) },
					"blank:arm=10m0s,suspend:arm=30m0s"},
			},
		},
		{
			name: "idle and resume events drive actions",
			onAC: true,
			set:  IdleSettings{BlankAc: 10 * time.Minute, SuspendAc: 30 * time.Minute},
			steps: []idleStep{
				{"config", func(m *idleMachine) []IdleDecision { return m.setSettings(m.settings) },
					"blank:arm=10m0s,suspend:arm=30m0s"},
				{"blank idle", func(m *idleMachine) []IdleDecision { return m.idleEvent(IdleBlank, true) }, "blank:blank"},
				{"blank idle again", func(m *idleMachine) []IdleDecision { return m.idleEvent(IdleBlank, true) }, "-"},
				{"blank resume", func(m *idleMachine) []IdleDecision { return m.idleEvent(IdleBlank, false) }, "blank:unblank"},
				{"blank resume again", func(m *idleMachine) []IdleDecision { return m.idleEvent(IdleBlank, false) }, "-"},
				{"suspend idle", func(m *idleMachine) []IdleDecision { return m.idleEvent(IdleSuspend, true) }, "suspend:suspend"},
				{"suspend resume has no action", func(m *idleMachine) []IdleDecision { return m.idleEvent(IdleSuspend, false) }, "-"},
			},
		},
		{
			name: "event for an un-armed behavior is stale",
			onAC: true,
			steps: []idleStep{
				{"idle blank unarmed", func(m *idleMachine) []IdleDecision { return m.idleEvent(IdleBlank, true) }, "-"},
				{"resume blank unarmed", func(m *idleMachine) []IdleDecision { return m.idleEvent(IdleBlank, false) }, "-"},
			},
		},
		{
			name: "re-arming while idled unblanks at re-arm time",
			onAC: true,
			set:  IdleSettings{BlankAc: 10 * time.Minute},
			steps: []idleStep{
				{"config", func(m *idleMachine) []IdleDecision { return m.setSettings(m.settings) }, "blank:arm=10m0s"},
				{"idle", func(m *idleMachine) []IdleDecision { return m.idleEvent(IdleBlank, true) }, "blank:blank"},
				// The trap (Noctalia idle_manager.cpp:170): the fresh timer will
				// never emit resumed for the old one's idled state, so the
				// resume action must run NOW or the screen stays black.
				{"timeout changed while still idled", func(m *idleMachine) []IdleDecision {
					return m.setSettings(IdleSettings{BlankAc: 20 * time.Minute})
				}, "blank:arm=20m0s,blank:unblank"},
				{"late resume event does not unblank twice", func(m *idleMachine) []IdleDecision { return m.idleEvent(IdleBlank, false) }, "-"},
			},
		},
		{
			name: "policy switched off while black raises the screen",
			onAC: true,
			set:  IdleSettings{BlankAc: 10 * time.Minute},
			steps: []idleStep{
				{"config", func(m *idleMachine) []IdleDecision { return m.setSettings(m.settings) }, "blank:arm=10m0s"},
				{"idle", func(m *idleMachine) []IdleDecision { return m.idleEvent(IdleBlank, true) }, "blank:blank"},
				{"disable", func(m *idleMachine) []IdleDecision { return m.setSettings(IdleSettings{}) }, "blank:disarm,blank:unblank"},
			},
		},
		{
			name: "inhibit taken while black disarms and raises",
			onAC: true,
			set:  IdleSettings{BlankAc: 10 * time.Minute},
			steps: []idleStep{
				{"config", func(m *idleMachine) []IdleDecision { return m.setSettings(m.settings) }, "blank:arm=10m0s"},
				{"idle", func(m *idleMachine) []IdleDecision { return m.idleEvent(IdleBlank, true) }, "blank:blank"},
				{"caffeine on", func(m *idleMachine) []IdleDecision { return m.setInhibit(true) }, "blank:disarm,blank:unblank"},
			},
		},
		{
			name: "wake raises the screen and re-arms every timer",
			onAC: true,
			set:  IdleSettings{BlankAc: 10 * time.Minute, SuspendAc: 30 * time.Minute},
			steps: []idleStep{
				{"config", func(m *idleMachine) []IdleDecision { return m.setSettings(m.settings) },
					"blank:arm=10m0s,suspend:arm=30m0s"},
				{"blank idle", func(m *idleMachine) []IdleDecision { return m.idleEvent(IdleBlank, true) }, "blank:blank"},
				{"suspend idle", func(m *idleMachine) []IdleDecision { return m.idleEvent(IdleSuspend, true) }, "suspend:suspend"},
				{"wake from sleep", func(m *idleMachine) []IdleDecision { return m.wake() },
					"blank:unblank,blank:arm=10m0s,suspend:arm=30m0s"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runIdleSteps(t, tt.onAC, tt.set, tt.steps)
		})
	}
}

func TestIdleServicePlumbing(t *testing.T) {
	reqs := make(chan wayland.IdleRequest, 8)
	evs := make(chan wayland.IdleEvent, 8)
	blanks := make(chan struct{}, 4)
	unblanks := make(chan struct{}, 4)
	svc := NewIdleService(IdleOptions{
		Requests: reqs,
		Events:   evs,
		Execs: IdleExecutors{
			Blank:   func() { blanks <- struct{}{} },
			Unblank: func() { unblanks <- struct{}{} },
		},
		ReadBattery: func() (metrics.BatterySnapshot, error) {
			return metrics.BatterySnapshot{}, errors.New("no battery: desktop")
		},
		PollRate: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.Run(ctx)

	svc.SetConfig(IdleSettings{BlankAc: 10 * time.Minute})
	requireReq(t, reqs, wayland.IdleRequest{ID: uint64(IdleBlank), TimeoutMS: 600000})

	evs <- wayland.IdleEvent{ID: uint64(IdleBlank), Idled: true}
	waitSignal(t, blanks, "blank executor")

	svc.SetInhibited(true)
	requireReq(t, reqs, wayland.IdleRequest{ID: uint64(IdleBlank), TimeoutMS: 0})
	waitSignal(t, unblanks, "unblank at disarm while idled")
}

func requireReq(t *testing.T, ch <-chan wayland.IdleRequest, want wayland.IdleRequest) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Errorf("request = %+v, want %+v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no request %+v within 2s", want)
	}
}

func waitSignal(t *testing.T, ch <-chan struct{}, want string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not run within 2s", want)
	}
}

// A full request queue must not wedge Run: setters keep returning, and an
// arm that had to be dropped is reissued on the next state change once the
// queue drains. gh #59.
func TestIdleServiceStalledRequestOwnerCannotWedge(t *testing.T) {
	reqs := make(chan wayland.IdleRequest, 1)
	reqs <- wayland.IdleRequest{ID: 99} // occupy the queue so every arm is dropped
	svc := NewIdleService(IdleOptions{
		Requests: reqs,
		ReadBattery: func() (metrics.BatterySnapshot, error) {
			return metrics.BatterySnapshot{}, errors.New("no battery: desktop")
		},
		PollRate: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.Run(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.SetConfig(IdleSettings{BlankAc: 10 * time.Minute})
		for i := 0; i < 24; i++ { // more posts than the inputs buffer, Run must keep draining
			svc.SetInhibited(i%2 == 0)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("setters stalled while the request queue stayed full")
	}

	if got := <-reqs; got.ID != 99 { // free the queue
		t.Fatalf("queued request = %+v, want the placeholder", got)
	}
	svc.SetInhibited(false) // the dropped arm must be reissued here
	requireReq(t, reqs, wayland.IdleRequest{ID: uint64(IdleBlank), TimeoutMS: 600000})
}
