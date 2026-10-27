package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// hostCaps is what this shell offers. Every test grants from this set so a
// capability the host does not support cannot leak in through a fixture.
var hostCaps = []Capability{CapNotifications, CapPanels, CapSettings, CapState}

// helperGrace is the shutdown grace these tests give a fake plugin.
//
// It is far longer than the one-second production value on purpose. The fake
// plugin is this test binary, and a race-instrumented Go binary costs about a
// second in runtime start-up and teardown alone: measured, 1.012s against
// 0.003s for the same program built without -race. A one-second grace would
// therefore time out on the fixture rather than on anything the supervisor
// does. What these tests prove is that an orderly shutdown is orderly and a
// deaf plugin is killed, not that one second is the right number.
const helperGrace = 5 * time.Second

func supervisor(m Manifest) *Supervisor {
	return &Supervisor{
		Manifest:         m,
		Supported:        hostCaps,
		Limits:           v1.DefaultLimits,
		HandshakeTimeout: 2 * time.Second,
		ShutdownGrace:    helperGrace,
	}
}

// startHelper starts a helper plugin and closes it when the test ends.
func startHelper(t *testing.T, mode string) *Session {
	t.Helper()
	s := supervisor(installHelper(t, mode))
	sess, err := s.Start(context.Background())
	if err != nil {
		t.Fatalf("Start(%q): %v", mode, err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// recvWithin reads one message, giving up after the deadline.
//
// A fixture that never answers has to fail the test rather than hang it, and
// the deadline has to be wide enough for a loaded machine: the point of the
// tests that use it is that the machine's speed is not what they measure.
func recvWithin(t *testing.T, sess *Session, d time.Duration) (v1.Message, error) {
	t.Helper()
	type result struct {
		msg v1.Message
		err error
	}
	done := make(chan result, 1)
	go func() {
		msg, err := sess.Recv()
		done <- result{msg, err}
	}()
	select {
	case got := <-done:
		return got.msg, got.err
	case <-time.After(d):
		return nil, fmt.Errorf("no message within %v", d)
	}
}

// TestStartWaitsOutABusyProgram proves the supervisor starts a program the
// kernel still refuses to exec.
//
// The kernel refuses to exec a file that is open for writing: the file stays
// busy until every process that inherited the write handle is gone. A plugin
// installed a moment ago lands in that state, and so does a fixture that
// writes a script and starts it back to back.
func TestStartWaitsOutABusyProgram(t *testing.T) {
	t.Parallel()

	m := installHelper(t, "ok")
	script, err := os.ReadFile(m.ExecPath)
	if err != nil {
		t.Fatalf("read helper script: %v", err)
	}
	// Rewrite the script and keep the write handle open: the program is busy
	// for as long as this handle is.
	f, err := os.OpenFile(m.ExecPath, os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatalf("open helper script: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if _, err := f.Write(script); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	// The program is busy right now, which is the state the start has to
	// survive.
	if err := exec.Command(m.ExecPath).Run(); !errors.Is(err, syscall.ETXTBSY) {
		t.Fatalf("expected the program to be busy, got %v", err)
	}

	// The handle closes shortly after, the way a writer that is done with the
	// file lets it go.
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = f.Close()
	}()

	s := supervisor(m)
	sess, err := s.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	if sess.PID() <= 0 {
		t.Error("session reports no process")
	}
}

func TestStartCompletesTheHandshake(t *testing.T) {
	t.Parallel()

	sess := startHelper(t, "ok")
	if sess.Protocol != (v1.Version{Major: 1, Minor: v1.ProtocolMinor}) {
		t.Errorf("protocol = %+v, want 1.%d", sess.Protocol, v1.ProtocolMinor)
	}
	if len(sess.Granted) != len(hostCaps) {
		t.Errorf("granted = %v, want all four", sess.Granted)
	}
	if !sess.Allows(CapState) {
		t.Error("state was requested and supported but not granted")
	}
	if sess.PID() <= 0 {
		t.Error("session reports no process")
	}
}

func TestGrantedCapabilitiesAreTheIntersection(t *testing.T) {
	t.Parallel()

	// The manifest asks for four; this host offers two. The plugin must be
	// told what it actually has, not what it asked for.
	s := supervisor(installHelper(t, "ok"))
	s.Supported = []Capability{CapSettings, CapState}
	sess, err := s.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sess.Close()

	if sess.Allows(CapNotifications) || sess.Allows(CapPanels) {
		t.Errorf("granted = %v, want only the supported two", sess.Granted)
	}
	if !sess.Allows(CapSettings) || !sess.Allows(CapState) {
		t.Errorf("granted = %v, want settings and state", sess.Granted)
	}
}

func TestStartRejectsAMismatchedIdentity(t *testing.T) {
	t.Parallel()

	s := supervisor(installHelper(t, "wrong-identity"))
	_, err := s.Start(context.Background())
	if err == nil {
		t.Fatal("Start accepted a plugin that claimed another identity")
	}
	if !strings.Contains(err.Error(), "org.sysc.impostor") {
		t.Fatalf("err = %v, want it to name the claimed identity", err)
	}
}

func TestStartRejectsAnUnsupportedMajorVersion(t *testing.T) {
	t.Parallel()

	s := supervisor(installHelper(t, "wrong-major"))
	_, err := s.Start(context.Background())
	if err == nil {
		t.Fatal("Start accepted protocol major 2")
	}
	var incompatible *IncompatibleError
	if !errors.As(err, &incompatible) {
		t.Fatalf("err = %v (%T), want an IncompatibleError so the manager can say so", err, err)
	}
}

func TestStartRejectsACapabilityThatWasNotGranted(t *testing.T) {
	t.Parallel()

	// Accepting more than was granted is not a negotiation; it means the two
	// sides disagree about what this process may do.
	s := supervisor(installHelper(t, "extra-capability"))
	if _, err := s.Start(context.Background()); err == nil {
		t.Fatal("Start accepted a capability the host never granted")
	}
}

func TestStartRejectsAViewBeforeTheHandshake(t *testing.T) {
	t.Parallel()

	s := supervisor(installHelper(t, "update-first"))
	_, err := s.Start(context.Background())
	if err == nil {
		t.Fatal("Start accepted a view update before plugin.hello")
	}
	if !strings.Contains(err.Error(), v1.TypeViewSnapshot) {
		t.Fatalf("err = %v, want it to name the offending message", err)
	}
}

func TestStartRejectsMalformedOutput(t *testing.T) {
	t.Parallel()

	s := supervisor(installHelper(t, "garbage"))
	if _, err := s.Start(context.Background()); err == nil {
		t.Fatal("Start accepted a plugin that does not speak the protocol")
	}
}

func TestStartReportsAProcessThatExitsImmediately(t *testing.T) {
	t.Parallel()

	s := supervisor(installHelper(t, "exit-before-hello"))
	_, err := s.Start(context.Background())
	if err == nil {
		t.Fatal("Start accepted a process that exited before the handshake")
	}
}

func TestStartTimesOutOnASilentPlugin(t *testing.T) {
	t.Parallel()

	s := supervisor(installHelper(t, "silent"))
	s.HandshakeTimeout = 150 * time.Millisecond
	// A plugin that never answered is going to be killed either way, so there
	// is nothing for the grace period to wait for here.
	s.ShutdownGrace = 150 * time.Millisecond

	began := time.Now()
	_, err := s.Start(context.Background())
	if err == nil {
		t.Fatal("Start waited forever for a silent plugin")
	}
	// Start owns the reap, so its bound is the handshake timeout plus the
	// grace, not the timeout alone.
	if elapsed := time.Since(began); elapsed > 2*time.Second {
		t.Fatalf("Start took %v, want it bounded by the handshake timeout and grace", elapsed)
	}
	if !errors.Is(err, ErrHandshakeTimeout) {
		t.Fatalf("err = %v, want ErrHandshakeTimeout", err)
	}
}

func TestStartHonoursACancelledContext(t *testing.T) {
	t.Parallel()

	s := supervisor(installHelper(t, "silent"))
	s.ShutdownGrace = 150 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	if _, err := s.Start(ctx); err == nil {
		t.Fatal("Start ignored a cancelled context")
	}
}

func TestAFailedStartLeavesNoProcessBehind(t *testing.T) {
	t.Parallel()

	s := supervisor(installHelper(t, "silent"))
	s.HandshakeTimeout = 100 * time.Millisecond
	s.ShutdownGrace = 150 * time.Millisecond
	if _, err := s.Start(context.Background()); err == nil {
		t.Fatal("Start succeeded unexpectedly")
	}
	// The supervisor owns the process it started; a rejected handshake must
	// reap it rather than leave it running against a shell that has forgotten
	// about it.
	if s.lastStrayPID != 0 {
		t.Fatalf("process %d outlived its failed handshake", s.lastStrayPID)
	}
}

func TestCloseShutsDownGracefully(t *testing.T) {
	t.Parallel()

	sess := startHelper(t, "ok")
	began := time.Now()
	reason := sess.Close()
	if reason.Kind != ExitOrderly {
		t.Fatalf("exit = %+v, want an orderly one", reason)
	}
	if reason.Code != 0 {
		t.Errorf("exit code = %d, want 0", reason.Code)
	}
	if elapsed := time.Since(began); elapsed >= helperGrace {
		t.Errorf("orderly shutdown took %v; it waited out the whole grace period", elapsed)
	}
}

func TestDefaultShutdownGraceAllowsSlowCleanup(t *testing.T) {
	s := supervisor(installHelper(t, "slow-shutdown"))
	s.ShutdownGrace = 0
	sess, err := s.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	reason := sess.Close()
	if reason.Kind != ExitOrderly {
		t.Fatalf("exit = %+v, want orderly after bounded cleanup", reason)
	}
}

func TestCloseKillsAPluginThatIgnoresShutdown(t *testing.T) {
	t.Parallel()

	s := supervisor(installHelper(t, "ignore-shutdown"))
	s.ShutdownGrace = 150 * time.Millisecond
	sess, err := s.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	began := time.Now()
	reason := sess.Close()
	if reason.Kind != ExitKilled {
		t.Fatalf("exit = %+v, want a kill", reason)
	}
	if elapsed := time.Since(began); elapsed > 3*time.Second {
		t.Fatalf("Close took %v, want it bounded by the grace period", elapsed)
	}
}

func TestCloseReturnsWhenThePluginStopsReading(t *testing.T) {
	t.Parallel()

	s := supervisor(installHelper(t, "ignore-shutdown"))
	s.ShutdownGrace = 150 * time.Millisecond
	sess, err := s.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Fill the plugin's stdin so the shutdown message cannot be written: the
	// helper never reads, and a pipe holds only so much. The write blocks
	// holding sendMu, which is the shape that used to hold Close forever.
	big := &v1.ViewSnapshot{ViewID: "v1", Revision: 1,
		Root: &v1.Node{Kind: v1.KindText, Text: strings.Repeat("x", 4<<20)}}
	written := make(chan struct{})
	go func() {
		_ = sess.Send(big)
		close(written)
	}()
	select {
	case <-written:
		t.Fatal("the fill write completed; the pipe never filled")
	case <-time.After(100 * time.Millisecond):
	}

	closed := make(chan ExitReason, 1)
	go func() { closed <- sess.Close() }()
	select {
	case reason := <-closed:
		if reason.Kind != ExitKilled {
			t.Fatalf("exit = %+v, want a kill", reason)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close blocked behind a full stdin pipe")
	}
}

// findChildPIDs reads back the descendant pids the crash-with-child helper
// leaves behind, so a test can end them.
func findChildPIDs(t *testing.T, path string) []int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		t.Fatalf("read child pid file: %v", err)
	}
	var pids []int
	for _, line := range strings.Fields(string(data)) {
		pid, err := strconv.Atoi(line)
		if err != nil {
			t.Fatalf("child pid %q: %v", line, err)
		}
		pids = append(pids, pid)
	}
	return pids
}

// killChildren ends every descendant the helper recorded. It is test cleanup,
// not an assertion about the kill path: stopping descendants is #378.
func killChildren(pids []int) {
	for _, pid := range pids {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	sess := startHelper(t, "ok")
	first := sess.Close()
	second := sess.Close()
	if first.Kind != second.Kind {
		t.Fatalf("second Close reported %+v, want the same reason as the first %+v", second, first)
	}
}

func TestSessionReportsACrashAfterTheHandshake(t *testing.T) {
	t.Parallel()

	s := supervisor(installHelper(t, "crash-after-hello"))
	sess, err := s.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sess.Close()

	// The reader sees end-of-stream, which is how the host learns the process
	// is gone without polling for it.
	if _, err := sess.Recv(); err == nil {
		t.Fatal("Recv succeeded after the plugin exited")
	}
	reason := sess.Close()
	if reason.Kind != ExitCrashed || reason.Code != 4 {
		t.Fatalf("exit = %+v, want a crash with code 4", reason)
	}
}

// TestCrashBehindAChildHoldingThePipesIsDetected is the shape of a script
// entry point that runs `helper &` and then execs the real binary. The plugin
// is gone immediately, but the descendant keeps both pipes open, so end of
// stream never arrives on its own.
func TestCrashBehindAChildHoldingThePipesIsDetected(t *testing.T) {
	t.Parallel()

	children := filepath.Join(t.TempDir(), "children")
	s := supervisor(installHelper(t, "crash-with-child:"+children))
	sess, err := s.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sess.Close()
	defer func() { killChildren(findChildPIDs(t, children)) }()

	// The host must notice the process is gone without waiting for the pipe.
	gone := make(chan error, 1)
	go func() {
		_, err := sess.Recv()
		gone <- err
	}()
	select {
	case err := <-gone:
		if err == nil {
			t.Fatal("Recv succeeded while a descendant held the pipe open")
		}
	case <-time.After(time.Second):
		t.Fatal("Recv did not return after the plugin process exited")
	}
	if !sess.Exited() {
		t.Fatal("Exited is false after the process left")
	}

	// Already-gone plugins must not cost the caller the shutdown grace.
	start := time.Now()
	reason := sess.Close()
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Errorf("Close took %v, want well under the %v grace", elapsed, s.ShutdownGrace)
	}
	if reason.Kind != ExitCrashed || reason.Code != 4 {
		t.Fatalf("exit = %+v, want a crash with code 4", reason)
	}
}

// TestCloseAfterContextCancelDoesNotTouchTheCommand closes a session whose
// process has already left while the command context is done.
//
// os/exec's watchCtx goroutine reads Cmd.WaitDelay as soon as the context
// reports done, so a shutdown that assigned the field would race that read.
// The session must still report the crash and return without waiting out the
// shutdown grace.
func TestCloseAfterContextCancelDoesNotTouchTheCommand(t *testing.T) {
	t.Parallel()

	children := filepath.Join(t.TempDir(), "children")
	s := supervisor(installHelper(t, "crash-with-child:"+children))
	ctx, cancel := context.WithCancel(context.Background())
	sess, err := s.Start(ctx)
	if err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}
	defer cancel()
	defer sess.Close()
	defer func() { killChildren(findChildPIDs(t, children)) }()

	gone := make(chan error, 1)
	go func() {
		_, err := sess.Recv()
		gone <- err
	}()
	select {
	case err := <-gone:
		if err == nil {
			t.Fatal("Recv succeeded while a descendant held the pipe open")
		}
	case <-time.After(time.Second):
		t.Fatal("Recv did not return after the plugin process exited")
	}
	if !sess.Exited() {
		t.Fatal("Exited is false after the process left")
	}

	// Cancelling is what wakes the command's watcher, so do it before the
	// close and let both paths run against the same session. The pause lets
	// the watcher reach its read of the command's fields; a close that then
	// assigned one of them would be reported here.
	cancel()
	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	reason := sess.Close()
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Errorf("Close took %v, want well under the %v grace", elapsed, s.ShutdownGrace)
	}
	if reason.Kind != ExitCrashed || reason.Code != 4 {
		t.Fatalf("exit = %+v, want a crash with code 4", reason)
	}
}

func TestStderrKeepsTheMostRecentBytes(t *testing.T) {
	t.Parallel()

	sess := startHelper(t, "loud-stderr")
	// The handshake completed after the noise, so everything is already
	// written; drain the reader to the process exit.
	_ = sess.Close()

	tail := sess.Stderr()
	if len(tail) > MaxStderrBytes {
		t.Fatalf("stderr tail = %d bytes, more than the %d retained", len(tail), MaxStderrBytes)
	}
	if len(tail) < MaxStderrBytes/2 {
		t.Fatalf("stderr tail = %d bytes, want the retained window to be filled", len(tail))
	}
	// The last line is the one worth keeping: it is nearest the failure.
	if !strings.Contains(string(tail), "line 004095") {
		t.Error("stderr tail dropped the most recent output")
	}
	if strings.Contains(string(tail), "line 000000") {
		t.Error("stderr tail kept the oldest output instead of the newest")
	}
}

func TestCloseRetainsStderrWrittenJustBeforeExit(t *testing.T) {
	t.Parallel()

	sess := startHelper(t, "stderr-tail-on-shutdown")
	sess.stderr.mu.Lock()
	closed := make(chan ExitReason, 1)
	go func() { closed <- sess.Close() }()

	msg, err := sess.Recv()
	if err != nil {
		sess.stderr.mu.Unlock()
		t.Fatalf("Recv(stderr-ready): %v", err)
	}
	if got, ok := msg.(*v1.ViewSnapshot); !ok || got.ViewID != "stderr-ready" {
		sess.stderr.mu.Unlock()
		t.Fatalf("message = %#v, want stderr-ready snapshot", msg)
	}
	// Keep the tail writer blocked until the helper emits its final marker and
	// exits. cmd.Wait must keep the stderr copy open until the writer catches up.
	time.Sleep(250 * time.Millisecond)
	sess.stderr.mu.Unlock()

	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not finish after the stderr drain was released")
	}
	if got := string(sess.Stderr()); !strings.Contains(got, "stderr final marker") {
		t.Fatalf("stderr tail = %q, want final marker", got)
	}
}

// TestHelperReportsStderrChildBeforeShutdown proves the fixture the bound
// test leans on: the descendant is reported before the host asks the plugin
// to stop.
//
// The grace in TestCloseBoundsStderrPipeHeldByChild starts when Close asks
// for the shutdown, so the pid has to be on the wire before that. A fixture
// that reported it afterwards would make the bound test depend on how fast
// the machine can fork.
func TestHelperReportsStderrChildBeforeShutdown(t *testing.T) {
	t.Parallel()

	s := supervisor(installHelper(t, "stderr-inherited-child"))
	s.ShutdownGrace = 100 * time.Millisecond
	sess, err := s.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	msg, err := recvWithin(t, sess, 30*time.Second)
	if err != nil {
		t.Fatalf("Recv(stderr-child): %v", err)
	}
	snapshot, ok := msg.(*v1.ViewSnapshot)
	if !ok || snapshot.ViewID != "stderr-child" || snapshot.Root == nil {
		t.Fatalf("message = %#v, want stderr-child snapshot", msg)
	}
	var pid int
	if _, err := fmt.Sscan(snapshot.Root.Text, &pid); err != nil || pid <= 0 {
		t.Fatalf("child PID = %q, err = %v", snapshot.Root.Text, err)
	}
	if sess.Exited() {
		t.Error("the session exited before it was asked to stop")
	}
}

func TestCloseBoundsStderrPipeHeldByChild(t *testing.T) {
	t.Parallel()

	s := supervisor(installHelper(t, "stderr-inherited-child"))
	s.ShutdownGrace = 100 * time.Millisecond
	sess, err := s.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	closed := make(chan ExitReason, 1)
	closeStarted := false
	closeFinished := false
	var child *os.Process
	defer func() {
		if child != nil {
			_ = child.Kill()
		}
		if !closeStarted {
			// The test gave up before it asked the session to stop, so
			// stop it here instead of leaving the plugin running.
			_ = sess.Close()
			return
		}
		if !closeFinished {
			select {
			case <-closed:
			case <-time.After(2 * time.Second):
				t.Error("Close stayed blocked after the stderr child was killed")
			}
		}
	}()

	// The helper starts the descendant and reports its pid before it reads
	// the shutdown request, so the snapshot is on the wire before any grace
	// can begin. The grace then bounds the shutdown and nothing else, which
	// is what the assertion below is about: a machine too slow to fork the
	// descendant cannot make this test fail.
	msg, err := recvWithin(t, sess, 30*time.Second)
	if err != nil {
		t.Fatalf("Recv(stderr-child): %v", err)
	}
	snapshot, ok := msg.(*v1.ViewSnapshot)
	if !ok || snapshot.ViewID != "stderr-child" || snapshot.Root == nil {
		t.Fatalf("message = %#v, want stderr-child snapshot", msg)
	}
	var pid int
	if _, err := fmt.Sscan(snapshot.Root.Text, &pid); err != nil || pid <= 0 {
		t.Fatalf("child PID = %q, err = %v", snapshot.Root.Text, err)
	}
	child, err = os.FindProcess(pid)
	if err != nil {
		t.Fatalf("find stderr child: %v", err)
	}

	closeStarted = true
	go func() { closed <- sess.Close() }()
	select {
	case <-closed:
		closeFinished = true
	case <-time.After(time.Second):
		t.Fatal("Close did not bound the inherited stderr pipe")
	}
	// The child shares the plugin's group, so the forced stop ends it too; the
	// deferred kill in the cleanup above is a no-op when this holds. The pid
	// can still answer for an instant after the kill, so poll briefly rather
	// than race the process's death.
	if !waitForExit(t, pid, time.Second) {
		t.Errorf("stderr child %d outlived the forced stop", pid)
	}
}

func TestSessionCarriesMessagesInEachDirection(t *testing.T) {
	t.Parallel()

	sess := startHelper(t, "ok")
	if err := sess.Send(&v1.ViewOpen{ViewID: "v1", View: v1.ViewBar, Entry: "bar", Instance: "t1"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	msg, err := sess.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	snap, ok := msg.(*v1.ViewSnapshot)
	if !ok {
		t.Fatalf("received %T, want *v1.ViewSnapshot", msg)
	}
	if snap.ViewID != "v1" {
		t.Errorf("view id = %q, want v1", snap.ViewID)
	}
}

func TestStartRefusesAPluginWithAMissingDependency(t *testing.T) {
	t.Parallel()

	withDep := edit(t, "requires", map[string]any{"commands": []string{"definitely-not-installed-xyz"}})
	s := supervisor(installHelperWith(t, "ok", withDep))
	_, err := s.Start(context.Background())
	if err == nil {
		t.Fatal("Start ran a plugin whose declared dependency is missing")
	}
	if !strings.Contains(err.Error(), "definitely-not-installed-xyz") {
		t.Fatalf("err = %v, want it to name the missing command", err)
	}
}

func TestSupervisorRejectsUnsupportedManifestMinor(t *testing.T) {
	m := installHelper(t, "ok")
	m.Protocol.Minor = 99
	sess, err := supervisor(m).Start(context.Background())
	if sess != nil {
		sess.Close()
	}
	var incompatible *IncompatibleError
	if !errors.As(err, &incompatible) {
		t.Fatalf("accepted unsupported manifest protocol: %v", err)
	}
}

func TestSupervisorCurrentMinorBounds(t *testing.T) {
	cur := v1.ProtocolMinor
	for _, tc := range []struct {
		name           string
		manifestMinor  int
		handshakeMinor int
		wantSuccess    bool
	}{
		{name: "current minor", manifestMinor: cur, handshakeMinor: cur, wantSuccess: true},
		{name: "manifest too new", manifestMinor: cur + 1, handshakeMinor: cur},
		{name: "handshake too new", manifestMinor: cur, handshakeMinor: cur + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := installHelper(t, "ok")
			m.Protocol.Minor = tc.manifestMinor
			script := fmt.Sprintf(`#!/bin/sh
IFS= read -r _
printf '%%s\n' '{"type":"plugin.hello","protocol":{"major":1,"minor":%d},"plugin":{"id":"org.sysc.timer","name":"Timer","version":"1.0.0"},"capabilities":["notifications","panels","settings","state"]}'
IFS= read -r _
`, tc.handshakeMinor)
			if err := os.WriteFile(m.ExecPath, []byte(script), 0o755); err != nil {
				t.Fatalf("write protocol helper: %v", err)
			}

			sess, err := supervisor(m).Start(context.Background())
			if tc.wantSuccess {
				if err != nil {
					t.Fatalf("Start: %v", err)
				}
				defer sess.Close()
				if sess.Protocol != (v1.Version{Major: 1, Minor: cur}) {
					t.Fatalf("protocol = %+v, want 1.%d", sess.Protocol, cur)
				}
				return
			}
			if sess != nil {
				_ = sess.Close()
			}
			var incompatible *IncompatibleError
			if !errors.As(err, &incompatible) {
				t.Fatalf("Start error = %v, want IncompatibleError", err)
			}
		})
	}
}

func TestRecvStopsAPluginThatFloods(t *testing.T) {
	s := supervisor(installHelper(t, "flood"))
	s.Limits = v1.Limits{UpdatesPerSecond: 10, UpdateBurst: 20}
	sess, err := s.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sess.Close()

	accepted := 0
	for {
		if _, err := sess.Recv(); err != nil {
			if !errors.Is(err, ErrFlooding) {
				t.Fatalf("Recv error = %v, want ErrFlooding", err)
			}
			break
		}
		accepted++
		if accepted > 100 {
			t.Fatal("the flood was never stopped")
		}
	}
	if accepted < 10 {
		t.Fatalf("accepted only %d messages, want the burst to be allowed", accepted)
	}
}

// waitForExit waits until the pid is gone or the deadline passes, and reports
// whether it left.
func waitForExit(t *testing.T, pid int, within time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// readChildPID reads the pid the spawn-child fixtures wrote.
func readChildPID(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read descendant pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		t.Fatalf("descendant pid = %q, err = %v", data, err)
	}
	return pid
}

// killIfAlive is cleanup for a descendant a test expects to survive; the
// grammar the shell promises is that only a plugin that stopped orderly may
// leave one behind, so the test owns ending it.
func killIfAlive(pid int) {
	if !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestCloseKillsPluginDescendantsWhenGraceExpires(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pidPath := filepath.Join(dir, "child.pid")
	s := supervisor(installHelper(t, "spawn-child-slow-shutdown:"+pidPath))
	s.ShutdownGrace = 100 * time.Millisecond
	sess, err := s.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	child := readChildPID(t, pidPath)
	defer killIfAlive(child)

	began := time.Now()
	reason := sess.Close()
	if reason.Kind != ExitKilled {
		t.Fatalf("exit = %+v, want a kill after the grace expired", reason)
	}
	if !waitForExit(t, child, time.Second) {
		t.Fatalf("descendant %d survived the forced stop", child)
	}
	// The grace is what bounds the forced path; a group kill must not add
	// waiting of its own.
	if elapsed := time.Since(began); elapsed > 3*time.Second {
		t.Fatalf("Close took %v, want it bounded by the grace period", elapsed)
	}
}

func TestCloseKillsPluginDescendantThatIgnoresSIGTERM(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pidPath := filepath.Join(dir, "child.pid")
	readyPath := filepath.Join(dir, "child.ready")
	m := installHelper(t, "slow-shutdown")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Start an in-group child that has installed SIGTERM ignore before the
	// plugin completes its handshake, then exec the normal slow-shutdown helper.
	script := fmt.Sprintf("#!/bin/sh\n/bin/sh -c 'trap \"\" TERM; : > \"$1\"; exec sleep 60' sh %s </dev/null >/dev/null 2>&1 &\necho $! > %s\nwhile [ ! -f %s ]; do sleep 0.01; done\nexec %s %s %s\n",
		shellQuote(readyPath), shellQuote(pidPath), shellQuote(readyPath),
		shellQuote(self), shellQuote(HelperFlag), shellQuote("slow-shutdown"))
	if err := os.WriteFile(m.ExecPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	s := supervisor(m)
	s.ShutdownGrace = 100 * time.Millisecond
	sess, err := s.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	child := readChildPID(t, pidPath)
	defer killIfAlive(child)

	if reason := sess.Close(); reason.Kind != ExitKilled {
		t.Fatalf("exit = %+v, want a kill after the grace expired", reason)
	}
	if !waitForExit(t, child, time.Second) {
		t.Fatalf("SIGTERM-ignoring descendant %d survived the forced stop", child)
	}
}

func TestCancelContextKillsPluginDescendants(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pidPath := filepath.Join(dir, "child.pid")
	s := supervisor(installHelper(t, "spawn-child:"+pidPath))
	ctx, cancel := context.WithCancel(context.Background())

	sess, err := s.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sess.Close()
	child := readChildPID(t, pidPath)
	defer killIfAlive(child)

	// Cancelling the context is the path a shell shutdown takes: the command
	// is cancelled rather than closed, which used to signal only the plugin.
	cancel()
	if !waitForExit(t, child, time.Second) {
		t.Fatalf("descendant %d survived context cancellation", child)
	}
}

func TestOrderlyExitLeavesADetachedChild(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pidPath := filepath.Join(dir, "child.pid")
	sess := startHelper(t, "spawn-child:"+pidPath)
	child := readChildPID(t, pidPath)
	defer killIfAlive(child)

	reason := sess.Close()
	if reason.Kind != ExitOrderly {
		t.Fatalf("exit = %+v, want an orderly one", reason)
	}
	// A plugin that handled host.shutdown may have started a user-visible
	// process on purpose, so the host leaves that decision with the plugin.
	if errors.Is(syscall.Kill(child, 0), syscall.ESRCH) {
		t.Fatalf("descendant %d was killed on an orderly exit", child)
	}
}

// TestACrashLeavesNoDescendantBehind is the half of the promise a crash
// does not get to keep: only a plugin that stopped orderly may leave a
// process behind.
//
// A plugin that crashed did not get to choose what it started, so whatever
// it left running is a leftover of a session the host has already ended. A
// leftover keeps working with nobody left to own it: a recorder's backend
// would keep recording the screen long after the plugin that started it is
// gone.
func TestACrashLeavesNoDescendantBehind(t *testing.T) {
	t.Parallel()

	children := filepath.Join(t.TempDir(), "children")
	s := supervisor(installHelper(t, "crash-with-child:"+children))
	sess, err := s.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sess.Close()

	// The close waits for the crash, and the helper records the descendant
	// before it crashes, so the pid is there to read.
	reason := sess.Close()
	if reason.Kind != ExitCrashed || reason.Code != 4 {
		t.Fatalf("exit = %+v, want a crash with code 4", reason)
	}
	child := readChildPID(t, children)
	defer killIfAlive(child)
	if !waitForExit(t, child, time.Second) {
		t.Fatalf("descendant %d survived a plugin that did not stop orderly", child)
	}
}

func TestPluginRunsInItsOwnProcessGroup(t *testing.T) {
	t.Parallel()

	sess := startHelper(t, "ok")
	plugPGID, err := syscall.Getpgid(sess.PID())
	if err != nil {
		t.Fatalf("Getpgid(plugin): %v", err)
	}
	if plugPGID == syscall.Getpgrp() {
		t.Fatalf("plugin pgid %d is the shell's own group; a group signal would reach the shell", plugPGID)
	}
	if plugPGID != sess.PID() {
		t.Fatalf("plugin pgid = %d, want its own pid %d as the group leader", plugPGID, sess.PID())
	}
}

func TestRejectedHandshakeLeavesNoDescendants(t *testing.T) {
	t.Parallel()

	pidPath := filepath.Join(t.TempDir(), "child.pid")
	s := supervisor(installHelper(t, "spawn-child-silent:"+pidPath))
	// The helper brings its descendant up before it answers, and the host
	// stops the group once the handshake runs out, so the plugin needs room
	// to record the descendant first. Two seconds is the bound the host
	// gives any plugin; a shorter one races the plugin's own start and
	// fails this test for a reason that has nothing to do with the stop.
	s.HandshakeTimeout = 2 * time.Second
	s.ShutdownGrace = 100 * time.Millisecond

	if _, err := s.Start(context.Background()); err == nil {
		t.Fatal("Start succeeded without a handshake")
	} else if !errors.Is(err, ErrHandshakeTimeout) {
		t.Fatalf("Start: %v, want a handshake timeout", err)
	}
	// A plugin that never answered is killed along with the children it
	// brought up; the shell never agreed to host it.
	child := readChildPID(t, pidPath)
	defer killIfAlive(child)
	if !waitForExit(t, child, time.Second) {
		t.Fatalf("descendant %d outlived its rejected handshake", child)
	}
	if s.lastStrayPID != 0 {
		t.Fatalf("process %d outlived its failed handshake", s.lastStrayPID)
	}
}
