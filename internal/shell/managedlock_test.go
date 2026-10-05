package shell

import (
	"fmt"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	locksession "github.com/Nomadcxx/sysc-shell/internal/lock"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/walls"
	"os"
	"path/filepath"
	"testing"
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
func TestManualSuspendUnavailableDoesNotRunCommand(t *testing.T) {
	r := &Registry{runArgv: func([]string) error { t.Fatal("ran without protection"); return nil }}
	if err := r.SuspendTracked(); err == nil {
		t.Fatal("accepted unavailable suspend")
	}
}

func TestManualSuspendHandlersRequireProtection(t *testing.T) {
	for _, panel := range []PanelID{PanelSession, PanelControlCenter} {
		t.Run(fmt.Sprint(panel), func(t *testing.T) {
			r := newPanelRegistry(t)
			called := make(chan struct{}, 1)
			r.runArgv = func([]string) error { called <- struct{}{}; return nil }
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
			waitFor(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return h.errLabel != "" })
			select {
			case <-called:
				t.Fatal("handler ran suspend without managed protection")
			default:
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
