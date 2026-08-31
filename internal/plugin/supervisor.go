package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// MaxStderrBytes is how much of a plugin's standard error the host retains.
// The window keeps the most recent output, because the lines nearest a failure
// are the ones that explain it.
const MaxStderrBytes = 64 << 10

// ErrHandshakeTimeout reports a plugin that did not answer host.hello in time.
var ErrHandshakeTimeout = errors.New("plugin: handshake timed out")

// IncompatibleError reports a plugin that speaks a protocol this shell does
// not. It is a distinct type because it is permanent: restarting the process
// will produce the same answer, so the manager says "incompatible" and stops
// rather than looping.
type IncompatibleError struct {
	Plugin string
	Want   int
	Got    v1.Version
}

func (e *IncompatibleError) Error() string {
	return fmt.Sprintf("plugin %s speaks protocol %d.%d; this shell speaks major %d",
		e.Plugin, e.Got.Major, e.Got.Minor, e.Want)
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

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	enc    *v1.Encoder
	dec    *v1.Decoder
	stderr *tail
	grace  time.Duration

	sendMu sync.Mutex
	recvMu sync.Mutex

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
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin: %s: stdin: %w", s.Manifest.ID, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin: %s: stdout: %w", s.Manifest.ID, err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin: %s: stderr: %w", s.Manifest.ID, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("plugin: start %s: %w", s.Manifest.ID, err)
	}

	sess := &Session{
		cmd:     cmd,
		stdin:   stdin,
		enc:     v1.NewEncoder(stdin),
		dec:     v1.NewDecoder(stdout, v1.ToHost),
		stderr:  newTail(MaxStderrBytes),
		grace:   s.ShutdownGrace,
		started: time.Now(),
	}
	go sess.stderr.drain(stderrPipe)

	if err := s.handshake(ctx, sess); err != nil {
		s.reap(sess)
		return nil, err
	}
	return sess, nil
}

// reap ends a process whose start was rejected and records any that survived.
func (s *Supervisor) reap(sess *Session) {
	pid := sess.PID()
	sess.Close()
	if sess.cmd.ProcessState == nil {
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
		Supported:    []v1.Version{{Major: 1, Minor: 0}},
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
	if reply.Protocol.Major != 1 {
		return &IncompatibleError{Plugin: s.Manifest.ID, Want: 1, Got: reply.Protocol}
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
func (s *Session) Recv() (v1.Message, error) {
	s.recvMu.Lock()
	defer s.recvMu.Unlock()
	return s.dec.Decode()
}

// Close ends the process and returns why it stopped.
//
// The sequence is the one a cooperating plugin can act on and an uncooperative
// one cannot evade: ask it to stop, close its standard input so a plugin
// blocked on a read also learns, wait out the grace period, then kill. Calling
// Close twice reports the same reason as the first call.
func (s *Session) Close() ExitReason {
	s.closeOnce.Do(func() { s.reason = s.shutdown() })
	return s.reason
}

func (s *Session) shutdown() ExitReason {
	if s.cmd == nil || s.cmd.Process == nil {
		return ExitReason{Kind: ExitCrashed, Detail: "never started"}
	}
	_ = s.Send(&v1.HostShutdown{})
	_ = s.stdin.Close()

	grace := s.grace
	if grace <= 0 {
		grace = time.Second
	}
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()

	timer := time.NewTimer(grace)
	defer timer.Stop()

	select {
	case err := <-done:
		return classify(err)
	case <-timer.C:
		_ = s.cmd.Process.Kill()
		<-done
		return ExitReason{Kind: ExitKilled}
	}
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

// drain copies a stream into the window until it ends.
func (t *tail) drain(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			_, _ = t.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}
