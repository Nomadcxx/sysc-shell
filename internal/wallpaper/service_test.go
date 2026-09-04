package wallpaper

import (
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

// fakeEngine records what the service asked for and never touches Wayland,
// exec, or a socket. A path listed in gate blocks until the test releases it,
// which is how a slow apply is made to land after a newer one.
type fakeEngine struct {
	mu       sync.Mutex
	applied  []Job
	restored []string
	paused   map[string]bool

	gate    map[string]chan struct{}
	preview map[string]string
	fail    map[string]error
	caps    Capabilities
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{
		paused:  map[string]bool{},
		gate:    map[string]chan struct{}{},
		preview: map[string]string{},
		fail:    map[string]error{},
		caps:    Capabilities{GSlapper: true, Statics: []string{"awww"}},
	}
}

func (f *fakeEngine) Apply(job Job, _ Settings) (string, error) {
	f.mu.Lock()
	gate := f.gate[job.Path]
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, job)
	if err := f.fail[job.Path]; err != nil {
		return "", err
	}
	return f.preview[job.Path], nil
}

func (f *fakeEngine) Restore(connector, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restored = append(f.restored, connector)
	return nil
}

func (f *fakeEngine) SetPaused(connector string, paused bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused[connector] = paused
	return nil
}

func (f *fakeEngine) Capabilities() Capabilities {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.caps
}

func (f *fakeEngine) appliedPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.applied))
	for _, j := range f.applied {
		out = append(out, j.Path)
	}
	return out
}

func newTestService(t *testing.T, engine Engine) *Service {
	t.Helper()
	svc := NewService(ServiceConfig{
		Engine:      engine,
		Settings:    Settings{Scale: "fill", Loop: true, FPS: 30, Hidden: HiddenNone},
		Connectors:  []string{"DP-1", "DP-3"},
		PersistPath: filepath.Join(t.TempDir(), "assignments.json"),
	})
	t.Cleanup(svc.Close)
	return svc
}

// awaitSnapshot drains updates until want is satisfied.
func awaitSnapshot(t *testing.T, svc *Service, want func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case snap := <-svc.Updates():
			if want(snap) {
				return snap
			}
		case <-deadline:
			t.Fatal("timed out waiting for a snapshot")
		}
	}
}

func TestServiceApplyPublishes(t *testing.T) {
	engine := newFakeEngine()
	svc := newTestService(t, engine)

	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/a.png", Kind: KindImage})
	snap := awaitSnapshot(t, svc, func(s Snapshot) bool {
		return s.Assignments["DP-1"].Path == "/w/a.png"
	})
	if snap.Assignments["DP-1"].Kind != KindImage {
		t.Fatalf("DP-1 = %+v", snap.Assignments["DP-1"])
	}
	if snap.Caps.GSlapper != true {
		t.Error("the snapshot must carry the engine capabilities for the banner")
	}
}

func TestServiceStaleApplyDoesNotCommit(t *testing.T) {
	engine := newFakeEngine()
	release := make(chan struct{})
	engine.gate["/w/slow.png"] = release
	svc := newTestService(t, engine)

	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/slow.png", Kind: KindImage})
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/fast.png", Kind: KindImage})
	awaitSnapshot(t, svc, func(s Snapshot) bool {
		return s.Assignments["DP-1"].Path == "/w/fast.png"
	})

	close(release) // the slow apply now lands, one generation behind
	deadline := time.After(time.Second)
	for {
		select {
		case snap := <-svc.Updates():
			if snap.Assignments["DP-1"].Path != "/w/fast.png" {
				t.Fatalf("a stale apply committed: %q", snap.Assignments["DP-1"].Path)
			}
		case <-deadline:
			if got := svc.Snapshot().Assignments["DP-1"].Path; got != "/w/fast.png" {
				t.Fatalf("final assignment = %q, want the newer apply", got)
			}
			return
		}
	}
}

func TestServiceReconnectReplays(t *testing.T) {
	engine := newFakeEngine()
	svc := newTestService(t, engine)

	svc.Enqueue(Command{Op: OpApply, Token: "DP-3", Path: "/w/b.mp4", Kind: KindVideo})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-3"].Path == "/w/b.mp4" })

	svc.Enqueue(Command{Op: OpDisconnect, Token: "DP-3"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return len(s.Connectors) == 1 })

	svc.Enqueue(Command{Op: OpConnect, Token: "DP-3"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return len(s.Connectors) == 2 })

	deadline := time.After(2 * time.Second)
	for {
		applied := engine.appliedPaths()
		count := 0
		for _, p := range applied {
			if p == "/w/b.mp4" {
				count++
			}
		}
		if count >= 2 {
			return // once on assign, once on reconnect
		}
		select {
		case <-deadline:
			t.Fatalf("reconnect did not replay the saved assignment: %v", applied)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestServiceSnapshotIsImmutable(t *testing.T) {
	engine := newFakeEngine()
	svc := newTestService(t, engine)
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/a.png", Kind: KindImage})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Path == "/w/a.png" })

	snap := svc.Snapshot()
	snap.Assignments["DP-1"] = Assignment{Path: "/tampered"}
	snap.Connectors[0] = "TAMPERED"

	fresh := svc.Snapshot()
	if fresh.Assignments["DP-1"].Path != "/w/a.png" {
		t.Fatalf("a caller mutated the service's state: %q", fresh.Assignments["DP-1"].Path)
	}
	if fresh.Connectors[0] == "TAMPERED" {
		t.Fatal("the connector list must be copied out")
	}
}

func TestServiceConfigHook(t *testing.T) {
	engine := newFakeEngine()
	engine.preview["/w/withstill.mp4"] = "/c/still.jpg"
	svc := newTestService(t, engine)

	var mu sync.Mutex
	var calls [][2]string
	svc.SetConfigHook(func(source, seed string) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, [2]string{source, seed})
	})
	seen := func() [][2]string {
		mu.Lock()
		defer mu.Unlock()
		return append([][2]string(nil), calls...)
	}

	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/a.png", Kind: KindImage})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Seed == "/w/a.png" })
	if got := seen(); len(got) != 1 || got[0] != [2]string{"wallpaper", "/w/a.png"} {
		t.Fatalf("image apply hook = %v", got)
	}

	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/withstill.mp4", Kind: KindVideo})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Seed == "/c/still.jpg" })
	if got := seen(); len(got) != 2 || got[1] != [2]string{"wallpaper", "/c/still.jpg"} {
		t.Fatalf("video-with-still hook = %v", got)
	}

	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/nostill.mkv", Kind: KindVideo})
	awaitSnapshot(t, svc, func(s Snapshot) bool {
		return s.Assignments["DP-1"].Path == "/w/nostill.mkv"
	})
	if got := seen(); len(got) != 2 {
		t.Fatalf("a video with no still must leave the seed alone, hook calls = %v", got)
	}
	if svc.Snapshot().Seed != "/c/still.jpg" {
		t.Fatalf("seed = %q, want the previous still", svc.Snapshot().Seed)
	}
}

func TestServiceFailureKeepsPriorAssignment(t *testing.T) {
	engine := newFakeEngine()
	engine.fail["/w/broken.png"] = errors.New("engine refused")
	svc := newTestService(t, engine)

	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/good.png", Kind: KindImage})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Path == "/w/good.png" })

	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/broken.png", Kind: KindImage})
	snap := awaitSnapshot(t, svc, func(s Snapshot) bool {
		return s.Runtime["DP-1"].State == StateError
	})
	if snap.Assignments["DP-1"].Path != "/w/good.png" {
		t.Fatalf("a failed apply must keep the prior assignment, got %q", snap.Assignments["DP-1"].Path)
	}
	if snap.Runtime["DP-1"].Err == "" {
		t.Error("a failed apply must carry its message into the snapshot")
	}
}

func TestServiceReconcilesSavedAssignmentsAtStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assignments.json")
	saved := map[string]Assignment{
		"DP-1": {Kind: KindImage, Path: "/w/saved.png", DesiredPlayback: StateStatic},
		// An output that is not connected must not be applied to.
		"HDMI-A-1": {Kind: KindImage, Path: "/w/absent.png", DesiredPlayback: StateStatic},
	}
	if err := SaveAssignments(path, saved); err != nil {
		t.Fatalf("seed: %v", err)
	}

	engine := newFakeEngine()
	svc := NewService(ServiceConfig{
		Engine:      engine,
		Connectors:  []string{"DP-1", "DP-3"},
		PersistPath: path,
	})
	t.Cleanup(svc.Close)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		applied := engine.appliedPaths()
		if slices.Contains(applied, "/w/saved.png") {
			if slices.Contains(applied, "/w/absent.png") {
				t.Fatal("a disconnected output must stay untouched at startup")
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("startup did not restore the saved assignment: %v", engine.appliedPaths())
}

// Adopt primes the store's seed from the persisted assignment so a snapshot has
// one before reconcile finishes. That must not be mistaken for having told the
// theme: the seed is deliberately not kept in the config file, so if startup
// skips the hook the palette falls back to defaults on every reboot and the
// compositor colours stop matching the wallpaper.
func TestServiceSeedsTheThemeFromAPersistedAssignment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assignments.json")
	saved := map[string]Assignment{
		"DP-1": {Kind: KindImage, Path: "/w/saved.png", DesiredPlayback: StateStatic},
	}
	if err := SaveAssignments(path, saved); err != nil {
		t.Fatalf("seed: %v", err)
	}

	seeds := make(chan string, 4)
	svc := NewService(ServiceConfig{
		Engine:     newFakeEngine(),
		Connectors: []string{"DP-1"},
		PersistPath: path,
		ConfigHook: func(_, seed string) { seeds <- seed },
	})
	t.Cleanup(svc.Close)

	select {
	case got := <-seeds:
		if got != "/w/saved.png" {
			t.Fatalf("startup seeded the theme with %q, want the saved wallpaper", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("startup restored the wallpaper but never seeded the theme from it")
	}
}
