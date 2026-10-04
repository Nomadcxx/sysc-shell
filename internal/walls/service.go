// Package walls reports and controls the installed sysc-walls user service.
package walls

import (
	"bufio"
	"context"
	"debug/buildinfo"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	unitName            = "sysc-walls.service"
	configGate          = "fc7172d31b8fbfc952df817e5b677f84a4f8e3b9"
	previewGate         = "e97eeb631d46e3147cbc79acdaf4644522ce414d"
	clientMain          = "github.com/Nomadcxx/sysc-walls/cmd/client"
	daemonMain          = "github.com/Nomadcxx/sysc-walls/cmd/daemon"
	displayMain         = "github.com/Nomadcxx/sysc-walls/cmd/display"
	commandTimeout      = 10 * time.Second
	previewMax          = 10 * time.Second
	previewMin          = 2 * time.Second
	previewCleanup      = 5 * time.Second
	previewReadyTimeout = 5 * time.Second
	configPathSuffix    = ".config/sysc-walls/daemon.conf"
)

// Snapshot is a scalar, immutable view of sysc-walls discovery and config.
// Config values remain strings so malformed saved values are not hidden from
// the Settings UI.
type Snapshot struct {
	UnitKnown     bool
	UnitStale     bool
	StateError    string
	LoadState     string
	FragmentPath  string
	UnitFileState string
	ActiveState   string
	SubState      string

	ServiceAvailable bool
	ServiceError     string

	ConfigSourceMatches bool
	ConfigSourceError   string
	ConfigKnown         bool
	ConfigPath          string
	ConfigExists        bool
	ConfigReadable      bool
	ConfigReadError     string
	Effect              string
	Theme               string
	Timeout             string
	Artwork             string
	DateTime            string
	DateTimePosition    string

	ClientPath           string
	DaemonPath           string
	DisplayPath          string
	ClientIdentityError  string
	DaemonIdentityError  string
	DisplayIdentityError string

	CanApply                 bool
	CanPreview               bool
	PreviewAvailabilityError string

	ActionPending   bool
	ActionError     string
	ActionMessage   string
	Previewing      bool
	PreviewReady    bool
	PreviewStopping bool
	PreviewDuration time.Duration
	PreviewError    string
}

// EnabledAtLogin reports only persistent enablement. systemd's runtime-only,
// static, masked, and unknown states remain visible through UnitFileState.
func (s Snapshot) EnabledAtLogin() bool {
	return s.UnitKnown && !s.UnitStale && s.UnitFileState == "enabled"
}

// Running reports the exact service state sysc-walls uses while idle.
func (s Snapshot) Running() bool {
	return s.UnitKnown && !s.UnitStale && s.ActiveState == "active" && s.SubState == "running"
}

// Service serializes discovery and file reads away from the Wayland owner.
// Callers request refreshes and receive immutable snapshots.
type Service struct {
	deps serviceDependencies

	commands chan serviceCommand
	updates  chan Snapshot
	quit     chan struct{}
	done     chan struct{}
	close    sync.Once
	closeErr error

	mu   sync.RWMutex
	snap Snapshot
}

// Setting is one persisted sysc-walls setting. Keys use the walls client
// vocabulary, not sysc-shell config paths.
type Setting struct {
	Key   string
	Value string
}

type serviceOp uint8

const (
	opRefresh serviceOp = iota
	opApply
	opEnable
	opRuntime
	opPreview
	opStopPreview
	opIdle
)

type serviceCommand struct {
	op     serviceOp
	patch  []Setting
	value  bool
	result chan error
}

type timerHandle struct {
	C    <-chan time.Time
	stop func() bool
}

type previewSession struct {
	child         *previewChild
	duration      time.Duration
	startupTimer  timerHandle
	timer         timerHandle
	ready         bool
	stopRequested bool
	stopMessage   string
	stopping      bool
	stopErr       error
}

type previewChild struct {
	cmd   *exec.Cmd
	ready <-chan struct{}
	done  <-chan error
}

type serviceDependencies struct {
	run           func(context.Context, string, ...string) ([]byte, error)
	lookPath      func(string) (string, error)
	stat          func(string) (os.FileInfo, error)
	evalSymlinks  func(string) (string, error)
	readFile      func(string) ([]byte, error)
	readBuildInfo func(string) (*debug.BuildInfo, error)
	homeDir       func() (string, error)
	defaultHome   func() (string, error)
	startPreview  func(string, []string) (*previewChild, error)
	newTimer      func(time.Duration) timerHandle
}

type directExec struct {
	daemonPath string
	configPath string
}

// NewService starts the discovery loop and queues its initial refresh.
func NewService() *Service {
	return newServiceWithDependencies(serviceDependencies{})
}

func newServiceWithDependencies(deps serviceDependencies) *Service {
	if deps.run == nil {
		deps.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		}
	}
	if deps.lookPath == nil {
		deps.lookPath = exec.LookPath
	}
	if deps.stat == nil {
		deps.stat = os.Stat
	}
	if deps.evalSymlinks == nil {
		deps.evalSymlinks = filepath.EvalSymlinks
	}
	if deps.readFile == nil {
		deps.readFile = os.ReadFile
	}
	if deps.readBuildInfo == nil {
		deps.readBuildInfo = buildinfo.ReadFile
	}
	if deps.homeDir == nil {
		deps.homeDir = os.UserHomeDir
	}
	if deps.defaultHome == nil {
		deps.defaultHome = func() (string, error) {
			current, err := user.Current()
			if err != nil {
				return "", err
			}
			return current.HomeDir, nil
		}
	}
	if deps.startPreview == nil {
		deps.startPreview = func(path string, environment []string) (*previewChild, error) {
			cmd := exec.Command(path, "-test")
			cmd.Env = environment
			reader, writer, err := os.Pipe()
			if err != nil {
				return nil, err
			}
			cmd.Stdout = writer
			cmd.Stderr = io.Discard
			if err := cmd.Start(); err != nil {
				_ = reader.Close()
				_ = writer.Close()
				return nil, err
			}
			_ = writer.Close()
			ready := make(chan struct{})
			go scanPreviewReadiness(reader, ready)
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			return &previewChild{cmd: cmd, ready: ready, done: done}, nil
		}
	}
	if deps.newTimer == nil {
		deps.newTimer = func(duration time.Duration) timerHandle {
			timer := time.NewTimer(duration)
			return timerHandle{C: timer.C, stop: timer.Stop}
		}
	}
	s := &Service{
		deps:     deps,
		commands: make(chan serviceCommand, 32),
		updates:  make(chan Snapshot, 1),
		quit:     make(chan struct{}),
		done:     make(chan struct{}),
		snap:     defaultSnapshot(),
	}
	go s.run()
	s.Refresh()
	return s
}

// Snapshot returns the latest published value.
func (s *Service) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap
}

// Updates carries coalesced snapshots. The latest value is also available
// through Snapshot, so a slow receiver never blocks discovery.
func (s *Service) Updates() <-chan Snapshot { return s.updates }

// Refresh queues a serialized discovery pass.
func (s *Service) Refresh() {
	s.enqueue(serviceCommand{op: opRefresh})
}

// Apply submits a validated batch to the matching sysc-walls client.
func (s *Service) Apply(patch []Setting) bool {
	snapshot := s.Snapshot()
	if !snapshot.CanApply || snapshot.Previewing {
		return false
	}
	return s.enqueue(serviceCommand{op: opApply, patch: append([]Setting(nil), patch...)})
}

// SetEnabled changes both this session's unit state and its login enablement.
func (s *Service) SetEnabled(enabled bool) bool {
	snapshot := s.Snapshot()
	if !snapshot.ServiceAvailable || !snapshot.UnitKnown || snapshot.UnitStale ||
		(snapshot.UnitFileState != "enabled" && snapshot.UnitFileState != "disabled") ||
		(enabled && snapshot.Previewing) {
		return false
	}
	return s.enqueue(serviceCommand{op: opEnable, value: enabled})
}

// ConfigureIdle serializes timeout and enablement, and reports their completed
// result. Call off the Wayland owner: commands use the existing bounded runner.
func (s *Service) ConfigureIdle(enabled bool, timeout string) error {
	result := make(chan error, 1)
	command := serviceCommand{op: opIdle, value: enabled, result: result}
	if enabled {
		command.patch = []Setting{{Key: "timeout", Value: timeout}}
	}
	if !s.enqueue(command) {
		return errors.New("screensaver command could not be queued")
	}
	select {
	case err := <-result:
		return err
	case <-s.done:
		return errors.New("screensaver service closed before idle settings completed")
	}
}

// SetRuntimeRunning starts or stops the user unit without changing whether it
// starts at login. It is used by the lock coordinator and retry action.
func (s *Service) SetRuntimeRunning(running bool) bool {
	snapshot := s.Snapshot()
	if !snapshot.ServiceAvailable || !snapshot.UnitKnown || snapshot.UnitStale || snapshot.UnitFileState == "" ||
		snapshot.UnitFileState == "not-found" || (running && strings.HasPrefix(snapshot.UnitFileState, "masked")) ||
		(running && snapshot.Previewing) {
		return false
	}
	return s.enqueue(serviceCommand{op: opRuntime, value: running})
}

// Preview starts the verified daemon with its saved config. If a preview is
// already active, the same action stops it.
func (s *Service) Preview() bool {
	snapshot := s.Snapshot()
	if !snapshot.CanPreview && !snapshot.Previewing {
		return false
	}
	return s.enqueue(serviceCommand{op: opPreview})
}

// StopPreview queues a stop even when Preview is still starting, so the
// service loop serializes the stop after startup.
func (s *Service) StopPreview() bool {
	return s.enqueue(serviceCommand{op: opStopPreview})
}

func (s *Service) enqueue(command serviceCommand) bool {
	select {
	case <-s.quit:
		return false
	default:
	}
	select {
	case s.commands <- command:
		return true
	default:
		return false
	}
}

// Close stops discovery and waits for Preview ownership and systemd cleanup.
func (s *Service) Close() error {
	s.close.Do(func() { close(s.quit) })
	<-s.done
	return s.closeErr
}

func (s *Service) run() {
	defer close(s.done)
	var preview *previewSession
	for {
		var previewDone <-chan error
		var previewReady <-chan struct{}
		var previewStartupDeadline <-chan time.Time
		var previewDeadline <-chan time.Time
		if preview != nil {
			previewDone = preview.child.done
			if !preview.stopping {
				previewDeadline = preview.timer.C
			}
			if !preview.ready && !preview.stopping {
				previewReady = preview.child.ready
				previewStartupDeadline = preview.startupTimer.C
			}
		}
		select {
		case <-s.quit:
			if preview != nil {
				remaining, err := s.stopPreviewForClose(preview, "Preview stopped while sysc-shell was closing.")
				s.closeErr = err
				if remaining != nil {
					if s.closeErr == nil {
						s.closeErr = errors.New("Preview cleanup timed out; sysc-shell is waiting for the owned child to exit")
					}
					log.Printf("sysc-shell: Preview cleanup exceeded %s; keeping the shell alive while its owned daemon exits", previewCleanup)
					// main calls Registry.Close synchronously during shutdown. Keep
					// this owner alive until the child has exited and been reaped;
					// a background waiter alone would die when main returns.
					<-remaining.child.done
				}
			}
			return
		case command := <-s.commands:
			switch command.op {
			case opPreview:
				preview = s.togglePreview(preview)
			case opStopPreview:
				preview, _ = s.stopPreview(preview, "Preview stopped.")
			default:
				s.handle(command, preview)
			}
		case waitErr := <-previewDone:
			if preview != nil {
				preview = s.previewExited(preview, waitErr)
			}
		case <-previewReady:
			if preview != nil && !preview.ready {
				if preview.stopRequested {
					preview, _ = s.stopPreview(preview, preview.stopMessage)
				} else {
					preview = s.previewReady(preview)
				}
			}
		case <-previewStartupDeadline:
			if preview != nil && !preview.ready {
				preview, _ = s.stopPreviewAfterStartupTimeout(preview)
			}
		case <-previewDeadline:
			if preview != nil && !preview.ready {
				preview, _ = s.stopPreviewBeforeReadyDeadline(preview)
			} else {
				preview, _ = s.stopPreview(preview, "Preview stopped after its time limit.")
			}
		}
	}
}

func (s *Service) handle(command serviceCommand, preview *previewSession) {
	if command.op == opRefresh {
		s.refreshAndPublish("", "", preview)
		return
	}
	current := s.Snapshot()
	current.ActionPending = true
	current.ActionError = ""
	current.ActionMessage = ""
	s.publish(current)
	message, actionErr := s.perform(command)
	s.refreshAndPublish(message, errorText(actionErr), preview)
	if command.result != nil {
		command.result <- actionErr
	}
}

func (s *Service) refreshAndPublish(message, actionError string, preview *previewSession) {
	previous := s.Snapshot()
	next := s.discover()
	next.ActionPending = false
	if message == "" && actionError == "" {
		next.ActionError = previous.ActionError
		next.ActionMessage = previous.ActionMessage
	} else {
		next.ActionError = actionError
		next.ActionMessage = message
	}
	if preview != nil {
		next.Previewing = true
		next.PreviewReady = preview.ready
		next.PreviewStopping = preview.stopping
		next.PreviewDuration = preview.duration
		next.PreviewError = previous.PreviewError
	}
	s.publish(next)
}

func (s *Service) perform(command serviceCommand) (string, error) {
	snapshot := s.Snapshot()
	switch command.op {
	case opApply:
		return s.apply(snapshot, command.patch)
	case opEnable:
		return s.enable(snapshot, command.value)
	case opIdle:
		if command.value {
			if _, err := s.apply(snapshot, command.patch); err != nil {
				return "", err
			}
		}
		message, err := s.enable(snapshot, command.value)
		if err != nil && command.value && !snapshot.EnabledAtLogin() {
			// A failed --now start can still enable login startup. Undo that partial change.
			_, cleanupErr := s.enable(snapshot, false)
			err = errors.Join(err, cleanupErr)
		}
		return message, err
	case opRuntime:
		return s.setRuntime(snapshot, command.value)
	default:
		return "", fmt.Errorf("unknown sysc-walls operation")
	}
}

func (s *Service) togglePreview(session *previewSession) *previewSession {
	if session != nil {
		next, _ := s.stopPreview(session, "Preview stopped.")
		return next
	}

	current := s.Snapshot()
	current.ActionPending = true
	current.ActionError = ""
	current.ActionMessage = ""
	s.publish(current)
	snapshot := s.discover()
	snapshot.ActionPending = true
	s.publish(snapshot)
	if !snapshot.CanPreview {
		err := snapshot.PreviewAvailabilityError
		if err == "" {
			err = "Preview is unavailable until the unit, config, and binary identities are verified"
		}
		snapshot.ActionPending = false
		snapshot.ActionError = err
		snapshot.PreviewError = err
		s.publish(snapshot)
		return nil
	}
	duration, err := previewDuration(snapshot)
	if err != nil {
		snapshot.ActionPending = false
		snapshot.ActionError = err.Error()
		snapshot.PreviewError = err.Error()
		snapshot.PreviewAvailabilityError = err.Error()
		snapshot.CanPreview = false
		s.publish(snapshot)
		return nil
	}
	path := snapshot.DaemonPath
	child, err := s.deps.startPreview(path, prefixPath(filepath.Dir(path), os.Environ()))
	if err != nil {
		message := fmt.Sprintf("start Preview with %s: %v", path, err)
		snapshot.ActionPending = false
		snapshot.ActionError = message
		snapshot.PreviewError = message
		s.publish(snapshot)
		return nil
	}
	startupTimer := s.deps.newTimer(previewReadyTimeout)
	deadlineTimer := s.deps.newTimer(duration)
	snapshot.ActionPending = false
	snapshot.ActionError = ""
	snapshot.ActionMessage = "Starting Preview session."
	snapshot.Previewing = true
	snapshot.PreviewReady = false
	snapshot.PreviewStopping = false
	snapshot.PreviewDuration = duration
	snapshot.PreviewError = ""
	snapshot.PreviewAvailabilityError = ""
	s.publish(snapshot)
	return &previewSession{child: child, duration: duration, startupTimer: startupTimer, timer: deadlineTimer}
}

func (s *Service) previewReady(session *previewSession) *previewSession {
	session.ready = true
	if session.startupTimer.stop != nil {
		session.startupTimer.stop()
	}
	snapshot := s.Snapshot()
	snapshot.PreviewReady = true
	snapshot.ActionMessage = "Preview session started."
	s.publish(snapshot)
	return session
}

func (s *Service) stopPreview(session *previewSession, message string) (*previewSession, error) {
	if session == nil {
		snapshot := s.Snapshot()
		snapshot.ActionPending = false
		snapshot.ActionMessage = "No Preview session is active."
		snapshot.ActionError = ""
		snapshot.Previewing = false
		snapshot.PreviewReady = false
		snapshot.PreviewStopping = false
		snapshot.PreviewDuration = 0
		s.publish(snapshot)
		return nil, nil
	}
	if !session.ready && !session.stopping {
		session.stopRequested = true
		session.stopMessage = message
		snapshot := s.Snapshot()
		snapshot.ActionPending = true
		snapshot.ActionError = ""
		snapshot.ActionMessage = "Waiting for Preview startup before stopping."
		snapshot.Previewing = true
		snapshot.PreviewStopping = true
		s.publish(snapshot)
		select {
		case <-session.child.ready:
			session.ready = true
		case waitErr := <-session.child.done:
			return nil, s.finishPreview(session, message, waitErr)
		case <-session.startupTimer.C:
			return s.stopPreviewAfterStartupTimeout(session)
		case <-session.timer.C:
			return s.stopPreviewBeforeReadyDeadline(session)
		}
	}
	return s.signalPreview(session, message)
}

func (s *Service) stopPreviewAfterStartupTimeout(session *previewSession) (*previewSession, error) {
	err := fmt.Errorf("Preview did not report readiness within %s; stopping the owned daemon", previewReadyTimeout)
	session.stopErr = errors.Join(session.stopErr, err)
	return s.signalPreview(session, "Preview stopped after startup timed out.")
}

func (s *Service) stopPreviewBeforeReadyDeadline(session *previewSession) (*previewSession, error) {
	err := errors.New("Preview did not become ready before its safety deadline; stopping the owned daemon")
	session.stopErr = errors.Join(session.stopErr, err)
	return s.signalPreview(session, "Preview stopped before startup completed.")
}

func (s *Service) stopPreviewForClose(session *previewSession, message string) (*previewSession, error) {
	if session.stopping {
		return session, session.stopErr
	}
	return s.signalPreview(session, message)
}

func (s *Service) signalPreview(session *previewSession, message string) (*previewSession, error) {
	if session.startupTimer.stop != nil {
		session.startupTimer.stop()
	}
	if session.timer.stop != nil {
		session.timer.stop()
	}
	if !session.stopping {
		session.stopping = true
		snapshot := s.Snapshot()
		snapshot.ActionPending = true
		snapshot.ActionError = ""
		snapshot.ActionMessage = "Stopping Preview…"
		snapshot.Previewing = true
		snapshot.PreviewReady = session.ready
		snapshot.PreviewStopping = true
		s.publish(snapshot)
		if err := session.child.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			session.stopErr = fmt.Errorf("signal Preview daemon %s: %w", session.child.cmd.Path, err)
		}
	}
	cleanupTimer := s.deps.newTimer(previewCleanup)
	if cleanupTimer.stop != nil {
		defer cleanupTimer.stop()
	}
	select {
	case waitErr := <-session.child.done:
		return nil, s.finishPreview(session, message, waitErr)
	case <-cleanupTimer.C:
		err := fmt.Errorf("Preview daemon did not finish cleanup within %s; it was not force-killed", previewCleanup)
		session.stopErr = errors.Join(session.stopErr, err)
		snapshot := s.Snapshot()
		snapshot.ActionPending = false
		snapshot.ActionError = session.stopErr.Error()
		snapshot.ActionMessage = "Preview cleanup is still running."
		snapshot.Previewing = true
		snapshot.PreviewStopping = true
		snapshot.PreviewError = session.stopErr.Error()
		s.publish(snapshot)
		return session, session.stopErr
	}
}

func (s *Service) finishPreview(session *previewSession, message string, waitErr error) error {
	if session.startupTimer.stop != nil {
		session.startupTimer.stop()
	}
	if session.timer.stop != nil {
		session.timer.stop()
	}
	combinedErr := session.stopErr
	if waitErr != nil {
		combinedErr = errors.Join(combinedErr, fmt.Errorf("Preview daemon exited during cleanup: %w", waitErr))
	}
	snapshot := s.Snapshot()
	snapshot.ActionPending = false
	snapshot.ActionMessage = message
	snapshot.ActionError = errorText(combinedErr)
	snapshot.Previewing = false
	snapshot.PreviewReady = false
	snapshot.PreviewStopping = false
	snapshot.PreviewDuration = 0
	snapshot.PreviewError = errorText(combinedErr)
	s.publish(snapshot)
	return combinedErr
}

func (s *Service) previewExited(session *previewSession, waitErr error) *previewSession {
	if session.startupTimer.stop != nil {
		session.startupTimer.stop()
	}
	if session.timer.stop != nil {
		session.timer.stop()
	}
	err := errors.Join(session.stopErr, waitErr)
	snapshot := s.Snapshot()
	snapshot.ActionPending = false
	snapshot.ActionError = errorText(err)
	snapshot.Previewing = false
	snapshot.PreviewReady = false
	snapshot.PreviewStopping = false
	snapshot.PreviewDuration = 0
	snapshot.PreviewError = errorText(err)
	if err == nil {
		snapshot.ActionMessage = "Preview session ended."
	}
	s.publish(snapshot)
	return nil
}

func prefixPath(directory string, environment []string) []string {
	path := ""
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if ok && key == "PATH" {
			if path == "" {
				path = value
			}
			continue
		}
		result = append(result, entry)
	}
	if path == "" {
		result = append(result, "PATH="+directory)
	} else {
		result = append(result, "PATH="+directory+string(os.PathListSeparator)+path)
	}
	return result
}

func scanPreviewReadiness(output *os.File, ready chan<- struct{}) {
	defer output.Close()
	reader := bufio.NewReader(output)
	readySent := false
	for {
		line, err := reader.ReadString('\n')
		if !readySent && strings.Contains(line, "✓ Screensaver launched") {
			close(ready)
			readySent = true
		}
		if err != nil {
			return
		}
	}
}

func (s *Service) publish(snapshot Snapshot) {
	s.mu.Lock()
	s.snap = snapshot
	s.mu.Unlock()
	select {
	case s.updates <- snapshot:
	default:
		select {
		case <-s.updates:
		default:
		}
		s.updates <- snapshot
	}
}

func defaultSnapshot() Snapshot {
	return Snapshot{
		Effect:           "matrix-art",
		Theme:            "rama",
		Timeout:          "5m",
		DateTime:         "false",
		DateTimePosition: "bottom",
	}
}

func (s *Service) discover() Snapshot {
	previous := s.Snapshot()
	output, err := s.systemd("show", "--no-pager", "--property=LoadState,FragmentPath,ExecStart,Environment,EnvironmentFiles,UnitFileState,ActiveState,SubState", unitName)
	if err != nil {
		previous.UnitStale = previous.UnitKnown
		previous.StateError = fmt.Sprintf("query %s: %v", unitName, err)
		previous.CanApply = false
		previous.CanPreview = false
		if !previous.UnitKnown {
			previous.ConfigKnown = false
			previous.ConfigSourceMatches = false
			previous.ConfigSourceError = "unit state is unavailable; config source was not verified"
		}
		return previous
	}
	properties := parseProperties(string(output))
	if properties["LoadState"] == "" || (properties["LoadState"] == "loaded" &&
		(properties["UnitFileState"] == "" || properties["ActiveState"] == "" || properties["SubState"] == "")) {
		previous.UnitStale = previous.UnitKnown
		previous.StateError = "systemctl returned no LoadState for " + unitName
		previous.CanApply = false
		previous.CanPreview = false
		return previous
	}

	snapshot := defaultSnapshot()
	snapshot.UnitKnown = true
	snapshot.LoadState = properties["LoadState"]
	snapshot.FragmentPath = properties["FragmentPath"]
	snapshot.UnitFileState = properties["UnitFileState"]
	snapshot.ActiveState = properties["ActiveState"]
	snapshot.SubState = properties["SubState"]

	switch snapshot.LoadState {
	case "not-found":
		snapshot.ConfigSourceMatches = true
		s.resolveStandaloneTools(&snapshot)
	case "loaded":
		// Unit management targets the named systemd unit. It stays safe even
		// when this shell cannot use the unit's executable or config source.
		snapshot.ServiceAvailable = true
		execInfo, execErr := parseDirectExec(properties["ExecStart"])
		if execErr != nil {
			snapshot.ServiceError = execErr.Error()
			snapshot.ConfigSourceError = execErr.Error()
			return snapshot
		}
		s.resolveUnitTools(&snapshot, execInfo.daemonPath)
		snapshot.ConfigSourceMatches, snapshot.ConfigSourceError = s.configSourceMatches(properties, execInfo.configPath)
	default:
		snapshot.ServiceError = fmt.Sprintf("unsupported unit load state %q", snapshot.LoadState)
		snapshot.ConfigSourceError = snapshot.ServiceError
		return snapshot
	}

	if snapshot.ConfigSourceMatches {
		snapshot.ConfigPath = s.defaultConfigPath()
		snapshot.ConfigKnown = snapshot.ConfigPath != ""
		if snapshot.ConfigKnown {
			s.readConfig(&snapshot)
		}
	}
	s.checkIdentities(&snapshot)
	completePrefix := snapshot.ClientPath != "" && snapshot.DaemonPath != "" && snapshot.DisplayPath != "" &&
		filepath.Dir(snapshot.ClientPath) == filepath.Dir(snapshot.DaemonPath) && filepath.Dir(snapshot.DisplayPath) == filepath.Dir(snapshot.DaemonPath)
	if snapshot.LoadState == "not-found" && !completePrefix {
		snapshot.ServiceError = "no complete sysc-walls installation with matching binaries was found on PATH"
	}
	canUseClient := snapshot.ClientIdentityError == "" && (snapshot.LoadState == "loaded" || completePrefix)
	canUsePreview := snapshot.DaemonIdentityError == "" && snapshot.DisplayIdentityError == "" &&
		filepath.Dir(snapshot.DisplayPath) == filepath.Dir(snapshot.DaemonPath) && (snapshot.LoadState == "loaded" || completePrefix)
	if snapshot.Artwork != "" {
		if err := s.validateArtwork(snapshot.Artwork); err != nil {
			snapshot.PreviewAvailabilityError = err.Error()
		}
	}
	snapshot.CanApply = snapshot.UnitKnown && snapshot.ConfigSourceMatches && snapshot.ConfigKnown && snapshot.ConfigReadable && canUseClient
	snapshot.CanPreview = snapshot.UnitKnown && snapshot.ConfigSourceMatches && snapshot.ConfigKnown && snapshot.ConfigReadable && canUsePreview && snapshot.PreviewAvailabilityError == ""
	if snapshot.CanPreview {
		if _, err := previewDuration(snapshot); err != nil {
			snapshot.CanPreview = false
			snapshot.PreviewAvailabilityError = err.Error()
		}
	}
	return snapshot
}

func parseIdleTimeout(value string) (time.Duration, error) {
	if len(value) < 2 {
		return 0, fmt.Errorf("idle timeout %q must be a positive whole number of seconds, minutes, or hours", value)
	}
	unit := value[len(value)-1]
	var multiplier time.Duration
	var maxValue uint64
	switch unit {
	case 's':
		multiplier, maxValue = time.Second, 24*60*60
	case 'm':
		multiplier, maxValue = time.Minute, 24*60
	case 'h':
		multiplier, maxValue = time.Hour, 24
	default:
		return 0, fmt.Errorf("idle timeout %q must use s, m, or h", value)
	}
	for _, r := range value[:len(value)-1] {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("idle timeout %q must be a positive whole number", value)
		}
	}
	amount, err := strconv.ParseUint(value[:len(value)-1], 10, 64)
	if err != nil || amount == 0 || amount > maxValue {
		return 0, fmt.Errorf("idle timeout %q must be between 1 second and 24 hours", value)
	}
	return time.Duration(amount) * multiplier, nil
}

func (s *Service) validateArtwork(value string) error {
	path := value
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := s.deps.homeDir()
		if err != nil || home == "" {
			return fmt.Errorf("saved artwork %q cannot resolve the home directory: %v", value, err)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	} else if strings.HasPrefix(path, "~") {
		return fmt.Errorf("saved artwork %q must use an absolute path or ~/ path", value)
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("saved artwork %q must use an absolute path or ~/ path", value)
	}
	path = filepath.Clean(path)
	info, err := s.deps.stat(path)
	if err != nil {
		return fmt.Errorf("saved artwork %q is unavailable: %w", value, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("saved artwork %q is not a regular file", value)
	}
	resolved, err := s.deps.evalSymlinks(path)
	if err != nil {
		return fmt.Errorf("saved artwork %q cannot be resolved: %w", value, err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return fmt.Errorf("saved artwork %q cannot be resolved: %w", value, err)
	}
	home, err := s.deps.homeDir()
	if err != nil || home == "" {
		return fmt.Errorf("saved artwork %q cannot resolve the home directory: %v", value, err)
	}
	roots := []string{
		filepath.Join(home, ".local", "share"), filepath.Join(home, ".config"),
		"/usr/share", "/usr/local/share",
	}
	for _, root := range roots {
		resolvedRoot, err := s.deps.evalSymlinks(root)
		if err != nil {
			continue
		}
		resolvedRoot, err = filepath.Abs(resolvedRoot)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(resolvedRoot, resolved)
		if err != nil || rel == "." || rel == ".." ||
			strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("saved artwork %q is not readable: %w", value, err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("saved artwork %q could not be checked for readability: %w", value, err)
		}
		return nil
	}
	return fmt.Errorf("saved artwork %q must resolve under ~/.local/share, ~/.config, /usr/share, or /usr/local/share", value)
}

func previewDuration(snapshot Snapshot) (time.Duration, error) {
	if !snapshot.UnitKnown || snapshot.UnitStale {
		return 0, errors.New("service state is unavailable")
	}
	if !snapshot.Running() {
		return previewMax, nil
	}
	timeout, err := parseIdleTimeout(snapshot.Timeout)
	if err != nil {
		return 0, fmt.Errorf("saved idle timeout cannot bound Preview: %w", err)
	}
	duration := timeout / 2
	if duration < previewMin {
		return 0, fmt.Errorf("saved idle timeout leaves less than %s for a useful preview", previewMin)
	}
	return min(duration, previewMax), nil
}

func (s *Service) systemd(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	argv := append([]string{"--user"}, args...)
	output, err := s.deps.run(ctx, "systemctl", argv...)
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return nil, fmt.Errorf("%w: %s", err, message)
		}
		return nil, err
	}
	return output, nil
}

func (s *Service) runBounded(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	output, err := s.deps.run(ctx, name, args...)
	if err != nil {
		if text := strings.TrimSpace(string(output)); text != "" {
			return nil, fmt.Errorf("%w: %s", err, text)
		}
		return nil, err
	}
	return output, nil
}

func (s *Service) apply(snapshot Snapshot, patch []Setting) (string, error) {
	if err := validateSettingsPatch(patch); err != nil {
		return "", err
	}
	if snapshot.Previewing {
		return "", errors.New("stop Preview before applying screensaver settings")
	}
	if !snapshot.CanApply || snapshot.ClientPath == "" {
		return "", errors.New("Apply is unavailable until the config source and client build are verified")
	}
	args := make([]string, 1, 1+2*len(patch))
	args[0] = "set"
	for _, setting := range patch {
		args = append(args, setting.Key, setting.Value)
	}
	if _, err := s.runBounded(snapshot.ClientPath, args...); err != nil {
		return "", fmt.Errorf("sysc-walls settings were not saved: %w", err)
	}
	if snapshot.Running() {
		if _, err := s.systemd("try-restart", unitName); err != nil {
			return "Settings were saved, but the running screensaver may still have old values.", fmt.Errorf("could not refresh the running screensaver: %w", err)
		}
		return "Settings saved and the running screensaver restarted.", nil
	}
	if !snapshot.UnitKnown || snapshot.UnitStale {
		return "Settings saved; service state is unavailable, so restart was skipped and the daemon may still have old values.", nil
	}
	return "Settings saved. The screensaver was not running, so it was not restarted.", nil
}

func (s *Service) enable(snapshot Snapshot, enabled bool) (string, error) {
	if !snapshot.ServiceAvailable || !snapshot.UnitKnown || snapshot.UnitStale ||
		(snapshot.UnitFileState != "enabled" && snapshot.UnitFileState != "disabled") {
		return "", errors.New("unit enablement is unavailable in the current systemd state")
	}
	if enabled && snapshot.Previewing {
		return "", errors.New("stop Preview before enabling the normal screensaver service")
	}
	verb := "disable"
	message := "Screensaver disabled for this session and at login."
	if enabled {
		verb = "enable"
		message = "Screensaver enabled for this session and at login."
	}
	if _, err := s.systemd(verb, "--now", unitName); err != nil {
		return "", fmt.Errorf("could not %s sysc-walls: %w", verb, err)
	}
	return message, nil
}

func (s *Service) setRuntime(snapshot Snapshot, running bool) (string, error) {
	if !snapshot.ServiceAvailable || !snapshot.UnitKnown || snapshot.UnitStale || snapshot.UnitFileState == "" ||
		snapshot.UnitFileState == "not-found" || (running && strings.HasPrefix(snapshot.UnitFileState, "masked")) {
		return "", errors.New("runtime control is unavailable in the current systemd state")
	}
	if running && snapshot.Previewing {
		return "", errors.New("stop Preview before starting the normal screensaver service")
	}
	verb := "stop"
	message := "Screensaver stopped for this session; login enablement was not changed."
	if running {
		verb = "start"
		message = "Screensaver started for this session; login enablement was not changed."
	}
	if _, err := s.systemd(verb, unitName); err != nil {
		return "", fmt.Errorf("could not %s sysc-walls for this session: %w", verb, err)
	}
	return message, nil
}

func validateSettingsPatch(patch []Setting) error {
	if len(patch) == 0 {
		return errors.New("settings batch is empty")
	}
	allowed := map[string]bool{
		"effect": true, "theme": true, "timeout": true, "file": true,
		"datetime": true, "datetime-position": true,
	}
	seen := make(map[string]bool, len(patch))
	for _, setting := range patch {
		if !allowed[setting.Key] {
			return fmt.Errorf("unsupported sysc-walls setting %q", setting.Key)
		}
		if seen[setting.Key] {
			return fmt.Errorf("duplicate sysc-walls setting %q", setting.Key)
		}
		if strings.IndexByte(setting.Value, 0) >= 0 {
			return fmt.Errorf("sysc-walls setting %q contains a NUL byte", setting.Key)
		}
		seen[setting.Key] = true
	}
	return nil
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func parseProperties(output string) map[string]string {
	properties := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			properties[key] = value
		}
	}
	return properties
}

func parseDirectExec(value string) (directExec, error) {
	const argvMarker = " ; argv[]="
	const tailMarker = " ; ignore_errors="
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
		return directExec{}, fmt.Errorf("unsupported %s ExecStart format", unitName)
	}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "{"), "}"))
	if !strings.HasPrefix(body, "path=") {
		return directExec{}, fmt.Errorf("unsupported %s ExecStart path", unitName)
	}
	pathAndRest := strings.SplitN(strings.TrimPrefix(body, "path="), argvMarker, 2)
	if len(pathAndRest) != 2 {
		return directExec{}, fmt.Errorf("unsupported %s ExecStart argv", unitName)
	}
	path := strings.TrimSpace(pathAndRest[0])
	argvAndRest := strings.SplitN(pathAndRest[1], tailMarker, 2)
	if len(argvAndRest) != 2 || strings.ContainsAny(path, "\\\"'\t\n%$") || !filepath.IsAbs(path) {
		return directExec{}, fmt.Errorf("unsupported %s ExecStart path or arguments", unitName)
	}
	argvText := strings.TrimSpace(argvAndRest[0])
	if strings.ContainsAny(argvText, "\\\"'\t\n%$") {
		return directExec{}, fmt.Errorf("unsupported %s ExecStart quoting or expansion", unitName)
	}
	argv := strings.Fields(argvText)
	if len(argv) == 0 || argv[0] != path {
		return directExec{}, fmt.Errorf("unsupported %s ExecStart wrapper", unitName)
	}
	start := false
	configPath := ""
	for i := 1; i < len(argv); i++ {
		arg := argv[i]
		switch arg {
		case "-start":
			if start {
				return directExec{}, fmt.Errorf("unsupported duplicate -start in %s ExecStart", unitName)
			}
			start = true
		case "-debug":
		case "-config":
			if configPath != "" || i+1 >= len(argv) {
				return directExec{}, fmt.Errorf("unsupported -config in %s ExecStart", unitName)
			}
			i++
			configPath = argv[i]
			if strings.ContainsAny(configPath, "\\\"'\t\n%$") {
				return directExec{}, fmt.Errorf("unsupported -config path in %s ExecStart", unitName)
			}
		default:
			return directExec{}, fmt.Errorf("unsupported %s ExecStart argument %q", unitName, arg)
		}
	}
	if !start {
		return directExec{}, fmt.Errorf("%s ExecStart does not include -start", unitName)
	}
	return directExec{daemonPath: path, configPath: configPath}, nil
}

func (s *Service) defaultConfigPath() string {
	home, err := s.deps.homeDir()
	if err != nil || !filepath.IsAbs(home) {
		return ""
	}
	return filepath.Join(home, filepath.FromSlash(configPathSuffix))
}

func (s *Service) configSourceMatches(properties map[string]string, configOverride string) (bool, string) {
	if !emptyEnvironmentFiles(properties["EnvironmentFiles"]) {
		return false, "sysc-walls has EnvironmentFiles; config source cannot be verified"
	}
	shellHome, err := s.deps.homeDir()
	if err != nil || !filepath.IsAbs(shellHome) {
		return false, "shell home cannot be resolved"
	}
	shellConfig := filepath.Join(shellHome, filepath.FromSlash(configPathSuffix))
	if configOverride != "" {
		resolved, resolveErr := s.resolveConfigOverride(configOverride, properties)
		if resolveErr != nil {
			return false, resolveErr.Error()
		}
		if samePath(s.deps.evalSymlinks, resolved, shellConfig) {
			return true, ""
		}
		return false, fmt.Sprintf("sysc-walls -config path %q does not match shell config %q", resolved, shellConfig)
	}
	unitHome, unitHomeSet, err := environmentValue(properties["Environment"], "HOME")
	if err != nil {
		return false, "sysc-walls Environment cannot be parsed: " + err.Error()
	}
	if unitHomeSet && unitHome == "" {
		return false, "sysc-walls Environment sets an empty HOME"
	}
	if !unitHomeSet {
		output, envErr := s.systemd("show-environment")
		if envErr != nil {
			return false, "systemd manager environment is unavailable: " + envErr.Error()
		}
		var unitHomeSet bool
		unitHome, unitHomeSet, err = environmentLine(string(output), "HOME")
		if err != nil {
			return false, "systemd manager HOME cannot be parsed: " + err.Error()
		}
		if unitHomeSet && unitHome == "" {
			return false, "systemd manager environment sets an empty HOME"
		}
		if !unitHomeSet {
			unitHome, err = s.deps.defaultHome()
			if err != nil {
				return false, "user home cannot be resolved: " + err.Error()
			}
		}
	}
	if !filepath.IsAbs(unitHome) || strings.ContainsAny(unitHome, "\\\"'\t\n%$") {
		return false, "sysc-walls HOME is not a supported absolute path"
	}
	if filepath.Clean(unitHome) != filepath.Clean(shellHome) {
		return false, fmt.Sprintf("sysc-walls HOME %q does not match shell HOME %q", unitHome, shellHome)
	}
	return true, ""
}

func (s *Service) resolveConfigOverride(configOverride string, properties map[string]string) (string, error) {
	if filepath.IsAbs(configOverride) {
		if strings.ContainsAny(configOverride, "\\\"'\t\n%$") {
			return "", errors.New("sysc-walls -config path contains unsupported expansion")
		}
		return filepath.Clean(configOverride), nil
	}
	if !strings.HasPrefix(configOverride, "~/") {
		return "", errors.New("sysc-walls -config path is not absolute or home-relative")
	}
	if !emptyEnvironmentFiles(properties["EnvironmentFiles"]) {
		return "", errors.New("sysc-walls -config uses an unresolved environment file")
	}
	unitHome, found, err := environmentValue(properties["Environment"], "HOME")
	if err != nil {
		return "", err
	}
	if found && unitHome == "" {
		return "", errors.New("sysc-walls Environment sets an empty HOME")
	}
	if !found {
		output, err := s.systemd("show-environment")
		if err != nil {
			return "", fmt.Errorf("systemd manager environment is unavailable: %w", err)
		}
		unitHome, found, err = environmentLine(string(output), "HOME")
		if err != nil {
			return "", err
		}
		if !found {
			unitHome, err = s.deps.defaultHome()
			if err != nil {
				return "", err
			}
		}
	}
	if !filepath.IsAbs(unitHome) || strings.ContainsAny(unitHome, "\\\"'\t\n%$") {
		return "", errors.New("sysc-walls HOME is not a supported absolute path")
	}
	return filepath.Join(unitHome, strings.TrimPrefix(configOverride, "~/")), nil
}

func samePath(resolve func(string) (string, error), left, right string) bool {
	leftPath, leftErr := resolve(left)
	rightPath, rightErr := resolve(right)
	if leftErr == nil && rightErr == nil {
		return filepath.Clean(leftPath) == filepath.Clean(rightPath)
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func environmentValue(environment, key string) (string, bool, error) {
	value := ""
	found := false
	for _, field := range strings.Fields(environment) {
		name, v, ok := strings.Cut(field, "=")
		if !ok || name != key {
			continue
		}
		if found {
			return "", false, fmt.Errorf("duplicate %s entry", key)
		}
		found = true
		value = v
	}
	return value, found, nil
}

func environmentLine(environment, key string) (string, bool, error) {
	value := ""
	found := false
	for _, line := range strings.Split(environment, "\n") {
		name, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || name != key {
			continue
		}
		if found {
			return "", false, fmt.Errorf("duplicate %s entry", key)
		}
		found = true
		value = v
	}
	return value, found, nil
}

func emptyEnvironmentFiles(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == "[]"
}

func (s *Service) resolveUnitTools(snapshot *Snapshot, daemon string) {
	resolved, err := s.resolveExecutable(daemon)
	if err != nil {
		snapshot.ServiceError = fmt.Sprintf("%s: %v", daemon, err)
		return
	}
	snapshot.DaemonPath = resolved
	snapshot.ClientPath, snapshot.ClientIdentityError = s.resolveSibling(resolved, "sysc-walls-client")
	snapshot.DisplayPath, snapshot.DisplayIdentityError = s.resolveSibling(resolved, "sysc-walls-display")
}

func (s *Service) resolveStandaloneTools(snapshot *Snapshot) {
	names := []string{"sysc-walls-daemon", "sysc-walls-client", "sysc-walls-display"}
	paths := make(map[string]string, len(names))
	errs := make(map[string]error, len(names))
	for _, name := range names {
		path, err := s.deps.lookPath(name)
		if err == nil {
			path, err = s.resolveExecutable(path)
		}
		paths[name], errs[name] = path, err
	}
	snapshot.DaemonPath = paths[names[0]]
	snapshot.ClientPath = paths[names[1]]
	snapshot.DisplayPath = paths[names[2]]
	if errs[names[0]] != nil {
		snapshot.DaemonIdentityError = fmt.Sprintf("%s: %v", names[0], errs[names[0]])
	}
	if errs[names[1]] != nil {
		snapshot.ClientIdentityError = fmt.Sprintf("%s: %v", names[1], errs[names[1]])
	}
	if errs[names[2]] != nil {
		snapshot.DisplayIdentityError = fmt.Sprintf("%s: %v", names[2], errs[names[2]])
	}
}

func (s *Service) resolveSibling(daemon, name string) (string, string) {
	path := filepath.Join(filepath.Dir(daemon), name)
	resolved, err := s.resolveExecutable(path)
	if err != nil {
		return "", fmt.Sprintf("%s: %v", path, err)
	}
	return resolved, ""
}

func (s *Service) resolveExecutable(path string) (string, error) {
	info, err := s.deps.stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("not an executable regular file")
	}
	resolved, err := s.deps.evalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(resolved)
}

func (s *Service) checkIdentities(snapshot *Snapshot) {
	if snapshot.ClientPath != "" && snapshot.ClientIdentityError == "" {
		snapshot.ClientIdentityError = s.checkIdentity(snapshot.ClientPath, clientMain, []string{configGate, previewGate})
	}
	if snapshot.DaemonPath != "" && snapshot.DaemonIdentityError == "" {
		snapshot.DaemonIdentityError = s.checkIdentity(snapshot.DaemonPath, daemonMain, []string{previewGate})
	}
	if snapshot.DisplayPath != "" && snapshot.DisplayIdentityError == "" {
		snapshot.DisplayIdentityError = s.checkIdentity(snapshot.DisplayPath, displayMain, []string{previewGate})
	}
}

func (s *Service) checkIdentity(path, main string, revisions []string) string {
	info, err := s.deps.readBuildInfo(path)
	if err != nil {
		return fmt.Sprintf("%s: cannot read Go build info: %v", path, err)
	}
	if err := verifyBuildIdentity(info, main, revisions); err != nil {
		return fmt.Sprintf("%s: %v", path, err)
	}
	return ""
}

func verifyBuildIdentity(info *debug.BuildInfo, main string, revisions []string) error {
	if info == nil {
		return errors.New("Go build info is missing")
	}
	if info.Path != main {
		return fmt.Errorf("Go build info package %q does not match %q", info.Path, main)
	}
	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if settings["vcs.revision"] == "" {
		return errors.New("Go build info has no vcs.revision")
	}
	if settings["vcs.modified"] != "false" {
		return fmt.Errorf("Go build info vcs.modified is %q, want false", settings["vcs.modified"])
	}
	for _, revision := range revisions {
		if settings["vcs.revision"] == revision {
			return nil
		}
	}
	return fmt.Errorf("Go build info revision %q is not accepted", settings["vcs.revision"])
}

func (s *Service) readConfig(snapshot *Snapshot) {
	data, err := s.deps.readFile(snapshot.ConfigPath)
	if errors.Is(err, fs.ErrNotExist) {
		snapshot.ConfigExists = false
		snapshot.ConfigReadable = true
		return
	}
	if err != nil {
		snapshot.ConfigReadError = fmt.Sprintf("read %s: %v", snapshot.ConfigPath, err)
		return
	}
	snapshot.ConfigExists = true
	snapshot.ConfigReadable = true
	readConfigValues(snapshot, data)
}

func readConfigValues(snapshot *Snapshot, data []byte) {
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch section + "." + key {
		case "idle.timeout":
			snapshot.Timeout = value
		case "animation.effect":
			snapshot.Effect = value
		case "animation.theme":
			snapshot.Theme = value
		case "animation.file":
			snapshot.Artwork = value
		case "animation.datetime":
			snapshot.DateTime = value
		case "datetime.position":
			snapshot.DateTimePosition = value
		}
	}
}
