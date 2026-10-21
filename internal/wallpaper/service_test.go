package wallpaper

import (
	"context"
	"errors"
	"image/color"
	"math"
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
	fps        map[string]int
	fpsErr     error
	pauseErr   error
	pauseCalls int
	pauseLog   []bool
	closeCalls int

	gate         map[string]chan struct{}
	restoreGate  map[string]chan struct{}
	preview      map[string]string
	fail         map[string]error
	restoreErr   error
	caps         Capabilities
	refreshCaps  *Capabilities
	refreshCalls int
	generation   map[string]uint64
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{
		paused:      map[string]bool{},
		fps:         map[string]int{},
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
	return f.restoreErr
}

type testRestoreStoppedError struct{ cause error }

func (e testRestoreStoppedError) Error() string { return e.cause.Error() }
func (e testRestoreStoppedError) Unwrap() error { return e.cause }
func (testRestoreStoppedError) restoreStopped() {}

func (f *fakeEngine) SetPaused(connector string, paused bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pauseCalls++
	if f.pauseErr != nil {
		return f.pauseErr
	}
	f.paused[connector] = paused
	f.pauseLog = append(f.pauseLog, paused)
	return nil
}

func (f *fakeEngine) SetFPS(connector string, fps int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fpsErr != nil {
		return f.fpsErr
	}
	f.fps[connector] = fps
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

func newCycleServiceForTest(t *testing.T, roots, connectors []string, assignments map[string]Assignment, policy CyclePolicy) (*Service, *fakeEngine) {
	t.Helper()
	engine := newFakeEngine()
	svc := &Service{
		engine:         engine,
		set:            Settings{Scale: "fill", Loop: true, FPS: 30, Hidden: HiddenNone},
		cyclePolicy:    policy,
		cycleStart:     time.Now().Add(-time.Hour),
		cycleDate:      dailyCycleBaseline(time.Now(), policy.Time),
		roots:          slices.Clone(roots),
		restarted:      make(map[string]bool),
		cmds:           make(chan Command, 32),
		cycleUpdates:   make(chan CyclePolicy, 1),
		results:        make(chan engineResult, 32),
		updates:        make(chan Snapshot, 1),
		quit:           make(chan struct{}),
		auto:           make(map[string]Reasons),
		paused:         make(map[string]bool),
		fps:            make(map[string]int),
		fpsUnsupported: make(map[string]bool),
	}
	svc.store.SetConnectors(connectors)
	svc.store.Adopt(assignments)
	svc.lib = Scan(roots)
	svc.caps = engine.Capabilities()
	return svc, engine
}

func writeCycleLibrary(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func finishCycleJobs(t *testing.T, svc *Service, count int) {
	t.Helper()
	for range count {
		select {
		case result := <-svc.results:
			svc.finish(result)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for wallpaper cycle apply")
		}
	}
	svc.work.Wait()
}

func TestServiceCycleAdvancesEachAssignedOutputIndependently(t *testing.T) {
	root := writeCycleLibrary(t, "a.png", "b.png", "c.png")
	policy := CyclePolicy{Enabled: true, Mode: CycleModeInterval, Interval: time.Minute}
	svc, engine := newCycleServiceForTest(t, []string{root}, []string{"DP-1", "DP-3"}, map[string]Assignment{
		"DP-1": {Kind: KindImage, Path: filepath.Join(root, "a.png"), DesiredPlayback: StateStatic},
		"DP-3": {Kind: KindVideo, Path: filepath.Join(root, "b.png"), DesiredPlayback: StatePlaying},
	}, policy)
	now := time.Now()
	svc.cycleStart = now.Add(-policy.Interval)
	svc.restarted["DP-1"] = true

	svc.runCycle(now)
	finishCycleJobs(t, svc, 2)
	if svc.restarted["DP-1"] {
		t.Fatal("new cycle selection retained the previous restart guard")
	}

	got := engine.appliedPaths()
	slices.Sort(got)
	want := []string{filepath.Join(root, "b.png"), filepath.Join(root, "c.png")}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("cycled paths = %v, want %v", got, want)
	}
	if got, _ := svc.store.Assignment("DP-1"); got.Path != filepath.Join(root, "b.png") {
		t.Errorf("DP-1 assignment = %+v, want b.png", got)
	}
	if got, _ := svc.store.Assignment("DP-3"); got.Path != filepath.Join(root, "c.png") {
		t.Errorf("DP-3 assignment = %+v, want c.png", got)
	}
}

func TestServiceCycleSkipsUnassignedAndApplyingOutputs(t *testing.T) {
	t.Run("unassigned output", func(t *testing.T) {
		root := writeCycleLibrary(t, "a.png", "b.png")
		policy := CyclePolicy{Enabled: true, Mode: CycleModeInterval, Interval: time.Minute}
		svc, engine := newCycleServiceForTest(t, []string{root}, []string{"DP-1", "DP-2"}, map[string]Assignment{
			"DP-1": {Kind: KindImage, Path: filepath.Join(root, "a.png"), DesiredPlayback: StateStatic},
		}, policy)
		now := time.Now()
		svc.cycleStart = now.Add(-policy.Interval)
		svc.runCycle(now)
		finishCycleJobs(t, svc, 1)
		if got := engine.appliedPaths(); !slices.Equal(got, []string{filepath.Join(root, "b.png")}) {
			t.Fatalf("applied paths = %v, want only DP-1's next entry", got)
		}
		if _, ok := svc.store.Assignment("DP-2"); ok {
			t.Fatal("cycling assigned an output that had no prior assignment")
		}
	})

	t.Run("apply in flight", func(t *testing.T) {
		root := writeCycleLibrary(t, "a.png", "b.png")
		policy := CyclePolicy{Enabled: true, Mode: CycleModeInterval, Interval: time.Minute}
		svc, engine := newCycleServiceForTest(t, []string{root}, []string{"DP-1"}, map[string]Assignment{
			"DP-1": {Kind: KindImage, Path: filepath.Join(root, "a.png"), DesiredPlayback: StateStatic},
		}, policy)
		svc.store.Apply("DP-1", filepath.Join(root, "a.png"), KindImage)
		now := time.Now()
		svc.cycleStart = now.Add(-policy.Interval)
		svc.runCycle(now)
		if got := engine.appliedPaths(); len(got) != 0 {
			t.Fatalf("cycle applied during another apply: %v", got)
		}
		if got := svc.store.Runtime("DP-1").State; got != StateStarting {
			t.Fatalf("in-flight runtime state = %v, want to remain starting", got)
		}
	})
}

func TestServiceCycleNoopsWhenDisabledOrWithoutMultipleEntries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		names   []string
		enabled bool
	}{
		{name: "disabled", names: []string{"a.png", "b.png"}},
		{name: "empty library", enabled: true},
		{name: "single entry", names: []string{"a.png"}, enabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := writeCycleLibrary(t, tc.names...)
			policy := CyclePolicy{Enabled: tc.enabled, Mode: CycleModeInterval, Interval: time.Minute}
			svc, engine := newCycleServiceForTest(t, []string{root}, []string{"DP-1"}, map[string]Assignment{
				"DP-1": {Kind: KindImage, Path: filepath.Join(root, "a.png"), DesiredPlayback: StateStatic},
			}, policy)
			now := time.Now()
			svc.cycleStart = now.Add(-time.Hour)
			svc.runCycle(now)
			if got := engine.appliedPaths(); len(got) != 0 {
				t.Fatalf("cycle applied paths %v, want none", got)
			}
			if got, _ := svc.store.Assignment("DP-1"); got.Path != filepath.Join(root, "a.png") {
				t.Fatalf("assignment = %+v, want original assignment", got)
			}
		})
	}
}

func TestServiceCycleFailurePreservesPreviousAssignment(t *testing.T) {
	root := writeCycleLibrary(t, "a.png", "b.png")
	policy := CyclePolicy{Enabled: true, Mode: CycleModeInterval, Interval: time.Minute}
	svc, engine := newCycleServiceForTest(t, []string{root}, []string{"DP-1"}, map[string]Assignment{
		"DP-1": {Kind: KindImage, Path: filepath.Join(root, "a.png"), DesiredPlayback: StateStatic},
	}, policy)
	engine.fail[filepath.Join(root, "b.png")] = errors.New("candidate failed")
	now := time.Now()
	svc.cycleStart = now.Add(-policy.Interval)

	svc.runCycle(now)
	finishCycleJobs(t, svc, 1)

	if got, _ := svc.store.Assignment("DP-1"); got.Path != filepath.Join(root, "a.png") {
		t.Fatalf("failed cycle assignment = %+v, want prior a.png", got)
	}
	if got := svc.store.Runtime("DP-1"); got.State != StateError || got.Err != "candidate failed" {
		t.Fatalf("failed cycle runtime = %+v, want visible candidate error", got)
	}
}

func TestServiceCyclePolicyUpdatesCoalesceAndManualApplyResetsInterval(t *testing.T) {
	old := CyclePolicy{Enabled: true, Mode: CycleModeInterval, Interval: time.Minute}
	svc, _ := newCycleServiceForTest(t, nil, nil, nil, old)
	first := CyclePolicy{Enabled: true, Mode: CycleModeInterval, Interval: 2 * time.Minute}
	latest := CyclePolicy{Enabled: true, Mode: CycleModeTime, Time: "08:30"}
	svc.SetCyclePolicy(first)
	svc.SetCyclePolicy(latest)
	if got := <-svc.cycleUpdates; got != latest {
		t.Fatalf("coalesced policy = %+v, want latest %+v", got, latest)
	}

	now := time.Now()
	svc.updateCyclePolicy(latest, now)
	if svc.cyclePolicy != latest || !svc.cycleStart.Equal(now) {
		t.Fatalf("updated policy/start = %+v/%v", svc.cyclePolicy, svc.cycleStart)
	}
	wantDate := dailyCycleBaseline(now, latest.Time)
	if svc.cycleDate != wantDate {
		t.Fatalf("daily baseline = %q, want %q", svc.cycleDate, wantDate)
	}

	svc.cyclePolicy = old
	svc.cycleStart = now.Add(-time.Hour)
	svc.handle(Command{Op: OpApply, Token: "missing", Kind: KindImage, Path: "/wallpaper/manual.png"})
	if svc.cycleStart.Before(now) {
		t.Fatalf("manual apply did not reset interval baseline: %v before %v", svc.cycleStart, now)
	}
	finishCycleJobs(t, svc, 0)
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

func TestServicePublishesImageLuminancePerOutput(t *testing.T) {
	dir := t.TempDir()
	whitePath := filepath.Join(dir, "white.png")
	blackPath := filepath.Join(dir, "black.png")
	writeLuminancePNG(t, whitePath, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	writeLuminancePNG(t, blackPath, color.NRGBA{A: 0xff})
	engine := newFakeEngine()
	engine.preview["/w/clip.mp4"] = whitePath

	svc := NewService(ServiceConfig{
		Engine:      engine,
		Connectors:  []string{"DP-1", "DP-3", "DP-5"},
		CacheDir:    filepath.Join(dir, "cache"),
		PersistPath: filepath.Join(dir, "assignments.json"),
	})
	t.Cleanup(svc.Close)

	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: whitePath, Kind: KindImage})
	svc.Enqueue(Command{Op: OpApply, Token: "DP-3", Path: blackPath, Kind: KindImage})
	svc.Enqueue(Command{Op: OpApply, Token: "DP-5", Path: "/w/clip.mp4", Kind: KindVideo})
	snap := awaitSnapshot(t, svc, func(s Snapshot) bool {
		_, white := s.ImageLuminance["DP-1"]
		_, black := s.ImageLuminance["DP-3"]
		_, video := s.ImageLuminance["DP-5"]
		return white && black && video
	})
	if got := snap.ImageLuminance["DP-1"]; math.Abs(got-1) > 1e-6 {
		t.Errorf("DP-1 luminance = %v, want 1", got)
	}
	if got := snap.ImageLuminance["DP-3"]; got != 0 {
		t.Errorf("DP-3 luminance = %v, want 0", got)
	}
	if got := snap.ImageLuminance["DP-5"]; math.Abs(got-1) > 1e-6 {
		t.Errorf("DP-5 video preview luminance = %v, want 1", got)
	}
	snap.ImageLuminance["DP-1"] = 0
	if got := svc.Snapshot().ImageLuminance["DP-1"]; got != 1 {
		t.Errorf("mutating a published snapshot changed service state: DP-1 luminance = %v", got)
	}
}

func TestServiceOmitsLuminanceForAChangedStillPath(t *testing.T) {
	svc := &Service{
		imageLuminance: map[string]assignedImageLuminance{
			"DP-1": {path: "/w/old.png", value: 0.8},
		},
	}
	svc.store.SetConnectors([]string{"DP-1"})
	job := svc.store.Apply("DP-1", "/w/new.png", KindImage)[0]
	if !svc.store.Commit(job, "", EngineGSlapper) {
		t.Fatal("new image assignment did not commit")
	}
	svc.recordImageLuminance(imageLuminanceResult{connector: "DP-1", path: "/w/old.png", value: 0.8})

	svc.publish()
	if got := svc.Snapshot().ImageLuminance; len(got) != 0 {
		t.Fatalf("luminance for a previous still was published: %v", got)
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

func TestServiceRestoreEffectPersistsStillAndDoesNotReplayEffect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assignments.json")
	newService := func(engine *fakeEngine) *Service {
		return NewService(ServiceConfig{
			Engine:      engine,
			Settings:    Settings{Scale: "fill", Loop: true, FPS: 30, Hidden: HiddenNone},
			Connectors:  []string{"DP-1"},
			PersistPath: path,
		})
	}

	engine := newFakeEngine()
	engine.caps.Terminal = true
	svc := newService(engine)
	t.Cleanup(svc.Close)
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/still.png", Kind: KindImage})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Path == "/w/still.png" })
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Kind: KindEffect, Effect: "fire", Theme: "nord"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Kind == KindEffect })
	svc.Enqueue(Command{Op: OpRestore, Token: "DP-1"})
	snap := awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Runtime["DP-1"].State == StateStatic })

	want := Assignment{Kind: KindImage, Path: "/w/still.png", DesiredPlayback: StateStatic}
	if got := snap.Assignments["DP-1"]; got != want {
		t.Fatalf("restored assignment = %+v, want static still %+v", got, want)
	}
	saved, err := LoadAssignments(path)
	if err != nil {
		t.Fatalf("load restored assignments: %v", err)
	}
	if got := saved["DP-1"]; got != want {
		t.Fatalf("persisted assignment = %+v, want static still %+v", got, want)
	}

	svc.Close()
	restartEngine := newFakeEngine()
	restarted := newService(restartEngine)
	t.Cleanup(restarted.Close)
	awaitSnapshot(t, restarted, func(s Snapshot) bool {
		return s.Assignments["DP-1"] == want &&
			s.Runtime["DP-1"].State == StateStatic &&
			slices.Equal(restartEngine.appliedPaths(), []string{"/w/still.png"})
	})
}

func TestServiceRestoreFailureAfterStopPersistsStillAndDoesNotReplayEffect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assignments.json")
	newService := func(engine *fakeEngine) *Service {
		return NewService(ServiceConfig{
			Engine:      engine,
			Settings:    Settings{Scale: "fill", Loop: true, FPS: 30, Hidden: HiddenNone},
			Connectors:  []string{"DP-1"},
			PersistPath: path,
		})
	}

	engine := newFakeEngine()
	engine.caps.Terminal = true
	engine.caps.Statics = nil
	engine.restoreErr = testRestoreStoppedError{cause: ErrNoStaticEngine}
	svc := newService(engine)
	t.Cleanup(svc.Close)
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/still.png", Kind: KindImage})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Path == "/w/still.png" })
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Kind: KindEffect, Effect: "fire", Theme: "nord"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Kind == KindEffect })
	svc.Enqueue(Command{Op: OpRestore, Token: "DP-1"})

	want := Assignment{Kind: KindImage, Path: "/w/still.png", DesiredPlayback: StateStatic}
	snap := awaitSnapshot(t, svc, func(s Snapshot) bool {
		return s.Runtime["DP-1"].State == StateError && s.Assignments["DP-1"] == want
	})
	if snap.Runtime["DP-1"].Err == "" {
		t.Fatal("fallback failure must remain visible after persisting the stopped assignment")
	}
	saved, err := LoadAssignments(path)
	if err != nil {
		t.Fatalf("load stopped assignment: %v", err)
	}
	if got := saved["DP-1"]; got != want {
		t.Fatalf("persisted assignment = %+v, want static still %+v", got, want)
	}

	svc.Close()
	restartEngine := newFakeEngine()
	restartEngine.caps.Terminal = true
	restartEngine.caps.Statics = nil
	restarted := newService(restartEngine)
	t.Cleanup(restarted.Close)
	awaitSnapshot(t, restarted, func(s Snapshot) bool { return s.Assignments["DP-1"] == want })
	restartEngine.mu.Lock()
	defer restartEngine.mu.Unlock()
	for _, job := range restartEngine.applied {
		if job.Kind == KindEffect {
			t.Fatalf("restart replayed stopped effect: %+v", job)
		}
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

func TestServiceCloseWaitsForThumbnailWorker(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "clip.mp4")
	if err := os.WriteFile(source, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := NewService(ServiceConfig{CacheDir: t.TempDir()})
	t.Cleanup(svc.Close)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	var releaseOnce sync.Once
	releaseWorker := func() { releaseOnce.Do(func() { close(release) }) }
	svc.thumbs.extract = func(ctx context.Context, _, _ string) error {
		defer close(exited)
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release
		return ctx.Err()
	}
	svc.thumbs.Enqueue([]Entry{{Name: filepath.Base(source), Path: source, Kind: KindVideo}}, "")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("thumbnail worker did not start")
	}
	t.Cleanup(func() {
		releaseWorker()
		select {
		case <-exited:
		case <-time.After(2 * time.Second):
			t.Error("thumbnail worker did not exit")
		}
	})

	closed := make(chan struct{})
	go func() {
		svc.Close()
		close(closed)
	}()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("service close did not cancel thumbnail work")
	}
	select {
	case <-svc.done:
	case <-time.After(2 * time.Second):
		t.Fatal("service loop did not stop")
	}
	select {
	case <-closed:
		t.Fatal("service close returned while a thumbnail worker was still running")
	case <-time.After(100 * time.Millisecond):
	}

	releaseWorker()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("service close did not return after thumbnail work stopped")
	}
}

func TestTerminalBlurServiceReplay(t *testing.T) {
	engine := newFakeEngine()
	engine.caps.Terminal = true
	svc := newTestService(t, engine)
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Kind: KindEffect, Effect: "fire", Theme: "nord", Blur: 16})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.Assignments["DP-1"].Blur == 16 })
	svc.Enqueue(Command{Op: OpDisconnect, Token: "DP-1"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return !slices.Contains(s.Connectors, "DP-1") })
	svc.Enqueue(Command{Op: OpConnect, Token: "DP-1"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { engine.mu.Lock(); defer engine.mu.Unlock(); return len(engine.applied) >= 2 })
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.applied[len(engine.applied)-1].Blur != 16 {
		t.Fatal("reconnect lost blur")
	}
}
