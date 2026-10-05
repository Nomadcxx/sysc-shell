package displays

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

// The recovery path is only worth anything if it works in a process that did not
// come from the shell's own memory, so the test exercises it as one: a fake
// compositor in its own process, a shell in its own process, and a helper in a
// third. The shell is killed with SIGKILL, which is the case a deferred
// cleanup would not survive.

const (
	testModeEnv   = "SYSC_DISPLAYS_TEST_MODE"
	fakeNiriMode  = "fake-niri"
	fakeShellMode = "fake-shell"
	fakeHelpMode  = "fake-helper"

	fakeSocketEnv = "SYSC_DISPLAYS_FAKE_SOCKET"
	fakeLogEnv    = "SYSC_DISPLAYS_FAKE_LOG"
	fakeRootEnv   = "SYSC_DISPLAYS_FAKE_ROOT"
	fakeStateEnv  = "SYSC_DISPLAYS_FAKE_STATE"
)

// TestMain turns this binary into the three processes the survival proof needs,
// so no separate fixture program has to be kept in step with the package.
func TestMain(m *testing.M) {
	switch os.Getenv(testModeEnv) {
	case "":
		os.Exit(m.Run())
	case fakeNiriMode:
		runFakeNiriProcess()
	case fakeShellMode:
		runFakeShellProcess()
	case fakeHelpMode:
		os.Exit(runFakeHelperProcess())
	default:
		fmt.Fprintf(os.Stderr, "unknown %s=%q\n", testModeEnv, os.Getenv(testModeEnv))
		os.Exit(2)
	}
}

func runFakeNiriProcess() {
	fake := newFakeNiri(nil, os.Getenv(fakeLogEnv))
	fake.t = tStub{}
	socket := os.Getenv(fakeSocketEnv)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fake.serve(ln)
	// The helper is the only thing that still needs this socket; exiting when
	// the listener closes keeps the fake's lifetime the test's business.
	select {}
}

// tStub satisfies the one *testing.T the fake uses, in a process that has no
// test framework: a failure there is a process that cannot answer, and the
// test waiting on it reports that.
type tStub struct{}

func (tStub) Fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func runFakeHelperProcess() int {
	ctx := context.Background()
	token := ""
	if len(os.Args) > 1 {
		token = os.Args[len(os.Args)-1]
	}
	if err := RunRecovery(ctx, token); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// runFakeShellProcess is the shell, reduced to the parts the proof needs: it
// sets up the owned file, arms recovery, previews, and then waits to be killed.
func runFakeShellProcess() {
	report := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
	socket, root := os.Getenv(fakeSocketEnv), os.Getenv(fakeRootEnv)
	exe, err := os.Executable()
	if err != nil {
		report("executable: %v", err)
		os.Exit(1)
	}
	// The helper is this binary in its third mode, which is what systemd-run
	// would have started. The test replaces the unit because a real user unit
	// is not available to an unprivileged test run.
	startRecoveryUnit = func(ctx context.Context, _ string, token string) error {
		// The environment is replaced rather than appended to: this process is
		// itself the shell mode, and a second copy of the same variable would
		// make the helper come back as another shell.
		os.Setenv(testModeEnv, fakeHelpMode)
		cmd := exec.CommandContext(ctx, exe, "--restore-displays", token)
		cmd.Env = os.Environ()
		log, err := os.OpenFile(helperLog(os.Getenv(fakeStateEnv)), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer log.Close()
		cmd.Stdout, cmd.Stderr = log, log
		return cmd.Start()
	}
	// A production window is twenty seconds; the proof needs seconds, not a
	// quarter of a minute, and the deadline is data on the ticket either way.
	armWindow = 3 * time.Second

	own, err := Setup(root)
	if err != nil {
		report("setup: %v", err)
		os.Exit(1)
	}
	ctx := context.Background()
	live, err := niri.Outputs(ctx, socket)
	if err != nil {
		report("outputs: %v", err)
		os.Exit(1)
	}
	draft := map[string]Policy{}
	before := make([]OutputState, 0, len(live))
	for _, o := range live {
		want, _ := effectivePolicy(o)
		want.Scale = ptr(1.5)
		draft[o.Name] = want
		before = append(before, beforeState(o))
	}
	t, err := arm(ctx, socket, own, draft, before)
	if err != nil {
		report("arm: %v", err)
		os.Exit(1)
	}
	if err := applyTarget(ctx, socket, draft, before); err != nil {
		report("apply: %v", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Getenv(fakeStateEnv), []byte(t.Token+"\n"), 0o600); err != nil {
		report("state: %v", err)
		os.Exit(1)
	}
	report("preview live as %s", t.Token)
	select {}
}

// TestRecoveryOutlivesTheShell is the proof the whole design rests on: arm
// recovery, preview, kill the shell, and see the outputs put back by a process
// that had no other reason to exist.
func TestRecoveryOutlivesTheShell(t *testing.T) {
	dir := t.TempDir()
	runtime := filepath.Join(dir, "run")
	mkdir(t, runtime)
	root := filepath.Join(dir, "config.kdl")
	if err := os.WriteFile(root, []byte("output \"DP-1\" {\n\tscale 1\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(runtime, "armed")
	t.Setenv("XDG_RUNTIME_DIR", runtime)

	logPath := filepath.Join(dir, "niri.log")
	newFakeNiri(t, logPath).startProcess(t, dir)
	socket := filepath.Join(dir, "niri.sock")

	shell := startFakeShell(t, dir, socket, root, state)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("fake niri saw:\n%s", readFile(t, logPath))
			t.Logf("the helper said: %s", readFile(t, helperLog(state)))
			rt, _ := os.ReadDir(dir)
			for _, e := range rt {
				t.Logf("left behind: %s", e.Name())
			}
		}
	})
	token := waitToken(t, state)
	// The helper must be alive before the shell dies, or the proof proves
	// nothing: arming waits for it to report ready.
	dir, err := TicketDir()
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the recovery helper's ready marker", 15*time.Second, func() bool {
		_, err := os.Stat(readyPath(dir, token))
		return err == nil
	})
	waitFor(t, "the preview to take", 15*time.Second, func() bool {
		return wireScales(t, logPath)["DP-1"] == "1.5"
	})

	if err := shell.Process.Kill(); err != nil {
		t.Fatalf("kill the shell: %v", err)
	}
	_, _ = shell.Process.Wait()

	waitFor(t, "the recovery to put the scale back", 30*time.Second, func() bool {
		return wireScales(t, logPath)["DP-1"] == "1"
	})
	// A clean resolution removes its own evidence, and says what it decided.
	waitFor(t, "the recovery to finish", 30*time.Second, func() bool {
		return strings.Contains(readFile(t, helperLog(state)), "reverted")
	})
	if _, err := os.Stat(ticketPathIn(dir, token)); !os.IsNotExist(err) {
		t.Fatalf("the ticket outlived a resolved recovery: %v", err)
	}
	// A revert must never reload the configuration: nothing was published, so
	// there is nothing for the compositor to re-read.
	if strings.Contains(readFile(t, logPath), "reload") {
		t.Errorf("recovery reloaded the configuration during a revert:\n%s", readFile(t, logPath))
	}
}

func startFakeShell(t *testing.T, dir, socket, root, state string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("executable: %v", err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(),
		testModeEnv+"="+fakeShellMode,
		fakeSocketEnv+"="+socket,
		fakeRootEnv+"="+root,
		fakeStateEnv+"="+state,
		"XDG_RUNTIME_DIR="+filepath.Join(dir, "run"),
	)
	var errLog strings.Builder
	cmd.Stderr = &errLog
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the shell: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		if t.Failed() {
			t.Logf("the shell said: %s", errLog.String())
		}
	})
	return cmd
}

func waitToken(t *testing.T, state string) string {
	t.Helper()
	var token string
	waitFor(t, "the armed ticket", 20*time.Second, func() bool {
		token = strings.TrimSpace(readFile(t, state))
		return validToken(token)
	})
	return token
}

// TestDecideRecovery is the durable decision table, as data. Every cell names
// the ticket state and what the owned file actually holds, because those two
// facts are the only inputs a recovery process has after a crash.
func TestDecideRecovery(t *testing.T) {
	const (
		old       = "old-hash"
		candidate = "candidate-hash"
		other     = "somebody-elses-hash"
	)
	for _, tc := range []struct {
		name                   string
		state, old, cand, seen string
		want                   recoveryAction
	}{
		{"preview and nothing published", StatePreview, old, "", old, actionRestoreRuntime},
		{"preview with a candidate is a conflict", StatePreview, old, candidate, candidate, actionRestoreRuntime},
		{"committing with the old file", StateCommitting, old, candidate, old, actionRestoreRuntime},
		{"committing with the candidate finishes the keep", StateCommitting, old, candidate, candidate, actionFinishKeep},
		{"committing with unrelated bytes", StateCommitting, old, candidate, other, actionConflict},
		{"undo with the candidate puts the old bytes back", StateUndo, old, candidate, candidate, actionUndoPublish},
		{"undo with the old file finishes the restore", StateUndo, old, candidate, old, actionRestoreRuntime},
		{"undo with unrelated bytes", StateUndo, old, candidate, other, actionConflict},
		{"kept does nothing", StateKept, old, candidate, candidate, actionNone},
		{"reverted does nothing", StateReverted, old, candidate, old, actionNone},
		{"a preview whose file vanished restores the runtime", StatePreview, old, "", "", actionRestoreRuntime},
		{"a preview with unrelated bytes", StatePreview, old, "", other, actionConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideRecovery(tc.state, tc.old, tc.cand, tc.seen); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRunRecoveryRefusesAStaleTicket proves the helper cannot act on a
// compositor other than the one the preview was armed against.
func TestRunRecoveryRefusesAStaleTicket(t *testing.T) {
	runtime := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	dir := filepath.Join(runtime, "sysc-displays")
	mkdir(t, dir)
	fake := newFakeNiri(t, "")
	socket := fake.start(t)
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	tk := Ticket{
		Version: ticketVersion, Token: token, State: StatePreview, Socket: socket,
		Root: filepath.Join(runtime, "config.kdl"), Managed: filepath.Join(runtime, "m.kdl"),
		Previous: previousPath(dir, token), Identity: Identity{UID: os.Getuid(), PID: 1, StartTicks: 1, BootID: "elsewhere"},
		OldHash: "old",
	}
	if err := tk.Store(); err != nil {
		t.Fatal(err)
	}
	err = RunRecovery(context.Background(), token)
	if !errors.Is(err, errStale) {
		t.Fatalf("got %v, want a stale ticket error", err)
	}
	if _, err := os.Stat(readyPath(dir, token)); !os.IsNotExist(err) {
		t.Fatal("a refused helper must not report ready")
	}
}

// TestRunRecoveryAcceptsOnlyAToken keeps the helper's whole surface to a name
// it can verify, so nothing that reaches it can be turned into a path.
func TestRunRecoveryAcceptsOnlyAToken(t *testing.T) {
	for _, token := range []string{"", "..", "../config", "../../etc/passwd", strings.Repeat("g", 32), "0123456789abcdef", "X"} {
		if err := RunRecovery(context.Background(), token); err == nil {
			t.Errorf("%q was accepted", token)
		}
	}
}

func TestValidToken(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if !validToken(token) {
		t.Errorf("a fresh token %q is not valid", token)
	}
	for _, bad := range []string{"", "abc", strings.ToUpper(token), token + "0", token[:31]} {
		if validToken(bad) {
			t.Errorf("%q is accepted", bad)
		}
	}
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func ticketPathIn(dir, token string) string { return filepath.Join(dir, token+".json") }

// helperLog is where the fake shell sends the helper's own output, named from
// the state file both processes are given.
func helperLog(state string) string { return state + ".helper.log" }

func ptr[T any](v T) *T { return &v }
