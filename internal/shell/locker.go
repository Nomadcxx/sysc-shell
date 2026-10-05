package shell

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
)

// lockHandshakeLine is emitted by sysc-lock on stdout the moment the
// compositor confirms the session lock (ext-session-lock-v1 "locked").
// Third-party lockers never send it; they simply count as unacquired,
// which keeps error reporting honest.
const lockHandshakeLine = "sysc-lock: locked"

// lockerRespawnBackoff is the pause before the single respawn attempt.
const lockerRespawnBackoff = 250 * time.Millisecond

var errLockerRunning = errors.New("locker already running")

type lockerSpawnFn func(argv []string) (io.ReadCloser, <-chan int, error)

// lockerManager tracks one session-lock process: single-flight request,
// handshake watch, wallpaper pause across its lifetime, and exactly one
// respawn if it crashes after having acquired the lock (niri readmits a
// new lock after client death, so re-acquire is load-bearing).
type lockerManager struct {
	// stateCB is called with m.mu held (order: manager -> registry).
	stateCB func(running, acquired bool)
	spawn   lockerSpawnFn
	paused  func(bool)
	sleep   func(time.Duration)

	mu          sync.Mutex
	running     bool
	acquired    bool
	respawnUsed bool
	exitCode    int
}

type LockState struct {
	Running       bool
	Acquired      bool
	RespawnedUsed bool
	ExitCode      int
}

func (m *lockerManager) State() LockState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return LockState{Running: m.running, Acquired: m.acquired,
		RespawnedUsed: m.respawnUsed, ExitCode: m.exitCode}
}

func (m *lockerManager) request(argv []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return errLockerRunning
	}
	m.respawnUsed = false // a fresh user/idle request re-arms the budget
	return m.beginLocked(argv)
}

// beginLocked starts the process; caller holds m.mu.
func (m *lockerManager) beginLocked(argv []string) error {
	out, exits, err := m.spawn(argv)
	if err != nil {
		return err
	}
	m.running = true
	m.acquired = false
	m.notifyLocked()
	m.exitCode = 0
	if m.paused != nil {
		m.paused(true)
	}
	go m.pump(out, exits, argv)
	return nil
}

func (m *lockerManager) pump(out io.Reader, exits <-chan int, argv []string) {
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 0, 512), 4096)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == lockHandshakeLine {
			m.mu.Lock()
			m.acquired = true
			m.notifyLocked()
			m.mu.Unlock()
		}
	}
	code := <-exits

	m.mu.Lock()
	m.exitCode = code
	m.notifyLocked()
	// ponytail: one respawn total; if the locker keeps crashing the shell
	// surfaces the failure instead of respawning in a loop.
	needRespawn := m.acquired && code != 0 && !m.respawnUsed
	if needRespawn {
		m.respawnUsed = true
		// Keep running=true across backoff so a concurrent request cannot
		// start a second locker (niri will refuse it with finished).
		m.mu.Unlock()
		m.sleep(lockerRespawnBackoff)
		m.mu.Lock()
		err := m.beginLocked(argv)
		m.mu.Unlock()
		if err == nil {
			return
		}
		m.mu.Lock()
		m.running = false
		m.notifyLocked()
		m.mu.Unlock()
	} else {
		m.running = false
		m.acquired = false
		m.notifyLocked()
		m.mu.Unlock()
	}
	if m.paused != nil {
		m.paused(false)
	}
}

// realLockerSpawn starts the locker with stdout piped for the handshake
// and stderr inherited so "refused" reasons stay visible in the journal.
func realLockerSpawn(argv []string) (io.ReadCloser, <-chan int, error) {
	// Same guard as runArgv (popout_session.go): exec'ing a real locker from a
	// test binary is always a mistake; tests inject Registry.lockerSpawn.
	if testing.Testing() {
		return nil, nil, fmt.Errorf("sysc-shell: refusing to spawn locker %q from a test binary: set Registry.lockerSpawn", argv[0])
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(path, argv[1:]...)
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	codes := make(chan int, 1)
	go func() {
		code := 0
		if err := cmd.Wait(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			} else {
				code = -1
			}
		}
		codes <- code
	}()
	return out, codes, nil
}

// LockTracked spawns the configured locker through the tracked manager.
func (r *Registry) LockTracked() error {
	r.mu.Lock()
	argv := sessionArgv("session-lock", r.cfg.Session.Locker)
	managed := r.managedLock
	native := isManagedLocker(argv) && r.lockerSpawn == nil
	var m *lockerManager
	if !native {
		m = r.lockerLocked()
	}
	r.mu.Unlock()
	if native {
		if managed == nil {
			return errors.New("managed locker unavailable")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err := managed.Lock(ctx)
		return err
	}
	if len(argv) == 0 {
		return errors.New("no locker configured")
	}
	return m.request(argv)
}

// notifyLocked publishes cached state for lock-held readers (panel rebuild).
func (m *lockerManager) notifyLocked() {
	if m.stateCB != nil {
		m.stateCB(m.running, m.acquired)
	}
}

// LockState reports the tracked locker's status (T17/CC status label).
func (r *Registry) LockState() (LockState, bool) {
	r.mu.Lock()
	m := r.locker
	if r.managedLock != nil && isManagedLocker(sessionArgv("session-lock", r.cfg.Session.Locker)) {
		v := r.managedState
		r.mu.Unlock()
		return LockState{Running: v.Phase != "idle" && v.Phase != "unavailable" && v.Phase != "failed-before-acquisition", Acquired: v.Known && v.Phase == "sealed"}, v.Known
	}
	r.mu.Unlock()
	if m == nil {
		return LockState{}, false
	}
	st := m.State()
	st.Acquired = false
	return st, false
}

// lockerLocked returns the manager; caller holds r.mu. The paused callback
// briefly re-takes r.mu (order: locker mu -> registry mu, never the
// reverse), which is safe because LockTracked releases r.mu first.
func (r *Registry) lockerLocked() *lockerManager {
	if r.locker == nil {
		spawn := r.lockerSpawn
		if spawn == nil {
			spawn = realLockerSpawn
		}
		r.locker = &lockerManager{spawn: spawn, sleep: time.Sleep,
			stateCB: func(running, acquired bool) {
				r.mu.Lock()
				r.lockerRunning, r.lockerAcquired = running, false
				if h := r.panelHosts[PanelControlCenter]; h != nil {
					r.rebuildPanel(h)
					r.publishSurface(h.output, panelSurfaceID(h.id))
				}
				if h := r.panelHosts[PanelSettings]; h != nil && h.section == "Screensaver" {
					r.rebuildPanel(h)
					r.publishSurface(h.output, panelSurfaceID(h.id))
				}
				if !running && !acquired {
					// A completed or refused lock leaves the current service state
					// to systemd; refresh the surfaces without starting anything.
					r.refreshWallsLocked()
				}
				r.mu.Unlock()
			},
			paused: func(p bool) {
				r.mu.Lock()
				svc := r.wallpaperSvc
				r.mu.Unlock()
				if svc == nil {
					return
				}
				op := wallpaper.OpResume
				if p {
					op = wallpaper.OpPause
				}
				svc.Enqueue(wallpaper.Command{Op: op, Token: wallpaper.AllOutputs})
			},
		}
	}
	return r.locker
}

// LockStateMap answers session.lock-state for the IPC server.
func (r *Registry) LockStateMap() map[string]any {
	st, ok := r.LockState()
	return map[string]any{
		"running":   st.Running,
		"acquired":  st.Acquired,
		"respawned": st.RespawnedUsed,
		"known":     ok,
		"exit_code": st.ExitCode,
	}
}

// lockActionLabel labels managed compositor state; legacy commands remain unverified.
// lockActionLabel reads the state cache; callers hold r.mu (panel rebuild)
// or lock it (the exported wrapper below).
func (r *Registry) lockActionLabel() string {
	if r.managedLock != nil && isManagedLocker(sessionArgv("session-lock", r.cfg.Session.Locker)) {
		switch r.managedState.Phase {
		case "requesting":
			return "Locking…"
		case "sealed":
			return "Locked"
		case "recovering":
			return "Recovering lock…"
		case "sealed/unknown":
			return "Lock state unknown"
		case "unavailable":
			return "Lock unavailable"
		}
	}
	if r.lockerRunning {
		return "Lock running (unverified)"
	}
	return "Lock"
}

// isManagedLocker reports whether argv is a command whose owner publishes the
// lock session protocol: the bare binary, or the same binary told to publish
// it on the session bus. That is the form a sysc-lock session service runs,
// so a command carrying --session is as managed as the bare name. A different
// binary, or this one running something else, stays legacy.
func isManagedLocker(argv []string) bool {
	if len(argv) == 0 || filepath.Base(argv[0]) != "sysc-lock" {
		return false
	}
	if len(argv) == 1 {
		return true
	}
	return len(argv) == 2 && argv[1] == "--session"
}

// SuspendTracked waits for the managed protocol event before calling logind.
func (r *Registry) SuspendTracked() error {
	r.mu.Lock()
	managed, run := r.managedLock, r.runArgv
	r.mu.Unlock()
	if managed == nil {
		return errors.New("managed sleep protection unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return managed.Suspend(ctx, func() error { return run([]string{"loginctl", "suspend"}) })
}
