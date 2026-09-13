package services

import "testing"

// This is the only test that touches the real backend constructor. Everything
// else runs against the fake: a test that needs NetworkManager running is a
// test that fails on a build machine.
func TestNMBackendUnavailableWithoutBus(t *testing.T) {
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "unix:path=/nonexistent")
	if _, err := newNMBackend(); err == nil {
		t.Fatal("expected an error when the system bus is unreachable")
	}
}
