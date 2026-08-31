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
