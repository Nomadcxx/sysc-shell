package plugin

import (
	"context"
	"errors"
	"strings"
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

func TestStartCompletesTheHandshake(t *testing.T) {
	t.Parallel()

	sess := startHelper(t, "ok")
	if sess.Protocol != (v1.Version{Major: 1, Minor: 0}) {
		t.Errorf("protocol = %+v, want 1.0", sess.Protocol)
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
