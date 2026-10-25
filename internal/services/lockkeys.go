package services

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const lockKeysPoll = 250 * time.Millisecond

// LockState is the session's Caps Lock and Num Lock state. Any keyboard with
// its LED lit counts: the kernel mirrors the lock state to every keyboard.
type LockState struct{ Caps, Num bool }

// LockKeys polls the keyboard LED class for lock-key changes. The shell
// cannot learn lock state from wl_keyboard because it only receives keyboard
// events while it has focus.
//
// The poll loop runs while a lease is held. A consumer that nobody asked for
// must not wake the process four times a second to read a sysfs file.
type LockKeys struct {
	root     string
	interval time.Duration
	changes  chan LockState

	mu      sync.Mutex
	ok      bool
	base    LockState
	last    LockState
	leases  int
	held    bool
	release func()
	stop    chan struct{}
	done    chan struct{}
}

func NewLockKeys(root string, interval time.Duration) *LockKeys {
	if root == "" {
		root = "/sys/class/leds"
	}
	if interval <= 0 {
		interval = lockKeysPoll
	}
	return &LockKeys{root: root, interval: interval, changes: make(chan LockState, 1)}
}

func (l *LockKeys) Available() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ok
}

func (l *LockKeys) Changes() <-chan LockState { return l.changes }

// Baseline is the state read when the poller started, before any change was
// published.
func (l *LockKeys) Baseline() LockState {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.base
}

// Polling reports whether the poll loop is running. It is the seam a test
// uses to show that a service nobody leased stays quiet.
func (l *LockKeys) Polling() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stop != nil
}

// Acquire takes a lease on the poller and returns the release. The poll loop
// starts with the first lease and stops with the last, so a consumer that
// goes away takes its wakeups with it. A lease holder sees every change
// within one poll interval.
func (l *LockKeys) Acquire() func() {
	l.mu.Lock()
	l.leases++
	if l.stop == nil {
		l.startPollerLocked()
	}
	l.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(l.dropLease)
	}
}

// dropLease gives one lease back. The poll loop stops with the last lease.
//
// The lease count and the loop move under one lock, so a release that races
// an acquire cannot leave the loop running without a lease, and the count
// never falls below zero.
func (l *LockKeys) dropLease() {
	l.mu.Lock()
	if l.leases > 0 {
		l.leases--
	}
	done := l.takePollerLocked()
	l.mu.Unlock()
	if done != nil {
		<-done
	}
}

// Start takes a lease that is held until Close.
func (l *LockKeys) Start() {
	l.mu.Lock()
	if l.held {
		l.mu.Unlock()
		return
	}
	l.held = true
	l.mu.Unlock()
	release := l.Acquire()
	l.mu.Lock()
	l.release = release
	l.mu.Unlock()
}

// Close stops the poller. It is the shutdown path, so it stops the loop even
// when a lease is still outstanding. The outstanding lease keeps its count,
// so the release that comes after it cannot push the count below zero.
func (l *LockKeys) Close() {
	l.mu.Lock()
	l.held = false
	release := l.release
	l.release = nil
	l.mu.Unlock()
	if release != nil {
		release()
	}
	l.mu.Lock()
	done := l.takePollerLocked()
	l.mu.Unlock()
	if done != nil {
		<-done
	}
}

// startPollerLocked reads the baseline and starts the poll loop. A machine
// with no lock-key LEDs never starts a polling goroutine.
//
// The caller holds the lock, so the baseline and the loop start together: a
// consumer that reads the baseline right after its lease sees the state the
// loop starts from.
func (l *LockKeys) startPollerLocked() {
	if l.stop != nil {
		return
	}
	st, ok := readLockState(l.root)
	if !ok {
		l.ok = false
		return
	}
	l.ok = true
	l.base, l.last = st, st
	l.stop, l.done = make(chan struct{}), make(chan struct{})
	stop, done := l.stop, l.done
	go l.run(stop, done)
}

// takePollerLocked ends the poll loop and returns the channel that closes
// when the loop is done.
//
// The caller waits for that channel outside the lock, because the loop takes
// the same lock when it polls.
func (l *LockKeys) takePollerLocked() chan struct{} {
	if l.stop == nil {
		return nil
	}
	close(l.stop)
	done := l.done
	l.stop, l.done = nil, nil
	l.ok = false
	return done
}

func (l *LockKeys) run(stop, done chan struct{}) {
	defer close(done)
	tick := time.NewTicker(l.interval)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			l.poll()
		}
	}
}

func (l *LockKeys) poll() {
	st, ok := readLockState(l.root)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ok = ok
	if !ok || st == l.last {
		return
	}
	l.last = st
	select {
	case l.changes <- st:
	default:
		select {
		case <-l.changes:
		default:
		}
		l.changes <- st
	}
}

func readLockState(root string) (LockState, bool) {
	caps, capsOK := anyLit(filepath.Join(root, "*::capslock", "brightness"))
	num, numOK := anyLit(filepath.Join(root, "*::numlock", "brightness"))
	return LockState{Caps: caps, Num: num}, capsOK || numOK
}

func anyLit(pattern string) (lit, found bool) {
	paths, _ := filepath.Glob(pattern)
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		found = true
		if value := strings.TrimSpace(string(contents)); value != "" && value != "0" {
			lit = true
		}
	}
	return lit, found
}
