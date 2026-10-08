package lock

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/godbus/dbus/v5"
	"io"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestManagedLockReattachesAfterShellRestart(t *testing.T) {
	c := New("session", "niri")
	if err := c.accept("owner", Snapshot{Session: "session", Compositor: "niri:1:2", Generation: 4, Sequence: 3, Phase: "sealed"}); err != nil {
		t.Fatal(err)
	}
	if !c.State().Known || c.State().Phase != "sealed" {
		t.Fatal(c.State())
	}
}
func TestManagedDisconnectNeverMeansUnlocked(t *testing.T) {
	c := New("session", "niri")
	_ = c.accept("owner", Snapshot{Session: "session", Compositor: "niri:1:2", Generation: 4, Sequence: 3, Phase: "sealed"})
	c.disconnect()
	if c.State().Known || c.State().Phase != "sealed/unknown" || c.State().ConfirmedUnlock != 0 {
		t.Fatal(c.State())
	}
}
func TestBusOwnerChangeInvalidatesSnapshot(t *testing.T) {
	c := New("session", "niri")
	_ = c.accept("old", Snapshot{Session: "session", Compositor: "niri:1:2", Generation: 4, Sequence: 30, Phase: "sealed"})
	c.disconnect()
	if err := c.accept("new", Snapshot{Session: "session", Compositor: "niri:1:2", Generation: 4, Sequence: 1, ConfirmedUnlock: 4, Phase: "idle"}); err != nil {
		t.Fatal(err)
	}
	if c.State().Owner != "new" || c.State().ConfirmedUnlock != 4 {
		t.Fatal(c.State())
	}
}
func TestSuspendFailureDoesNotCallLogind(t *testing.T) {
	called := false
	c := New("session", "niri")
	if err := c.Suspend(context.Background(), func() error { called = true; return nil }); err == nil {
		t.Fatal("unavailable suspend accepted")
	}
	if called {
		t.Fatal("called logind before sealed")
	}
}
func TestManagedRejectsAnotherSession(t *testing.T) {
	c := New("session", "niri")
	if err := c.accept("owner", Snapshot{Session: "other", Compositor: "niri:1:2", Phase: "sealed"}); err == nil {
		t.Fatal("wrong session")
	}
}

func TestManagedLateReplyCannotRestoreOldOwner(t *testing.T) {
	c := New("session", "niri")
	old, current := new(liveBus), new(liveBus)
	v := Snapshot{Session: "session", Compositor: "niri:1:2", Generation: 4, Sequence: 3, Phase: "sealed"}
	_ = c.accept("old", v)
	c.conn = old
	c.disconnect()
	_ = c.accept("new", v)
	c.conn = current
	v.Phase = "idle"
	v.ConfirmedUnlock = 4
	v.Sequence++
	if err := c.acceptReply(old, "old", "old", v); err == nil {
		t.Fatal("accepted old connection reply")
	}
	if c.State().Owner != "new" || c.State().Phase != "sealed" {
		t.Fatal(c.State())
	}
}

func TestManagedConnectionLossInvalidatesSnapshot(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	conn, err := dbus.NewConn(local)
	if err != nil {
		t.Fatal(err)
	}
	c := New("session", "niri")
	_ = c.accept("owner", Snapshot{Session: "session", Compositor: "niri:1:2", Generation: 4, Phase: "sealed"})
	bus := &liveBus{conn: conn, signals: make(chan *dbus.Signal, 32)}
	c.conn = bus
	changed := c.changed
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); c.run(ctx, bus) }()
	defer func() { cancel(); <-done }()
	conn.Close()
	select {
	case <-changed:
		if c.State().Known || c.State().Phase != "sealed/unknown" {
			t.Fatal("connection loss retained trusted state", c.State())
		}
	case <-time.After(time.Second):
		t.Fatal("connection loss waited for an owner signal")
	}
}

type replacingLocker struct {
	owner       *dbus.Conn
	replacement *dbus.Conn
	release     bool
}

func (s replacingLocker) GetState() (string, *dbus.Error) {
	return encodeTestSnapshot(Snapshot{Session: "session", Compositor: "niri:1:2", Generation: 4, Sequence: 1, Phase: "sealed", SleepProtected: true})
}

func (s replacingLocker) Lock() (string, *dbus.Error) {
	var err error
	if s.release {
		_, err = s.owner.ReleaseName(busName)
	} else {
		_, err = s.replacement.RequestName(busName, dbus.NameFlagReplaceExisting)
	}
	if err != nil {
		return "", dbus.MakeFailedError(err)
	}
	return encodeTestSnapshot(Snapshot{Session: "session", Compositor: "niri:1:2", Generation: 4, Sequence: 2, ConfirmedUnlock: 4, Phase: "idle", SleepProtected: true})
}

func encodeTestSnapshot(v Snapshot) (string, *dbus.Error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", dbus.MakeFailedError(err)
	}
	return string(b), nil
}

func TestManagedLockRejectsQueuedBusOwnerReplacement(t *testing.T) {
	c := managedOwnerRaceClient(t, false)
	if _, err := c.Lock(context.Background()); err == nil {
		t.Fatal("accepted Lock reply after the bus name moved to another owner")
	}
	if got := c.State(); got.Known || got.Phase != "sealed/unknown" {
		t.Fatalf("replacement owner left the old lock snapshot trusted: %+v", got)
	}
}

func TestManagedLockInvalidatesWhenOwnerLookupFails(t *testing.T) {
	c := managedOwnerRaceClient(t, true)
	if _, err := c.Lock(context.Background()); err == nil {
		t.Fatal("accepted Lock reply after the bus name was released")
	}
	if got := c.State(); got.Known || got.Phase != "sealed/unknown" {
		t.Fatalf("failed owner lookup left the old lock snapshot trusted: %+v", got)
	}
}

type blockingLocker struct {
	started chan struct{}
	release chan struct{}
}

func (s blockingLocker) GetState() (string, *dbus.Error) {
	return encodeTestSnapshot(Snapshot{Session: "session", Compositor: "niri:1:2", Generation: 4, ConfirmedUnlock: 3, Sequence: 1, Phase: "idle", SleepProtected: true})
}

func (s blockingLocker) Lock() (string, *dbus.Error) {
	close(s.started)
	<-s.release
	return encodeTestSnapshot(Snapshot{Session: "session", Compositor: "niri:1:2", Generation: 5, Sequence: 2, Phase: "requesting", SleepProtected: true})
}

func TestManagedLockCancellationInvalidatesUnconfirmedState(t *testing.T) {
	startPrivateSessionBus(t)
	owner, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if reply, err := owner.RequestName(busName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("owner request: reply=%v err=%v", reply, err)
	}
	service := blockingLocker{started: make(chan struct{}), release: make(chan struct{})}
	defer close(service.release)
	if err := owner.Export(service, busPath, busName); err != nil {
		t.Fatal(err)
	}
	c := New("session", "niri")
	conn, err := dialFn()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	c.snapshot(context.Background(), conn)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := c.Lock(ctx); result <- err }()
	<-service.started
	cancel()
	if err := <-result; err == nil {
		t.Fatal("cancelled Lock unexpectedly succeeded")
	}
	if got := c.State(); got.Known {
		t.Fatalf("unconfirmed Lock retained stale state: %+v", got)
	}
}

func managedOwnerRaceClient(t *testing.T, release bool) *Client {
	t.Helper()
	startPrivateSessionBus(t)
	oldOwner, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = oldOwner.Close() })
	newOwner, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = newOwner.Close() })
	if reply, err := oldOwner.RequestName(busName, dbus.NameFlagAllowReplacement); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("old owner request: reply=%v err=%v", reply, err)
	}
	if err := oldOwner.Export(replacingLocker{owner: oldOwner, replacement: newOwner, release: release}, busPath, busName); err != nil {
		t.Fatal(err)
	}
	c := New("session", "niri")
	conn, err := dialFn()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	c.snapshot(context.Background(), conn)
	return c
}

func startPrivateSessionBus(t *testing.T) {
	t.Helper()
	command, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(command, "--session", "--nofork", "--nopidfile", "--print-address=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = io.Discard
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
	var value string
	select {
	case value = <-address:
	case <-time.After(5 * time.Second):
		t.Fatal("dbus-daemon did not report its address")
	}
	if value == "" {
		t.Fatal("dbus-daemon did not report its address")
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", strings.TrimSpace(value))
}

func TestManagedSuspendWaitsForSeal(t *testing.T) {
	c := New("session", "niri")
	v := Snapshot{Session: "session", Compositor: "niri:1:2", Generation: 2, Phase: "requesting", SleepProtected: true}
	_ = c.accept("owner", v)
	result := make(chan error, 1)
	go func() { result <- c.waitSealed(context.Background(), "owner", 2) }()
	select {
	case err := <-result:
		t.Fatalf("returned before seal: %v", err)
	default:
	}
	v.Sequence++
	v.Phase = "sealed"
	_ = c.accept("owner", v)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	c.disconnect()
	if err := c.waitSealed(context.Background(), "owner", 2); err == nil {
		t.Fatal("lost owner allowed suspend")
	}
}

func TestManagedRejectsGenerationRegression(t *testing.T) {
	c := New("session", "niri")
	v := Snapshot{Session: "session", Compositor: "niri:1:2", Generation: 4, ConfirmedUnlock: 3, Sequence: 5, Phase: "sealed"}
	if err := c.accept("owner", v); err != nil {
		t.Fatal(err)
	}
	v.Sequence++
	v.Generation = 3
	if err := c.accept("owner", v); err == nil {
		t.Fatal("accepted lower generation with newer sequence")
	}
}
