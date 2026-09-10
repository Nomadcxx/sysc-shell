package services

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestBrightnessReadsSysfs(t *testing.T) {
	t.Parallel()
	root := fixtureSysfs(t, "intel_backlight", 400, 1000)
	b := NewBrightness(root, "/nonexistent/brightnessctl", time.Second)
	if !b.Available() {
		t.Fatal("device present must be available")
	}
	if got := b.Level(); got != 40 {
		t.Fatalf("level %d, want 40", got)
	}
}

func TestBrightnessZeroDevicesUnavailable(t *testing.T) {
	t.Parallel()
	b := NewBrightness(t.TempDir(), "brightnessctl", time.Second)
	if b.Available() {
		t.Fatal("no devices must be unavailable")
	}
}

func TestBrightnessCachedStateDoesNotReadSysfs(t *testing.T) {
	root := fixtureSysfs(t, "intel_backlight", 400, 1000)
	b := NewBrightness(root, "/nonexistent/brightnessctl", time.Hour)
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
	b := NewBrightness(root, fake.path, time.Second)
	if err := b.Step(+10); err != nil {
		t.Fatal(err)
	}
	fake.expect(t, "set", "+10%")
}

func TestBrightnessSetShellsOut(t *testing.T) {
	t.Parallel()
	root := fixtureSysfs(t, "intel_backlight", 400, 1000)
	fake := fakeBrightnessctl(t)
	b := NewBrightness(root, fake.path, time.Second)
	if err := b.Set(73); err != nil {
		t.Fatal(err)
	}
	fake.expect(t, "set", "73%")
}

func fixtureSysfs(t *testing.T, name string, cur, max int) string {
	t.Helper()
	root := t.TempDir()
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
	return root
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
