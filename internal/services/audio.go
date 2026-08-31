package services

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultPoll = 500 * time.Millisecond
const execTimeout = 2 * time.Second

type AudioState struct {
	Level int
	Muted bool
}

type Audio struct {
	mu       sync.Mutex
	leases   leaseSet
	interval time.Duration
	bin      string
	ok       bool
	last     AudioState
	hasLast  bool
	stop     chan struct{}
	done     chan struct{}
	changes  chan AudioState
}

func NewAudio(interval time.Duration, path string) *Audio {
	if interval <= 0 {
		interval = defaultPoll
	}
	bin, err := resolveBin(path, "wpctl")
	a := &Audio{
		interval: interval,
		bin:      bin,
		ok:       err == nil,
		changes:  make(chan AudioState, 1),
	}
	return a
}

func (a *Audio) Changes() <-chan AudioState { return a.changes }

func (a *Audio) State() AudioState {
	st, err := a.read()
	if err != nil {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.last
	}
	return st
}

func (a *Audio) Available() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bin != "" && a.ok
}

func (a *Audio) Acquire() (*Lease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	lease := &Lease{audio: a, boundary: a.interval}
	a.leases.add(lease)
	if a.stop == nil {
		a.startLocked()
	}
	return lease, nil
}

func (a *Audio) release(l *Lease) {
	a.mu.Lock()
	if !a.leases.remove(l) {
		a.mu.Unlock()
		return
	}
	done := a.stopIfUnusedLocked()
	a.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (a *Audio) Close() {
	a.mu.Lock()
	for _, l := range a.leases.clear() {
		l.audio = nil
	}
	done := a.stopIfUnusedLocked()
	a.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (a *Audio) startLocked() {
	a.stop, a.done = make(chan struct{}), make(chan struct{})
	go a.run(a.stop, a.done)
}

func (a *Audio) stopIfUnusedLocked() chan struct{} {
	if a.leases.len() > 0 || a.stop == nil {
		return nil
	}
	close(a.stop)
	done := a.done
	a.stop, a.done = nil, nil
	return done
}

func (a *Audio) run(stop, done chan struct{}) {
	defer close(done)
	a.poll(true)
	tick := time.NewTicker(a.interval)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			a.poll(false)
		}
	}
}

func (a *Audio) poll(baseline bool) {
	st, err := a.read()
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.ok = false
		return
	}
	a.ok = true
	if baseline || !a.hasLast {
		a.last, a.hasLast = st, true
		return
	}
	if a.last == st {
		return
	}
	a.last = st
	select {
	case a.changes <- st:
	default:
		select {
		case <-a.changes:
		default:
		}
		a.changes <- st
	}
}

func (a *Audio) read() (AudioState, error) {
	a.mu.Lock()
	bin := a.bin
	a.mu.Unlock()
	if bin == "" {
		return AudioState{}, fmt.Errorf("services: wpctl unavailable")
	}
	out, err := runCmd(bin, "get-volume", "@DEFAULT_AUDIO_SINK@")
	if err != nil {
		return AudioState{}, err
	}
	level, muted, err := parseWpctlVolume(out)
	if err != nil {
		return AudioState{}, err
	}
	return AudioState{Level: level, Muted: muted}, nil
}

func (a *Audio) Set(level int) error {
	if level < 0 {
		level = 0
	}
	if level > 100 {
		level = 100
	}
	return a.wpctl("set-volume", "@DEFAULT_AUDIO_SINK@", strconv.Itoa(level)+"%")
}

func (a *Audio) SetMute(on bool) error {
	arg := "0"
	if on {
		arg = "1"
	}
	return a.wpctl("set-mute", "@DEFAULT_AUDIO_SINK@", arg)
}

func (a *Audio) Step(delta int) error {
	if delta == 0 {
		return nil
	}
	arg := fmt.Sprintf("%+d%%", delta)
	return a.wpctl("set-volume", "@DEFAULT_AUDIO_SINK@", arg)
}

func (a *Audio) wpctl(args ...string) error {
	a.mu.Lock()
	bin := a.bin
	a.mu.Unlock()
	if bin == "" {
		return fmt.Errorf("services: wpctl unavailable")
	}
	_, err := runCmd(bin, args...)
	a.mu.Lock()
	a.ok = err == nil
	a.mu.Unlock()
	return err
}

func parseWpctlVolume(out string) (int, bool, error) {
	muted := strings.Contains(out, "[MUTED]")
	const prefix = "Volume:"
	i := strings.Index(out, prefix)
	if i < 0 {
		return 0, false, fmt.Errorf("services: wpctl: no Volume line")
	}
	rest := strings.TrimSpace(out[i+len(prefix):])
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return 0, false, fmt.Errorf("services: wpctl: empty volume")
	}
	f, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false, fmt.Errorf("services: wpctl: %w", err)
	}
	level := int(math.Round(f * 100))
	if level < 0 {
		level = 0
	}
	if level > 100 {
		level = 100
	}
	return level, muted, nil
}

func resolveBin(path, name string) (string, error) {
	if path == "" {
		return exec.LookPath(name)
	}
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}

func runCmd(bin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("services: %s: %w", bin, err)
	}
	return stdout.String(), nil
}
