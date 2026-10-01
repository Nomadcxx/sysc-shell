package plugin

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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

func TestRuntimeRetainsProtocolFailure(t *testing.T) {
	r := NewRuntime(Candidate{Manifest: installHelper(t, "malformed-after-hello")}, helperOptions())
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st := r.Status()
		if st.State == StateFailed && strings.Contains(st.Failure, "unexpected") && st.Starts == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("protocol error discarded: %+v", r.Status())
}

func TestRuntimeSessionEndedRunsOnceWhenStopped(t *testing.T) {
	var ended atomic.Int32
	opts := helperOptions()
	opts.SessionEnded = func() { ended.Add(1) }
	r := NewRuntime(Candidate{Manifest: installHelper(t, "ok")}, opts)
	r.Stop()
	if got := ended.Load(); got != 0 {
		t.Fatalf("SessionEnded before a live session = %d, want 0", got)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.Stop()
	r.Stop()
	if got := ended.Load(); got != 1 {
		t.Fatalf("SessionEnded after repeated Stop = %d, want 1", got)
	}
}

func TestRuntimeSessionEndedRunsAfterOrderlyExit(t *testing.T) {
	var ended atomic.Int32
	opts := helperOptions()
	opts.SessionEnded = func() { ended.Add(1) }
	r := NewRuntime(Candidate{Manifest: installHelper(t, "ok")}, opts)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.Send(&v1.HostShutdown{}); err != nil {
		t.Fatal(err)
	}
	waitRuntimeState(t, r, StateDisabled)
	if got := ended.Load(); got != 1 {
		t.Fatalf("SessionEnded after orderly exit = %d, want 1", got)
	}
}

func TestRuntimeSessionEndedRunsOnProtocolFailure(t *testing.T) {
	var ended atomic.Int32
	opts := helperOptions()
	opts.SessionEnded = func() { ended.Add(1) }
	r := NewRuntime(Candidate{Manifest: installHelper(t, "malformed-after-hello")}, opts)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitRuntimeState(t, r, StateFailed)
	if got := ended.Load(); got != 1 {
		t.Fatalf("SessionEnded after protocol failure = %d, want 1", got)
	}
}

func TestRuntimeSessionEndedPrecedesEachAutomaticRestart(t *testing.T) {
	var mu sync.Mutex
	var startsAtEnd []int
	var r *Runtime
	opts := helperOptions()
	opts.SessionEnded = func() {
		mu.Lock()
		startsAtEnd = append(startsAtEnd, r.Status().Starts)
		mu.Unlock()
	}
	r = NewRuntime(Candidate{Manifest: installHelper(t, "crash-after-hello")}, opts)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status := r.Status()
		if status.State == StateFailed && status.Starts == MaxAutoStarts {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status := r.Status(); status.State != StateFailed || status.Starts != MaxAutoStarts {
		t.Fatalf("runtime did not finish its restart budget: %+v", status)
	}
	mu.Lock()
	got := append([]int(nil), startsAtEnd...)
	mu.Unlock()
	want := []int{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("starts at SessionEnded = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("starts at SessionEnded = %v, want %v", got, want)
		}
	}
}

func waitRuntimeState(t *testing.T, r *Runtime, want State) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := r.Status().State; got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("runtime state = %q, want %q", r.Status().State, want)
}

func TestRuntimeDegradesAPluginThatFloods(t *testing.T) {
	opts := helperOptions()
	opts.Limits = v1.Limits{UpdatesPerSecond: 10, UpdateBurst: 20}
	r := NewRuntime(Candidate{Manifest: installHelper(t, "flood")}, opts)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()

	waitRuntimeState(t, r, StateDegraded)
	st := r.Status()
	if !strings.Contains(st.Failure, "budget") {
		t.Fatalf("degraded without a reason: %+v", st)
	}
	if st.Starts != 1 {
		t.Fatalf("flooding plugin restarted itself: %+v", st)
	}
}

// A session cancel must release supervise even when it is blocked publishing
// to a messages channel with no pump; only the parent context staying alive
// used to let the goroutine leak. gh #58.
func TestRuntimeSuperviseStopsAfterSessionCancelWhilePublishing(t *testing.T) {
	sess, err := supervisor(installHelper(t, "flood")).Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()

	r := NewRuntime(Candidate{Manifest: installHelper(t, "flood")}, helperOptions())
	for len(r.messages) < cap(r.messages) {
		r.messages <- &v1.ViewSnapshot{ViewID: "filler"} // fill the buffer: the next publish blocks
	}

	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()
	sessionCtx, cancelSession := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.supervise(ctx, sessionCtx, cancelSession, sess, 0)
	}()

	time.Sleep(150 * time.Millisecond) // let supervise block in the publish select
	cancelSession()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("supervise stayed blocked publishing after the session was cancelled")
	}
}

func TestEarlyHostCallDoesNotOutliveItsSession(t *testing.T) {
	ctxErr := make(chan error, 1) // receives only if the hook runs
	d := NewDispatcher(CallEnv{
		PluginID: "org.sysc.timer",
		Granted:  []Capability{CapNotifications},
		Notify: func(ctx context.Context, _ v1.NotifyParams) (v1.NotifyResult, error) {
			ctxErr <- ctx.Err()
			return v1.NotifyResult{ID: 1}, nil
		},
	})
	r := NewRuntime(Candidate{Manifest: installHelper(t, "notify-then-snapshot")}, helperOptions())
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		held := len(r.early)
		r.mu.Unlock()
		if held == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the call before SetCalls was not held")
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.Stop()
	r.SetCalls(d)
	select {
	case <-ctxErr:
		t.Fatal("a call from a stopped session ran its side effect")
	case <-time.After(300 * time.Millisecond):
	}
	select {
	case msg := <-r.Messages():
		if _, ok := msg.(*v1.HostCall); ok {
			t.Fatal("host call reached the message queue")
		}
	default:
	}
}

// A call that finds the early buffer full must still get its one reply; before
// the fix it was dropped and the plugin waited on its id for the whole session.
func TestEarlyHostCallOverflowIsAnsweredWithAnError(t *testing.T) {
	r := NewRuntime(Candidate{Manifest: installHelper(t, "call-flood")}, helperOptions())
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()

	// The dispatcher is attached late on purpose, so the first maxEarlyCalls
	// calls are held and every later one overflows.
	overflowed := map[string]bool{}
	deadline := time.After(3 * time.Second)
	for len(overflowed) < 8 {
		select {
		case msg := <-r.Messages():
			st, ok := msg.(*v1.PluginStatus)
			if !ok {
				continue
			}
			id, errText, _ := strings.Cut(st.Message, ":")
			if errText == "" {
				t.Fatalf("call %s was answered before SetCalls", id)
			}
			overflowed[id] = true
		case <-deadline:
			t.Fatalf("overflowing calls never got a reply: %v", overflowed)
		}
	}
	for i := maxEarlyCalls; i < maxEarlyCalls+8; i++ {
		if id := fmt.Sprintf("f%d", i); !overflowed[id] {
			t.Fatalf("call %s got no reply", id)
		}
	}
}
