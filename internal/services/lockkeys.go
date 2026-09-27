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
type LockKeys struct {
	root     string
	interval time.Duration
	changes  chan LockState

	mu      sync.Mutex
	ok      bool
	base    LockState
	last    LockState
	started bool
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

// Baseline is the state read by Start, before any change was published.
func (l *LockKeys) Baseline() LockState {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.base
}

// Start reads the baseline synchronously, then polls. A machine with no
// lock-key LEDs never starts a polling goroutine.
func (l *LockKeys) Start() {
	l.mu.Lock()
	if l.started {
		l.mu.Unlock()
		return
	}
	l.started = true
	l.mu.Unlock()

	st, ok := readLockState(l.root)
	l.mu.Lock()
	l.ok = ok
	if !ok {
		l.started = false
		l.mu.Unlock()
		return
	}
	l.base, l.last = st, st
	l.stop, l.done = make(chan struct{}), make(chan struct{})
	stop, done := l.stop, l.done
	l.mu.Unlock()
	go l.run(stop, done)
}

func (l *LockKeys) Close() {
	l.mu.Lock()
	stop, done := l.stop, l.done
	l.stop, l.done = nil, nil
	l.started = false
	l.mu.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
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
