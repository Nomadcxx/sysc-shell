package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type BrightnessState struct {
	Level int
}

type Brightness struct {
	mu       sync.Mutex
	leases   leaseSet
	interval time.Duration
	root     string
	bin      string
	ok       bool
	last     BrightnessState
	hasLast  bool
	stop     chan struct{}
	done     chan struct{}
	changes  chan BrightnessState
}

func NewBrightness(root, ctlPath string, interval time.Duration) *Brightness {
	if interval <= 0 {
		interval = defaultPoll
	}
	if root == "" {
		root = "/sys/class/backlight"
	}
	bin, _ := resolveBin(ctlPath, "brightnessctl")
	b := &Brightness{
		interval: interval,
		root:     root,
		bin:      bin,
		ok:       hasBacklight(root),
		changes:  make(chan BrightnessState, 1),
	}
	return b
}

func (b *Brightness) Changes() <-chan BrightnessState { return b.changes }

func (b *Brightness) Available() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ok = hasBacklight(b.root)
	return b.ok
}

func (b *Brightness) Level() int {
	st, err := readBrightness(b.root)
	if err != nil {
		return 0
	}
	return st.Level
}

func (b *Brightness) Acquire() (*Lease, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	lease := &Lease{brightness: b, boundary: b.interval}
	b.leases.add(lease)
	if b.stop == nil {
		b.startLocked()
	}
	return lease, nil
}

func (b *Brightness) release(l *Lease) {
	b.mu.Lock()
	if !b.leases.remove(l) {
		b.mu.Unlock()
		return
	}
	done := b.stopIfUnusedLocked()
	b.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (b *Brightness) Close() {
	b.mu.Lock()
	for _, l := range b.leases.clear() {
		l.brightness = nil
	}
	done := b.stopIfUnusedLocked()
	b.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (b *Brightness) startLocked() {
	b.stop, b.done = make(chan struct{}), make(chan struct{})
	go b.run(b.stop, b.done)
}

func (b *Brightness) stopIfUnusedLocked() chan struct{} {
	if b.leases.len() > 0 || b.stop == nil {
		return nil
	}
	close(b.stop)
	done := b.done
	b.stop, b.done = nil, nil
	return done
}

func (b *Brightness) run(stop, done chan struct{}) {
	defer close(done)
	b.poll(true)
	tick := time.NewTicker(b.interval)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			b.poll(false)
		}
	}
}

func (b *Brightness) poll(baseline bool) {
	st, err := readBrightness(b.root)
	b.mu.Lock()
	defer b.mu.Unlock()
	if err != nil {
		b.ok = false
		return
	}
	b.ok = true
	if baseline || !b.hasLast {
		b.last, b.hasLast = st, true
		return
	}
	if b.last == st {
		return
	}
	b.last = st
	select {
	case b.changes <- st:
	default:
		select {
		case <-b.changes:
		default:
		}
		b.changes <- st
	}
}

func (b *Brightness) Step(delta int) error {
	b.mu.Lock()
	bin := b.bin
	b.mu.Unlock()
	if bin == "" {
		return fmt.Errorf("services: brightnessctl unavailable")
	}
	if delta == 0 {
		return nil
	}
	arg := fmt.Sprintf("%+d%%", delta)
	_, err := runCmd(bin, "set", arg)
	return err
}

func hasBacklight(root string) bool {
	_, err := readBrightness(root)
	return err == nil
}

func readBrightness(root string) (BrightnessState, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return BrightnessState{}, err
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		cur, err := readIntFile(filepath.Join(dir, "brightness"))
		if err != nil {
			continue
		}
		max, err := readIntFile(filepath.Join(dir, "max_brightness"))
		if err != nil || max <= 0 {
			continue
		}
		level := int(float64(cur)/float64(max)*100 + 0.5)
		if level < 0 {
			level = 0
		}
		if level > 100 {
			level = 100
		}
		return BrightnessState{Level: level}, nil
	}
	return BrightnessState{}, fmt.Errorf("services: no backlight device")
}

func readIntFile(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}
