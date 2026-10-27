package walls

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	testSystemdBusName      = "org.freedesktop.systemd1"
	testSystemdManagerPath  = dbus.ObjectPath("/org/freedesktop/systemd1")
	testSystemdManagerIface = "org.freedesktop.systemd1.Manager"
)

type testSystemdManager struct{ subscribed chan struct{} }

func (m *testSystemdManager) Subscribe() *dbus.Error {
	select {
	case m.subscribed <- struct{}{}:
	default:
	}
	return nil
}

func TestSystemdServiceChangesRefreshWalls(t *testing.T) {
	owner, manager := startSystemdSignalBus(t)

	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	callLog := filepath.Join(tmp, "systemctl.log")
	script := "#!/bin/sh\nprintf 'call\\n' >> \"$WALLS_TEST_SYSTEMCTL_LOG\"\n" +
		"printf 'LoadState=not-found\\nUnitFileState=not-found\\nActiveState=inactive\\nSubState=dead\\n'\n"
	if err := os.WriteFile(filepath.Join(tmp, "systemctl"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmp)
	t.Setenv("HOME", home)
	t.Setenv("WALLS_TEST_SYSTEMCTL_LOG", callLog)

	service := NewService()
	t.Cleanup(func() { _ = service.Close() })
	select {
	case <-manager.subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("service did not subscribe to systemd unit changes")
	}
	initial := waitSnapshot(t, service, func(snapshot Snapshot) bool { return snapshot.SystemdRevision >= 1 })
	initialRevision := initial.SystemdRevision
	waitForSystemctlCalls(t, callLog, 1)

	before := systemctlCallCount(t, callLog)
	if err := owner.Emit(testSystemdManagerPath, testSystemdManagerIface+".UnitFilesChanged"); err != nil {
		t.Fatal(err)
	}
	waitForSystemctlCalls(t, callLog, before+1)
	waitSnapshot(t, service, func(snapshot Snapshot) bool { return snapshot.SystemdRevision >= initialRevision+1 })

	before = systemctlCallCount(t, callLog)
	if err := owner.Emit(testSystemdManagerPath, testSystemdManagerIface+".JobRemoved",
		uint32(1), dbus.ObjectPath("/org/freedesktop/systemd1/job/1"), unitName, "done"); err != nil {
		t.Fatal(err)
	}
	waitForSystemctlCalls(t, callLog, before+1)
	waitSnapshot(t, service, func(snapshot Snapshot) bool { return snapshot.SystemdRevision >= initialRevision+2 })
}

func TestSystemdLoadChurnDoesNotRefreshWalls(t *testing.T) {
	owner, manager := startSystemdSignalBus(t)

	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	callLog := filepath.Join(tmp, "systemctl.log")
	script := "#!/bin/sh\nprintf 'call\\n' >> \"$WALLS_TEST_SYSTEMCTL_LOG\"\n" +
		"printf 'LoadState=not-found\\nUnitFileState=not-found\\nActiveState=inactive\\nSubState=dead\\n'\n"
	if err := os.WriteFile(filepath.Join(tmp, "systemctl"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmp)
	t.Setenv("HOME", home)
	t.Setenv("WALLS_TEST_SYSTEMCTL_LOG", callLog)

	service := NewService()
	t.Cleanup(func() { _ = service.Close() })
	select {
	case <-manager.subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("service did not subscribe to systemd unit changes")
	}
	waitSnapshot(t, service, func(snapshot Snapshot) bool { return snapshot.SystemdRevision >= 1 })
	waitForSystemctlCalls(t, callLog, 1)
	time.Sleep(50 * time.Millisecond)
	settled := systemctlCallCount(t, callLog)

	// Discovery runs 'systemctl --user show'. When systemd has already
	// garbage-collected the unit that query loads it, emits UnitNew, then
	// drops it again and emits UnitRemoved. Both arrive for sysc-walls itself,
	// and reacting to either one makes discovery drive the next discovery.
	for i := 0; i < 8; i++ {
		if err := owner.Emit(testSystemdManagerPath, testSystemdManagerIface+".UnitNew",
			unitName, systemdUnitPath); err != nil {
			t.Fatal(err)
		}
		if err := owner.Emit(testSystemdManagerPath, testSystemdManagerIface+".UnitRemoved",
			unitName, systemdUnitPath); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)

	// A real transition must still be picked up, so prove the monitor is awake
	// on the same connection rather than merely quiet.
	if err := owner.Emit(testSystemdManagerPath, testSystemdManagerIface+".UnitFilesChanged"); err != nil {
		t.Fatal(err)
	}
	waitForSystemctlCalls(t, callLog, settled+1)

	if got := systemctlCallCount(t, callLog); got > settled+1 {
		t.Fatalf("unit load churn triggered %d extra discovery passes, want none", got-settled-1)
	}
}

func TestSystemdSignalAffectsWalls(t *testing.T) {
	tests := []struct {
		name string
		sig  *dbus.Signal
		want bool
	}{
		{
			name: "unit files changed",
			sig:  &dbus.Signal{Name: testSystemdManagerIface + ".UnitFilesChanged", Path: testSystemdManagerPath},
			want: true,
		},
		{
			name: "walls job removed",
			sig: &dbus.Signal{Name: testSystemdManagerIface + ".JobRemoved", Path: testSystemdManagerPath, Body: []any{
				uint32(1), dbus.ObjectPath("/org/freedesktop/systemd1/job/1"), unitName, "done",
			}},
			want: true,
		},
		{
			name: "unrelated job removed",
			sig: &dbus.Signal{Name: testSystemdManagerIface + ".JobRemoved", Path: testSystemdManagerPath, Body: []any{
				uint32(1), dbus.ObjectPath("/org/freedesktop/systemd1/job/1"), "other.service", "done",
			}},
		},
		{
			// 'systemctl show' loads a garbage-collected unit and systemd drops
			// it again afterwards, so these two signals fire on every discovery
			// pass. Reacting to them closes a loop that never idles.
			name: "walls unit loaded by our own query",
			sig: &dbus.Signal{Name: testSystemdManagerIface + ".UnitNew", Path: testSystemdManagerPath, Body: []any{
				unitName, systemdUnitPath,
			}},
		},
		{
			name: "walls unit dropped after our own query",
			sig: &dbus.Signal{Name: testSystemdManagerIface + ".UnitRemoved", Path: testSystemdManagerPath, Body: []any{
				unitName, systemdUnitPath,
			}},
		},
		{
			name: "walls unit state changed",
			sig:  &dbus.Signal{Name: "org.freedesktop.DBus.Properties.PropertiesChanged", Path: systemdUnitPath},
			want: true,
		},
		{
			name: "other unit state changed",
			sig:  &dbus.Signal{Name: "org.freedesktop.DBus.Properties.PropertiesChanged", Path: dbus.ObjectPath("/org/freedesktop/systemd1/unit/other_2eservice")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := systemdSignalAffectsWalls(tt.sig); got != tt.want {
				t.Fatalf("systemdSignalAffectsWalls() = %v, want %v", got, tt.want)
			}
		})
	}
}

func startSystemdSignalBus(t *testing.T) (*dbus.Conn, *testSystemdManager) {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is not on PATH")
	}
	cmd := exec.Command(daemon, "--session", "--nofork", "--nopidfile", "--print-address=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	address := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		address <- line
	}()
	select {
	case value := <-address:
		if strings.TrimSpace(value) == "" {
			waitErr := cmd.Wait()
			if strings.Contains(stderr.String(), "Operation not permitted") {
				t.Skipf("sandbox does not allow a private D-Bus socket: %s", strings.TrimSpace(stderr.String()))
			}
			t.Fatalf("dbus-daemon printed no address: %v: %s", waitErr, strings.TrimSpace(stderr.String()))
		}
		t.Setenv("DBUS_SESSION_BUS_ADDRESS", strings.TrimSpace(value))
	case <-time.After(5 * time.Second):
		t.Fatal("dbus-daemon did not report its address")
	}

	owner, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	reply, err := owner.RequestName(testSystemdBusName, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("requesting fake systemd name: reply=%v err=%v", reply, err)
	}
	manager := &testSystemdManager{subscribed: make(chan struct{}, 1)}
	if err := owner.Export(manager, testSystemdManagerPath, testSystemdManagerIface); err != nil {
		t.Fatal(err)
	}
	return owner, manager
}

func systemctlCallCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "call\n")
}

func waitForSystemctlCalls(t *testing.T, path string, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if systemctlCallCount(t, path) >= count {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("systemctl calls = %d, want at least %d", systemctlCallCount(t, path), count)
}
