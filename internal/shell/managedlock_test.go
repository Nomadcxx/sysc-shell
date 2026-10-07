package shell

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	locksession "github.com/Nomadcxx/sysc-shell/internal/lock"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/walls"
	"github.com/godbus/dbus/v5"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func backgroundRegistry(t *testing.T, phase string) (*Registry, *fakeWallsService) {
	t.Helper()
	svc := newFakeWallsService(walls.Snapshot{UnitKnown: true, ActiveState: "active", SubState: "running"})
	r := &Registry{closed: make(chan struct{}), wallsService: svc, backgroundLeasePath: filepath.Join(t.TempDir(), "private", "lease"), backgroundHeld: true}
	r.managedState = locksession.State{Known: true, Owner: "owner", Snapshot: locksession.Snapshot{Session: "s", Compositor: "n:1", Generation: 5, ConfirmedUnlock: 5, Phase: phase}}
	return r, svc
}
func TestBackgroundLeaseInitialRefusalDoesNothing(t *testing.T) {
	r, svc := backgroundRegistry(t, "failed-before-acquisition")
	r.coordinateBackground()
	if len(svc.runtime) != 0 || svc.stops != 0 {
		t.Fatal("refusal changed screensaver")
	}
	if _, err := os.Stat(r.backgroundLeasePath); !os.IsNotExist(err) {
		t.Fatal("refusal wrote lease", err)
	}
}
func TestBackgroundLeaseSealedKeepsOriginalState(t *testing.T) {
	r, svc := backgroundRegistry(t, "sealed")
	r.coordinateBackground()
	l, err := locksession.LoadLease(r.backgroundLeasePath)
	if err != nil || l == nil || !l.WallsRunning {
		t.Fatal(l, err)
	}
	svc.snapshot.ActiveState = "inactive"
	r.managedState.Generation++
	r.coordinateBackground()
	l, err = locksession.LoadLease(r.backgroundLeasePath)
	if err != nil || !l.WallsRunning || l.Generation != 6 {
		t.Fatal(l, err)
	}
	if len(svc.enables) != 0 {
		t.Fatal("changed enablement")
	}
}

func TestBackgroundLeaseSleepRecaptureRequiresNewReceipt(t *testing.T) {
	r, svc := backgroundRegistry(t, "sealed")
	r.coordinateBackground()
	r.managedState.Generation = 6
	r.managedState.ConfirmedUnlock = 5
	r.managedState.Phase = "requesting"
	r.coordinateBackground()
	l, err := locksession.LoadLease(r.backgroundLeasePath)
	if err != nil || l == nil || l.Generation != 5 || !r.backgroundHeld {
		t.Fatal("old receipt released recapture lease", l, err)
	}
	r.managedState.Phase = "sealed"
	r.coordinateBackground()
	l, err = locksession.LoadLease(r.backgroundLeasePath)
	if err != nil || l == nil || l.Generation != 6 || !l.WallsRunning {
		t.Fatal("recapture lost original runtime state", l, err)
	}
	r.managedState.Phase = "idle"
	r.coordinateBackground()
	if !r.backgroundHeld {
		t.Fatal("old receipt restored new acquisition")
	}
	r.managedState.ConfirmedUnlock = 6
	r.coordinateBackground()
	if r.backgroundHeld {
		t.Fatal("matching receipt did not restore")
	}
	if _, err = os.Stat(r.backgroundLeasePath); !os.IsNotExist(err) {
		t.Fatal("confirmed restore retained lease", err)
	}
	if len(svc.runtime) < 2 || !svc.runtime[len(svc.runtime)-1] || len(svc.enables) != 0 {
		t.Fatal("changed enablement or restored before confirmation", svc.runtime, svc.enables)
	}
	for _, running := range svc.runtime[:len(svc.runtime)-1] {
		if running {
			t.Fatal("restored before new receipt", svc.runtime)
		}
	}
}

// A state change inside Snapshot reproduces a new lock during restore confirmation.
type relockWalls struct {
	*fakeWallsService
	r     *Registry
	armed bool
}

func (f *relockWalls) Snapshot() walls.Snapshot {
	if f.armed {
		f.armed = false
		f.r.mu.Lock()
		f.r.managedState.Phase = "sealed"
		f.r.managedState.Generation++
		f.r.mu.Unlock()
	}
	return f.fakeWallsService.Snapshot()
}
func TestBackgroundLeaseRelockDuringRestoreRetainsLease(t *testing.T) {
	r, svc := backgroundRegistry(t, "idle")
	l := locksession.Lease{Session: "s", Compositor: "n:1", Generation: 5, WallsKnown: true, WallsRunning: true}
	if err := l.Save(r.backgroundLeasePath); err != nil {
		t.Fatal(err)
	}
	r.wallsService = &relockWalls{fakeWallsService: svc, r: r, armed: true}
	r.coordinateBackground()
	if _, err := os.Stat(r.backgroundLeasePath); err != nil {
		t.Fatal("lost lease", err)
	}
	if !r.backgroundHeld {
		t.Fatal("resumed wallpaper during relock")
	}
	if len(svc.runtime) != 2 || !svc.runtime[0] || svc.runtime[1] {
		t.Fatal(svc.runtime)
	}
}

// A suspend must never be refused: without a managed locker there is no seal
// to wait for, so logind is called plainly (GH #116).
func TestManualSuspendFallsBackWithoutAManagedClient(t *testing.T) {
	var ran [][]string
	r := &Registry{runArgv: func(argv []string) error { ran = append(ran, argv); return nil }}
	if err := r.SuspendTracked(); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || strings.Join(ran[0], " ") != "loginctl suspend" {
		t.Fatalf("fallback ran %v", ran)
	}
}

func TestManualSuspendFallsBackWhenTheLockOwnerIsMissing(t *testing.T) {
	for _, locker := range []string{"", "swaylock --session", "sysc-lock"} {
		t.Run(locker, func(t *testing.T) {
			called := make(chan []string, 1)
			r := &Registry{cfg: config.Config{Session: config.Session{Locker: locker}}, managedLock: locksession.New("session", "niri"), runArgv: func(argv []string) error { called <- argv; return nil }}
			if err := r.SuspendTracked(); err != nil {
				t.Fatal(err)
			}
			select {
			case argv := <-called:
				if strings.Join(argv, " ") != "loginctl suspend" {
					t.Fatalf("ran %v", argv)
				}
			case <-time.After(time.Second):
				t.Fatal("no suspend without a lock owner")
			}
		})
	}
}

// The #105 guarantee: with an owner on the bus, logind waits for the seal.
func TestManualSuspendWaitsForTheSealWithAKnownOwner(t *testing.T) {
	service := &suspendingLocker{started: make(chan struct{}), release: make(chan struct{})}
	called := make(chan []string, 1)
	r := &Registry{cfg: config.Config{Session: config.Session{Locker: "sysc-lock"}}, managedLock: startManagedOwner(t, service), runArgv: func(argv []string) error { called <- argv; return nil }}
	result := make(chan error, 1)
	go func() { result <- r.SuspendTracked() }()
	<-service.started
	select {
	case argv := <-called:
		t.Fatalf("loginctl ran before the seal: %v", argv)
	case <-time.After(100 * time.Millisecond):
	}
	close(service.release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	select {
	case argv := <-called:
		if strings.Join(argv, " ") != "loginctl suspend" {
			t.Fatalf("ran %v", argv)
		}
	default:
		t.Fatal("loginctl suspend did not run after the seal")
	}
}

func TestManualSuspendHandlersFallBackWithoutProtection(t *testing.T) {
	for _, panel := range []PanelID{PanelSession, PanelControlCenter} {
		t.Run(fmt.Sprint(panel), func(t *testing.T) {
			r := newPanelRegistry(t)
			called := make(chan []string, 1)
			r.runArgv = func(argv []string) error { called <- argv; return nil }
			if err := r.OpenPanel(panel, 7, Trigger{}); err != nil {
				t.Fatal(err)
			}
			r.mu.Lock()
			h := r.panelHosts[panel]
			if panel == PanelSession {
				r.runSessionAction(h, "session-suspend")
			} else {
				h.activateControlCentre(r, &ui.Node{Action: "session-suspend"})
			}
			r.mu.Unlock()
			select {
			case argv := <-called:
				if strings.Join(argv, " ") != "loginctl suspend" {
					t.Fatalf("handler ran %v", argv)
				}
			case <-time.After(time.Second):
				t.Fatal("handler did not suspend")
			}
		})
	}
}

func TestBackgroundAdmissionWaitsForKnownOwner(t *testing.T) {
	for _, phase := range []string{"idle", "failed-before-acquisition", "unavailable", "sealed/unknown", "requesting", "recovering", "unlocking", "sealed"} {
		s := locksession.State{Snapshot: locksession.Snapshot{Phase: phase}}
		if lockStateAllowsBackground(s) {
			t.Fatalf("unknown %s admitted startup", phase)
		}
		s.Known = true
		want := phase == "idle" || phase == "failed-before-acquisition"
		if lockStateAllowsBackground(s) != want {
			t.Fatalf("known %s admission differs", phase)
		}
	}
}

func TestManagedLockRejectsRelativeRuntimeDirectory(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "relative")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/dev/null")
	cfg := config.Default()
	cfg.Session.Locker = "sysc-lock"
	r := &Registry{cfg: cfg}
	r.initManagedLock()
	t.Cleanup(r.lockCancel)
	if r.backgroundLeasePath != "" {
		t.Fatalf("relative runtime directory became lease path %q", r.backgroundLeasePath)
	}
	if !r.backgroundHeld {
		t.Fatal("started backgrounds while the runtime directory was invalid")
	}
}

// The managed protocol's bus identity, mirrored from internal/lock so a shell
// check can stand in for the real locker.
const testBusName = "org.sysc.LockSession1"
const testBusPath = dbus.ObjectPath("/org/sysc/LockSession1")

// suspendingLocker holds Lock open so a check can see whether logind was called
// before the seal arrived.
type suspendingLocker struct {
	started chan struct{}
	release chan struct{}
}

func encodeSuspendSnapshot(v locksession.Snapshot) (string, *dbus.Error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", dbus.MakeFailedError(err)
	}
	return string(b), nil
}

func (s *suspendingLocker) GetState() (string, *dbus.Error) {
	return encodeSuspendSnapshot(locksession.Snapshot{Session: "session", Compositor: "niri:1:2", Generation: 4, ConfirmedUnlock: 3, Sequence: 1, Phase: "idle", SleepProtected: true})
}

func (s *suspendingLocker) Lock() (string, *dbus.Error) {
	close(s.started)
	<-s.release
	return encodeSuspendSnapshot(locksession.Snapshot{Session: "session", Compositor: "niri:1:2", Generation: 5, ConfirmedUnlock: 3, Sequence: 2, Phase: "sealed", SleepProtected: true})
}

// startManagedOwner runs a private session bus with service as the only owner
// and returns a client that has already talked to it.
func startManagedOwner(t *testing.T, service any) *locksession.Client {
	t.Helper()
	startPrivateSessionBus(t)
	owner, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if reply, err := owner.RequestName(testBusName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("owner request: reply=%v err=%v", reply, err)
	}
	if err := owner.Export(service, testBusPath, testBusName); err != nil {
		t.Fatal(err)
	}
	c := locksession.New("session", "niri")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.Start(ctx)
	if !c.State().Known {
		t.Fatalf("managed owner never reported state: %+v", c.State())
	}
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
	select {
	case value := <-address:
		if value != "" {
			t.Setenv("DBUS_SESSION_BUS_ADDRESS", strings.TrimSpace(value))
			return
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dbus-daemon did not report its address")
	}
	t.Fatal("dbus-daemon did not report its address")
}
