package wallpaper

import (
	"errors"
	"testing"
)

func newTestStore() *Store {
	s := &Store{}
	s.SetConnectors([]string{"DP-1", "DP-3"})
	return s
}

// commitAll runs every request in jobs to success with the given preview.
func commitAll(t *testing.T, s *Store, jobs []Job, preview string) {
	t.Helper()
	for _, r := range jobs {
		if !s.Commit(r, preview, EngineGSlapper) {
			t.Fatalf("commit %s gen %d refused", r.Connector, r.Gen)
		}
	}
}

func TestAssignIndependentOutputs(t *testing.T) {
	s := newTestStore()
	commitAll(t, s, s.Apply("DP-1", "/w/a.png", KindImage), "")
	commitAll(t, s, s.Apply("DP-3", "/w/b.mp4", KindVideo), "/c/b.jpg")

	one, ok := s.Assignment("DP-1")
	if !ok || one.Path != "/w/a.png" || one.Kind != KindImage {
		t.Fatalf("DP-1 = %+v, %v", one, ok)
	}
	three, ok := s.Assignment("DP-3")
	if !ok || three.Path != "/w/b.mp4" || three.Kind != KindVideo {
		t.Fatalf("DP-3 = %+v, %v", three, ok)
	}
	if three.PreviewPath != "/c/b.jpg" {
		t.Fatalf("preview = %q", three.PreviewPath)
	}
}

func TestAssignAllExpands(t *testing.T) {
	s := newTestStore()
	jobs := s.Apply(AllOutputs, "/w/a.png", KindImage)
	if len(jobs) != 2 {
		t.Fatalf("all expanded to %d requests, want 2", len(jobs))
	}
	seen := map[string]bool{}
	for _, r := range jobs {
		seen[r.Connector] = true
	}
	if !seen["DP-1"] || !seen["DP-3"] {
		t.Fatalf("all covered %v, want each connected output", seen)
	}
	commitAll(t, s, jobs, "")
	for _, c := range []string{"DP-1", "DP-3"} {
		if a, ok := s.Assignment(c); !ok || a.Path != "/w/a.png" {
			t.Fatalf("%s = %+v, %v", c, a, ok)
		}
	}
}

func TestAssignStaleGeneration(t *testing.T) {
	s := newTestStore()
	first := s.Apply("DP-1", "/w/slow.png", KindImage)
	second := s.Apply("DP-1", "/w/fast.png", KindImage)
	if first[0].Gen >= second[0].Gen {
		t.Fatalf("generation did not advance: %d then %d", first[0].Gen, second[0].Gen)
	}
	commitAll(t, s, second, "")
	if s.Commit(first[0], "", EngineGSlapper) {
		t.Fatal("a stale generation must not commit")
	}
	if a, _ := s.Assignment("DP-1"); a.Path != "/w/fast.png" {
		t.Fatalf("stale commit clobbered the newer apply: %q", a.Path)
	}
}

func TestAssignDisconnectKeepsAssignment(t *testing.T) {
	s := newTestStore()
	commitAll(t, s, s.Apply("DP-3", "/w/b.mp4", KindVideo), "/c/b.jpg")
	s.Disconnect("DP-3")

	if a, ok := s.Assignment("DP-3"); !ok || a.Path != "/w/b.mp4" {
		t.Fatalf("disconnect dropped the assignment: %+v %v", a, ok)
	}
	if rt := s.Runtime("DP-3"); rt.State != StateStatic || rt.Socket != "" {
		t.Fatalf("disconnect kept runtime: %+v", rt)
	}
	if jobs := s.Apply(AllOutputs, "/w/a.png", KindImage); len(jobs) != 1 {
		t.Fatalf("all covered %d outputs, want only the connected one", len(jobs))
	}
}

func TestAssignReconnectReplays(t *testing.T) {
	s := newTestStore()
	commitAll(t, s, s.Apply("DP-3", "/w/b.mp4", KindVideo), "/c/b.jpg")
	s.Disconnect("DP-3")

	jobs := s.Reconnect("DP-3")
	if len(jobs) != 1 || jobs[0].Path != "/w/b.mp4" || jobs[0].Kind != KindVideo {
		t.Fatalf("reconnect = %+v, want a replay of the saved assignment", jobs)
	}
	if jobs := s.Reconnect("HDMI-A-1"); len(jobs) != 0 {
		t.Fatalf("an unassigned output must stay untouched, got %+v", jobs)
	}
}

func TestAssignPartialAll(t *testing.T) {
	s := newTestStore()
	commitAll(t, s, s.Apply("DP-3", "/w/prior.mp4", KindVideo), "/c/prior.jpg")

	jobs := s.Apply(AllOutputs, "/w/new.png", KindImage)
	for _, r := range jobs {
		if r.Connector == "DP-3" {
			s.Fail(r, errors.New("engine refused"))
			continue
		}
		s.Commit(r, "", EngineGSlapper)
	}

	if a, _ := s.Assignment("DP-1"); a.Path != "/w/new.png" {
		t.Fatalf("DP-1 should have taken the apply: %q", a.Path)
	}
	if a, _ := s.Assignment("DP-3"); a.Path != "/w/prior.mp4" {
		t.Fatalf("a failed output must keep its prior assignment, got %q", a.Path)
	}
	if rt := s.Runtime("DP-3"); rt.State != StateError || rt.Err == "" {
		t.Fatalf("a failed output must report the error: %+v", rt)
	}
}

func TestAssignSeedPath(t *testing.T) {
	s := newTestStore()
	if s.SeedPath() != "" {
		t.Fatalf("a fresh store has no seed, got %q", s.SeedPath())
	}

	commitAll(t, s, s.Apply("DP-1", "/w/a.png", KindImage), "")
	if s.SeedPath() != "/w/a.png" {
		t.Fatalf("image seed = %q, want the image path", s.SeedPath())
	}

	commitAll(t, s, s.Apply("DP-1", "/w/b.mp4", KindVideo), "/c/b.jpg")
	if s.SeedPath() != "/c/b.jpg" {
		t.Fatalf("video seed = %q, want the still", s.SeedPath())
	}

	commitAll(t, s, s.Apply("DP-1", "/w/c.mkv", KindVideo), "")
	if s.SeedPath() != "/c/b.jpg" {
		t.Fatalf("a video with no still must leave the seed, got %q", s.SeedPath())
	}
}

// The picker used to read Runtime.Socket and Runtime.FallbackPID to say which
// engine was driving an output. Nothing outside the engine's private handles
// ever writes those, so no engine was ever named.
func TestRuntimeRecordsTheEngineThatPainted(t *testing.T) {
	s := newTestStore()

	jobs := s.Apply("DP-1", "/w/a.png", KindImage)
	if !s.Commit(jobs[0], "", EngineGSlapper) {
		t.Fatal("commit refused")
	}
	if got := s.Runtime("DP-1").Engine; got != EngineGSlapper {
		t.Errorf("engine after apply = %q, want %q", got, EngineGSlapper)
	}

	s.SetRestored("DP-1", "swaybg")
	if got := s.Runtime("DP-1").Engine; got != "swaybg" {
		t.Errorf("engine after restore = %q, want swaybg", got)
	}
}

// An effect has no image of its own, so committing one records the still the
// output showed before it. Restore hands that to the static fallback instead
// of leaving the output blank.
func TestEffectCommitKeepsPriorStill(t *testing.T) {
	s := newTestStore()
	effect := func(connector, id string) {
		t.Helper()
		jobs := s.Apply(connector, "", KindEffect)
		for i := range jobs {
			jobs[i].Effect = id
		}
		commitAll(t, s, jobs, "")
	}
	still := func(connector string) string {
		a, _ := s.Assignment(connector)
		return a.PreviewPath
	}

	commitAll(t, s, s.Apply("DP-1", "/w/a.png", KindImage), "")
	effect("DP-1", "fire")
	if got := still("DP-1"); got != "/w/a.png" {
		t.Fatalf("effect over an image: still = %q, want /w/a.png", got)
	}
	effect("DP-1", "rain")
	if got := still("DP-1"); got != "/w/a.png" {
		t.Fatalf("effect over an effect: still = %q, want the image from before both", got)
	}

	commitAll(t, s, s.Apply("DP-3", "/w/b.mp4", KindVideo), "/c/b.jpg")
	effect("DP-3", "fire")
	if got := still("DP-3"); got != "/c/b.jpg" {
		t.Fatalf("effect over a video: still = %q, want its extracted still", got)
	}

	s = newTestStore()
	effect("DP-1", "fire")
	if got := still("DP-1"); got != "" {
		t.Fatalf("effect over nothing: still = %q, want empty", got)
	}
}

func TestEffectJobCarriesPriorAssignmentForLaunchRollback(t *testing.T) {
	s := newTestStore()
	image := s.Apply("DP-1", "/w/a.png", KindImage)[0]
	if !s.Commit(image, "", EngineGSlapper) {
		t.Fatal("commit image")
	}
	jobs := s.Apply("DP-1", "", KindEffect)
	if len(jobs) != 1 || !jobs[0].HasPrevious || jobs[0].Previous.Kind != KindImage ||
		jobs[0].Previous.Path != "/w/a.png" || jobs[0].PreviousState != StateStatic ||
		jobs[0].PreviousEngine != EngineGSlapper {
		t.Fatalf("effect job = %+v, want prior assignment and runtime for rollback", jobs)
	}
}

func TestFailRestoredApplyPreservesActualRuntimeAndError(t *testing.T) {
	s := newTestStore()
	image := s.Apply("DP-1", "/w/a.png", KindImage)[0]
	s.Commit(image, "", EngineGSlapper)
	job := s.Apply("DP-1", "", KindEffect)[0]
	err := &restoredApplyError{cause: errors.New("sysc-terminal failed"), state: StateStatic, engine: "swaybg"}
	if !s.Fail(job, err) {
		t.Fatal("current failed job was rejected")
	}
	rt := s.Runtime("DP-1")
	if rt.State != StateStatic || rt.Engine != "swaybg" || rt.Err != err.Error() {
		t.Fatalf("runtime = %+v, want restored static engine and apply error", rt)
	}
	if a, ok := s.Assignment("DP-1"); !ok || a.Kind != KindImage || a.Path != "/w/a.png" {
		t.Fatalf("failed apply replaced prior assignment: %+v, %v", a, ok)
	}
}

func TestFailedRollbackDoesNotReplaceASeedCommittedAfterTheApplyStarted(t *testing.T) {
	s := newTestStore()
	first := s.Apply("DP-1", "/w/old.png", KindImage)[0]
	s.Commit(first, "", EngineGSlapper)
	failed := s.Apply("DP-1", "", KindEffect)[0]
	failed.Effect = "fire"
	newer := s.Apply("DP-3", "/w/newer.png", KindImage)[0]
	if !s.Commit(newer, "", EngineGSlapper) {
		t.Fatal("newer image commit")
	}
	err := &restoredApplyError{
		cause: errors.New("effect launch failed"), state: StateStatic, engine: EngineGSlapper,
		assignment: Assignment{Kind: KindImage, Path: "/w/old-restore.png", DesiredPlayback: StateStatic}, hasAssignment: true,
	}
	if !s.Fail(failed, err) {
		t.Fatal("current failed job was rejected")
	}
	if got := s.SeedPath(); got != "/w/newer.png" {
		t.Fatalf("seed = %q, want newer output's committed image", got)
	}
}
