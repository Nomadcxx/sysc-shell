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

// processWatch owns the one Wait call for a process. The engine needs to reap
// children during shutdown, while waitForQuery also needs to notice a child
// that exits before its socket becomes ready.
type processWatch struct {
	Process
	done chan error
}

func watchProcess(proc Process) *processWatch {
	w := &processWatch{Process: proc, done: make(chan error, 1)}
	go func() {
		w.done <- proc.Wait()
		close(w.done)
	}()
	return w
}

func waitProcess(proc Process, timeout time.Duration) bool {
	w, ok := proc.(*processWatch)
	if !ok {
		return false
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-w.done:
		return true
	case <-timer.C:
		return false
	}
}

const (
	// defaultReadyWait bounds the wait for a freshly launched gSlapper to
	// answer. The socket file appearing is not readiness: sysc-greet proved
	// that the file can exist well before the server behind it will answer.
	//
	// It is generous because it has to cover the work, not just the start-up:
	// a real wallpaper is a 4K still of twenty megabytes or more, and gSlapper
	// decodes and uploads it before it will answer anything. Three seconds was
	// sized against small test files and rejected genuine wallpapers. A child
	// that dies still fails immediately, so a wrong path costs nothing.
	defaultReadyWait = 20 * time.Second
	defaultPoll      = 50 * time.Millisecond
	// stopWait bounds the wait for a stopped instance to release its socket.
	// Shutdown does no decoding, so it stays short.
	stopWait = 3 * time.Second
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
	locks     map[string]*sync.Mutex
	closed    bool
	awww      Process
	awwwMu    sync.Mutex
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
		locks:     map[string]*sync.Mutex{},
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
	caps.Statics = installedFallbacks(lookup)
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

func (e *gslapperEngine) lockConnector(connector string) func() {
	e.mu.Lock()
	if e.locks == nil {
		e.locks = map[string]*sync.Mutex{}
	}
	lock := e.locks[connector]
	if lock == nil {
		lock = &sync.Mutex{}
		e.locks[connector] = lock
	}
	e.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func (e *gslapperEngine) isClosed() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closed
}

var errEngineClosed = errors.New("wallpaper: engine is closed")

// Apply puts one path on one output.
func (e *gslapperEngine) Apply(job Job, set Settings) (string, error) {
	if err := checkPath(job.Path); err != nil {
		return "", err
	}
	if _, err := os.Stat(job.Path); err != nil {
		return "", fmt.Errorf("wallpaper: %w", err)
	}
	unlock := e.lockConnector(job.Connector)
	defer unlock()
	if e.isClosed() {
		return "", errEngineClosed
	}

	caps := e.Capabilities()
	if caps.EngineFor(job.Kind) != EngineGSlapper {
		if job.Kind == KindVideo {
			return "", errors.New("wallpaper: gslapper is not installed, so video cannot play")
		}
		return "", e.startFallback(job.Connector, job.Path)
	}

	// A fallback we started is stopped before gSlapper takes the output, so
	// the two never fight over the same surface (D15).
	if err := e.stopFallback(job.Connector); err != nil {
		return "", err
	}

	socket := socketPath(e.dir, job.Connector)
	owned := e.ownedProcess(job.Connector)
	_, socketErr := os.Stat(socket)
	switch {
	case socketErr == nil:
		if owned == nil {
			return "", fmt.Errorf("wallpaper: %s socket is not owned by this shell", job.Connector)
		}
		if e.liveSocket(socket) {
			// gSlapper needs --auto-stop to change a video path, so at any other
			// hidden setting the change is known to fail and is not attempted.
			attemptChange := job.Kind != KindVideo || !videoChangeNeedsRestart(set.Hidden)
			if attemptChange {
				reply, err := e.request(socket, "change "+job.Path, ipcTimeout)
				if err == nil {
					err = checkOK(reply)
					if err == nil {
						return e.stillFor(job), nil
					}
				}
				if classifyChangeError(err.Error()) == changeKeep {
					return "", fmt.Errorf("wallpaper: change refused: %w", err)
				}
			}
		}
		if err := e.stopOwned(job.Connector, socket); err != nil {
			return "", err
		}
	case !os.IsNotExist(socketErr):
		return "", fmt.Errorf("wallpaper: stat %s: %w", socket, socketErr)
	case owned != nil:
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
	if still := CachedStillPath(job.Path); still != "" {
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
	tracked := watchProcess(proc)
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		_ = tracked.Stop()
		_ = waitProcess(tracked, stopWait)
		return errEngineClosed
	}
	e.owned[job.Connector] = tracked
	e.mu.Unlock()

	if err := e.waitForQuery(tracked, socket); err != nil {
		_ = tracked.Stop()
		_ = waitProcess(tracked, stopWait)
		e.mu.Lock()
		if e.owned[job.Connector] == tracked {
			delete(e.owned, job.Connector)
		}
		e.mu.Unlock()
		return err
	}
	return nil
}

// waitForQuery polls until the new instance answers, the child dies, or the
// budget runs out. The socket file existing is not readiness.
func (e *gslapperEngine) waitForQuery(proc Process, socket string) error {
	var exited <-chan error
	if tracked, ok := proc.(*processWatch); ok {
		exited = tracked.done
	}
	deadline := time.After(e.readyWait)
	for {
		if reply, err := e.request(socket, "query", ipcTimeout); err == nil && checkOK(reply) == nil {
			return nil
		}
		select {
		case <-exited:
			return errors.New("wallpaper: gslapper exited before it was ready")
		case <-deadline:
			return fmt.Errorf("wallpaper: gslapper did not answer within %s", e.readyWait)
		case <-time.After(e.poll):
		}
	}
}

// liveSocket reports whether our socket for this output answers.
func (e *gslapperEngine) liveSocket(socket string) bool {
	if _, err := os.Stat(socket); err != nil {
		return false
	}
	reply, err := e.request(socket, "query", ipcTimeout)
	return err == nil && checkOK(reply) == nil
}

// stopOwned ends the gSlapper on one output: IPC first, then the handle we
// hold, then a bounded wait for the socket to disappear.
//
// There is deliberately no match-by-name step. If the socket outlives both and
// we hold no handle for it, that is reported rather than resolved by killing
// something that merely looks like ours.
func (e *gslapperEngine) stopOwned(connector, socket string) error {
	e.mu.Lock()
	proc := e.owned[connector]
	e.mu.Unlock()
	if proc == nil {
		if _, err := os.Stat(socket); os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("wallpaper: %s still holds %s and is not ours to stop", connector, socket)
	}
	if _, err := os.Stat(socket); err == nil {
		_, _ = e.request(socket, "stop", ipcTimeout)
	}

	if waitProcess(proc, 100*time.Millisecond) {
		_ = os.Remove(socket)
		e.clearOwned(connector, proc)
		return nil
	}
	if err := proc.Stop(); err != nil {
		return fmt.Errorf("wallpaper: stop gslapper on %s: %w", connector, err)
	}
	if !waitProcess(proc, stopWait) {
		if _, err := os.Stat(socket); os.IsNotExist(err) {
			e.clearOwned(connector, proc)
			return nil
		}
		return fmt.Errorf("wallpaper: gslapper on %s did not exit", connector)
	}
	_ = os.Remove(socket)
	e.clearOwned(connector, proc)
	return nil
}

func (e *gslapperEngine) clearOwned(connector string, proc Process) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.owned[connector] == proc {
		delete(e.owned, connector)
	}
}

// SetPaused holds or releases playback through IPC.
func (e *gslapperEngine) SetPaused(connector string, paused bool) error {
	unlock := e.lockConnector(connector)
	defer unlock()
	if e.isClosed() {
		return errEngineClosed
	}
	if e.ownedProcess(connector) == nil {
		return fmt.Errorf("wallpaper: %s socket is not owned by this shell", connector)
	}
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
	unlock := e.lockConnector(connector)
	defer unlock()
	if e.isClosed() {
		return errEngineClosed
	}
	socket := socketPath(e.dir, connector)
	if err := e.stopOwned(connector, socket); err != nil {
		return err
	}
	if err := e.stopFallback(connector); err != nil {
		return err
	}
	if still == "" {
		return nil
	}
	return e.startFallback(connector, still)
}

// startFallback puts one still on one output through awww or swaybg.
//
// The two engines are shaped differently. swaybg owns the surface, so its
// process is recorded and stopped later. awww is a client for awww-daemon: it
// exits at once, so there is no pid worth keeping, its exit status is the only
// evidence the wallpaper was set, and the daemon has to be up first (D16).
func (e *gslapperEngine) startFallback(connector, path string) error {
	caps := e.Capabilities()
	static := caps.Static()
	if static == "" {
		return ErrNoStaticEngine
	}
	argv, err := fallbackArgs(static, path, connector)
	if err != nil {
		return err
	}
	if err := e.stopFallback(connector); err != nil {
		return err
	}

	if fallbackIsOneShot(static) {
		if err := e.ensureAwwwDaemon(); err != nil {
			return err
		}
		return e.runToCompletion(argv)
	}

	proc, err := e.spawn(argv)
	if err != nil {
		return fmt.Errorf("wallpaper: launch %s: %w", static, err)
	}
	tracked := watchProcess(proc)
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		_ = tracked.Stop()
		_ = waitProcess(tracked, stopWait)
		return errEngineClosed
	}
	e.fallbacks[connector] = tracked
	e.mu.Unlock()
	return nil
}

// runToCompletion runs a one-shot client and reports a non-zero exit. Without
// this a missing daemon looks like a successful restore and the user is left
// staring at an unchanged desktop with nothing to explain it.
func (e *gslapperEngine) runToCompletion(argv []string) error {
	proc, err := e.spawn(argv)
	if err != nil {
		return fmt.Errorf("wallpaper: launch %s: %w", argv[0], err)
	}
	done := make(chan error, 1)
	go func() { done <- proc.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("wallpaper: %s: %w", argv[0], err)
		}
		return nil
	case <-time.After(e.readyWait):
		_ = proc.Stop()
		select {
		case <-done:
		case <-time.After(stopWait):
		}
		return fmt.Errorf("wallpaper: %s did not finish", argv[0])
	}
}

// ensureAwwwDaemon starts awww-daemon when it is not already answering. A
// daemon someone else started is reused rather than replaced.
func (e *gslapperEngine) ensureAwwwDaemon() error {
	e.awwwMu.Lock()
	defer e.awwwMu.Unlock()
	if e.isClosed() {
		return errEngineClosed
	}
	if e.runToCompletion(awwwQueryArgs()) == nil {
		return nil
	}
	proc, err := e.spawn(awwwDaemonArgs())
	if err != nil {
		return fmt.Errorf("wallpaper: launch %s: %w", engineAwwwDaemon, err)
	}
	tracked := watchProcess(proc)
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		_ = tracked.Stop()
		_ = waitProcess(tracked, stopWait)
		return errEngineClosed
	}
	e.awww = tracked
	e.mu.Unlock()
	deadline := time.Now().Add(e.readyWait)
	for time.Now().Before(deadline) {
		if e.isClosed() {
			return errEngineClosed
		}
		time.Sleep(e.poll)
		if e.runToCompletion(awwwQueryArgs()) == nil {
			return nil
		}
	}
	return fmt.Errorf("wallpaper: %s did not come up", engineAwwwDaemon)
}

// stopFallback ends only a fallback this process started. A swaybg or awww the
// user is running is left alone.
func (e *gslapperEngine) stopFallback(connector string) error {
	e.mu.Lock()
	proc := e.fallbacks[connector]
	e.mu.Unlock()
	if proc == nil {
		return nil
	}
	if err := proc.Stop(); err != nil {
		return fmt.Errorf("wallpaper: stop fallback on %s: %w", connector, err)
	}
	if !waitProcess(proc, stopWait) {
		return fmt.Errorf("wallpaper: fallback on %s did not exit", connector)
	}
	e.mu.Lock()
	if e.fallbacks[connector] == proc {
		delete(e.fallbacks, connector)
	}
	e.mu.Unlock()
	return nil
}

// Close stops only wallpaper processes this engine started and waits for their
// Wait calls to finish. The service invokes it during shutdown before waiting
// on in-flight applies, so a readiness wait cannot keep the unit alive.
func (e *gslapperEngine) Close() {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	e.closed = true
	owned := make(map[string]Process, len(e.owned))
	for connector, proc := range e.owned {
		owned[connector] = proc
	}
	fallbacks := make([]Process, 0, len(e.fallbacks))
	for connector, proc := range e.fallbacks {
		fallbacks = append(fallbacks, proc)
		delete(e.fallbacks, connector)
	}
	awww := e.awww
	e.awww = nil
	e.mu.Unlock()

	var wg sync.WaitGroup
	for connector, proc := range owned {
		wg.Add(1)
		go func(connector string, proc Process) {
			defer wg.Done()
			socket := socketPath(e.dir, connector)
			if _, err := os.Stat(socket); err == nil {
				_, _ = e.request(socket, "stop", ipcTimeout)
			}
			_ = proc.Stop()
			_ = waitProcess(proc, stopWait)
			_ = os.Remove(socket)
		}(connector, proc)
	}
	for _, proc := range fallbacks {
		wg.Add(1)
		go func(proc Process) {
			defer wg.Done()
			_ = proc.Stop()
			_ = waitProcess(proc, stopWait)
		}(proc)
	}
	if awww != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = awww.Stop()
			_ = waitProcess(awww, stopWait)
		}()
	}
	wg.Wait()
	e.mu.Lock()
	e.owned = map[string]Process{}
	e.mu.Unlock()
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

// fallbackProcess returns the persistent fallback we started for one output,
// or nil when there is none (a one-shot client leaves no handle).
func (e *gslapperEngine) fallbackProcess(connector string) Process {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.fallbacks[connector]
}
