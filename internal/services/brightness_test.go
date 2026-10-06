package services

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// testBrightness builds a service that never touches real hardware: empty
// device roots and no DDC probe seam unless the test installs one.
func testBrightness(t *testing.T, root, bin string, interval time.Duration) *Brightness {
	t.Helper()
	if interval == 0 {
		interval = time.Hour
	}
	return newBrightness(root, bin, interval, t.TempDir(), t.TempDir(), t.TempDir())
}

func TestBrightnessReadsSysfs(t *testing.T) {
	t.Parallel()
	root := fixtureSysfs(t, "intel_backlight", 400, 1000)
	b := testBrightness(t, root, "/nonexistent/brightnessctl", 0)
	if !b.Available() {
		t.Fatal("device present must be available")
	}
	if got := b.Level(); got != 40 {
		t.Fatalf("level %d, want 40", got)
	}
}

func TestBrightnessZeroDevicesUnavailable(t *testing.T) {
	t.Parallel()
	b := testBrightness(t, t.TempDir(), "brightnessctl", 0)
	if b.Available() {
		t.Fatal("no devices must be unavailable")
	}
}

func TestBrightnessCachedStateDoesNotReadSysfs(t *testing.T) {
	root := fixtureSysfs(t, "intel_backlight", 400, 1000)
	b := testBrightness(t, root, "/nonexistent/brightnessctl", 0)
	t.Cleanup(b.Close)
	if _, err := b.Acquire(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		b.mu.Lock()
		sampled := b.hasLast
		b.mu.Unlock()
		if sampled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("baseline brightness poll never completed")
		}
		time.Sleep(time.Millisecond)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	st, ok := b.CachedState()
	if !ok || st.Level != 40 {
		t.Fatalf("CachedState() = %+v, %v, want level 40 from the completed poll", st, ok)
	}
}

func TestBrightnessStepShellsOut(t *testing.T) {
	t.Parallel()
	root := fixtureSysfs(t, "intel_backlight", 400, 1000)
	fake := fakeBrightnessctl(t)
	b := testBrightness(t, root, fake.path, 0)
	b.Available()
	if err := b.Step(+10); err != nil {
		t.Fatal(err)
	}
	fake.expect(t, "-d", "intel_backlight", "set", "+10%")
}

func TestBrightnessSetShellsOut(t *testing.T) {
	t.Parallel()
	root := fixtureSysfs(t, "intel_backlight", 400, 1000)
	fake := fakeBrightnessctl(t)
	b := testBrightness(t, root, fake.path, 0)
	b.Available()
	if err := b.Set(73); err != nil {
		t.Fatal(err)
	}
	fake.expect(t, "-d", "intel_backlight", "set", "73%")
}

func TestBrightnessPerDisplayRouting(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fixtureSysfsIn(t, root, "intel_backlight", 500, 1000)
	fixtureSysfsIn(t, root, "dp_aux", 250, 1000)
	fake := fakeBrightnessctl(t)
	b := testBrightness(t, root, fake.path, 0)
	if !b.Available() {
		t.Fatal("two backlights must be available")
	}
	t.Cleanup(b.Close)
	if _, err := b.Acquire(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		b.mu.Lock()
		sampled := b.hasLast
		b.mu.Unlock()
		if sampled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("baseline brightness poll never completed")
		}
		time.Sleep(time.Millisecond)
	}
	displays := b.Displays()
	if len(displays) != 2 {
		t.Fatalf("displays = %d, want 2", len(displays))
	}
	cached := b.CachedDisplays()
	if len(cached) != 2 || cached[0].Level != 25 || cached[1].Level != 50 {
		t.Fatalf("CachedDisplays after discovery = %+v, want levels 25,50", cached)
	}
	// Default is the lowest-ID sysfs device: dp_aux.
	if err := b.Set(60); err != nil {
		t.Fatal(err)
	}
	fake.expect(t, "-d", "dp_aux", "set", "60%")
	if err := b.SetDisplay("sysfs:intel_backlight", 20); err != nil {
		t.Fatal(err)
	}
	if err := b.StepDisplay("sysfs:dp_aux", -5); err != nil {
		t.Fatal(err)
	}
	if err := b.SetDisplay("sysfs:missing", 20); !errors.Is(err, nil) && err == nil {
		t.Fatal("unknown display id must error")
	}
}

func TestDDCWriteFrame(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		frame []byte
		want  []byte
	}{
		{"get vcp 0x10", ddcWriteFrame(ddcOpGet, ddcVCPBright),
			[]byte{0x51, 0x82, 0x01, 0x10, 0xac}},
		{"set vcp 0x10 to 50", ddcWriteFrame(ddcOpSet, ddcVCPBright, 0x00, 0x32),
			[]byte{0x51, 0x84, 0x03, 0x10, 0x00, 0x32, 0x9a}},
	}
	for _, tc := range tests {
		if fmt.Sprint(tc.frame) != fmt.Sprint(tc.want) {
			t.Errorf("%s: frame = %x, want %x", tc.name, tc.frame, tc.want)
		}
	}
}

func TestParseVCPReply(t *testing.T) {
	t.Parallel()
	valid := []byte{0x6e, 0x80 | 0x09, 0x02, 0x00, 0x10, 0x01, 0x00, 0x64, 0x00, 0x28, 0x00}
	tests := []struct {
		name    string
		buf     []byte
		wantCur int
		wantMax int
		wantErr bool
	}{
		{"valid", valid, 40, 100, false},
		{"short", valid[:10], 0, 0, true},
		{"bad addr", append([]byte{0x50}, valid[1:]...), 0, 0, true},
		{"bad len", []byte{0x6e, 0, 0x03, 0, 0x10, 0x01, 0, 100, 0, 40, 0}, 0, 0, true},
		{"unsupported", []byte{0x6e, 0, 0x02, 0x01, 0x10, 0x01, 0, 100, 0, 40, 0}, 0, 0, true},
		{"wrong echo", []byte{0x6e, 0, 0x02, 0x00, 0xd6, 0x01, 0, 100, 0, 40, 0}, 0, 0, true},
	}
	for _, tc := range tests {
		cur, max, err := parseVCPReply(tc.buf, ddcVCPBright)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tc.name, err, tc.wantErr)
			continue
		}
		if cur != tc.wantCur || max != tc.wantMax {
			t.Errorf("%s: got %d/%d, want %d/%d", tc.name, cur, max, tc.wantCur, tc.wantMax)
		}
	}
}

func TestIgnorableAdapterName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		ignorable bool
	}{
		{"SMBus I801 adapter at e000", true},
		{"Synopsys DesignWare I2C adapter", true},
		{"NVIDIA i2c adapter 1 at 8:00.0", false},
		{"nouveau", true},
		{"nvkm-i2c-bus-0", false},
		{"i915 gma ddi", false},
	}
	for _, tc := range tests {
		if got := ignorableAdapterName(tc.name); got != tc.ignorable {
			t.Errorf("ignorableAdapterName(%q) = %v, want %v", tc.name, got, tc.ignorable)
		}
	}
}

func TestMatchEDIDToConnector(t *testing.T) {
	t.Parallel()
	drm := t.TempDir()
	edidA := make([]byte, edidLength)
	edidB := make([]byte, edidLength)
	edidA[0], edidA[1] = 0x00, 0xff
	edidB[0], edidB[1] = 0x00, 0xfe
	for _, tc := range []struct {
		dir, want string
		edid      []byte
		write     bool
	}{
		{"card1-DP-1", "DP-1", edidA, true},
		{"card1-DP-2", "DP-2", edidB, true},
		{"renderD128", "", nil, false},
		{"card0", "", nil, false},
	} {
		if !tc.write {
			continue
		}
		dir := filepath.Join(drm, tc.dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "edid"), tc.edid, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(drm, "renderD128"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(drm, "card0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := matchEDIDToConnector(edidA, drm); got != "DP-1" {
		t.Errorf("match EDID A = %q, want DP-1", got)
	}
	if got := matchEDIDToConnector(edidB, drm); got != "DP-2" {
		t.Errorf("match EDID B = %q, want DP-2", got)
	}
	other := make([]byte, edidLength)
	other[0] = 0x11
	if got := matchEDIDToConnector(other, drm); got != "" {
		t.Errorf("unmatched EDID = %q, want empty", got)
	}
	if got := matchEDIDToConnector(edidA[:64], drm); got != "" {
		t.Errorf("short EDID = %q, want empty", got)
	}
}

func TestDDCPercentMapping(t *testing.T) {
	t.Parallel()
	if got := ddcPercentToValue(0, 100); got != 1 {
		t.Errorf("percent 0 -> %d, want 1 (clamped to min)", got)
	}
	if got := ddcPercentToValue(100, 100); got != 100 {
		t.Errorf("percent 100 -> %d, want 100", got)
	}
	if got := ddcPercentToValue(50, 255); got != 1+49*254/99 {
		t.Errorf("percent 50 max 255 -> %d, want linear", got)
	}
	if got := ddcValueToPercent(1, 100); got != 1 {
		t.Errorf("value 1 -> %d, want 1", got)
	}
	if got := ddcValueToPercent(100, 100); got != 100 {
		t.Errorf("value 100 -> %d, want 100", got)
	}
	if got := ddcValueToPercent(50, 1); got != 100 {
		t.Errorf("degenerate max -> %d, want 100", got)
	}
}

func TestBrightnessDDCDiscoveryNoReprobe(t *testing.T) {
	t.Parallel()
	b := testBrightness(t, t.TempDir(), "brightnessctl", 0)
	calls := map[int]int{}
	b.probe = func(bus int) (string, int, int, error) {
		calls[bus]++
		if bus == 1 {
			return "DP-1", 100, 100, nil
		}
		return "", 0, 0, errors.New("absent")
	}
	// Make the scan look like a /dev with four buses.
	devRoot := b.devRoot
	for i := 0; i < 4; i++ {
		if err := os.WriteFile(filepath.Join(devRoot, "i2c-"+strconv.Itoa(i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !b.Available() {
		t.Fatal("ddc display on bus 1 must be available")
	}
	// Detection must seed the render cache: sliders exist before any lease
	// or poll, without opening the bus again.
	seeded := b.CachedDisplays()
	if len(seeded) != 1 || seeded[0].ID != "ddc:DP-1" || seeded[0].Level != 100 || !seeded[0].OK {
		t.Fatalf("seeded cache = %+v, want ddc:DP-1 level 100 ok", seeded)
	}
	displays := b.Displays()
	if len(displays) != 1 || displays[0].ID != "ddc:DP-1" {
		t.Fatalf("displays = %+v, want one ddc:DP-1", displays)
	}
	first := len(calls)
	b.mu.Lock()
	b.lastScan = time.Now().Add(-time.Hour) // outside the rescan gap
	b.mu.Unlock()
	b.refresh()
	if len(calls) != first {
		t.Fatalf("known connector was re-probed: %v", calls)
	}
}

func TestBrightnessDDCCooldown(t *testing.T) {
	t.Parallel()
	b := testBrightness(t, t.TempDir(), "brightnessctl", 0)
	if err := os.WriteFile(filepath.Join(b.devRoot, "i2c-1"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	b.probe = func(bus int) (string, int, int, error) {
		if bus == 1 {
			return "DP-1", 100, 100, nil
		}
		return "", 0, 0, errors.New("absent")
	}
	b.refresh()
	b.mu.Lock()
	dev := b.devices[0]
	failing := errors.New("i/o error")
	for i := 0; i < ddcFailLimit; i++ {
		b.trackFailureLocked(dev, failing)
	}
	inCooldown := len(b.cooldown) == 1
	deleteKnown := len(b.knownDDC) == 0
	b.mu.Unlock()
	if !inCooldown || !deleteKnown {
		t.Fatalf("cooldown state: cool=%v known=%d", inCooldown, len(b.knownDDC))
	}
	// The next refresh treats the cooled-down bus as absent.
	b.refresh()
	b.mu.Lock()
	devicesGone := len(b.devices) == 0
	b.mu.Unlock()
	if !devicesGone {
		t.Fatal("cooled-down bus must produce no device")
	}
	// assemble() with empty known list drops the device; poll re-reads.
	// Expired cooldown re-probes the bus.
	b.mu.Lock()
	b.cooldown[1] = time.Now().Add(-time.Second)
	b.mu.Unlock()
	b.refresh()
	b.mu.Lock()
	recovered := len(b.devices) == 1
	b.mu.Unlock()
	if !recovered {
		t.Fatal("expired cooldown must re-probe and restore the display")
	}
}

func TestBrightnessAssembleReusesDDCInstance(t *testing.T) {
	t.Parallel()
	first := &ddcDev{bus: 1, connector: "DP-1", max: 100}
	known := map[int]ddcIdent{1: {connector: "DP-1", max: 100}}
	out := assemble(nil, known, "/dev", []brightnessDevice{first})
	if out[0] != brightnessDevice(first) {
		t.Fatal("same connector+max must reuse the instance (keeps debounce timer and cache)")
	}
	changed := assemble(nil, map[int]ddcIdent{1: {connector: "DP-1", max: 255}}, "/dev", out)
	if changed[0] == brightnessDevice(first) {
		t.Fatal("changed max must rebuild the device")
	}
}

func fixtureSysfs(t *testing.T, name string, cur, max int) string {
	t.Helper()
	root := t.TempDir()
	fixtureSysfsIn(t, root, name, cur, max)
	return root
}

func fixtureSysfsIn(t *testing.T, root, name string, cur, max int) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "brightness"), []byte(strconv.Itoa(cur)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "max_brightness"), []byte(strconv.Itoa(max)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fakeBrightnessctl(t *testing.T) *fakeCmd {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
echo "$*" >> "` + dir + `/log"
`
	path := filepath.Join(dir, "brightnessctl")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &fakeCmd{path: path, dir: dir}
}

func TestBrightnessSeedsCacheFromSysfsDetection(t *testing.T) {
	t.Parallel()
	root := fixtureSysfs(t, "intel_backlight", 400, 1000)
	b := testBrightness(t, root, "/nonexistent/brightnessctl", 0)
	if !b.Available() {
		t.Fatal("device present must be available")
	}
	seeded := b.CachedDisplays()
	if len(seeded) != 1 || seeded[0].ID != "sysfs:intel_backlight" || seeded[0].Level != 40 || !seeded[0].OK {
		t.Fatalf("seeded cache = %+v, want one sysfs display at level 40", seeded)
	}
}

func TestBrightnessInotifyPoke(t *testing.T) {
	root := fixtureSysfs(t, "first", 250, 1000)
	b := testBrightness(t, root, "/nonexistent/brightnessctl", 0)
	t.Cleanup(b.Close)
	if _, err := b.Acquire(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		b.mu.Lock()
		sampled := b.hasLast
		b.mu.Unlock()
		if sampled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("baseline brightness poll never completed")
		}
		time.Sleep(time.Millisecond)
	}
	fixtureSysfsIn(t, root, "second", 500, 1000)
	// The ticker is an hour away; only an inotify poke can make the poll
	// loop notice the new backlight.
	deadline = time.Now().Add(20 * time.Second) // slack for a loaded -race runner; the poke itself is immediate
	for {
		if got := b.CachedDisplays(); len(got) == 2 {
			for _, d := range got {
				if d.ID == "sysfs:second" && d.Level == 50 && d.OK {
					return
				}
			}
			t.Fatalf("poke picked up a wrong second device: %+v", got)
		}
		if time.Now().After(deadline) {
			t.Fatal("inotify poke did not surface a backlight created after the lease")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
