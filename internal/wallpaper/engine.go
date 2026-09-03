package wallpaper

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Process is one engine we started. It is an interface so the whole apply path
// is testable without exec.
type Process interface {
	// Wait blocks until the process exits.
	Wait() error
	// Stop ends the process. It is only ever called on a handle we own.
	Stop() error
}

const (
	// readyWait bounds the wait for a freshly launched gSlapper to answer.
	// The socket file appearing is not readiness: sysc-greet proved that the
	// file can exist well before the server behind it will answer.
	defaultReadyWait = 3 * time.Second
	defaultPoll      = 50 * time.Millisecond
)

// gslapperEngine drives one gSlapper per output over sockets we own, with awww
// or swaybg as the static fallback.
//
// Every process this holds is one we started, and the only handles it will
// ever stop are in owned and fallbacks. Nothing here matches a process by
// name: a gSlapper the user started, or the shell that is running these
// tests, must never be a candidate (D17/D18).
type gslapperEngine struct {
	dir       string
	caps      Capabilities
	readyWait time.Duration
	poll      time.Duration

	spawn   func(argv []string) (Process, error)
	request func(socket, command string, timeout time.Duration) (string, error)

	mu        sync.Mutex
	owned     map[string]Process
	fallbacks map[string]Process
}

// NewEngine builds the real engine: exec for spawn, unix sockets for IPC.
func NewEngine(dir string, lookup func(string) bool) *gslapperEngine {
	return &gslapperEngine{
		dir:       dir,
		caps:      probeCapabilities(lookup),
		readyWait: defaultReadyWait,
		poll:      defaultPoll,
		spawn:     spawnDetached,
		request:   Request,
		owned:     map[string]Process{},
		fallbacks: map[string]Process{},
	}
}

// probeCapabilities decides what is installed. gSlapper is probed by running
// its help rather than by trusting the name on PATH, because a pre-1.5 build
// has the binary but not the IPC contract this slice depends on (D12).
func probeCapabilities(lookup func(string) bool) Capabilities {
	if lookup == nil {
		lookup = func(name string) bool { _, err := exec.LookPath(name); return err == nil }
	}
	var caps Capabilities
	if lookup("gslapper") {
		if help, err := exec.Command("gslapper", "--help").CombinedOutput(); err == nil {
			caps.GSlapper = helpSupports(help)
		}
	}
	if static, err := pickFallback(lookup); err == nil {
		caps.Static = static
	}
	return caps
}

func (e *gslapperEngine) Capabilities() Capabilities {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.caps
}

func (e *gslapperEngine) ownedProcess(connector string) Process {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.owned[connector]
}

// Apply puts one path on one output.
func (e *gslapperEngine) Apply(job Job, set Settings) (string, error) {
	if err := checkPath(job.Path); err != nil {
		return "", err
	}
	if _, err := os.Stat(job.Path); err != nil {
		return "", fmt.Errorf("wallpaper: %w", err)
	}

	caps := e.Capabilities()
	if !caps.GSlapper {
		if job.Kind == KindVideo {
			return "", errors.New("wallpaper: gslapper is not installed, so video cannot play")
		}
		return "", e.startFallback(job.Connector, job.Path)
	}

	// A fallback we started is stopped before gSlapper takes the output, so
	// the two never fight over the same surface (D15).
	e.stopFallback(job.Connector)

	socket := socketPath(e.dir, job.Connector)
	if e.liveSocket(socket) {
		// gSlapper needs --auto-stop to change a video path, so at any other
		// hidden setting the change is known to fail and is not attempted.
		attemptChange := job.Kind != KindVideo || !videoChangeNeedsRestart(set.Hidden)
		if attemptChange {
			reply, err := e.request(socket, "change "+job.Path, ipcTimeout)
			if err == nil && checkOK(reply) == nil {
				return e.stillFor(job), nil
			}
			if err != nil && classifyChangeError(err.Error()) == changeKeep {
				return "", fmt.Errorf("wallpaper: change refused: %w", err)
			}
		}
		if err := e.stopOwned(job.Connector, socket); err != nil {
			return "", err
		}
	}
	if err := e.launch(job, set, socket); err != nil {
		return "", err
	}
	return e.stillFor(job), nil
}

// stillFor is the preview a video apply reports back. Extraction is not wired
// in this slice: a video with no cached still simply leaves the theme seed
// alone, which is what the design asks for (D15).
func (e *gslapperEngine) stillFor(job Job) string {
	if job.Kind != KindVideo {
		return ""
	}
	if still := cachedStillPath(job.Path); still != "" {
		if _, err := os.Stat(still); err == nil {
			return still
		}
	}
	return ""
}

// launch starts one gSlapper and waits until it actually answers.
func (e *gslapperEngine) launch(job Job, set Settings, socket string) error {
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		return fmt.Errorf("wallpaper: mkdir socket dir: %w", err)
	}
	// A leftover socket file from a previous run would make the readiness
	// check pass against nothing.
	_ = os.Remove(socket)

	proc, err := e.spawn(launchArgs(set, socket, job.Connector, job.Path))
	if err != nil {
		return fmt.Errorf("wallpaper: launch gslapper: %w", err)
	}
	e.mu.Lock()
	e.owned[job.Connector] = proc
	e.mu.Unlock()

	if err := e.waitForQuery(proc, socket); err != nil {
		_ = proc.Stop()
		e.mu.Lock()
		delete(e.owned, job.Connector)
		e.mu.Unlock()
		return err
	}
	return nil
}

// waitForQuery polls until the new instance answers, the child dies, or the
// budget runs out. The socket file existing is not readiness.
func (e *gslapperEngine) waitForQuery(proc Process, socket string) error {
	exited := make(chan struct{})
	go func() {
		_ = proc.Wait()
		close(exited)
	}()

	deadline := time.After(e.readyWait)
	for {
		if _, err := e.request(socket, "query", ipcTimeout); err == nil {
			return nil
		}
		select {
		case <-exited:
			return errors.New("wallpaper: gslapper exited before it was ready")
		case <-deadline:
			return errors.New("wallpaper: gslapper did not answer within the startup budget")
		case <-time.After(e.poll):
		}
	}
}

// liveSocket reports whether our socket for this output answers.
func (e *gslapperEngine) liveSocket(socket string) bool {
	if _, err := os.Stat(socket); err != nil {
		return false
	}
	_, err := e.request(socket, "query", ipcTimeout)
	return err == nil
}

// stopOwned ends the gSlapper on one output: IPC first, then the handle we
// hold, then a bounded wait for the socket to disappear.
//
// There is deliberately no match-by-name step. If the socket outlives both and
// we hold no handle for it, that is reported rather than resolved by killing
// something that merely looks like ours.
func (e *gslapperEngine) stopOwned(connector, socket string) error {
	if _, err := os.Stat(socket); err == nil {
		_, _ = e.request(socket, "stop", ipcTimeout)
	}
	e.mu.Lock()
	proc := e.owned[connector]
	delete(e.owned, connector)
	e.mu.Unlock()

	deadline := time.Now().Add(e.readyWait)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socket); os.IsNotExist(err) {
			return nil
		}
		time.Sleep(e.poll)
	}
	if proc != nil {
		if err := proc.Stop(); err != nil {
			return fmt.Errorf("wallpaper: stop gslapper on %s: %w", connector, err)
		}
		_ = os.Remove(socket)
		return nil
	}
	return fmt.Errorf("wallpaper: %s still holds %s and is not ours to stop", connector, socket)
}

// SetPaused holds or releases playback through IPC.
func (e *gslapperEngine) SetPaused(connector string, paused bool) error {
	command := "resume"
	if paused {
		command = "pause"
	}
	socket := socketPath(e.dir, connector)
	reply, err := e.request(socket, command, ipcPlaybackTimeout)
	if err != nil {
		return err
	}
	return checkOK(reply)
}

// Restore stops our gSlapper on one output and puts the last still on the
// static fallback. This is the one place gSlapper-first is deliberately given
// up (D16); with no still the output is simply left empty.
func (e *gslapperEngine) Restore(connector, still string) error {
	socket := socketPath(e.dir, connector)
	if _, err := os.Stat(socket); err == nil {
		if err := e.stopOwned(connector, socket); err != nil {
			return err
		}
	}
	if still == "" {
		return nil
	}
	return e.startFallback(connector, still)
}

// startFallback puts one still on one output through awww or swaybg, recording
// the process so we can stop exactly the one we started.
func (e *gslapperEngine) startFallback(connector, path string) error {
	caps := e.Capabilities()
	if caps.Static == "" {
		return ErrNoStaticEngine
	}
	argv, err := fallbackArgs(caps.Static, path, connector)
	if err != nil {
		return err
	}
	e.stopFallback(connector)
	proc, err := e.spawn(argv)
	if err != nil {
		return fmt.Errorf("wallpaper: launch %s: %w", caps.Static, err)
	}
	e.mu.Lock()
	e.fallbacks[connector] = proc
	e.mu.Unlock()
	return nil
}

// stopFallback ends only a fallback this process started. A swaybg or awww the
// user is running is left alone.
func (e *gslapperEngine) stopFallback(connector string) {
	e.mu.Lock()
	proc := e.fallbacks[connector]
	delete(e.fallbacks, connector)
	e.mu.Unlock()
	if proc != nil {
		_ = proc.Stop()
	}
}

// osProcess adapts exec.Cmd to Process.
type osProcess struct{ cmd *exec.Cmd }

func (p *osProcess) Wait() error { return p.cmd.Wait() }

// Stop signals the process group we created, so a wallpaper engine that forked
// helpers takes them with it.
func (p *osProcess) Stop() error {
	if p.cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM); err != nil {
		return p.cmd.Process.Kill()
	}
	return nil
}

// spawnDetached starts one engine in its own process group with no streams
// attached, so it neither writes into the shell's output nor dies with a
// terminal.
func spawnDetached(argv []string) (Process, error) {
	if len(argv) == 0 {
		return nil, errors.New("wallpaper: empty argv")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &osProcess{cmd: cmd}, nil
}
