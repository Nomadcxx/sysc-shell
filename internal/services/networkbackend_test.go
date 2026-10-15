package services

import (
	"bufio"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These are the only tests that touch the real backend constructor. Everything
// else runs against the fake: a test that needs NetworkManager running is a
// test that fails on a build machine.
func TestNMBackendUnavailableWithoutBus(t *testing.T) {
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "unix:path=/nonexistent")
	if _, err := newNMBackend(); err == nil {
		t.Fatal("expected an error when the system bus is unreachable")
	}
}

// TestNMBackendUnavailableWhenNothingAnswers is the machine the kernel tier
// exists for: a system bus that works, with no NetworkManager on it. A
// constructor that only connects would claim the NM tier there and leave the
// panel reporting a radio that is off.
func TestNMBackendUnavailableWhenNothingAnswers(t *testing.T) {
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is not installed")
	}
	addr := "unix:path=" + filepath.Join(t.TempDir(), "bus")
	cmd := exec.Command(daemon, "--session", "--nofork", "--print-address", "--address="+addr)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("dbus-daemon printed no address: %v", err)
	}
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", strings.TrimSpace(line))
	if _, err := newNMBackend(); err == nil {
		t.Fatal("a bus with no NetworkManager on it was taken for NetworkManager")
	}
}
