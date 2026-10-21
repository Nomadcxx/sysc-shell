package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// MaxStderrBytes is how much of a plugin's standard error the host retains.
// The window keeps the most recent output, because the lines nearest a failure
// are the ones that explain it.
const MaxStderrBytes = 64 << 10

// shutdownSendTimeout bounds the best-effort host.shutdown write. A plugin
// that stopped reading its standard input must not be able to hold Close in
// Send; closing the pipe below unblocks the write either way.
const shutdownSendTimeout = 100 * time.Millisecond

// defaultShutdownGrace leaves time for plugins to flush state and stop owned
// child processes before the host kills an unresponsive process.
const defaultShutdownGrace = 4 * time.Second

// killGroupSoftWait is how long a forced stop gives the group to act on SIGTERM
// before SIGKILL. It is short on purpose: this is the arm a plugin reached only
// after ignoring the whole shutdown grace, so nothing is waiting on a
// cooperative exit any more.
const killGroupSoftWait = 250 * time.Millisecond

// exitedWaitDelay bounds how long cmd.Wait keeps reading a plugin's pipes after
// the process has left or has been killed. A descendant holding a pipe would
// wait out the whole shutdown grace, and that grace is for a process that can
// still be asked to stop.
//
// The command carries this one value for its whole life: os/exec's context
// watcher reads Cmd.WaitDelay from its own goroutine as soon as the command
// context is done, so the field is set before Start and never written again
// (sysc-1532).
const exitedWaitDelay = 250 * time.Millisecond

// exitDrainDelay is how long a reader gets to empty the standard output after
// the process leaves, before the exit watcher closes the pipe. Closing it
// discards unread bytes, and a plugin's last snapshot is often the interesting
// one; a pipe holds at most 64 KiB, which a reader takes well inside this.
const exitDrainDelay = 50 * time.Millisecond

// ErrHandshakeTimeout reports a plugin that did not answer host.hello in time.
var ErrHandshakeTimeout = errors.New("plugin: handshake timed out")

// ErrFlooding reports that a plugin kept sending after its inbound budget was
// exhausted. The session stops reading; the runtime marks the plugin degraded.
var ErrFlooding = errors.New("plugin: exceeded its update budget")

// IncompatibleError reports a plugin that speaks a protocol this shell does
// not. It is a distinct type because it is permanent: restarting the process
// will produce the same answer, so the manager says "incompatible" and stops
// rather than looping.
type IncompatibleError struct {
	Plugin   string
	Want     int
	MaxMinor int
	Got      v1.Version
}

func (e *IncompatibleError) Error() string {
	return fmt.Sprintf("plugin %s requires protocol %d.%d; this shell supports %d.0 through %d.%d",
		e.Plugin, e.Got.Major, e.Got.Minor, e.Want, e.Want, e.MaxMinor)
}

// ExitKind classifies why a process is no longer running.
type ExitKind string

const (
	// ExitOrderly is a zero exit after host.shutdown.
	ExitOrderly ExitKind = "orderly"
	// ExitCrashed is any other exit the plugin chose.
	ExitCrashed ExitKind = "crashed"
	// ExitSignalled is a termination by signal the host did not send.
	ExitSignalled ExitKind = "signalled"
	// ExitKilled is the host's own kill after the grace period.
	ExitKilled ExitKind = "killed"
)

// ExitReason is the structured account of one process's end, kept so the
// manager can tell a user what happened without them reading a log.
type ExitReason struct {
	Kind   ExitKind
	Code   int
	Signal syscall.Signal
	Detail string
}

func (e ExitReason) String() string {
	switch e.Kind {
	case ExitOrderly:
		return "exited normally"
	case ExitKilled:
		return "did not exit after shutdown and was killed"
	case ExitSignalled:
		return fmt.Sprintf("terminated by signal %v", e.Signal)
	default:
		if e.Detail != "" {
			return fmt.Sprintf("exited with status %d: %s", e.Code, e.Detail)
		}
		return fmt.Sprintf("exited with status %d", e.Code)
	}
}

// Supervisor starts one plugin process and completes its handshake.
//
// It owns a single attempt. Restart policy belongs to Runtime, so that the
// question "did this start work" stays separate from "should we try again".
type Supervisor struct {
	Manifest Manifest
	// Supported is what this host offers. The grant is the intersection with
	// what the manifest requested.
	Supported []Capability
	Limits    v1.Limits
	// HandshakeTimeout bounds the wait for plugin.hello.
	HandshakeTimeout time.Duration
	// ShutdownGrace is how long an orderly shutdown may take before the
	// process is killed.
	ShutdownGrace time.Duration

	// lastStrayPID records a process that outlived a failed start. It stays
	// zero in correct operation and exists so a test can prove that.
	lastStrayPID int
}

// Session is a live plugin process that has completed its handshake.
//
// Send and Recv are each serialised, but they are independent: the host writes
// while the plugin is writing back, which is what keeps a slow plugin from
// blocking the host and the other way round.
type Session struct {
	Protocol v1.Version
	// Granted is what the host offered and the plugin accepted.
	Granted []Capability

	cmd     *exec.Cmd
	stdin   io.WriteCloser
	enc     *v1.Encoder
	dec     *v1.Decoder
	stderr  *tail
	grace   time.Duration
	inbound *Inbound

	sendMu sync.Mutex
	recvMu sync.Mutex

	// exited is closed once the process has been seen to leave. It is the
	// death signal end of stream cannot be: a descendant that inherited the
	// standard output keeps that pipe open long after the plugin is gone.
	exited chan struct{}

	closeOnce sync.Once
	reason    ExitReason
	// started is when the process began, so a caller can tell a crash loop
	// from a plugin that ran well and then stopped.
	started time.Time
}

// Start launches the process and completes the handshake.
//
// A failed handshake reaps the process before returning. The supervisor owns
// what it started: leaving a rejected plugin running against a shell that has
// forgotten about it is worse than never having started it.
func (s *Supervisor) Start(ctx context.Context) (*Session, error) {
	if p := s.Manifest.Protocol; !HostSupports(p) {
		return nil, &IncompatibleError{Plugin: s.Manifest.ID, Want: HostProtocolMajor, MaxMinor: HostProtocolMinor, Got: p}
	}
	if s.Manifest.ExecPath == "" {
		return nil, fmt.Errorf("plugin: %s has no resolved entry point", s.Manifest.ID)
	}
	if missing := s.Manifest.MissingCommands(); len(missing) > 0 {
		return nil, fmt.Errorf("plugin: %s needs %v, which %s not on PATH",
			s.Manifest.ID, missing, plural(len(missing), "is", "are"))
	}

	// An explicit empty argument array: the host passes nothing the manifest
	// could have chosen, and starts the executable directly rather than
	// through a command shell.
	cmd := exec.CommandContext(ctx, s.Manifest.ExecPath)
	cmd.Args = []string{s.Manifest.ExecPath}
	cmd.Dir = s.Manifest.Dir
	// A plugin gets its own process group. It inherits the shell's group
	// otherwise, and a plugin that forks would then share one with the shell
	// and the compositor: signalling the group to stop a plugin would reach
	// both, which is why the forced paths below signal the group at all.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Cancelling the host context is a forced stop, so it takes the same group
	// signal a grace expiry does rather than killing the leader alone and
	// leaving the plugin's children reparented to the user manager.
	cmd.Cancel = func() error { return killGroup(cmd, syscall.SIGKILL) }
	// GH #11: a third-party process gets the handful of variables it needs to
	// find the compositor and its own config, not the whole session
	// environment (which can carry tokens, proxy credentials, search paths).
	cmd.Env = pluginEnvironment()
	grace := s.ShutdownGrace
	if grace <= 0 {
		grace = defaultShutdownGrace
	}
	stderr := newTail(MaxStderrBytes)
	// Cmd.Wait owns the pipe copy and does not close stderr until it finishes.
	cmd.Stderr = stderr
	// The post-exit I/O bound is fixed before Start: os/exec reads this field
	// from its own goroutine when the command context is done, so nothing may
	// assign it afterwards (sysc-1532).
	cmd.WaitDelay = exitedWaitDelay

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin: %s: stdin: %w", s.Manifest.ID, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin: %s: stdout: %w", s.Manifest.ID, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("plugin: start %s: %w", s.Manifest.ID, err)
	}

	sess := &Session{
		cmd:     cmd,
		stdin:   stdin,
		enc:     v1.NewEncoder(stdin),
		dec:     v1.NewDecoder(stdout, v1.ToHost),
		stderr:  stderr,
		grace:   grace,
		inbound: NewInbound(nil, s.Limits),
		started: time.Now(),
		exited:  make(chan struct{}),
	}
	sess.watchExit(stdout)

	if err := s.handshake(ctx, sess); err != nil {
		s.reap(sess)
		return nil, err
	}
	return sess, nil
}

// watchExit notices the process leaving without reaping it, so cmd.Wait stays
// the only reaper and ProcessState keeps its meaning.
//
// End of stream on the standard output is not a death signal on its own: a
// descendant that inherited that pipe keeps it open, and a session waiting for
// EOF alone reports a dead plugin as running for as long as the descendant
// lives. Closing the read side is what unblocks a reader held that way.
func (s *Session) watchExit(stdout io.Closer) {
	pid := s.PID()
	go func() {
		var info unix.Siginfo
		for {
			// WNOWAIT observes the exit and leaves the zombie behind for
			// cmd.Wait to reap. P_PID and never P_ALL: the shell's other
			// children are not this session's to reap.
			err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
			if err != unix.EINTR {
				break
			}
		}
		close(s.exited)
		// A descendant's copy of the pipe keeps ReadFrame blocked for good.
		// Closing the read side is what unblocks it, but closing it discards
		// whatever the dying plugin had already written, so give the reader a
		// moment to empty the pipe first.
		time.Sleep(exitDrainDelay)
		// The standard error tail is a separate pipe and is left alone: it is
		// what explains a failure, so it must survive this.
		_ = stdout.Close()
	}()
}

// reap ends a process whose start was rejected and records any that survived.
func (s *Supervisor) reap(sess *Session) {
	pid := sess.PID()
	sess.Close()
	if sess.cmd.ProcessState == nil {
		// Close already signalled the group; if the leader is still unreaped
		// something survived the kill, so insist once before recording it as
		// stray rather than leaving it to the user manager.
		_ = killGroup(sess.cmd, syscall.SIGKILL)
		s.lastStrayPID = pid
	}
}

// handshake sends host.hello and validates the answer.
//
// The wait runs on a goroutine so that a plugin which never writes, and a
// plugin which writes something else entirely, are both bounded by the same
// timer rather than by a blocking read the host cannot abandon.
func (s *Supervisor) handshake(ctx context.Context, sess *Session) error {
	granted := intersect(s.Manifest.Capabilities, s.Supported)
	hello := &v1.HostHello{
		Supported:    hostSupportedVersions(),
		Plugin:       v1.Identity{ID: s.Manifest.ID, Name: s.Manifest.Name, Version: s.Manifest.Version},
		Capabilities: capabilityNames(granted),
		Limits:       s.Limits,
	}
	if err := sess.Send(hello); err != nil {
		return fmt.Errorf("plugin: %s: send host.hello: %w", s.Manifest.ID, err)
	}

	type result struct {
		msg v1.Message
		err error
	}
	answered := make(chan result, 1)
	go func() {
		msg, err := sess.Recv()
		answered <- result{msg, err}
	}()

	timeout := s.HandshakeTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var got result
	select {
	case got = <-answered:
	case <-timer.C:
		return fmt.Errorf("plugin: %s: %w after %v", s.Manifest.ID, ErrHandshakeTimeout, timeout)
	case <-ctx.Done():
		return fmt.Errorf("plugin: %s: %w", s.Manifest.ID, ctx.Err())
	}
	if got.err != nil {
		return fmt.Errorf("plugin: %s: read plugin.hello: %w", s.Manifest.ID, got.err)
	}

	reply, ok := got.msg.(*v1.PluginHello)
	if !ok {
		// A view before the handshake is a protocol violation rather than an
		// early optimisation: the host has not yet agreed what this process is.
		return fmt.Errorf("plugin: %s sent %s before plugin.hello",
			s.Manifest.ID, v1.TypeOf(got.msg))
	}
	if !HostSupports(reply.Protocol) {
		return &IncompatibleError{Plugin: s.Manifest.ID, Want: HostProtocolMajor, MaxMinor: HostProtocolMinor, Got: reply.Protocol}
	}
	want := hello.Plugin
	if reply.Plugin != want {
		return fmt.Errorf("plugin: started %s %s but it identified as %s %s",
			want.ID, want.Version, reply.Plugin.ID, reply.Plugin.Version)
	}

	accepted, err := accept(reply.Capabilities, granted)
	if err != nil {
		return fmt.Errorf("plugin: %s: %w", s.Manifest.ID, err)
	}
	sess.Protocol = reply.Protocol
	sess.Granted = accepted
	return nil
}

// intersect narrows requested capabilities to those the host supports.
func intersect(requested, supported []Capability) []Capability {
	have := make(map[Capability]bool, len(supported))
	for _, c := range supported {
		have[c] = true
	}
	out := make([]Capability, 0, len(requested))
	for _, c := range requested {
		if have[c] {
			out = append(out, c)
		}
	}
	return out
}

// accept checks the plugin's answer against the grant. Accepting more than was
// granted is not a negotiation; it means the two sides disagree about what the
// process may do, and continuing would leave that disagreement in place.
func accept(names []string, granted []Capability) ([]Capability, error) {
	allowed := make(map[Capability]bool, len(granted))
	for _, c := range granted {
		allowed[c] = true
	}
	out := make([]Capability, 0, len(names))
	seen := make(map[Capability]bool, len(names))
	for _, name := range names {
		c := Capability(name)
		if !allowed[c] {
			return nil, fmt.Errorf("accepted capability %q, which was not granted", name)
		}
		if seen[c] {
			return nil, fmt.Errorf("accepted capability %q twice", name)
		}
		seen[c] = true
		out = append(out, c)
	}
	return out, nil
}

func capabilityNames(caps []Capability) []string {
	out := make([]string, len(caps))
	for i, c := range caps {
		out[i] = string(c)
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Allows reports whether a capability was granted and accepted.
func (s *Session) Allows(c Capability) bool {
	for _, have := range s.Granted {
		if have == c {
			return true
		}
	}
	return false
}

// PID is the process identifier, or zero once the process has gone.
func (s *Session) PID() int {
	if s.cmd == nil || s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

// Started is when the process began.
func (s *Session) Started() time.Time { return s.started }

// Exited reports whether the process has been seen to leave. It reaps
// nothing, and it is true whatever the plugin's descendants do with the pipes
// they inherited.
func (s *Session) Exited() bool {
	select {
	case <-s.exited:
		return true
	default:
		return false
	}
}

// Stderr returns the retained tail of the plugin's standard error.
func (s *Session) Stderr() []byte { return s.stderr.bytes() }

// Send writes one message to the plugin.
func (s *Session) Send(m v1.Message) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.enc.Encode(m)
}

// Recv reads one message from the plugin. It returns an error at end of
// stream, which is how the host learns the process is gone without polling.
//
// The budget is charged per frame, before decoding, so a plugin that floods
// costs the host a read and a token rather than a parse and a layout. A frame
// over budget is dropped; repeated drops end the session with ErrFlooding.
func (s *Session) Recv() (v1.Message, error) {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	for {
		line, err := s.dec.ReadFrame()
		if err != nil {
			return nil, err
		}
		if _, ok := s.inbound.Accept(line); !ok {
			if s.inbound.Degraded() {
				return nil, ErrFlooding
			}
			continue
		}
		return s.dec.DecodeFrame(line)
	}
}

// Close ends the process and returns why it stopped.
//
// The sequence is the one a cooperating plugin can act on and an uncooperative
// one cannot evade: ask it to stop, close its standard input so a plugin
// blocked on a read also learns, wait out the grace period, then kill. Calling
// Close twice reports the same reason as the first call.
//
// A process that has already been seen to leave skips the asking and the
// grace, so closing a plugin that crashed behind a descendant holding its
// pipes returns promptly instead of waiting out a grace nobody can use.
func (s *Session) Close() ExitReason {
	s.closeOnce.Do(func() { s.reason = s.shutdown() })
	return s.reason
}

func (s *Session) shutdown() ExitReason {
	if s.cmd == nil || s.cmd.Process == nil {
		return ExitReason{Kind: ExitCrashed, Detail: "never started"}
	}
	// Already gone. There is nothing left to ask to stop and no grace a dead
	// process could use, so skip both. Its descendants may still be holding
	// the I/O pipes, which is what makes cmd.Wait slow here; the command's
	// post-exit I/O bound is set before Start and already covers that.
	if s.Exited() {
		return classify(s.cmd.Wait())
	}
	sent := make(chan struct{})
	go func() {
		_ = s.Send(&v1.HostShutdown{})
		close(sent)
	}()
	select {
	case <-sent:
	case <-time.After(shutdownSendTimeout):
	}
	_ = s.stdin.Close()

	grace := s.grace
	if grace <= 0 {
		grace = defaultShutdownGrace
	}
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()

	timer := time.NewTimer(grace)
	defer timer.Stop()

	select {
	case err := <-done:
		return classify(err)
	case <-timer.C:
		// The plugin did not take host.shutdown. Ask the whole group once,
		// briefly, then insist: descendants that survived the leader would
		// otherwise outlive the session in the user manager's care.
		_ = killGroup(s.cmd, syscall.SIGTERM)
		select {
		case <-done:
			return ExitReason{Kind: ExitKilled}
		case <-time.After(killGroupSoftWait):
		}
		_ = killGroup(s.cmd, syscall.SIGKILL)
		<-done
		return ExitReason{Kind: ExitKilled}
	}
}

// killGroup signals a plugin's whole process group, falling back to the leader.
//
// Cmd.Cancel and the grace-expiry arm both call it. A negative pid addresses
// the group Setpgid gave the plugin, so a forced stop reaches the children the
// plugin forked; a signal to the leader alone leaves them reparented and
// running after the user disabled the plugin, after an automatic restart, and
// after the shell exits. When there is no group left to signal — because every
// member is gone, or because the leader moved out of it — the leader is the
// last place to look, and signalling a finished process reports
// os.ErrProcessDone rather than a spurious failure.
func killGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
		return cmd.Process.Signal(sig)
	}
	return nil
}

// classify turns a Wait error into a reason a user can read.
func classify(err error) ExitReason {
	if err == nil {
		return ExitReason{Kind: ExitOrderly}
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return ExitReason{Kind: ExitSignalled, Signal: status.Signal()}
		}
		return ExitReason{Kind: ExitCrashed, Code: exit.ExitCode()}
	}
	return ExitReason{Kind: ExitCrashed, Detail: err.Error()}
}

// tail retains the most recent bytes of a stream in a fixed window.
//
// The window is the whole allocation: a plugin cannot make the host hold more
// by writing more, and the bytes it keeps are the ones nearest whatever went
// wrong.
type tail struct {
	mu   sync.Mutex
	max  int
	data []byte
}

func newTail(max int) *tail { return &tail{max: max} }

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = append(t.data, p...)
	if len(t.data) > t.max {
		t.data = append(t.data[:0], t.data[len(t.data)-t.max:]...)
	}
	return len(p), nil
}

func (t *tail) bytes() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]byte(nil), t.data...)
}

// pluginEnvironment is the allowlisted view of the host environment a plugin
// process starts with.
func pluginEnvironment() []string {
	var env []string
	for _, kv := range os.Environ() {
		key := strings.SplitN(kv, "=", 2)[0]
		// SYSC_* is our own config namespace (e.g. the weather plugin's
		// endpoint override); everything else must be on the allowlist.
		if strings.HasPrefix(key, "SYSC_") {
			env = append(env, kv)
			continue
		}
		if slices.Contains(pluginEnvKeys, key) {
			env = append(env, kv)
		}
	}
	return env
}

// pluginEnvKeys covers: finding binaries (PATH), config and state dirs (HOME,
// XDG_*), locale and timezone, the session buses a widget draws on
// (WAYLAND_DISPLAY, NIRI_SOCKET), and the display session a desktop app the
// plugin hands off to needs (DISPLAY and XAUTHORITY for X11 apps under
// Xwayland, the D-Bus session bus, the desktop and session type). These name
// sockets and files the user's processes already reach; none is a credential.
var pluginEnvKeys = []string{
	"PATH", "HOME", "USER", "LOGNAME", "SHELL",
	"LANG", "LC_ALL", "TZ",
	"XDG_RUNTIME_DIR", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME",
	"WAYLAND_DISPLAY", "WAYLAND_SOCKET", "NIRI_SOCKET",
	"DISPLAY", "XAUTHORITY", "DBUS_SESSION_BUS_ADDRESS", "XDG_CURRENT_DESKTOP", "XDG_SESSION_TYPE",
}
