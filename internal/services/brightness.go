package services

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type BrightnessState struct {
	Level int
}

// DisplayInfo describes one controllable display for the UI surfaces.
type DisplayInfo struct {
	ID    string
	Label string
	Kind  string // "sysfs" or "ddc"
	Level int
	OK    bool
}

// brightnessDevice is a single controllable display path. Two
// implementations exist because the kernel exposes exactly two: a backlight
// class device, and a DDC/CI-capable I2C bus matched to a connector by EDID.
type brightnessDevice interface {
	ID() string
	Label() string
	Kind() string
	Read() (int, error)
	Set(percent int) error
	Step(delta int) error
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

	devRoot      string
	drmRoot      string
	i2cSysfsRoot string
	probe        func(bus int) (connector string, current, max int, err error)

	devices      []brightnessDevice
	lastDisplays []DisplayInfo
	knownDDC     map[int]ddcIdent
	cooldown     map[int]time.Time
	lastScan     time.Time
	failures     map[string]int
	firstScan    bool
	poke         chan struct{}
}

type ddcIdent struct {
	connector string
	current   int
	max       int
}

const (
	ddcRescanGap       = 30 * time.Second
	ddcCooldown        = 5 * time.Minute
	ddcFailLimit       = 3
	ddcReadCacheWindow = 2 * time.Second
	ddcWriteDebounce   = 200 * time.Millisecond
)

// NewBrightness builds a sysfs-only controller. DDC/CI probing opens real
// i2c buses, so it is opt-in via NewBrightnessDDC from the process
// composition root; unit and shell tests must not put live traffic on a
// monitor.
func NewBrightness(root, ctlPath string, interval time.Duration) *Brightness {
	if interval <= 0 {
		interval = defaultPoll
	}
	if root == "" {
		root = "/sys/class/backlight"
	}
	bin, _ := resolveBin(ctlPath, "brightnessctl")
	return newBrightness(root, bin, interval, "/dev", "/sys/class/drm", "/sys/bus/i2c/devices")
}

// NewBrightnessDDC additionally probes /dev/i2c-* for DDC/CI monitors,
// joining each bus to a DRM connector by EDID.
func NewBrightnessDDC(root, ctlPath string, interval time.Duration) *Brightness {
	b := NewBrightness(root, ctlPath, interval)
	b.probe = func(bus int) (string, int, int, error) {
		return probeDDCBus(bus, b.devRoot, b.drmRoot, b.i2cSysfsRoot)
	}
	return b
}

func newBrightness(root, bin string, interval time.Duration, devRoot, drmRoot, i2cSysfsRoot string) *Brightness {
	return &Brightness{
		interval:     interval,
		root:         root,
		bin:          bin,
		changes:      make(chan BrightnessState, 1),
		devRoot:      devRoot,
		drmRoot:      drmRoot,
		i2cSysfsRoot: i2cSysfsRoot,
		knownDDC:     map[int]ddcIdent{},
		cooldown:     map[int]time.Time{},
		failures:     map[string]int{},
		poke:         make(chan struct{}, 1),
	}
}

func (b *Brightness) Changes() <-chan BrightnessState { return b.changes }

// Available reports any controllable display, sysfs or DDC. The first call
// performs a synchronous discovery so capability flags are honest before any
// lease starts the poll loop; the result is then cached.
func (b *Brightness) Available() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.firstScan {
		b.firstScan = true
		b.mu.Unlock()
		b.refresh()
		b.mu.Lock()
	}
	b.ok = len(b.devices) > 0
	return b.ok
}

func (b *Brightness) Level() int {
	b.mu.Lock()
	def := b.defaultDeviceLocked()
	b.mu.Unlock()
	if def == nil {
		return 0
	}
	level, err := def.Read()
	if err != nil {
		return 0
	}
	return level
}

// Displays snapshots every controllable display for the control centre.
func (b *Brightness) Displays() []DisplayInfo {
	b.mu.Lock()
	devices := append([]brightnessDevice(nil), b.devices...)
	b.mu.Unlock()
	out := make([]DisplayInfo, 0, len(devices))
	for _, d := range devices {
		level, err := d.Read()
		out = append(out, DisplayInfo{ID: d.ID(), Label: d.Label(), Kind: d.Kind(), Level: level, OK: err == nil})
	}
	return out
}

// CachedDisplays returns the per-display snapshot refreshed by the poll
// loop. Render paths must use this instead of Displays: it opens no buses.
func (b *Brightness) CachedDisplays() []DisplayInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]DisplayInfo(nil), b.lastDisplays...)
}

// seedDisplaysLocked fills the render cache from detection so the control
// centre shows per-display sliders before the first lease-triggered poll.
// The poll overwrites this with live reads; seeding never opens a bus.
func (b *Brightness) seedDisplaysLocked() {
	if b.lastDisplays != nil || len(b.devices) == 0 {
		return
	}
	infos := make([]DisplayInfo, 0, len(b.devices))
	for _, d := range b.devices {
		level, ok := seedOf(d)
		infos = append(infos, DisplayInfo{ID: d.ID(), Label: d.Label(), Kind: d.Kind(), Level: level, OK: ok})
	}
	b.lastDisplays = infos
}

func seedOf(d brightnessDevice) (int, bool) {
	if dd, ok := d.(*ddcDev); ok {
		dd.mu.Lock()
		defer dd.mu.Unlock()
		return dd.cache, !dd.cacheAt.IsZero()
	}
	// sysfs: two small file reads, no ioctl, safe under the lock.
	level, err := d.Read()
	return level, err == nil
}

// SetDisplay and StepDisplay address one display by its ID.
func (b *Brightness) SetDisplay(id string, level int) error {
	d := b.deviceByID(id)
	if d == nil {
		return fmt.Errorf("services: no display %q", id)
	}
	return d.Set(level)
}

func (b *Brightness) StepDisplay(id string, delta int) error {
	d := b.deviceByID(id)
	if d == nil {
		return fmt.Errorf("services: no display %q", id)
	}
	return d.Step(delta)
}

func (b *Brightness) deviceByID(id string) brightnessDevice {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, d := range b.devices {
		if d.ID() == id {
			return d
		}
	}
	return nil
}

// CachedState returns the last completed poll without touching sysfs. The
// boolean distinguishes a real zero level from an unsampled service.
func (b *Brightness) CachedState() (BrightnessState, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.last, b.hasLast
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
	// External backlight edits must reach the shell without waiting a full
	// poll: watch the sysfs tree and wake early on any change. DDC displays
	// have no inotify surface; they ride the poll interval. Arm the watcher
	// before the initial poll so startup changes are queued for another poll.
	watcher := newBacklightWatcher(b.root, b.poke)
	defer watcher.close()
	b.poll(true)
	tick := time.NewTicker(b.interval)
	defer tick.Stop()
	watcher.sync(b.sysfsNames())
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			b.poll(false)
		case <-b.poke:
			b.poll(false)
		}
		watcher.sync(b.sysfsNames())
	}
}

func (b *Brightness) poll(baseline bool) {
	b.refresh()
	b.mu.Lock()
	devices := append([]brightnessDevice(nil), b.devices...)
	def := b.defaultDeviceLocked()
	var st BrightnessState
	err := error(errNoDisplay)
	infos := make([]DisplayInfo, 0, len(devices))
	for _, d := range devices {
		level, dErr := d.Read()
		infos = append(infos, DisplayInfo{ID: d.ID(), Label: d.Label(), Kind: d.Kind(), Level: level, OK: dErr == nil})
		b.trackFailureLocked(d, dErr)
		if d == def {
			st = BrightnessState{Level: level}
			err = dErr
		}
	}
	b.lastDisplays = infos
	b.ok = len(b.devices) > 0
	if err != nil {
		b.mu.Unlock()
		return
	}
	defer b.mu.Unlock()
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

var errNoDisplay = fmt.Errorf("services: no controllable display")

// trackFailureLocked cools down a display whose reads keep failing. The
// commission's hard rule: a monitor with repeated DDC failures is treated as
// absent for a while, never as an error loop.
func (b *Brightness) trackFailureLocked(d brightnessDevice, err error) {
	if err == nil {
		delete(b.failures, d.ID())
		return
	}
	b.failures[d.ID()]++
	if b.failures[d.ID()] < ddcFailLimit {
		return
	}
	delete(b.failures, d.ID())
	if dd, ok := d.(*ddcDev); ok {
		b.cooldown[dd.bus] = time.Now().Add(ddcCooldown)
		delete(b.knownDDC, dd.bus)
		slog.Warn("brightness: cooling down failed DDC display", "connector", dd.connector, "bus", dd.bus)
	}
}

// refresh rebuilds the device list. Sysfs enumeration is cheap and runs every
// poll. DDC identification is memoised: an already-known connector is never
// re-probed, because DDC traffic during a wake sequence can disturb some
// monitors' own brightness handling. Unknown buses are scanned at most every
// ddcRescanGap, which is also how a second hotplugged monitor appears.
func (b *Brightness) refresh() {
	b.mu.Lock()
	sysfs := enumerateBacklights(b.root, b.bin)
	known, cooldown := b.knownDDC, b.cooldown
	needScan := b.probe != nil && (!b.firstScan ||
		time.Since(b.lastScan) >= ddcRescanGap ||
		hasExpiredCooldown(cooldown, time.Now()))
	if needScan {
		b.firstScan = true
		b.lastScan = time.Now()
	}
	b.mu.Unlock()
	if !needScan {
		b.mu.Lock()
		b.devices = assemble(sysfs, known, b.devRoot, b.devices)
		b.seedDisplaysLocked()
		b.mu.Unlock()
		return
	}
	found := map[int]ddcIdent{}
	entries, err := os.ReadDir(b.devRoot)
	if err == nil {
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), "i2c-") {
				continue
			}
			bus, err := strconv.Atoi(strings.TrimPrefix(e.Name(), "i2c-"))
			if err != nil || bus < 0 || bus > 63 {
				continue
			}
			b.mu.Lock()
			cool := time.Now().Before(cooldown[bus])
			ident, wasKnown := known[bus]
			b.mu.Unlock()
			if cool {
				if wasKnown {
					found[bus] = ident
				}
				continue
			}
			if wasKnown { // never re-probe an identified monitor
				found[bus] = ident
				continue
			}
			connector, current, max, err := b.probe(bus)
			if err != nil || connector == "" {
				continue // absent, not an error
			}
			found[bus] = ddcIdent{connector: connector, current: current, max: max}
		}
	}
	b.mu.Lock()
	b.knownDDC = found
	b.devices = assemble(sysfs, found, b.devRoot, b.devices)
	b.seedDisplaysLocked()
	for bus, until := range cooldown {
		if time.Now().After(until) {
			delete(b.cooldown, bus)
		}
	}
	b.mu.Unlock()
}

func hasExpiredCooldown(cooldown map[int]time.Time, now time.Time) bool {
	for _, until := range cooldown {
		if now.After(until) {
			return true
		}
	}
	return false
}

// sysfsNames feeds the inotify watch set.
func (b *Brightness) sysfsNames() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var names []string
	for _, d := range b.devices {
		if sd, ok := d.(*sysfsDev); ok {
			names = append(names, sd.name)
		}
	}
	return names
}

// assemble rebuilds the device list, reusing existing DDC instances by
// connector so a poll never drops an in-flight debounce timer or a fresh read
// cache.
func assemble(sysfs []brightnessDevice, known map[int]ddcIdent, devRoot string, prev []brightnessDevice) []brightnessDevice {
	reuse := map[string]*ddcDev{}
	for _, d := range prev {
		if dd, ok := d.(*ddcDev); ok {
			reuse[dd.ID()] = dd
		}
	}
	out := append([]brightnessDevice{}, sysfs...)
	for bus, ident := range known {
		id := "ddc:" + ident.connector
		if dd, ok := reuse[id]; ok && dd.max == ident.max {
			out = append(out, dd)
			continue
		}
		dd := &ddcDev{bus: bus, connector: ident.connector, max: ident.max, devRoot: devRoot}
		if ident.current > 0 {
			// Seed from the value the probe already read: detection costs no
			// second bus round-trip and the first poll can serve the cache.
			dd.cache, dd.cacheAt = ddcValueToPercent(ident.current, ident.max), time.Now()
		}
		out = append(out, dd)
	}
	return out
}

// defaultDeviceLocked picks the machine's primary panel: the kernel's own
// backlight first (it is the one with a hardware switch), else the first DDC
// display sorted by connector so the choice is stable.
func (b *Brightness) defaultDeviceLocked() brightnessDevice {
	var fallback brightnessDevice
	for _, d := range b.devices {
		if d.Kind() == "sysfs" {
			if d.ID() < nameOf(fallback) {
				fallback = d
			}
		}
	}
	if fallback != nil {
		return fallback
	}
	var best brightnessDevice
	for _, d := range b.devices {
		if d.Kind() == "ddc" && (best == nil || d.ID() < best.ID()) {
			best = d
		}
	}
	return best
}

func nameOf(d brightnessDevice) string {
	if d == nil {
		return "\xff"
	}
	return d.ID()
}

func (b *Brightness) Step(delta int) error {
	b.mu.Lock()
	def := b.defaultDeviceLocked()
	b.mu.Unlock()
	if def == nil {
		return errNoDisplay
	}
	return def.Step(delta)
}

func (b *Brightness) Set(level int) error {
	b.mu.Lock()
	def := b.defaultDeviceLocked()
	b.mu.Unlock()
	if def == nil {
		return errNoDisplay
	}
	return def.Set(level)
}

// --- sysfs backlight device ---

type sysfsDev struct {
	root string
	name string
	bin  string
}

func enumerateBacklights(root, bin string) []brightnessDevice {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []brightnessDevice
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if _, err := readIntFile(filepath.Join(dir, "brightness")); err != nil {
			continue
		}
		if _, err := readIntFile(filepath.Join(dir, "max_brightness")); err != nil {
			continue
		}
		out = append(out, &sysfsDev{root: root, name: e.Name(), bin: bin})
	}
	return out
}

func (d *sysfsDev) ID() string    { return "sysfs:" + d.name }
func (d *sysfsDev) Label() string { return d.name }
func (d *sysfsDev) Kind() string  { return "sysfs" }

func (d *sysfsDev) Read() (int, error) {
	dir := filepath.Join(d.root, d.name)
	cur, err := readIntFile(filepath.Join(dir, "brightness"))
	if err != nil {
		return 0, err
	}
	maximum, err := readIntFile(filepath.Join(dir, "max_brightness"))
	if err != nil || maximum <= 0 {
		return 0, fmt.Errorf("services: backlight %s has no usable max_brightness", d.name)
	}
	level := int(float64(cur)/float64(maximum)*100 + 0.5)
	return min(max(level, 0), 100), nil
}

func (d *sysfsDev) Set(percent int) error {
	if d.bin == "" {
		return fmt.Errorf("services: brightnessctl unavailable")
	}
	percent = min(max(percent, 0), 100)
	_, err := runCmd(d.bin, "-d", d.name, "set", strconv.Itoa(percent)+"%")
	return err
}

func (d *sysfsDev) Step(delta int) error {
	if d.bin == "" {
		return fmt.Errorf("services: brightnessctl unavailable")
	}
	if delta == 0 {
		return nil
	}
	_, err := runCmd(d.bin, "-d", d.name, "set", fmt.Sprintf("%+d%%", delta))
	return err
}

// --- DDC/CI device ---

type ddcDev struct {
	bus       int
	connector string
	max       int
	devRoot   string

	mu      sync.Mutex
	cache   int
	cacheAt time.Time
	pending int
	hasPend bool
	timer   *time.Timer
}

func (d *ddcDev) ID() string    { return "ddc:" + d.connector }
func (d *ddcDev) Label() string { return d.connector }
func (d *ddcDev) Kind() string  { return "ddc" }

func (d *ddcDev) path() string { return filepath.Join(d.devRoot, fmt.Sprintf("i2c-%d", d.bus)) }

func (d *ddcDev) Read() (int, error) {
	d.mu.Lock()
	fresh := time.Since(d.cacheAt) < ddcReadCacheWindow
	cache := d.cache
	d.mu.Unlock()
	if fresh {
		return cache, nil
	}
	fd, err := openI2C(d.path())
	if err != nil {
		return 0, err
	}
	defer closeI2C(fd)
	current, _, err := ddcGetVCP(fd, ddcVCPBright)
	if err != nil {
		return 0, err
	}
	percent := ddcValueToPercent(current, d.max)
	d.mu.Lock()
	d.cache, d.cacheAt = percent, time.Now()
	d.mu.Unlock()
	return percent, nil
}

// Set stores the newest request and writes it out after the debounce window,
// the same trailing-edge pattern DMS uses: a dragged slider produces one bus
// transaction, not one per pixel.
func (d *ddcDev) Set(percent int) error {
	percent = min(max(percent, 0), 100)
	d.mu.Lock()
	d.pending, d.hasPend = percent, true
	if d.timer == nil {
		d.timer = time.AfterFunc(ddcWriteDebounce, d.writePending)
	} else {
		d.timer.Reset(ddcWriteDebounce)
	}
	d.mu.Unlock()
	return nil
}

func (d *ddcDev) writePending() {
	d.mu.Lock()
	percent, ok := d.pending, d.hasPend
	d.hasPend, d.timer = false, nil
	d.mu.Unlock()
	if !ok {
		return
	}
	fd, err := openI2C(d.path())
	if err != nil {
		slog.Warn("brightness: ddc write open failed", "connector", d.connector, "err", err)
		return
	}
	defer closeI2C(fd)
	if err := ddcSetVCP(fd, ddcVCPBright, ddcPercentToValue(percent, d.max)); err != nil {
		slog.Warn("brightness: ddc write failed", "connector", d.connector, "err", err)
		return
	}
	d.mu.Lock()
	d.cache, d.cacheAt = percent, time.Now()
	d.mu.Unlock()
}

func (d *ddcDev) Step(delta int) error {
	if delta == 0 {
		return nil
	}
	level, err := d.Read()
	if err != nil {
		return err
	}
	return d.Set(level + delta)
}

func openI2C(path string) (int, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	if err := ddcSetSlave(fd); err != nil {
		syscall.Close(fd)
		return 0, err
	}
	return fd, nil
}

func closeI2C(fd int) { syscall.Close(fd) }

// backlightWatcher turns sysfs edits into early polls. A write to a
// brightness file, or hotplug of a whole device, wakes the poll loop instead
// of waiting out the interval. If the watch setup fails the service still
// rides its timer: inotify is a latency optimisation here, not the source of
// truth.
type backlightWatcher struct {
	fd    int
	root  string
	files map[string]int32
	poke  chan struct{}
	stop  chan struct{}
	done  chan struct{}
}

func newBacklightWatcher(root string, poke chan struct{}) *backlightWatcher {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC)
	if err != nil {
		return &backlightWatcher{fd: -1, root: root, poke: poke}
	}
	w := &backlightWatcher{fd: fd, root: root, files: map[string]int32{},
		poke: poke, stop: make(chan struct{}), done: make(chan struct{})}
	unix.InotifyAddWatch(fd, root, unix.IN_CREATE|unix.IN_DELETE|unix.IN_MOVED_FROM|unix.IN_MOVED_TO)
	go w.pump()
	return w
}

// sync watches the brightness file of every current sysfs device, dropping
// files whose device is gone.
func (w *backlightWatcher) sync(names []string) {
	if w.fd < 0 {
		return
	}
	want := make(map[string]bool, len(names))
	for _, name := range names {
		path := filepath.Join(w.root, name, "brightness")
		want[path] = true
		if _, ok := w.files[path]; !ok {
			if wd, err := unix.InotifyAddWatch(w.fd, path, unix.IN_MODIFY|unix.IN_CLOSE_WRITE); err == nil {
				w.files[path] = int32(wd)
			}
		}
	}
	for path, wd := range w.files {
		if !want[path] {
			unix.InotifyRmWatch(w.fd, uint32(wd))
			delete(w.files, path)
		}
	}
}

func (w *backlightWatcher) pump() {
	defer close(w.done)
	buf := make([]byte, 4096)
	pfd := []unix.PollFd{{Fd: int32(w.fd), Events: unix.POLLIN}}
	for {
		// Poll with a short timeout rather than a blocking read: closing the
		// inotify fd does not wake a blocked read, so a blocking pump would
		// hang service shutdown. The timeout bounds shutdown latency only.
		n, err := unix.Poll(pfd, 50)
		select {
		case <-w.stop:
			return
		default:
		}
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return
		}
		if n <= 0 {
			continue
		}
		rn, err := unix.Read(w.fd, buf)
		if rn <= 0 {
			if err != nil && !errors.Is(err, syscall.EINTR) && !errors.Is(err, syscall.EAGAIN) {
				return
			}
			continue
		}
		// A root-directory event may have changed the device set; the poll
		// that follows re-enumerates, and sync() adjusts the file watches.
		w.notify()
	}
}

func (w *backlightWatcher) notify() {
	select {
	case w.poke <- struct{}{}:
	default:
	}
}

func (w *backlightWatcher) close() {
	if w.fd < 0 {
		return
	}
	close(w.stop)
	unix.Close(w.fd)
	<-w.done
}

func readIntFile(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}
