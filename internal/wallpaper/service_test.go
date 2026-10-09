package wallpaper

import (
	"errors"
	"os"
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
	mu         sync.Mutex
	applied    []Job
	restored   []string
	stills     []string
	paused     map[string]bool
	closeCalls int

	gate         map[string]chan struct{}
	restoreGate  map[string]chan struct{}
	preview      map[string]string
	fail         map[string]error
	caps         Capabilities
	refreshCaps  *Capabilities
	refreshCalls int
	generation   map[string]uint64
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{
		paused:      map[string]bool{},
		gate:        map[string]chan struct{}{},
		restoreGate: map[string]chan struct{}{},
		preview:     map[string]string{},
		fail:        map[string]error{},
		generation:  map[string]uint64{},
		caps:        Capabilities{GSlapper: true, Statics: []string{"awww"}},
	}
}

func (f *fakeEngine) AdvanceGeneration(connector string, generation uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if generation > f.generation[connector] {
		f.generation[connector] = generation
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
	if job.Gen < f.generation[job.Connector] {
		return "", nil
	}
	f.applied = append(f.applied, job)
	if job.Kind == KindEffect {
		// The real terminal engine replaces its process on every apply.
		f.paused[job.Connector] = false
	}
	if err := f.fail[job.Path]; err != nil {
		return "", err
	}
	return f.preview[job.Path], nil
}

func (f *fakeEngine) Restore(connector, still string) error {
	f.mu.Lock()
	gate := f.restoreGate[connector]
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restored = append(f.restored, connector)
	f.stills = append(f.stills, still)
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

func (f *fakeEngine) RefreshTerminalCatalog() Capabilities {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshCalls++
	if f.refreshCaps != nil {
		f.caps = *f.refreshCaps
	}
	return f.caps
}

func (f *fakeEngine) Close() {
	f.mu.Lock()
	f.closeCalls++
	f.mu.Unlock()
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

func TestServiceRefreshesTerminalCatalog(t *testing.T) {
	engine := newFakeEngine()
	engine.caps = Capabilities{
		Terminal: true,
		Catalog:  Catalog{Effects: []string{"fire"}, Themes: []string{"nord"}},
	}
	updated := Capabilities{
		Terminal: true,
		Catalog: Catalog{
			Effects: []string{"fire", "rain"},
			Themes:  []string{"nord", "dracula"},
		},
	}
	engine.refreshCaps = &updated
	svc := newTestService(t, engine)

	svc.RefreshTerminalCatalog()
	got := awaitSnapshot(t, svc, func(s Snapshot) bool {
		return len(s.Caps.Catalog.Effects) == 2 && len(s.Caps.Catalog.Themes) == 2
	})
	if got.Caps.Catalog.Effects[1] != "rain" {
		t.Fatalf("catalog = %+v, want the refreshed rain effect", got.Caps.Catalog)
	}
	engine.mu.Lock()
	calls := engine.refreshCalls
	engine.mu.Unlock()
	if calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", calls)
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
	engine.gate["/w/slow.mp4"] = release
	svc := newTestService(t, engine)

	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/slow.mp4", Kind: KindVideo})
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

func TestServiceSkipsSupersededApplyBeforeEngineWork(t *testing.T) {
	engine := newFakeEngine()
	release := make(chan struct{})
	engine.gate["/w/slow.mp4"] = release
	svc := newTestService(t, engine)

	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/slow.mp4", Kind: KindVideo})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Runtime["DP-1"].State == StateStarting })
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/fast.png", Kind: KindImage})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Path == "/w/fast.png" })
	close(release)
	svc.work.Wait()

	if got := engine.appliedPaths(); !slices.Equal(got, []string{"/w/fast.png"}) {
		t.Fatalf("engine work = %v, want only the latest assignment", got)
	}
}

func TestServiceRestoreInvalidatesWaitingApply(t *testing.T) {
	engine := newFakeEngine()
	release := make(chan struct{})
	engine.gate["/w/slow.mp4"] = release
	svc := newTestService(t, engine)

	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/slow.mp4", Kind: KindVideo})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Runtime["DP-1"].State == StateStarting })
	svc.Enqueue(Command{Op: OpRestore, Token: "DP-1"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Runtime["DP-1"].State == StateStatic })
	close(release)
	svc.work.Wait()

	if got := engine.appliedPaths(); len(got) != 0 {
		t.Fatalf("engine applied a request superseded by Restore: %v", got)
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if !slices.Equal(engine.restored, []string{"DP-1"}) {
		t.Fatalf("restored outputs = %v, want DP-1", engine.restored)
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

func TestServiceDisconnectStopsEngine(t *testing.T) {
	engine := newFakeEngine()
	svc := newTestService(t, engine)

	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/a.png", Kind: KindImage})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Path == "/w/a.png" })

	svc.Enqueue(Command{Op: OpDisconnect, Token: "DP-1"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return !slices.Contains(s.Connectors, "DP-1") })

	svc.Close()

	engine.mu.Lock()
	defer engine.mu.Unlock()
	if !slices.Contains(engine.restored, "DP-1") {
		t.Fatalf("disconnect did not stop the engine: restored=%v", engine.restored)
	}
}

func TestServiceDisconnectPublishesBeforeEngineStops(t *testing.T) {
	engine := newFakeEngine()
	restoreGate := make(chan struct{})
	engine.restoreGate["DP-1"] = restoreGate
	svc := newTestService(t, engine)
	defer close(restoreGate)

	svc.Enqueue(Command{Op: OpDisconnect, Token: "DP-1"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return !slices.Contains(s.Connectors, "DP-1") })
}

func TestServiceCloseClosesEngine(t *testing.T) {
	engine := newFakeEngine()
	svc := NewService(ServiceConfig{Engine: engine, Connectors: []string{"DP-1"}})

	svc.Close()
	svc.Close()

	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.closeCalls != 1 {
		t.Fatalf("engine close calls = %d, want 1", engine.closeCalls)
	}
}

func TestServicePausePreservesInFlightApply(t *testing.T) {
	engine := newFakeEngine()
	svc := newTestService(t, engine)
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/current.mp4", Kind: KindVideo})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Path == "/w/current.mp4" })

	release := make(chan struct{})
	engine.gate["/w/new.mp4"] = release
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/new.mp4", Kind: KindVideo})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Runtime["DP-1"].State == StateStarting })
	svc.Enqueue(Command{Op: OpPause, Token: "DP-1"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Runtime["DP-1"].State == StatePaused })

	close(release)
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Path == "/w/new.mp4" })
	snap := svc.Snapshot()
	if snap.Assignments["DP-1"].DesiredPlayback != StatePaused || snap.Runtime["DP-1"].State != StatePaused {
		t.Fatalf("pause was lost when apply completed: %+v", snap)
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
	// publish lands on Updates before notifySeed runs the hook, so the calls
	// lag the awaited snapshot; await them the way awaitSnapshot awaits state.
	awaitCalls := func(n int) [][2]string {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for {
			if got := seen(); len(got) >= n {
				return got
			}
			select {
			case <-deadline:
				t.Fatalf("config hook never reached %d calls", n)
			case <-time.After(time.Millisecond):
			}
		}
	}

	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/a.png", Kind: KindImage})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Seed == "/w/a.png" })
	if got := awaitCalls(1); len(got) != 1 || got[0] != [2]string{"wallpaper", "/w/a.png"} {
		t.Fatalf("image apply hook = %v", got)
	}

	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/withstill.mp4", Kind: KindVideo})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Seed == "/c/still.jpg" })
	if got := awaitCalls(2); len(got) != 2 || got[1] != [2]string{"wallpaper", "/c/still.jpg"} {
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
		Engine:      newFakeEngine(),
		Connectors:  []string{"DP-1"},
		PersistPath: path,
		ConfigHook:  func(_, seed string) { seeds <- seed },
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

func TestEngineForNamesTheEngineAnApplyWillUse(t *testing.T) {
	both := Capabilities{GSlapper: true, Statics: []string{"awww", "swaybg"}}
	if got := both.EngineFor(KindVideo); got != EngineGSlapper {
		t.Errorf("video with gslapper = %q, want %q", got, EngineGSlapper)
	}
	if got := both.EngineFor(KindImage); got != EngineGSlapper {
		t.Errorf("image with gslapper = %q, want %q", got, EngineGSlapper)
	}

	// Without gSlapper an image falls to the first static in preference order,
	// and a video has nowhere to go at all.
	static := Capabilities{Statics: []string{"awww", "swaybg"}}
	if got := static.EngineFor(KindImage); got != "awww" {
		t.Errorf("image without gslapper = %q, want awww", got)
	}
	if got := static.EngineFor(KindVideo); got != "" {
		t.Errorf("video without gslapper = %q, want none", got)
	}
	if got := (Capabilities{}).EngineFor(KindImage); got != "" {
		t.Errorf("image with nothing installed = %q, want none", got)
	}
}

func TestEngineForEffectComesBeforeGSlapper(t *testing.T) {
	both := Capabilities{GSlapper: true, Terminal: true, Statics: []string{"awww"}}
	if got := both.EngineFor(KindEffect); got != EngineTerminal {
		t.Errorf("effect with both engines = %q, want %q", got, EngineTerminal)
	}
	if got := both.EngineFor(KindVideo); got != EngineGSlapper {
		t.Errorf("video with gslapper = %q, want %q", got, EngineGSlapper)
	}
	if got := both.EngineFor(KindImage); got != EngineGSlapper {
		t.Errorf("image with gslapper = %q, want %q", got, EngineGSlapper)
	}
	gslapperOnly := Capabilities{GSlapper: true, Statics: []string{"awww"}}
	if got := gslapperOnly.EngineFor(KindEffect); got != "" {
		t.Errorf("effect without terminal = %q, want none", got)
	}
	termOnly := Capabilities{Terminal: true, Statics: []string{"awww"}}
	if got := termOnly.EngineFor(KindEffect); got != EngineTerminal {
		t.Errorf("effect without gslapper = %q, want %q", got, EngineTerminal)
	}
}

func TestServicePauseWorksForEffect(t *testing.T) {
	engine := newFakeEngine()
	engine.caps.Terminal = true
	svc := newTestService(t, engine)
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Kind: KindEffect, Effect: "fire", Theme: "nord"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Kind == KindEffect })
	svc.Enqueue(Command{Op: OpPause, Token: "DP-1"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Runtime["DP-1"].State == StatePaused })
	engine.mu.Lock()
	paused := engine.paused["DP-1"]
	engine.mu.Unlock()
	if !paused {
		t.Fatal("KindEffect pause did not reach the engine")
	}
}

func TestReapplyingPausedEffectKeepsEnginePaused(t *testing.T) {
	engine := newFakeEngine()
	engine.caps.Terminal = true
	svc := newTestService(t, engine)
	apply := Command{Op: OpApply, Token: "DP-1", Kind: KindEffect, Effect: "fire", Theme: "nord"}
	svc.Enqueue(apply)
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Kind == KindEffect })
	svc.Enqueue(Command{Op: OpPause, Token: "DP-1"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Runtime["DP-1"].State == StatePaused })
	svc.Enqueue(apply)
	snap := awaitSnapshot(t, svc, func(s Snapshot) bool {
		engine.mu.Lock()
		applied := len(engine.applied)
		engine.mu.Unlock()
		return s.Assignments["DP-1"].Kind == KindEffect && s.Runtime["DP-1"].State == StatePaused && applied == 2
	})
	if snap.Runtime["DP-1"].State != StatePaused {
		t.Fatalf("runtime = %+v, want paused", snap.Runtime["DP-1"])
	}
	engine.mu.Lock()
	paused := engine.paused["DP-1"]
	engine.mu.Unlock()
	if !paused {
		t.Fatal("reapplying a paused effect restarted it without pausing the new process")
	}
	apply.Theme = "dracula"
	svc.Enqueue(apply)
	snap = awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Theme == "dracula" })
	if snap.Runtime["DP-1"].State != StatePaused {
		t.Fatalf("changing a paused effect's palette changed playback to %v", snap.Runtime["DP-1"].State)
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if !engine.paused["DP-1"] {
		t.Fatal("changing a paused effect's palette restarted it without pausing the new process")
	}
}

func TestServicePersistsActualAssignmentAfterFailedApplyRollback(t *testing.T) {
	engine := newFakeEngine()
	engine.caps.Terminal = true
	path := filepath.Join(t.TempDir(), "assignments.json")
	seeded := make(chan string, 1)
	svc := NewService(ServiceConfig{
		Engine:      engine,
		Settings:    Settings{Scale: "fill", Loop: true, FPS: 30, Hidden: HiddenNone},
		Connectors:  []string{"DP-1"},
		PersistPath: path,
		ConfigHook:  func(_, seed string) { seeded <- seed },
	})
	t.Cleanup(svc.Close)

	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Kind: KindImage, Path: "/w/first.png"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Path == "/w/first.png" })
	select {
	case seed := <-seeded:
		if seed != "/w/first.png" {
			t.Fatalf("initial seed = %q, want the applied image", seed)
		}
	case <-time.After(time.Second):
		t.Fatal("applied image did not update the theme seed")
	}
	engine.mu.Lock()
	engine.fail[""] = &restoredApplyError{
		cause: errors.New("new effect failed"), state: StateStatic, engine: EngineGSlapper,
		assignment: Assignment{Kind: KindImage, Path: "/w/restored.png", DesiredPlayback: StateStatic}, hasAssignment: true,
	}
	engine.mu.Unlock()
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Kind: KindEffect, Effect: "storm", Theme: "nord"})
	awaitSnapshot(t, svc, func(s Snapshot) bool {
		return s.Assignments["DP-1"].Path == "/w/restored.png" && s.Runtime["DP-1"].State == StateStatic && s.Seed == "/w/restored.png"
	})

	saved, err := LoadAssignments(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved["DP-1"].Kind != KindImage || saved["DP-1"].Path != "/w/restored.png" {
		t.Fatalf("persisted assignment = %+v, want the actual restored image", saved["DP-1"])
	}
	select {
	case seed := <-seeded:
		if seed != "/w/restored.png" {
			t.Fatalf("seed hook = %q, want the restored image", seed)
		}
	case <-time.After(time.Second):
		t.Fatal("restored image did not update the theme seed")
	}
}

func TestServiceNotifiesAdoptedSeedWhenReconcileFails(t *testing.T) {
	engine := newFakeEngine()
	engine.fail["/w/adopted.png"] = errors.New("wallpaper unavailable")
	path := filepath.Join(t.TempDir(), "assignments.json")
	if err := SaveAssignments(path, map[string]Assignment{
		"DP-1": {Kind: KindImage, Path: "/w/adopted.png", DesiredPlayback: StateStatic},
	}); err != nil {
		t.Fatal(err)
	}
	seeded := make(chan string, 1)
	svc := NewService(ServiceConfig{
		Engine: engine, Settings: Settings{Scale: "fill", Loop: true, FPS: 30, Hidden: HiddenNone},
		Connectors: []string{"DP-1"}, PersistPath: path,
		ConfigHook: func(_, seed string) { seeded <- seed },
	})
	t.Cleanup(svc.Close)

	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Runtime["DP-1"].State == StateError })
	select {
	case seed := <-seeded:
		if seed != "/w/adopted.png" {
			t.Fatalf("theme seed = %q, want adopted assignment", seed)
		}
	case <-time.After(time.Second):
		t.Fatal("failed startup reconcile did not publish the adopted theme seed")
	}
}

func TestServiceReconnectWaitsForDisconnectCleanup(t *testing.T) {
	engine := newFakeEngine()
	svc := newTestService(t, engine)
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/a.png", Kind: KindImage})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Path == "/w/a.png" })
	gate := make(chan struct{})
	defer close(gate)
	engine.mu.Lock()
	engine.restoreGate["DP-1"] = gate
	engine.mu.Unlock()
	svc.Enqueue(Command{Op: OpDisconnect, Token: "DP-1"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return !slices.Contains(s.Connectors, "DP-1") })
	svc.Enqueue(Command{Op: OpConnect, Token: "DP-1"})
	time.Sleep(30 * time.Millisecond)
	if len(engine.appliedPaths()) != 1 {
		t.Fatal("reconnect applied before disconnect cleanup finished")
	}
}

func TestRestoreEffectReturnsToPriorStill(t *testing.T) {
	engine := newFakeEngine()
	engine.caps.Terminal = true
	svc := newTestService(t, engine)
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/a.png", Kind: KindImage})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Path == "/w/a.png" })
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Kind: KindEffect, Effect: "fire", Theme: "nord"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Kind == KindEffect })
	svc.Enqueue(Command{Op: OpRestore, Token: "DP-1"})
	awaitSnapshot(t, svc, func(Snapshot) bool {
		engine.mu.Lock()
		defer engine.mu.Unlock()
		return len(engine.stills) > 0
	})
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if got := engine.stills[len(engine.stills)-1]; got != "/w/a.png" {
		t.Fatalf("Restore got still %q, want the image the effect replaced", got)
	}
}

func TestServicePauseAllReachesEachPlayer(t *testing.T) {
	engine := newFakeEngine()
	svc := newTestService(t, engine)
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/a.mp4", Kind: KindVideo})
	svc.Enqueue(Command{Op: OpApply, Token: "DP-3", Path: "/w/b.mp4", Kind: KindVideo})
	awaitSnapshot(t, svc, func(s Snapshot) bool {
		return s.Runtime["DP-1"].State == StatePlaying && s.Runtime["DP-3"].State == StatePlaying
	})
	svc.Enqueue(Command{Op: OpRestore, Token: "DP-3"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Runtime["DP-3"].State == StateStatic })

	svc.Enqueue(Command{Op: OpPause, Token: AllOutputs})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Runtime["DP-1"].State == StatePaused })
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if _, touched := engine.paused["DP-3"]; touched {
		t.Fatal("pause reached an output that was restored to a still")
	}
}

func TestServiceRestartsExitedWallpaperOnce(t *testing.T) {
	for _, kind := range []Kind{KindEffect, KindImage} {
		t.Run(map[Kind]string{KindEffect: "effect", KindImage: "image"}[kind], func(t *testing.T) {
			h := newEngineHarness(t)
			h.withTerminal()
			svc := newTestService(t, h.eng)
			svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Kind: kind, Path: h.media("still.png"), Effect: "fire", Theme: "nord"})
			awaitSnapshot(t, svc, func(s Snapshot) bool { return len(s.Assignments) == 1 && s.Runtime["DP-1"].State != StateStarting })
			if kind == KindEffect {
				svc.Enqueue(Command{Op: OpPause, Token: "DP-1"})
				awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Runtime["DP-1"].State == StatePaused })
			}
			original := svc.Snapshot().Assignments["DP-1"]
			h.mu.Lock()
			proc := h.procs[0]
			h.mu.Unlock()
			_ = proc.Stop() // simulate an unexpected child exit after readiness
			awaitSnapshot(t, svc, func(s Snapshot) bool {
				return len(h.argvs()) == 2 && s.Runtime["DP-1"].State == original.DesiredPlayback
			})
			if got := svc.Snapshot().Assignments["DP-1"]; got != original {
				t.Fatalf("restart changed assignment: got %+v, want %+v", got, original)
			}
			h.mu.Lock()
			proc = h.procs[1]
			h.mu.Unlock()
			_ = proc.Stop()
			awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Runtime["DP-1"].State == StateError })
			if got := len(h.argvs()); got != 2 {
				t.Fatalf("crash loop launched %d children, want 2", got)
			}
			svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Kind: kind, Path: original.Path, Effect: "fire", Theme: "nord"})
			awaitSnapshot(t, svc, func(s Snapshot) bool {
				return len(h.argvs()) == 3 && s.Runtime["DP-1"].State == original.DesiredPlayback
			})
		})
	}
}

// Opening a folder in the picker moves its previews to the front, and the
// snapshot reports that folder's own progress next to the library's.
func TestServiceReportsTheOpenFolderProgress(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(root, "a.png"), filepath.Join(sub, "x.png"), filepath.Join(sub, "y.png")} {
		writePNG(t, p, 320, 180)
	}
	svc := NewService(ServiceConfig{
		Engine:      newFakeEngine(),
		Settings:    Settings{Scale: "fill", Loop: true, FPS: 30, Hidden: HiddenNone},
		Connectors:  []string{"DP-1"},
		PersistPath: filepath.Join(t.TempDir(), "assignments.json"),
		Roots:       []string{root},
		CacheDir:    t.TempDir(),
	})
	t.Cleanup(svc.Close)
	svc.Enqueue(Command{Op: OpFocusFolder, Path: sub})
	snap := awaitSnapshot(t, svc, func(s Snapshot) bool {
		return s.ThumbsFolder == sub && s.ThumbsFolderTotal == 2 && s.ThumbsFolderDone == 2 && s.ThumbsDone == s.ThumbsTotal
	})
	if snap.ThumbsTotal != 3 {
		t.Fatalf("library total %d, want 3 files", snap.ThumbsTotal)
	}
}
