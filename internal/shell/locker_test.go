package shell

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

type fakeLock struct {
	mu     sync.Mutex
	codes  []int
	lines  []string
	slept  []time.Duration
	starts int
	exits  []chan int
	outs   []*io.PipeWriter
}

func newFakeLock(codes []int, lines []string) *fakeLock {
	return &fakeLock{codes: codes, lines: lines}
}

func (f *fakeLock) spawn(argv []string) (io.ReadCloser, <-chan int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.starts
	f.starts++
	r, w := io.Pipe()
	ch := make(chan int, 1)
	f.exits = append(f.exits, ch)
	f.outs = append(f.outs, w)
	go func(i int) {
		if i < len(f.lines) {
			for _, l := range strings.Split(f.lines[i], "\n") {
				if l == "" {
					continue
				}
				w.Write([]byte(l + "\n"))
				time.Sleep(time.Millisecond)
			}
		}
		if i < len(f.codes) {
			w.Close()
			ch <- f.codes[i]
		}
	}(i)
	return r, ch, nil
}

func (f *fakeLock) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts
}

func (f *fakeLock) finish(i int, code int) {
	f.mu.Lock()
	w, ch := f.outs[i], f.exits[i]
	f.mu.Unlock()
	w.Close()
	ch <- code
}

func waitWhile(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if !cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(msg)
}

func TestLockerHandshakePauseResume(t *testing.T) {
	f := newFakeLock([]int{0}, []string{"sysc-lock: locked"})
	var pauses []bool
	var pmu sync.Mutex
	locked := make(chan struct{}, 1)
	m := &lockerManager{
		spawn: f.spawn,
		stateCB: func(running, acquired bool) {
			if acquired {
				locked <- struct{}{}
			}
		},
		paused: func(p bool) {
			pmu.Lock()
			pauses = append(pauses, p)
			pmu.Unlock()
		},
		sleep: func(time.Duration) {},
	}
	if err := m.request([]string{"sysc-lock"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-locked:
	case <-time.After(time.Second):
		t.Fatal("handshake not seen")
	}
	waitWhile(t, func() bool { return m.State().Running }, "still running")
	st := m.State()
	if st.Running || st.Acquired || st.ExitCode != 0 {
		t.Fatalf("st=%+v, unlocked exit must clear acquired state", st)
	}
	pmu.Lock()
	defer pmu.Unlock()
	if len(pauses) != 2 || pauses[0] != true || pauses[1] != false {
		t.Fatalf("pauses=%v", pauses)
	}
	if f.count() != 1 {
		t.Fatalf("respawned clean exit: starts=%d", f.count())
	}
}

func TestLockerRefusedNoRespawn(t *testing.T) {
	f := newFakeLock([]int{2}, []string{"sysc-lock: refused (already locked)"})
	m := &lockerManager{spawn: f.spawn, paused: func(bool) {}, sleep: func(time.Duration) {}}
	if err := m.request([]string{"x"}); err != nil {
		t.Fatal(err)
	}
	waitWhile(t, func() bool { return m.State().Running }, "still running")
	if st := m.State(); st.ExitCode != 2 || st.Acquired {
		t.Fatalf("st=%+v", st)
	}
	if f.count() != 1 {
		t.Fatalf("refused must not respawn: %d", f.starts)
	}
}

func TestLockerCrashAfterLockedRespawnsOnce(t *testing.T) {
	f := newFakeLock([]int{1, 1}, []string{"sysc-lock: locked", "sysc-lock: locked"})
	var slept []time.Duration
	m := &lockerManager{
		spawn:  f.spawn,
		paused: func(bool) {},
		sleep:  func(d time.Duration) { slept = append(slept, d) },
	}
	if err := m.request([]string{"x"}); err != nil {
		t.Fatal(err)
	}
	// first crash respawns; second crash exhausts the budget
	waitWhile(t, func() bool { return f.count() < 2 }, "no respawn")
	f.finish(1, 1)
	waitWhile(t, func() bool { return m.State().Running }, "still running")
	if f.count() != 2 {
		t.Fatalf("budget exhausted must stop at 2 starts, got %d", f.starts)
	}
	if len(slept) == 0 || slept[0] != lockerRespawnBackoff {
		t.Fatalf("slept=%v", slept)
	}
	if st := m.State(); !st.RespawnedUsed {
		t.Fatalf("st=%+v", st)
	}
}

func TestLockerSigtermBeforeLockedNoRespawn(t *testing.T) {
	f := newFakeLock([]int{3}, []string{"early exit before locked"})
	m := &lockerManager{spawn: f.spawn, paused: func(bool) {}, sleep: func(time.Duration) {}}
	m.request([]string{"x"})
	waitWhile(t, func() bool { return m.State().Running }, "still running")
	if f.count() != 1 || m.State().Acquired {
		t.Fatalf("starts=%d st=%+v", f.starts, m.State())
	}
}

func TestLockerSingleFlightDuringRespawnBackoff(t *testing.T) {
	f := newFakeLock([]int{1, 0}, []string{"sysc-lock: locked", "sysc-lock: locked"})
	inBackoff := make(chan struct{})
	var once sync.Once
	m := &lockerManager{
		spawn:  f.spawn,
		paused: func(bool) {},
		sleep: func(time.Duration) {
			once.Do(func() { close(inBackoff) })
			time.Sleep(30 * time.Millisecond)
		},
	}
	if err := m.request([]string{"x"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-inBackoff:
	case <-time.After(2 * time.Second):
		t.Fatal("never entered respawn backoff")
	}
	if err := m.request([]string{"x"}); err != errLockerRunning {
		t.Fatalf("request during backoff = %v, want errLockerRunning", err)
	}
	waitWhile(t, func() bool { return m.State().Running }, "still running")
}

func TestLockerSingleFlight(t *testing.T) {
	f := newFakeLock(nil, []string{"sysc-lock: locked"})
	f.codes = nil // keep it running
	m := &lockerManager{spawn: f.spawn, paused: func(bool) {}, sleep: func(time.Duration) {}}
	if err := m.request([]string{"x"}); err != nil {
		t.Fatal(err)
	}
	if err := m.request([]string{"x"}); err != errLockerRunning {
		t.Fatalf("second request=%v", err)
	}
	f.finish(0, 0)
	waitWhile(t, func() bool { return m.State().Running }, "still running")
}

func TestLockerUserRequestResetsBudget(t *testing.T) {
	f := newFakeLock([]int{1, 1, 0}, []string{"sysc-lock: locked", "sysc-lock: locked", "sysc-lock: locked"})
	m := &lockerManager{spawn: f.spawn, paused: func(bool) {}, sleep: func(time.Duration) {}}
	m.request([]string{"x"})
	waitWhile(t, func() bool { return f.count() < 2 }, "no respawn")
	f.finish(1, 1)
	waitWhile(t, func() bool { return m.State().Running }, "still running")
	if f.count() != 2 {
		t.Fatalf("want respawn then stop, starts=%d", f.count())
	}
	// fresh user request re-arms
	if err := m.request([]string{"x"}); err != nil {
		t.Fatal(err)
	}
	waitWhile(t, func() bool { return f.count() < 3 }, "third run")
	waitWhile(t, func() bool { return m.State().Running }, "still running")
	if f.count() != 3 {
		t.Fatalf("starts=%d", f.count())
	}
}

func TestLockActionLabel(t *testing.T) {
	t.Parallel()
	reg, _ := newSessionHost(t, "swaylock")
	pr, pw := io.Pipe()
	exits := make(chan int, 1)
	reg.lockerSpawn = func([]string) (io.ReadCloser, <-chan int, error) {
		go func() {
			pw.Write([]byte(lockHandshakeLine + "\n"))
		}()
		return pr, exits, nil
	}
	if got := lockedLabel(reg); got != "Lock" {
		t.Fatalf("before spawn: %q", got)
	}
	if err := reg.LockTracked(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for lockedLabel(reg) != "Locked" {
		if time.Now().After(deadline) {
			t.Fatalf("never reached Locked, got %q", lockedLabel(reg))
		}
		time.Sleep(5 * time.Millisecond)
	}
	pw.Close()
	exits <- 0
	for lockedLabel(reg) != "Lock" {
		if time.Now().After(deadline) {
			t.Fatalf("never returned to Lock, got %q", lockedLabel(reg))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestControlCentreLockRowFollowsHandshake(t *testing.T) {
	cfg := config.Default()
	cfg.Session.Locker = "sysc-lock"
	cfg.Accessibility.ReducedMotion = true
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	withTestBar(t, reg, 1, cfg)
	if err := reg.OpenPanelByName("control-center"); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelControlCenter]
	if h == nil {
		reg.mu.Unlock()
		t.Fatal("no control centre")
	}
	h.selectControlCentreSection(reg, "power")
	reg.mu.Unlock()

	pr, pw := io.Pipe()
	exits := make(chan int, 1)
	reg.lockerSpawn = func([]string) (io.ReadCloser, <-chan int, error) {
		return pr, exits, nil
	}
	if err := reg.LockTracked(); err != nil {
		t.Fatal(err)
	}
	if _, err := pw.Write([]byte(lockHandshakeLine + "\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		reg.mu.Lock()
		h = reg.panelHosts[PanelControlCenter]
		name := ""
		if h != nil {
			if n := findNode(h.root, func(n *ui.Node) bool { return n.Action == "session-lock" }); n != nil {
				name = n.Name
			}
		}
		reg.mu.Unlock()
		if name == "Locked" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("CC session-lock row = %q, want Locked after handshake", name)
		}
		time.Sleep(5 * time.Millisecond)
	}
	pw.Close()
	exits <- 0
}

// lockedLabel mirrors the panel-rebuild call path: readers hold r.mu.
func lockedLabel(reg *Registry) string {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return reg.lockActionLabel()
}
