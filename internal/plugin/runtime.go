package plugin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// The automatic-restart policy.
const (
	// MaxAutoStarts is how many starts one plugin gets inside RestartWindow.
	MaxAutoStarts = 3
	// RestartWindow is the rolling failure window.
	RestartWindow = 60 * time.Second
	// StableRun is how long a process must run before its life is taken as
	// evidence that it works. A plugin that has run this long and then dies is
	// not a crash loop.
	StableRun = 5 * time.Minute
)

// State is the lifecycle state the manager shows for one plugin.
type State string

const (
	// StateDisabled is not running, by the user's choice or before a start.
	StateDisabled State = "disabled"
	// StateStarting is between the process starting and the handshake.
	StateStarting State = "starting"
	// StateRunning is handshaken and serving views.
	StateRunning State = "running"
	// StateDegraded is running but suppressed for exceeding a budget.
	StateDegraded State = "degraded"
	// StateFailed is stopped after exhausting its restart budget, or after a
	// fault a restart could not fix.
	StateFailed State = "failed"
	// StateIncompatible is a plugin speaking a protocol this shell does not.
	// It is separate from failed because it is permanent: another start would
	// produce the same answer.
	StateIncompatible State = "incompatible"
	// StateMissingDependency is a valid plugin whose declared command is not
	// installed. It stays visible so the user can see what to install.
	StateMissingDependency State = "missing-dependency"
)

// Status is one plugin's state as the manager renders it.
type Status struct {
	State State
	PID   int
	// Failure is the last reason, empty while healthy.
	Failure string
	// Starts counts process starts in the current failure window.
	Starts int
	// Stderr is the retained tail from the most recent process.
	Stderr []byte
	// Since is when the current state began.
	Since time.Time
}

// RuntimeOptions configures one runtime.
type RuntimeOptions struct {
	Supported        []Capability
	Limits           v1.Limits
	HandshakeTimeout time.Duration
	ShutdownGrace    time.Duration
	// Now is the clock, injected so restart policy is testable without
	// waiting out a sixty-second window.
	Now func() time.Time
}

func (o RuntimeOptions) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// Runtime supervises one enabled plugin across restarts.
//
// It owns the answer to "should we try again", which the Supervisor
// deliberately does not: a start that failed and a plugin that should be given
// up on are different questions, and keeping them apart is what lets a
// permanent fault stop the loop immediately while a transient one gets its
// budget.
type Runtime struct {
	candidate Candidate
	opts      RuntimeOptions
	messages  chan v1.Message

	mu      sync.Mutex
	state   State
	since   time.Time
	failure string
	starts  int
	stderr  []byte
	session *Session
	disp    *Dispatcher
	budget  *restartBudget
	// generation rises on every deliberate stop, so a supervise goroutine
	// belonging to a previous life cannot restart a plugin the user disabled.
	generation int
	stopping   bool
}

// NewRuntime returns a stopped runtime for one discovered candidate.
func NewRuntime(c Candidate, opts RuntimeOptions) *Runtime {
	return &Runtime{
		candidate: c,
		opts:      opts,
		messages:  make(chan v1.Message, 32),
		state:     StateDisabled,
		since:     opts.now(),
		budget:    newRestartBudget(),
	}
}

// Messages carries every message the plugin sends after its handshake. The
// read loop has to put what it reads somewhere, and delivering it is the only
// alternative to discarding a view the plugin meant the user to see.
func (r *Runtime) Messages() <-chan v1.Message { return r.messages }

// SetCalls attaches the host-call dispatcher. Calls are answered off the
// shell's message pump so a slow notify cannot stall view updates.
func (r *Runtime) SetCalls(d *Dispatcher) {
	r.mu.Lock()
	r.disp = d
	r.mu.Unlock()
}

// Send writes one host message to the live session.
func (r *Runtime) Send(m v1.Message) error {
	r.mu.Lock()
	sess := r.session
	r.mu.Unlock()
	if sess == nil {
		return errors.New("plugin: not running")
	}
	return sess.Send(m)
}

// Allows reports whether the live session was granted c.
func (r *Runtime) Allows(c Capability) bool {
	r.mu.Lock()
	sess := r.session
	r.mu.Unlock()
	if sess == nil {
		return false
	}
	return sess.Allows(c)
}

// Manifest is the validated declaration this runtime serves.
func (r *Runtime) Manifest() Manifest { return r.candidate.Manifest }

// Status reports the current lifecycle state.
func (r *Runtime) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := Status{
		State:   r.state,
		Failure: r.failure,
		Starts:  r.starts,
		Stderr:  r.stderr,
		Since:   r.since,
	}
	if r.session != nil {
		st.PID = r.session.PID()
		if len(st.Stderr) == 0 {
			st.Stderr = r.session.Stderr()
		}
	}
	return st
}

// Start launches the plugin and supervises it.
//
// A candidate that cannot run is refused with the state that says why, rather
// than started and allowed to fail: a missing dependency and a malformed
// manifest are conditions the user has to act on, and a restart cannot change
// either.
func (r *Runtime) Start(ctx context.Context) error {
	if r.candidate.Err != nil {
		r.setState(StateFailed, r.candidate.Err.Error())
		return fmt.Errorf("plugin: %s was rejected during discovery: %w", r.candidate.Dir, r.candidate.Err)
	}
	if missing := r.candidate.MissingCommands; len(missing) > 0 {
		reason := fmt.Sprintf("needs %v, which %s not installed", missing, plural(len(missing), "is", "are"))
		r.setState(StateMissingDependency, reason)
		return fmt.Errorf("plugin: %s %s", r.candidate.Manifest.ID, reason)
	}

	r.mu.Lock()
	r.stopping = false
	r.mu.Unlock()
	return r.launch(ctx)
}

// Retry restarts a plugin after the user asked for it.
//
// An explicit decision resets the budget the automatic restarts spent. The
// budget exists to stop the shell looping on its own, not to make a user argue
// with it after they have fixed something.
func (r *Runtime) Retry(ctx context.Context) error {
	r.Stop()
	r.mu.Lock()
	r.budget = newRestartBudget()
	r.starts = 0
	r.failure = ""
	r.mu.Unlock()
	return r.Start(ctx)
}

// launch performs one start attempt and, on success, supervises the session.
func (r *Runtime) launch(ctx context.Context) error {
	now := r.opts.now()

	r.mu.Lock()
	if !r.budget.allow(now) {
		r.mu.Unlock()
		reason := fmt.Sprintf("stopped after %d starts within %v", MaxAutoStarts, RestartWindow)
		r.setState(StateFailed, reason)
		return errors.New("plugin: " + reason)
	}
	r.starts++
	generation := r.generation
	r.mu.Unlock()

	r.setState(StateStarting, "")

	sup := &Supervisor{
		Manifest:         r.candidate.Manifest,
		Supported:        r.opts.Supported,
		Limits:           r.opts.Limits,
		HandshakeTimeout: r.opts.HandshakeTimeout,
		ShutdownGrace:    r.opts.ShutdownGrace,
	}
	sess, err := sup.Start(ctx)
	if err != nil {
		var incompatible *IncompatibleError
		if errors.As(err, &incompatible) {
			r.setState(StateIncompatible, err.Error())
			return err
		}
		r.setState(StateFailed, err.Error())
		return err
	}

	r.mu.Lock()
	if r.generation != generation || r.stopping {
		// The user disabled this plugin while it was starting.
		r.mu.Unlock()
		sess.Close()
		return nil
	}
	r.session = sess
	r.mu.Unlock()

	r.setState(StateRunning, "")
	go r.supervise(ctx, sess, generation)
	return nil
}

// supervise drains the plugin's output and decides what a dead process means.
//
// It runs outside the runtime's mutex. A plugin's writes are its own business;
// holding the lock across them would let one slow process stall every status
// read the manager makes.
func (r *Runtime) supervise(ctx context.Context, sess *Session, generation int) {
	for {
		msg, err := sess.Recv()
		if err != nil {
			break
		}
		if call, ok := msg.(*v1.HostCall); ok {
			r.mu.Lock()
			d := r.disp
			r.mu.Unlock()
			if d != nil {
				go r.answer(ctx, sess, d, call)
				continue
			}
		}
		select {
		case r.messages <- msg:
		case <-ctx.Done():
			return
		}
	}

	reason := sess.Close()
	stderr := sess.Stderr()
	ended := r.opts.now()

	r.mu.Lock()
	stale := r.generation != generation || r.stopping
	if !stale {
		r.session = nil
		r.stderr = stderr
		r.budget.ranSteadily(sess.Started(), ended)
	}
	r.mu.Unlock()
	if stale {
		return
	}

	if reason.Kind == ExitOrderly {
		// The plugin chose to stop. That is not a fault to restart around.
		r.setState(StateDisabled, "")
		return
	}
	r.setState(StateFailed, reason.String())
	if ctx.Err() != nil {
		return
	}
	// launch reports its own state; a denied budget is already StateFailed.
	_ = r.launch(ctx)
}

// Stop ends the plugin. It is safe to call on a runtime that is not running.
func (r *Runtime) Stop() {
	r.mu.Lock()
	r.stopping = true
	r.generation++
	sess := r.session
	r.session = nil
	r.mu.Unlock()

	if sess != nil {
		sess.Close()
		r.mu.Lock()
		r.stderr = sess.Stderr()
		r.mu.Unlock()
	}
	r.setState(StateDisabled, "")
}

func (r *Runtime) answer(ctx context.Context, sess *Session, d *Dispatcher, call *v1.HostCall) {
	reply := d.Handle(ctx, call)
	r.mu.Lock()
	live := r.session == sess && !r.stopping
	r.mu.Unlock()
	if !live {
		return
	}
	_ = sess.Send(&reply)
}

// markFailed records a fault the runtime did not produce itself.
func (r *Runtime) markFailed(reason string) { r.setState(StateFailed, reason) }

func (r *Runtime) setState(s State, failure string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = s
	r.since = r.opts.now()
	// A healthy state clears the previous reason; a fault keeps whichever
	// explanation the caller had.
	if failure != "" || s == StateRunning || s == StateDisabled || s == StateStarting {
		r.failure = failure
	}
}

// restartBudget bounds automatic restarts to MaxAutoStarts inside a rolling
// RestartWindow.
//
// It records start times rather than failures because the thing to bound is
// how often the shell spawns a process, not how many ways one can fail.
type restartBudget struct {
	starts []time.Time
}

func newRestartBudget() *restartBudget { return &restartBudget{} }

// allow reports whether a start may happen now, and records it if so.
func (b *restartBudget) allow(now time.Time) bool {
	cutoff := now.Add(-RestartWindow)
	kept := b.starts[:0]
	for _, t := range b.starts {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	b.starts = kept
	if len(b.starts) >= MaxAutoStarts {
		return false
	}
	b.starts = append(b.starts, now)
	return true
}

// ranSteadily clears the window when a process lived long enough to count as
// working. Without it, three quick failures at startup would hold against a
// plugin that then ran all day.
func (b *restartBudget) ranSteadily(started, ended time.Time) {
	if ended.Sub(started) >= StableRun {
		b.starts = nil
	}
}
