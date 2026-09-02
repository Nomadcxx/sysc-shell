package plugin

import (
	"context"
	"testing"
	"time"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// helperOptions run a runtime against the fake plugin, with the same enlarged
// grace period the supervisor tests explain.
func helperOptions() RuntimeOptions {
	return RuntimeOptions{
		Supported:     hostCaps,
		Limits:        v1.DefaultLimits,
		ShutdownGrace: helperGrace,
	}
}

func TestRestartBudgetAllowsThreeStartsInTheWindow(t *testing.T) {
	t.Parallel()

	b := newRestartBudget()
	base := time.Unix(1_700_000_000, 0)
	for i := 0; i < MaxAutoStarts; i++ {
		if !b.allow(base.Add(time.Duration(i) * time.Second)) {
			t.Fatalf("start %d denied inside the window", i+1)
		}
	}
	if b.allow(base.Add(3 * time.Second)) {
		t.Fatalf("a %dth start was allowed inside the window", MaxAutoStarts+1)
	}
}

func TestRestartBudgetForgetsStartsOlderThanTheWindow(t *testing.T) {
	t.Parallel()

	b := newRestartBudget()
	base := time.Unix(1_700_000_000, 0)
	for i := 0; i < MaxAutoStarts; i++ {
		b.allow(base)
	}
	if b.allow(base.Add(RestartWindow - time.Second)) {
		t.Fatal("a start was allowed while the window still held three")
	}
	if !b.allow(base.Add(RestartWindow + time.Second)) {
		t.Fatal("the window never expired")
	}
}

func TestASteadyRunClearsTheRestartBudget(t *testing.T) {
	t.Parallel()

	// A plugin that has run well for minutes and then dies once is not a crash
	// loop, and must not spend a budget earned by earlier failures.
	b := newRestartBudget()
	base := time.Unix(1_700_000_000, 0)
	for i := 0; i < MaxAutoStarts; i++ {
		b.allow(base)
	}
	b.ranSteadily(base, base.Add(StableRun))
	if !b.allow(base.Add(StableRun + time.Second)) {
		t.Fatal("a steady run did not clear the failure window")
	}
}

func TestAShortRunDoesNotClearTheRestartBudget(t *testing.T) {
	t.Parallel()

	b := newRestartBudget()
	base := time.Unix(1_700_000_000, 0)
	for i := 0; i < MaxAutoStarts; i++ {
		b.allow(base)
	}
	b.ranSteadily(base, base.Add(StableRun-time.Second))
	if b.allow(base.Add(time.Second)) {
		t.Fatal("a run shorter than the steady threshold cleared the window")
	}
}

func TestRuntimeStartsAndReportsRunning(t *testing.T) {
	t.Parallel()

	r := NewRuntime(Candidate{Dir: "", Manifest: installHelper(t, "ok")}, helperOptions())
	if got := r.Status().State; got != StateDisabled {
		t.Fatalf("state before start = %q, want %q", got, StateDisabled)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Stop()

	st := r.Status()
	if st.State != StateRunning {
		t.Fatalf("state = %q, want %q", st.State, StateRunning)
	}
	if st.PID <= 0 {
		t.Errorf("status reports no process: %+v", st)
	}
}

func TestRuntimeStopLeavesTheProcessStopped(t *testing.T) {
	t.Parallel()

	r := NewRuntime(Candidate{Manifest: installHelper(t, "ok")}, helperOptions())
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	r.Stop()

	st := r.Status()
	if st.State != StateDisabled {
		t.Fatalf("state after Stop = %q, want %q", st.State, StateDisabled)
	}
	if st.PID != 0 {
		t.Errorf("status still reports process %d", st.PID)
	}
	// Stopping twice must not panic or resurrect anything.
	r.Stop()
}

func TestRuntimeMarksAnIncompatiblePluginRatherThanRetrying(t *testing.T) {
	t.Parallel()

	// A plugin that speaks another major version will speak it again on the
	// next start, so retrying is pure noise.
	r := NewRuntime(Candidate{Manifest: installHelper(t, "wrong-major")}, helperOptions())
	if err := r.Start(context.Background()); err == nil {
		t.Fatal("Start accepted an incompatible plugin")
	}
	st := r.Status()
	if st.State != StateIncompatible {
		t.Fatalf("state = %q, want %q", st.State, StateIncompatible)
	}
	if st.Failure == "" {
		t.Error("status carries no reason for the user to read")
	}
}

func TestRuntimeRefusesToStartWithAMissingDependency(t *testing.T) {
	t.Parallel()

	m := installHelperWith(t, "ok", edit(t, "requires", map[string]any{"commands": []string{"definitely-not-installed-xyz"}}))
	r := NewRuntime(Candidate{Manifest: m, MissingCommands: []string{"definitely-not-installed-xyz"}}, helperOptions())
	if err := r.Start(context.Background()); err == nil {
		t.Fatal("Start ran a plugin with a missing dependency")
	}
	if got := r.Status().State; got != StateMissingDependency {
		t.Fatalf("state = %q, want %q", got, StateMissingDependency)
	}
}

func TestRuntimeRefusesToStartARejectedCandidate(t *testing.T) {
	t.Parallel()

	r := NewRuntime(Candidate{Dir: "/nowhere", Err: errDiscovery}, helperOptions())
	if err := r.Start(context.Background()); err == nil {
		t.Fatal("Start ran a candidate that failed discovery")
	}
	if got := r.Status().State; got != StateFailed {
		t.Fatalf("state = %q, want %q", got, StateFailed)
	}
}

func TestRuntimeFailsAfterExhaustingItsRestartBudget(t *testing.T) {
	t.Parallel()

	// Each start crashes straight after the handshake, so the runtime spends
	// its whole budget and then stops trying.
	r := NewRuntime(Candidate{Manifest: installHelper(t, "crash-after-hello")}, helperOptions())
	defer r.Stop()

	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if r.Status().State == StateFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	st := r.Status()
	if st.State != StateFailed {
		t.Fatalf("state = %q, want %q after the budget ran out", st.State, StateFailed)
	}
	if st.Starts != MaxAutoStarts {
		t.Errorf("starts = %d, want the %d the budget allows", st.Starts, MaxAutoStarts)
	}
	if st.Failure == "" {
		t.Error("a failed plugin carries no reason")
	}
}

func TestRetryClearsAFailedRuntime(t *testing.T) {
	t.Parallel()

	// A user pressing Retry is an explicit decision, so it resets the budget
	// the automatic restarts spent.
	r := NewRuntime(Candidate{Manifest: installHelper(t, "ok")}, helperOptions())
	defer r.Stop()

	r.markFailed("earlier failure")
	if got := r.Status().State; got != StateFailed {
		t.Fatalf("state = %q, want %q", got, StateFailed)
	}
	if err := r.Retry(context.Background()); err != nil {
		t.Fatalf("Retry: %v", err)
	}
	st := r.Status()
	if st.State != StateRunning {
		t.Fatalf("state = %q, want %q", st.State, StateRunning)
	}
	if st.Failure != "" {
		t.Errorf("failure = %q, want it cleared", st.Failure)
	}
}

func TestRuntimeExposesStderrFromAFailedStart(t *testing.T) {
	t.Parallel()

	r := NewRuntime(Candidate{Manifest: installHelper(t, "loud-stderr")}, helperOptions())
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	r.Stop()
	if len(r.Status().Stderr) == 0 {
		t.Fatal("the manager has no stderr to show")
	}
}

func TestNotificationReplyDoesNotBlockTheShell(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	d := NewDispatcher(CallEnv{
		PluginID: "org.sysc.timer",
		Granted:  []Capability{CapNotifications},
		Notify: func(context.Context, v1.NotifyParams) (v1.NotifyResult, error) {
			close(started)
			<-release
			return v1.NotifyResult{ID: 1}, nil
		},
	})
	r := NewRuntime(Candidate{Manifest: installHelper(t, "notify-then-snapshot")}, helperOptions())
	r.SetCalls(d)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("notify never started")
	}
	select {
	case msg := <-r.Messages():
		if _, ok := msg.(*v1.ViewSnapshot); !ok {
			t.Fatalf("got %T, want a snapshot while notify is still in flight", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("snapshot blocked behind notify")
	}
	close(release)
}
