// Package services: idle holds the display-power policy. The Wayland objects
// live in internal/platform/wayland; this file owns when they should exist.
// idleMachine is pure: it turns inputs into decisions and never touches a
// socket, a process, or the compositor, so the re-arm edge is table-testable.
package services

import (
	"context"
	"log/slog"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"
	wayland "github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

// IdleBehavior names one policy timer. The value doubles as the opaque id the
// Wayland owner keys its notification objects by.
type IdleBehavior uint64

const (
	IdleBlank IdleBehavior = iota + 1
	IdleSuspend
)

const idleBehaviorCount = 2

// IdleDecision is one instruction produced by a policy input. The service
// forwards Arm to the Wayland owner (timeout 0 disarms) and runs Actions
// through the executors it was given.
type IdleDecision struct {
	Behavior IdleBehavior
	Arm      *time.Duration // non-nil: (re)create the notification at this timeout; 0 means disarm
	Action   IdleAction
}

// IdleAction names a side effect the policy wants taken.
type IdleAction uint8

const (
	IdleActionNone IdleAction = iota
	IdleActionBlank
	IdleActionUnblank
	IdleActionSuspend
)

// IdleSettings is the resolved idle configuration. A zero timeout disables
// that behavior on that power source.
type IdleSettings struct {
	BlankAc, BlankBattery     time.Duration
	SuspendAc, SuspendBattery time.Duration
	MediaExempt               bool
}

// idleMachine tracks, per behavior, the timeout the compositor was told about
// and whether the last event from it was idled.
type idleMachine struct {
	settings     IdleSettings
	onAC         bool
	mediaPlaying bool
	inhibited    bool
	armed        [idleBehaviorCount]time.Duration // 0: not armed
	idled        [idleBehaviorCount]bool
}

func idleIndex(b IdleBehavior) int { return int(b) - 1 }

// desired returns the timeout the policy wants live for b right now, or 0 for
// none. Inhibits and the media exemption take the timers away rather than
// extending them: a held inhibit means "not now", and gating explicitly beats
// trusting compositor-side suppression of the same object (DMS rule).
func (m *idleMachine) desired(b IdleBehavior) time.Duration {
	if m.inhibited || (m.settings.MediaExempt && m.mediaPlaying) {
		return 0
	}
	switch b {
	case IdleBlank:
		if m.onAC {
			return m.settings.BlankAc
		}
		return m.settings.BlankBattery
	case IdleSuspend:
		if m.onAC {
			return m.settings.SuspendAc
		}
		return m.settings.SuspendBattery
	}
	return 0
}

// idleResumeAction names what re-arming a currently-idled behavior must undo.
// Only blank has a visible undo; suspend's undo arrives through the logind
// wake path, which calls wake.
func idleResumeAction(b IdleBehavior) IdleAction {
	if b == IdleBlank {
		return IdleActionUnblank
	}
	return IdleActionNone
}

// transition computes the decisions for one behavior when its desired timeout
// changes. Re-arming while idled must run the resume action now: the fresh
// object will not answer for the old one's idled state, so a screen blanked
// under the previous arming would stay black (Noctalia idle_manager.cpp:170).
func (m *idleMachine) transition(b IdleBehavior) []IdleDecision {
	i := idleIndex(b)
	want := m.desired(b)
	cur := m.armed[i]
	if want == cur && !(want == 0 && m.idled[i]) {
		return nil
	}
	var out []IdleDecision
	if want == 0 {
		if cur != 0 {
			out = append(out, IdleDecision{Behavior: b, Arm: durationPtr(0)})
			m.armed[i] = 0
		}
		if m.idled[i] {
			out = append(out, IdleDecision{Behavior: b, Action: idleResumeAction(b)})
			m.idled[i] = false
		}
		return out
	}
	out = append(out, IdleDecision{Behavior: b, Arm: &want})
	m.armed[i] = want
	if m.idled[i] {
		out = append(out, IdleDecision{Behavior: b, Action: idleResumeAction(b)})
		m.idled[i] = false
	}
	return out
}

func (m *idleMachine) recompute() []IdleDecision {
	var out []IdleDecision
	for b := IdleBehavior(1); int(b) <= idleBehaviorCount; b++ {
		out = append(out, m.transition(b)...)
	}
	return out
}

func (m *idleMachine) setSettings(s IdleSettings) []IdleDecision {
	m.settings = s
	return m.recompute()
}

func (m *idleMachine) setPower(onAC bool) []IdleDecision {
	if m.onAC == onAC {
		return nil
	}
	m.onAC = onAC
	return m.recompute()
}

func (m *idleMachine) setMedia(playing bool) []IdleDecision {
	if m.mediaPlaying == playing {
		return nil
	}
	m.mediaPlaying = playing
	return m.recompute()
}

func (m *idleMachine) setInhibit(held bool) []IdleDecision {
	if m.inhibited == held {
		return nil
	}
	m.inhibited = held
	return m.recompute()
}

// idleEvent reports the compositor's verdict for an armed behavior.
func (m *idleMachine) idleEvent(b IdleBehavior, idled bool) []IdleDecision {
	i := idleIndex(b)
	if idled {
		if m.armed[i] == 0 || m.idled[i] {
			return nil // stale or duplicate: an un-armed object cannot go idle
		}
		m.idled[i] = true
		if b == IdleBlank {
			return []IdleDecision{{Behavior: b, Action: IdleActionBlank}}
		}
		return []IdleDecision{{Behavior: b, Action: IdleActionSuspend}}
	}
	if !m.idled[i] {
		return nil
	}
	m.idled[i] = false
	if act := idleResumeAction(b); act != IdleActionNone {
		return []IdleDecision{{Behavior: b, Action: act}}
	}
	return nil
}

// wake is the post-suspend path: after a real sleep every action state is
// suspect, so blanked screens are raised and every armed behavior is
// re-armed, because the compositor's idle state did not survive the sleep.
func (m *idleMachine) wake() []IdleDecision {
	var out []IdleDecision
	for b := IdleBehavior(1); int(b) <= idleBehaviorCount; b++ {
		i := idleIndex(b)
		if m.idled[i] {
			if act := idleResumeAction(b); act != IdleActionNone {
				out = append(out, IdleDecision{Behavior: b, Action: act})
			}
			m.idled[i] = false
		}
		if m.armed[i] != 0 {
			d := m.armed[i]
			out = append(out, IdleDecision{Behavior: b, Arm: &d})
		}
	}
	return out
}

func durationPtr(d time.Duration) *time.Duration { return &d }

// IdleExecutors are the side effects. Blank and Unblank are wired to the niri
// monitor-power actions by sysc-718; until then the shell passes executors
// that log, which is also the live-verification line for sysc-695.
type IdleExecutors struct {
	Blank   func()
	Unblank func()
	Suspend func()
}

// IdleService runs the machine on its own goroutine. Setters are safe from
// any goroutine; decisions flow to the Wayland owner through Requests and
// events flow back through Events, both connected by Callbacks.
type IdleService struct {
	machine  idleMachine
	reqs     chan wayland.IdleRequest
	evs      chan wayland.IdleEvent
	inputs   chan func(*idleMachine)
	execs    IdleExecutors
	pollRate time.Duration
	// readBattery is a seam; tests replace it. nil means "always AC" and is
	// correct on a desktop without a battery: a failed read is AC, not an
	// error state, because the timer choice must never depend on telemetry.
	readBattery func() (metrics.BatterySnapshot, error)
}

// IdleOptions configures NewIdleService. Zero values choose the defaults:
// 16-slot request and 32-slot event channels and a 30 s battery poll.
type IdleOptions struct {
	Requests chan wayland.IdleRequest
	Events   chan wayland.IdleEvent
	Execs    IdleExecutors
	// ReadBattery overrides the sysfs battery read. Nil uses metrics.ReadBattery.
	ReadBattery func() (metrics.BatterySnapshot, error)
	// PollRate overrides the battery poll interval. Zero means 30 s.
	PollRate time.Duration
}

func NewIdleService(opt IdleOptions) *IdleService {
	reqs := opt.Requests
	if reqs == nil {
		reqs = make(chan wayland.IdleRequest, 16)
	}
	evs := opt.Events
	if evs == nil {
		evs = make(chan wayland.IdleEvent, 32)
	}
	read := opt.ReadBattery
	if read == nil {
		read = metrics.ReadBattery
	}
	rate := opt.PollRate
	if rate <= 0 {
		rate = 30 * time.Second
	}
	return &IdleService{
		reqs:        reqs,
		evs:         evs,
		inputs:      make(chan func(*idleMachine), 16),
		execs:       opt.Execs,
		pollRate:    rate,
		readBattery: read,
	}
}

// Requests and Events are the two channels Callbacks wires to the owner.
func (s *IdleService) Requests() <-chan wayland.IdleRequest { return s.reqs }
func (s *IdleService) Events() chan<- wayland.IdleEvent     { return s.evs }

// SetConfig posts the resolved idle configuration.
func (s *IdleService) SetConfig(set IdleSettings) {
	s.post(func(m *idleMachine) { s.apply(m, m.setSettings(set)) })
}

// SetMediaPlaying posts the MPRIS exemption input.
func (s *IdleService) SetMediaPlaying(playing bool) {
	s.post(func(m *idleMachine) { s.apply(m, m.setMedia(playing)) })
}

// SetInhibited posts the caffeine input; held inhibitors disarm everything.
func (s *IdleService) SetInhibited(held bool) {
	s.post(func(m *idleMachine) { s.apply(m, m.setInhibit(held)) })
}

// SetOnAC posts the power-source input; it selects the timeout variant.
func (s *IdleService) SetOnAC(onAC bool) {
	s.post(func(m *idleMachine) { s.apply(m, m.setPower(onAC)) })
}

// Wake posts the post-suspend recovery: raise blanked screens, re-arm timers.
// sysc-717's logind subscription calls this on PrepareForSleep(false).
func (s *IdleService) Wake() {
	s.post(func(m *idleMachine) { s.apply(m, m.wake()) })
}

func (s *IdleService) post(f func(*idleMachine)) { s.inputs <- f }

// Run owns the loop until ctx ends. The machine is only touched here, so no
// lock is needed; every setter's closure executes in this goroutine.
// ponytail: actions run inline, so executors must be fast (spawn, never
// block). If a blocking executor appears, move action dispatch to its own
// queue goroutine so a slow suspend cannot stall event drain.
func (s *IdleService) Run(ctx context.Context) {
	ticker := time.NewTicker(s.pollRate)
	defer ticker.Stop()
	s.refreshPower()
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-s.inputs:
			f(&s.machine)
		case ev := <-s.evs:
			s.apply(&s.machine, s.machine.idleEvent(IdleBehavior(ev.ID), ev.Idled))
		case <-ticker.C:
			s.refreshPower()
		}
	}
}

// refreshPower maps a battery snapshot onto the AC/battery policy input. No
// readable battery means a desktop: AC.
func (s *IdleService) refreshPower() {
	onAC := true
	if snap, err := s.readBattery(); err == nil {
		onAC = snap.State == metrics.BatteryCharging || snap.State == metrics.BatteryFull
	}
	s.SetOnAC(onAC)
}

func (s *IdleService) apply(m *idleMachine, decisions []IdleDecision) {
	for _, d := range decisions {
		if d.Arm != nil {
			timeoutMS := uint32(*d.Arm / time.Millisecond)
			s.reqs <- wayland.IdleRequest{ID: uint64(d.Behavior), TimeoutMS: timeoutMS}
			continue
		}
		switch d.Action {
		case IdleActionBlank:
			s.run(s.execs.Blank)
		case IdleActionUnblank:
			s.run(s.execs.Unblank)
		case IdleActionSuspend:
			s.run(s.execs.Suspend)
		}
	}
}

func (s *IdleService) run(f func()) {
	if f != nil {
		f()
		return
	}
	slog.Debug("idle: action with no executor dropped")
}
