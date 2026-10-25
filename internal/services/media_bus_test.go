package services

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestSessionBusEmitGivesUpAndReportsOverflow(t *testing.T) {
	b := &sessionBus{
		changes: make(chan nameChange, 1),
		owners:  map[string]string{},
	}
	b.emit(nameChange{Name: "org.mpris.MediaPlayer2.a"})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 3; i++ {
			b.emit(nameChange{Name: "org.mpris.MediaPlayer2.a"})
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("emit waited for a reader on a full changes channel")
	}
	if !b.Overflowed() {
		t.Fatal("overflow was not reported")
	}
	if b.Overflowed() {
		t.Fatal("overflow stayed set after it was read")
	}
}

func TestMediaReEnumeratesAfterADroppedEvent(t *testing.T) {
	b := newFakeBus("org.mpris.MediaPlayer2.a")
	m := NewMedia(b)
	defer m.Close()
	lease, err := m.Acquire()
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release()
	waitForEnumerations(t, b, 1)

	b.overflow.Store(true)
	b.nameCh <- nameChange{Name: "org.mpris.MediaPlayer2.a", Acquired: true}
	waitForEnumerations(t, b, 2)
}

func waitForEnumerations(t *testing.T, b *fakeBus, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.enumerations() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("enumerations = %d, want at least %d", b.enumerations(), want)
}

// TestSessionMediaUnleasedTrafficDoesNotGrowGoroutines is the reported leak:
// with no lease, nothing drains the event queue, and once it is full the pump
// stops reading its signal channel. godbus then answers every new signal by
// parking a goroutine until somebody reads again. Run it under
// dbus-run-session.
func TestSessionMediaUnleasedTrafficDoesNotGrowGoroutines(t *testing.T) {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Skip("no session bus address; run under dbus-run-session or set DBUS_SESSION_BUS_ADDRESS")
	}
	m := NewSessionMedia()
	defer m.Close()
	lease, err := m.Acquire()
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	lease.Release()

	probe, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatalf("probe connection: %v", err)
	}
	defer probe.Close()
	bus := probe.BusObject()
	for i := 0; i < 100; i++ {
		var got uint32
		if err := bus.Call("org.freedesktop.DBus.RequestName", 0, "org.mpris.MediaPlayer2.probe", uint32(0)).Store(&got); err != nil {
			t.Fatalf("RequestName: %v", err)
		}
		if err := bus.Call("org.freedesktop.DBus.ReleaseName", 0, "org.mpris.MediaPlayer2.probe").Store(&got); err != nil {
			t.Fatalf("ReleaseName: %v", err)
		}
	}

	time.Sleep(50 * time.Millisecond)
	runtime.GC()
	base := runtime.NumGoroutine()
	for i := 0; i < 2000; i++ {
		if err := probe.Emit("/org/example/App", "org.freedesktop.DBus.Properties.PropertiesChanged",
			"org.example.App", map[string]dbus.Variant{}, []string{}); err != nil {
			t.Fatalf("Emit: %v", err)
		}
	}
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	if n := runtime.NumGoroutine(); n > base+10 {
		t.Fatalf("goroutines = %d, want at most %d after unleased bus traffic", n, base+10)
	}
}

func TestSessionBusTracksInitialAndChangingMPRISOwners(t *testing.T) {
	b := &sessionBus{
		changes: make(chan nameChange, 2),
		owners:  map[string]string{},
	}

	b.handleNameOwnerChanged("org.mpris.MediaPlayer2.spotify", "", ":1.42")
	if got := b.ownerFor(":1.42"); got != "org.mpris.MediaPlayer2.spotify" {
		t.Fatalf("initial owner = %q, want spotify", got)
	}

	b.handleNameOwnerChanged("org.mpris.MediaPlayer2.spotify", ":1.42", ":1.43")
	if got := b.ownerFor(":1.42"); got != "" {
		t.Fatalf("old owner still mapped to %q", got)
	}
	if got := b.ownerFor(":1.43"); got != "org.mpris.MediaPlayer2.spotify" {
		t.Fatalf("replacement owner = %q, want spotify", got)
	}

	b.handleNameOwnerChanged("org.freedesktop.Notifications", ":1.43", ":1.44")
	if got := b.ownerFor(":1.43"); got != "org.mpris.MediaPlayer2.spotify" {
		t.Fatalf("non-MPRIS change disturbed owner map: %q", got)
	}
}
